package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
)

// The doorbell path (ENT-209).
//
// # WHY THIS SHARES THE CHANNEL AND NOT THE DRAIN
//
// ENT-219's dispatcher drains `transactional_outbox`, where a row is one
// rendered message with one recipient decided at mint. This drains
// `notification_outbox`, where a row is a fact and the recipients are decided
// here, now, from memberships and preferences. Same delivery seam, different
// resolution, which is exactly the split doc §23.6 asks for: one delivery
// mechanism, not one queue.
//
// Sharing `delivery.Channel` is what makes that true. Telegram at step 9 is an
// adapter behind that interface and neither of these loops changes. Sharing the
// drain instead would have meant forcing a recipient into the row at enqueue,
// which is the thing ENT-192's as-built note explicitly warns against.

// Doorbells is the store half of the doorbell path.
type Doorbells interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	ClaimDoorbell(ctx context.Context, tx pgx.Tx) (postgres.Doorbell, error)
	Recipients(ctx context.Context, tx pgx.Tx, outboxID string) ([]postgres.Recipient, error)
	MintCapabilityToken(ctx context.Context, tx pgx.Tx, orgID, userID, kind, tokenHash, lifetime string) error
	MarkDoorbellSent(ctx context.Context, tx pgx.Tx, id string) error
	MarkDoorbellSkipped(ctx context.Context, tx pgx.Tx, id, reason string) error
	MarkDoorbellFailed(ctx context.Context, tx pgx.Tx, id string, cause error) error
}

// UnsubscribeLifetime is how long a link in an email stays usable.
//
// Longer than an invitation's seven days, because people read compliance email
// late and an expired unsubscribe link is worse than an expired invitation: the
// person is trying to stop mail they did not want, and failing them teaches
// them to use their mail client's spam button instead, which costs the sender's
// domain reputation rather than one message.
const UnsubscribeLifetime = "30 days"

// DoorbellDispatcher turns pending notifications into email.
type DoorbellDispatcher struct {
	store    Doorbells
	channel  delivery.Channel
	logger   *slog.Logger
	baseURL  string
	interval time.Duration
	batch    int
	now      func() time.Time
}

// NewDoorbell builds the doorbell dispatcher.
//
// `now` is injectable because quiet hours are the one rule here that depends on
// the clock, and a rule that can only be exercised by waiting until 22:00 is
// one nobody tests.
func NewDoorbell(
	store Doorbells, channel delivery.Channel, logger *slog.Logger,
	baseURL string, interval time.Duration, batch int,
) *DoorbellDispatcher {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if batch <= 0 {
		batch = DefaultBatch
	}
	return &DoorbellDispatcher{
		store: store, channel: channel, logger: logger, baseURL: baseURL,
		interval: interval, batch: batch, now: time.Now,
	}
}

// Run drains until the context is cancelled.
func (d *DoorbellDispatcher) Run(ctx context.Context) {
	d.logger.Info("doorbell dispatcher started",
		"channel", d.channel.Name(), "interval", d.interval.String(), "batch", d.batch)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.once(ctx)
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("doorbell dispatcher stopped")
			return
		case <-ticker.C:
			d.once(ctx)
		}
	}
}

func (d *DoorbellDispatcher) once(ctx context.Context) {
	var sent, skipped int

	for range d.batch {
		outcome, err := d.deliverOne(ctx)
		if err != nil {
			if ctx.Err() == nil {
				d.logger.Error("draining the notification outbox failed", "error", err)
			}
			return
		}
		switch outcome {
		case doorbellEmpty:
			if sent > 0 || skipped > 0 {
				d.logger.Info("delivered notifications", "sent", sent, "skipped", skipped)
			}
			return
		case doorbellSent:
			sent++
		case doorbellSkipped:
			skipped++
		}
	}
}

type doorbellOutcome int

const (
	doorbellEmpty doorbellOutcome = iota
	doorbellSent
	doorbellSkipped
	doorbellFailed
)

// deliverOne claims one notification, works out who wants it, and sends.
//
// One transaction, held across the sends. Same trade as the transactional
// outbox: the row lock only exists inside a transaction, so a claim taken
// outside one protects nothing at all against a second dispatcher.
func (d *DoorbellDispatcher) deliverOne(ctx context.Context) (doorbellOutcome, error) {
	tx, err := d.store.Begin(ctx)
	if err != nil {
		return doorbellEmpty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bell, err := d.store.ClaimDoorbell(ctx, tx)
	if errors.Is(err, pgx.ErrNoRows) {
		return doorbellEmpty, nil
	}
	if err != nil {
		return doorbellEmpty, fmt.Errorf("claiming a notification: %w", err)
	}

	recipients, err := d.store.Recipients(ctx, tx, bell.ID)
	if err != nil {
		return doorbellEmpty, err
	}

	// The decision, in Go, over rows the database merely fetched.
	var wanted []postgres.Recipient
	var whyNot string
	for _, r := range recipients {
		local := d.localTime(r.Timezone)
		ok, reason := notify.ShouldNotify(
			r.FindingSeverity, r.MinSeverity, local, r.QuietHoursStart, r.QuietHoursEnd)
		if ok {
			wanted = append(wanted, r)
			continue
		}
		if whyNot == "" {
			whyNot = reason
		}
	}

	if len(wanted) == 0 {
		// Terminal, and `skipped` rather than `sent`. Nobody wanted this one,
		// which is a legitimate outcome, but recording it as a delivery would
		// put a claim in the database that no message supports.
		//
		// The reason is stored so an operator asking "why did nothing go out"
		// gets an answer. Quiet hours are the awkward case: the notification is
		// dropped rather than held, which is a known limitation of dispatching
		// on a plain timer and is why §17 puts scheduled dispatch on Temporal
		// at step 8. Recorded here rather than hidden.
		if whyNot == "" {
			whyNot = "no member of this organisation has a deliverable address"
		}
		if err := d.store.MarkDoorbellSkipped(ctx, tx, bell.ID, whyNot); err != nil {
			return doorbellEmpty, err
		}
		if err := tx.Commit(ctx); err != nil {
			return doorbellEmpty, fmt.Errorf("committing a skipped notification: %w", err)
		}
		return doorbellSkipped, nil
	}

	if sendErr := d.sendAll(ctx, tx, bell, wanted); sendErr != nil {
		if err := d.store.MarkDoorbellFailed(ctx, tx, bell.ID, sendErr); err != nil {
			return doorbellEmpty, err
		}
		if err := tx.Commit(ctx); err != nil {
			return doorbellEmpty, fmt.Errorf("committing a failed notification: %w", err)
		}
		d.logger.Warn("a notification was not delivered",
			"outbox_id", bell.ID, "error", sendErr)
		return doorbellFailed, nil
	}

	if err := d.store.MarkDoorbellSent(ctx, tx, bell.ID); err != nil {
		return doorbellEmpty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return doorbellEmpty, fmt.Errorf("committing a notification: %w", err)
	}
	return doorbellSent, nil
}

// sendAll mints one unsubscribe link per recipient and sends to each.
//
// One token per person, never one per notification. An unsubscribe link is a
// bearer credential that turns off somebody's email, so a shared one would let
// any recipient unsubscribe every other recipient, and the schema would record
// it as their own choice.
//
// The first failure stops the batch and the whole row is retried. That means a
// second attempt can re-send to somebody who already received it, which is the
// same trade the transactional outbox makes and the same direction: a duplicate
// doorbell is a nuisance, a finding nobody hears about is the failure this
// product exists to prevent.
func (d *DoorbellDispatcher) sendAll(
	ctx context.Context, tx pgx.Tx, bell postgres.Doorbell, recipients []postgres.Recipient,
) error {
	for _, r := range recipients {
		// Built per recipient from the row's own organisation, so the link
		// lands in the organisation the notification is about rather than
		// wherever the reader's session last pointed. §8 names the failure that
		// prevents: a consultant with three clients acting against the wrong
		// company from a stale link.
		findingURL := fmt.Sprintf("%s/o/%s/feed/%s", d.baseURL, r.OrgSlug, bell.FindingID)

		token, err := newCapabilityToken()
		if err != nil {
			return err
		}
		if err := d.store.MintCapabilityToken(ctx, tx, bell.OrgID, r.UserID,
			"unsubscribe", postgres.HashInvitationToken(token), UnsubscribeLifetime); err != nil {
			return err
		}

		msg := notify.FindingNotification(
			r.Email, r.OrgName, r.FindingSeverity, findingURL,
			fmt.Sprintf("%s/unsubscribe/%s", d.baseURL, token))

		if err := d.channel.Send(ctx, delivery.Message{
			To:       msg.RecipientEmail,
			Subject:  msg.Subject,
			BodyText: msg.BodyText,
		}); err != nil {
			return err
		}
	}
	return nil
}

// localTime is `now` in somebody's zone, falling back to UTC.
//
// A zone that will not load falls back rather than failing the delivery. The
// preferences write already refuses an unknown zone, so reaching this means the
// tzdata on this host differs from the one that accepted it, and dropping a
// compliance notification over a timezone database mismatch is the wrong trade.
func (d *DoorbellDispatcher) localTime(zone string) time.Time {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return d.now().UTC()
	}
	return d.now().In(loc)
}

// newCapabilityToken mints a bearer capability, the same shape as an invitation
// token: 32 bytes from crypto/rand, base64url unpadded so it survives a URL
// path segment untouched.
func newCapabilityToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating a capability token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
