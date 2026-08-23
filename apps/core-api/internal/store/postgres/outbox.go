package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	Attempts       int32
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

// PendingMessageIDs lists up to `limit` undelivered messages, oldest first, by
// id alone (ENT-256, part three).
//
// Ids and nothing else, on purpose. The caller is the relay activity on
// `workers`, and what it returns is written into a Temporal workflow history
// that is kept for the namespace's retention and readable in the UI. The
// address, the subject and the body (which for an invitation holds a bearer
// token in the clear, 00030) are read by DeliverMessage inside this process
// and never cross that boundary.
func (a *AgentStore) PendingMessageIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := a.pool.Query(ctx, `
		select id::text
		  from transactional_outbox
		 where status = 'pending'
		 order by created_at
		 limit $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing pending messages: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres: reading a pending message id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: listing pending messages: %w", err)
	}
	return ids, nil
}

// Delivery is what one DeliverMessage call did.
type Delivery struct {
	// Sent is true when the message left on this call. False means there was
	// nothing pending by that id: it went earlier, the reclaim gave up on it,
	// or it never existed. Either way the row is settled, which is the
	// outcome the caller is retrying towards.
	Sent     bool
	Attempts int32
	SentAt   time.Time
}

// ErrBadMessageID is returned when the id is not a uuid at all, which is a
// caller bug rather than a state of the table, and is the one failure a
// retry cannot fix.
var ErrBadMessageID = errors.New("postgres: the message id is not a uuid")

// DeliverMessage claims one message by id, sends it, and records what happened
// (ENT-256, part three; the transaction discipline is ENT-219's).
//
// # WHY THE SEND IS INSIDE THE TRANSACTION, STILL
//
// This replaces a drain that took the oldest pending row with `for update skip
// locked`, and the reason that drain held its transaction across the SMTP
// conversation has not changed: the row lock only exists for the life of a
// transaction, so a claim taken outside one protects nothing against a second
// deliverer. A duplicate invitation is not merely noise; it teaches the
// recipient that repeated credential-bearing mail from this product is normal,
// which is the habit a phishing attempt needs them to have.
//
// # WHY `for update` AND NOT `for update skip locked`, WHICH IS THE CHANGE
//
// The drain skipped locked rows because two drains racing over one queue should
// take disjoint sets. This is one row, named by the caller, and the only thing
// that can be holding it is an earlier attempt at delivering the very same
// message: Temporal guarantees one running workflow per id, so the race is a
// retry arriving while the attempt it is retrying is still mid-send (the
// activity timed out; the SMTP conversation did not). Skipping would report
// "nothing pending" while the mail is in flight and the row is about to be
// marked sent, which is true a second later and misleading now. Waiting on the
// lock instead means the retry sees the settled row, and reports that.
//
// # THE OUTCOME IS COMMITTED WHICHEVER WAY THE SEND WENT
//
// A failure persists its attempt count and the mail server's answer, or a
// relay that is refusing everything produces a silent hot loop with nothing in
// the table to show for it. The row stays `pending` rather than becoming
// `failed`, so the next attempt finds it: `failed` is reserved for giving up,
// and the only thing that gives up is the reclaim (00030), when the invitation
// the message carries can no longer be accepted. How many times to try is not
// decided here; it is the retry policy on the workflow, which is the point of
// the move.
//
// A crash between the send and the commit redelivers rather than drops, which
// is the correct direction: an invitation arriving twice is a nuisance, one
// never arriving cannot be repaired because the raw token is gone.
func (a *AgentStore) DeliverMessage(ctx context.Context, id string, deliver Deliver) (Delivery, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Delivery{}, ErrBadMessageID
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return Delivery{}, fmt.Errorf("postgres: beginning a delivery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var msg PendingMessage
	err = tx.QueryRow(ctx, `
		select id::text, kind, recipient_email, subject, body_text,
		       coalesce(body_html, ''), attempts
		  from transactional_outbox
		 where id = $1::uuid and status = 'pending'
		 for update
	`, id).Scan(&msg.ID, &msg.Kind, &msg.RecipientEmail, &msg.Subject,
		&msg.BodyText, &msg.BodyHTML, &msg.Attempts)

	if errors.Is(err, pgx.ErrNoRows) {
		return Delivery{}, nil
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("postgres: claiming a transactional message: %w", err)
	}

	if sendErr := deliver(ctx, msg); sendErr != nil {
		if _, err := tx.Exec(ctx, `
			update transactional_outbox
			   set attempts = attempts + 1, last_error = $2
			 where id = $1::uuid
		`, msg.ID, sendErr.Error()); err != nil {
			return Delivery{}, fmt.Errorf("postgres: recording a failed delivery: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Delivery{}, fmt.Errorf("postgres: committing a failed delivery: %w", err)
		}
		return Delivery{Attempts: msg.Attempts + 1}, fmt.Errorf("%w: %w", ErrNotDelivered, sendErr)
	}

	// `status` and `sent_at` move together because the check constraint refuses
	// any other combination, and that constraint is what makes "delivered" one
	// fact rather than two that can disagree.
	var result Delivery
	err = tx.QueryRow(ctx, `
		update transactional_outbox
		   set status = 'sent', sent_at = now(), attempts = attempts + 1, last_error = null
		 where id = $1::uuid and status = 'pending'
		returning attempts, sent_at
	`, msg.ID).Scan(&result.Attempts, &result.SentAt)
	if err != nil {
		return Delivery{}, fmt.Errorf("postgres: marking a message sent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Delivery{}, fmt.Errorf("postgres: committing a delivery: %w", err)
	}
	result.Sent = true
	return result, nil
}

// ErrNotDelivered wraps the channel's error when a send fails. The attempt has
// been recorded on the row by the time a caller sees this; what the caller
// decides is whether to try again, and when.
var ErrNotDelivered = errors.New("postgres: the message was not delivered")

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
