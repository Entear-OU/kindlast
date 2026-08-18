package postgres

import (
	"context"
	"errors"
	"fmt"

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
