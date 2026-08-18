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

// Outbox is the store half: claim a message, deliver it, record the outcome,
// and clear out what no longer needs keeping.
type Outbox interface {
	Drain(ctx context.Context, batch int, deliver postgres.Deliver) (postgres.DrainResult, error)
	ReclaimOutbox(ctx context.Context, bodyRetention time.Duration, batch int) (postgres.ReclaimResult, error)
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

// Retention on the outbox (ENT-242).
//
// # WHY THERE IS A PERIOD AT ALL, AND WHY IT IS SHORT
//
// `body_text` holds the rendered invitation, and the rendered invitation holds
// the raw token in a path segment, because the accept link is the message.
// 00003 stores only that token's hash and says why: a database dump must not
// yield a working invitation. The outbox is the one place that rule is
// suspended, and it was meant to be suspended only until the dispatcher drained
// the row. Nothing drained it, so every address and every message body ever
// sent was still there.
//
// # WHY SEVEN DAYS, RATHER THAN NONE AND RATHER THAN NINETY
//
// Not zero, because a delivered body answers one real question: "what did we
// actually send this person", asked when somebody reports a link that did not
// work. That complaint arrives within days.
//
// Not ninety, because after a week the body answers nothing actionable. Seven
// days is `postgres.InvitationLifetime`, so the window is exactly as long as
// the token inside it can still be used, and not one day longer. Aligning the
// two is the point rather than a coincidence: the period the body is worth
// keeping is the period the link still works.
//
// It is an upper bound rather than the usual case. The reclaim redacts at the
// earlier of this window and the invitation ceasing to be acceptable, so an
// invitation accepted ten minutes after it arrives has its body cleared on the
// next pass. A spent link is worth nothing to anybody, and holding somebody's
// address for a further week to keep it would be the wrong trade.
//
// # AND WHAT IS NOT RECLAIMED
//
// A message that has not been delivered and whose invitation can still be
// accepted is never touched, at any window. The raw token exists nowhere else,
// so clearing that body destroys an invitation somebody is waiting for and
// nobody can tell which ones need reissuing. The predicate that protects it
// lives in the database and takes no argument from here, which is deliberate:
// this constant is a decision and could be edited by anybody, and it must not
// be able to reach that case however it is edited.
const DeliveredBodyRetention = 7 * 24 * time.Hour

// Defaults for the reclaim pass itself.
//
// Hourly, because every window this job applies is measured in days: running it
// more often would buy an accuracy nobody can perceive and cost a scan an hour.
// Running it daily would mean a spent token sitting in the clear for most of a
// day after the invitation it belongs to was accepted, which is the case the
// second disjunct exists to close quickly.
//
// The batch bounds one pass. It is much larger than the delivery batch because
// this is one indexed statement against a partial index sized to the backlog,
// not a network conversation per row.
const (
	DefaultReclaimInterval = time.Hour
	DefaultReclaimBatch    = 500
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
