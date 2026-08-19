package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
)

// The transactional outbox, from both ends (ENT-219, migration 00014).
//
// Two roles touch this table and neither can do the other's half. The
// application enqueues inside the transaction that makes the announced fact
// true, and cannot mark anything delivered. The agent delivers across every
// organisation, and cannot author a message. See the migration header for why
// the split is drawn there.

// PendingMessage is a row the dispatcher has claimed.
type PendingMessage struct {
	ID             string
	Kind           string
	RecipientEmail string
	Subject        string
	BodyText       string
	BodyHTML       string
	Attempts       int
}

// EnqueueMessage writes a message onto the outbox in the caller's transaction.
//
// It takes `*Tenant` rather than the pool deliberately: the row must land in the
// same transaction as the fact it announces, or the two can disagree. An
// invitation committed without its message is permanently undeliverable, since
// the raw token is gone; a message committed without its invitation names a
// link that redeems nothing. Both are avoided by there being one transaction and
// no way to reach this except through it.
func (t *Tenant) EnqueueMessage(ctx context.Context, msg notify.Message) error {
	var html any
	if msg.BodyHTML != "" {
		html = msg.BodyHTML
	}

	_, err := t.tx.Exec(ctx, `
		insert into transactional_outbox
			(org_id, kind, recipient_email, subject, body_text, body_html)
		values ($1, $2, $3, $4, $5, $6)
	`, t.orgID, msg.Kind, msg.RecipientEmail, msg.Subject, msg.BodyText, html)
	if err != nil {
		return fmt.Errorf("postgres: enqueuing a transactional message: %w", err)
	}
	return nil
}

// OrganisationName reads the caller's organisation name.
//
// Its own method rather than a join inside CreateInvitation, because the name
// is needed to render the message before the invitation row exists, and reading
// it through the tenant's own policy is what proves the caller may see it.
func (t *Tenant) OrganisationName(ctx context.Context) (string, error) {
	var name string
	err := t.tx.QueryRow(ctx,
		`select name from organisations where id = $1`, t.orgID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", pgx.ErrNoRows
	}
	if err != nil {
		return "", fmt.Errorf("postgres: reading the organisation name: %w", err)
	}
	return name, nil
}

// OrganisationSlug reads the caller's organisation slug.
//
// Read through the tenant's own policy rather than passed in, which is the
// same argument OrganisationName makes and matters more here: the answer is
// the URL an approve-from-email interstitial sends somebody to, and §8's named
// failure is a consultant landing in the wrong company's console. A slug that
// came from the request could be any of the three they have open.
func (t *Tenant) OrganisationSlug(ctx context.Context) (string, error) {
	var slug string
	err := t.tx.QueryRow(ctx,
		`select slug from organisations where id = $1`, t.orgID).Scan(&slug)
	if err != nil {
		return "", fmt.Errorf("postgres: reading the organisation slug: %w", err)
	}
	return slug, nil
}

// Deliver sends one message somewhere. Supplied by the caller so the store owns
// the transaction discipline and the channel owns the sending, and neither
// knows how the other works.
type Deliver func(ctx context.Context, msg PendingMessage) error

// DrainResult is what one pass over the outbox achieved.
type DrainResult struct {
	Sent   int
	Failed int
}

// Drain delivers up to `batch` pending messages, one transaction each.
//
// # WHY ONE TRANSACTION PER MESSAGE, WITH THE SEND INSIDE IT
//
// `for update skip locked` is what makes two dispatchers safe. Without it both
// read the same pending row and both deliver it, and a duplicate invitation is
// not merely noise: it teaches the recipient that repeated credential-bearing
// mail from this product is normal, which is the habit a phishing attempt needs
// them to have.
//
// The lock only exists for the life of its transaction, so the send has to
// happen inside one or the guarantee evaporates: a `select ... for update` on a
// pooled connection with no explicit transaction releases the row the instant
// the query returns, and the code reads exactly as though it were protected.
//
// Holding a transaction across a network call is a real cost and it is bounded
// deliberately: one message per transaction rather than a batch, so the lock is
// held for one SMTP conversation rather than for the whole drain. The
// alternative, claiming a batch by moving rows to a `claimed` status first,
// avoids the open transaction and buys a new failure: a dispatcher that dies
// mid-batch leaves rows claimed forever, and nothing reaps them without a
// visibility timeout this issue does not need yet.
//
// A crash between the send and the commit redelivers rather than drops, which
// is the correct direction here. An invitation arriving twice is a nuisance; an
// invitation never arriving cannot be repaired, because the raw token is gone.
func (a *AgentStore) Drain(ctx context.Context, batch int, deliver Deliver) (DrainResult, error) {
	var result DrainResult

	for range batch {
		delivered, err := a.deliverOne(ctx, deliver)
		if err != nil {
			return result, err
		}
		switch delivered {
		case outcomeEmpty:
			return result, nil
		case outcomeSent:
			result.Sent++
		case outcomeFailed:
			result.Failed++
		}
	}
	return result, nil
}

type outcome int

const (
	outcomeEmpty outcome = iota
	outcomeSent
	outcomeFailed
)

// deliverOne claims a single message, sends it, and records what happened.
//
// The outcome is committed whichever way the send went: a failure has to
// persist its attempt count and error text, or a mail server that is refusing
// everything produces a silent hot loop with nothing in the table to show for
// it.
func (a *AgentStore) deliverOne(ctx context.Context, deliver Deliver) (outcome, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return outcomeEmpty, fmt.Errorf("postgres: beginning a delivery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var msg PendingMessage
	err = tx.QueryRow(ctx, `
		select id::text, kind, recipient_email, subject, body_text,
		       coalesce(body_html, ''), attempts
		  from transactional_outbox
		 where status = 'pending'
		 order by created_at
		 limit 1
		 for update skip locked
	`).Scan(&msg.ID, &msg.Kind, &msg.RecipientEmail, &msg.Subject,
		&msg.BodyText, &msg.BodyHTML, &msg.Attempts)

	if errors.Is(err, pgx.ErrNoRows) {
		return outcomeEmpty, nil
	}
	if err != nil {
		return outcomeEmpty, fmt.Errorf("postgres: claiming a transactional message: %w", err)
	}

	sendErr := deliver(ctx, msg)

	if sendErr != nil {
		// The row stays `pending` rather than becoming `failed`, so the next
		// drain retries it. `failed` is reserved for giving up, which nothing
		// does yet: with no maximum attempt count, a message that cannot be
		// delivered now is one that delivers when the mail server returns, and
		// that is what a self-hoster wants from a queue holding invitations.
		//
		// The error text is stored so an operator can see the cause without
		// reading logs from a container that has since been replaced.
		if _, err := tx.Exec(ctx, `
			update transactional_outbox
			   set attempts = attempts + 1, last_error = $2
			 where id = $1::uuid
		`, msg.ID, sendErr.Error()); err != nil {
			return outcomeEmpty, fmt.Errorf("postgres: recording a failed delivery: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return outcomeEmpty, fmt.Errorf("postgres: committing a failed delivery: %w", err)
		}
		return outcomeFailed, nil
	}

	// `status` and `sent_at` move together because the check constraint refuses
	// any other combination, and that constraint is what makes "delivered" one
	// fact rather than two that can disagree.
	if _, err := tx.Exec(ctx, `
		update transactional_outbox
		   set status = 'sent', sent_at = now(), attempts = attempts + 1, last_error = null
		 where id = $1::uuid and status = 'pending'
	`, msg.ID); err != nil {
		return outcomeEmpty, fmt.Errorf("postgres: marking a message sent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return outcomeEmpty, fmt.Errorf("postgres: committing a delivery: %w", err)
	}
	return outcomeSent, nil
}

// ReclaimResult is what one pass of the retention job achieved.
type ReclaimResult struct {
	// Delivered messages whose body was cleared.
	Redacted int
	// Undelivered messages given up on, because the invitation they carry can
	// no longer be accepted.
	Abandoned int
}

// ReclaimOutbox clears the personal data out of messages that no longer need it
// (ENT-242, migration 00030).
//
// # WHY THIS DELETES NOTHING
//
// An outbox row is two separable things: a delivery fact, and a rendered
// message holding a recipient's address and, for an invitation, the raw bearer
// token in the clear. Only the second is personal data. Deleting the row drops
// the data by throwing away the fact; keeping the row holds the fact by keeping
// the data. Redaction is the only option that does not force that trade, so
// nothing here or in the migration removes a row: the only thing that does is
// the cascade from `organisations`, which is how erasing an organisation
// already works.
//
// The contrast worth drawing is with `audit_log`, which deliberately has no
// retention at all (db/README.md) because a regulator may be shown it. The
// outbox is the envelope rather than the letter: what a customer or a regulator
// would be shown about an invitation is in `invitations`, which keeps the
// address, the inviter and the outcome and is untouched by this. What the
// outbox adds on top is the rendered text, and a few days after delivery that
// is a dead credential and somebody's email address.
//
// # WHY A FUNCTION AND NOT A STATEMENT HERE
//
// Deciding an undelivered message's fate means asking whether the invitation it
// carries can still be accepted, which is a read of `invitations`. The agent
// role has no grant there by design (00008), because a role that can fabricate
// a finding should not also be able to enumerate every invited address in the
// deployment. The definer function answers that one question about rows it is
// already looking at, and nothing adjacent. Same argument 00015 made for
// `notification_recipients`.
//
// The period is passed in rather than living in the function, because a
// retention period consults nothing and could reasonably be different next
// quarter, which is the test for a decision (§14.5). What is not passed in is
// the rule that a message which can still be delivered is never touched: that
// predicate takes no argument, so no value of `bodyRetention`, including zero,
// can reach a live invitation.
func (a *AgentStore) ReclaimOutbox(ctx context.Context, bodyRetention time.Duration, batch int) (ReclaimResult, error) {
	var result ReclaimResult

	// Seconds rather than a pgx interval, matching CreateInvitation: the driver
	// has no native interval and this is the encoding the rest of the store
	// already uses.
	err := a.pool.QueryRow(ctx, `
		select redacted, abandoned
		  from reclaim_transactional_outbox($1::interval, $2)
	`, fmt.Sprintf("%d seconds", int(bodyRetention.Seconds())), batch).
		Scan(&result.Redacted, &result.Abandoned)
	if err != nil {
		return ReclaimResult{}, fmt.Errorf("postgres: reclaiming the transactional outbox: %w", err)
	}
	return result, nil
}
