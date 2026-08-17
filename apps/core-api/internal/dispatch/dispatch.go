// Package dispatch drains the transactional outbox onto a channel.
//
// The composition layer between storage and delivery: the store guarantees that
// no two dispatchers take the same row, the channel knows how to send, and this
// knows only how often to try and what to say when it cannot.
//
// At build-order step 8 Temporal absorbs this loop, and the workflow input
// becomes the durable rendered message. What survives that change unaltered is
// the rule this exists to serve: the message is written in the transaction that
// makes its fact true, and delivery is a separate concern that may fail, retry
// and lag (ENT-219).
package dispatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
)

// Outbox is the store half: claim a message, deliver it, record the outcome.
type Outbox interface {
	Drain(ctx context.Context, batch int, deliver postgres.Deliver) (postgres.DrainResult, error)
}

// Defaults chosen for a queue whose traffic is invitations.
//
// An invitation is created by a human clicking a button and is expected in the
// recipient's inbox within a minute or so, not within a second. Ten seconds is
// well inside that and costs one trivial indexed query per tick against a
// partial index sized to the backlog, which on an idle deployment is empty.
//
// The batch bounds how long one tick can run: each message is its own
// transaction holding one row lock across one SMTP conversation, so twenty is
// twenty short locks rather than one long one.
const (
	DefaultInterval = 10 * time.Second
	DefaultBatch    = 20
)

// Dispatcher drains an outbox onto a channel on a timer.
type Dispatcher struct {
	outbox   Outbox
	channel  delivery.Channel
	logger   *slog.Logger
	interval time.Duration
	batch    int
}

// New builds a dispatcher. A zero interval or batch takes the default.
func New(outbox Outbox, channel delivery.Channel, logger *slog.Logger,
	interval time.Duration, batch int,
) *Dispatcher {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if batch <= 0 {
		batch = DefaultBatch
	}
	return &Dispatcher{
		outbox: outbox, channel: channel, logger: logger,
		interval: interval, batch: batch,
	}
}

// Run drains until the context is cancelled.
//
// Blocking, so the caller decides whether it owns a goroutine. It returns only
// on cancellation: a failing drain is logged and retried on the next tick
// rather than ending the loop, because every reason a drain fails (the mail
// server is down, the database is restarting) is one that resolves on its own,
// and a dispatcher that exits on the first of them stops delivering
// permanently while the process stays up and healthy.
func (d *Dispatcher) Run(ctx context.Context) {
	d.logger.Info("outbox dispatcher started",
		"channel", d.channel.Name(),
		"interval", d.interval.String(),
		"batch", d.batch)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// One pass before the first tick, so a restart delivers a backlog straight
	// away instead of after an idle interval.
	d.once(ctx)

	for {
		select {
		case <-ctx.Done():
			d.logger.Info("outbox dispatcher stopped")
			return
		case <-ticker.C:
			d.once(ctx)
		}
	}
}

func (d *Dispatcher) once(ctx context.Context) {
	result, err := d.outbox.Drain(ctx, d.batch, d.send)
	if err != nil {
		if ctx.Err() != nil {
			// Shutdown, not a fault. Logging this at error level would put a
			// red line in the logs of every clean stop.
			return
		}
		d.logger.Error("draining the outbox failed", "error", err)
		return
	}

	// Silent when there was nothing to do, which is the common case and would
	// otherwise be a log line every ten seconds forever. A failure is always
	// worth a line: the row records it too, but an operator watching logs
	// should not have to query the database to notice mail is not going out.
	if result.Failed > 0 {
		d.logger.Warn("some transactional messages were not delivered",
			"sent", result.Sent, "failed", result.Failed, "channel", d.channel.Name())
		return
	}
	if result.Sent > 0 {
		d.logger.Info("delivered transactional messages",
			"sent", result.Sent, "channel", d.channel.Name())
	}
}

// send adapts a claimed row onto the channel.
//
// The channel is given the recipient, subject and body and nothing else. It
// deliberately does not receive the row id: a channel that knew it could update
// the row, and then two things would be recording the outcome.
func (d *Dispatcher) send(ctx context.Context, msg postgres.PendingMessage) error {
	return d.channel.Send(ctx, delivery.Message{
		To:       msg.RecipientEmail,
		Subject:  msg.Subject,
		BodyText: msg.BodyText,
		BodyHTML: msg.BodyHTML,
	})
}
