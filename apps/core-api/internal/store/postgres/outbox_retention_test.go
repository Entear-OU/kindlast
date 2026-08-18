package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Retention on the transactional outbox (ENT-242, migration 00030), against the
// real database, because every interesting property here is a property of the
// database: which rows the definer function may touch, and which it cannot
// reach at any argument the caller passes.
//
// The suite in db/tests covers the same ground from SQL. This covers the seam
// the Go side owns: that the store passes the window through in a form Postgres
// accepts as an interval, and reads both counts back. A window encoded wrongly
// is not a compile error and not a test failure anywhere else; it is a job that
// runs every hour and reclaims nothing.

// deliveredBodyRetention is the production window, restated here rather than
// imported. `internal/dispatch` imports this package, so the dependency cannot
// run the other way and the constant cannot live in one place for both.
const deliveredBodyRetention = 7 * 24 * time.Hour

// seedRetentionOrg makes an organisation with an owner, so the owner-only
// insert policy on `transactional_outbox` is satisfiable, and removes it after.
//
// The cleanup is the erasure path exercised in passing: `transactional_outbox`
// and `invitations` both cascade from `organisations`, and after 00030 that
// cascade is the only thing in the deployment that removes a row from either.
func seedRetentionOrg(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()

	conn := migratorConn(t)
	org := uuid.New()
	owner := uuid.New()

	if _, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "outbox-retention-"+org.String()[:8], "Outbox retention test"); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	if _, err := conn.Exec(t.Context(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org, owner); err != nil {
		t.Fatalf("seeding an owner: %v", err)
	}

	t.Cleanup(func() {
		// context.WithoutCancel because the test's own context is already done
		// by the time cleanups run, matching seedOrg above.
		//nolint:errcheck // best effort cleanup on a test fixture
		conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, org)
	})
	return org, owner
}

// seedInvitationAndMessage writes the pair the reclaim reasons about: an
// invitation, and the outbox message carrying its link.
//
// The two are written together because that is how `InviteMember` writes them,
// in one transaction, and because the reclaim decides a message's fate by
// asking about its invitation. A fixture seeding one without the other would be
// testing a state the product cannot produce.
func seedInvitationAndMessage(
	t *testing.T, org, owner uuid.UUID, email, expiresIn, body string,
) {
	t.Helper()

	conn := migratorConn(t)
	// FORCE ROW LEVEL SECURITY applies to the table owner too, so the
	// owner-only insert policy is evaluated for this connection exactly as it
	// is for the application.
	if _, err := conn.Exec(t.Context(),
		`select set_config('app.current_org_id', $1, false),
		        set_config('app.current_user_id', $2, false)`,
		org.String(), owner.String()); err != nil {
		t.Fatalf("setting the tenant: %v", err)
	}

	if _, err := conn.Exec(t.Context(),
		`insert into invitations (org_id, email, role, token_hash, expires_at)
		 values ($1, $2, 'member', $3, now() + $4::interval)`,
		org, email, "hash-"+uuid.NewString(), expiresIn); err != nil {
		t.Fatalf("seeding an invitation: %v", err)
	}
	if _, err := conn.Exec(t.Context(),
		`insert into transactional_outbox
		   (org_id, kind, recipient_email, subject, body_text)
		 values ($1, 'invitation', $2, 'You are invited', $3)`,
		org, email, body); err != nil {
		t.Fatalf("seeding a message: %v", err)
	}
}

func TestReclaimLeavesAMessageThatCanStillBeDelivered(t *testing.T) {
	// THE TEST THIS FILE EXISTS FOR, and the one whose failure is silent.
	//
	// The window is zero, which is the most aggressive thing this store method
	// can ask for, and the message must come back untouched. What protects it
	// is not the window: it is that the invitation can still be accepted, and
	// the raw token in that body exists nowhere else, because 00003 keeps only
	// the hash. Clearing it destroys an invitation somebody is waiting for, and
	// nobody could tell which ones needed reissuing.
	store := agentStore(t)
	org, owner := seedRetentionOrg(t)

	body := "Accept at https://example.test/i/tok-" + uuid.NewString()
	seedInvitationAndMessage(t, org, owner,
		"live-"+uuid.NewString()[:8]+"@example.invalid", "7 days", body)

	if _, err := store.ReclaimOutbox(t.Context(), 0, 100); err != nil {
		t.Fatalf("reclaiming: %v", err)
	}

	conn := migratorConn(t)
	var status, got string
	var redactedAt *time.Time
	if err := conn.QueryRow(t.Context(),
		`select status, body_text, redacted_at from transactional_outbox where org_id = $1`,
		org).Scan(&status, &got, &redactedAt); err != nil {
		t.Fatalf("reading the message back: %v", err)
	}

	if status != "pending" {
		t.Errorf("a message that can still be delivered was moved to %q", status)
	}
	if redactedAt != nil {
		t.Error("a message that can still be delivered was recorded as redacted")
	}
	if got != body {
		t.Errorf("the body of a deliverable message changed to %q", got)
	}
}

func TestReclaimAbandonsAMessageThatCanNoLongerBeDelivered(t *testing.T) {
	// An expired invitation cannot be accepted, so the token in the body is
	// inert. What is left is a dead credential and the address of somebody who
	// never accepted, and who therefore has no account to be erased with.
	store := agentStore(t)
	org, owner := seedRetentionOrg(t)

	seedInvitationAndMessage(t, org, owner,
		"expired-"+uuid.NewString()[:8]+"@example.invalid", "-1 day",
		"Accept at https://example.test/i/tok-expired")

	result, err := store.ReclaimOutbox(t.Context(), deliveredBodyRetention, 100)
	if err != nil {
		t.Fatalf("reclaiming: %v", err)
	}
	if result.Abandoned == 0 {
		t.Fatal("an undeliverable message was not abandoned")
	}

	conn := migratorConn(t)
	var status, body, recipient, lastError string
	var redactedAt *time.Time
	if err := conn.QueryRow(t.Context(),
		`select status, body_text, recipient_email, coalesce(last_error, ''), redacted_at
		   from transactional_outbox where org_id = $1`,
		org).Scan(&status, &body, &recipient, &lastError, &redactedAt); err != nil {
		t.Fatalf("reading the message back: %v", err)
	}

	// `failed` is 00014's word for giving up, and this is the first thing in
	// the codebase to write it. That matters beyond bookkeeping: the drain
	// claims `status = 'pending'` and has no maximum attempt count, so an
	// undeliverable message is otherwise retried every ten seconds forever.
	if status != "failed" {
		t.Errorf("an abandoned message is %q, want failed", status)
	}
	if redactedAt == nil {
		t.Error("an abandoned message was not recorded as redacted")
	}
	if body != "" || recipient != "" {
		t.Errorf("an abandoned message kept its body or address: %q / %q", body, recipient)
	}
	if lastError == "" {
		t.Error("an abandoned message does not say why it was abandoned")
	}
}

func TestReclaimIsIdempotent(t *testing.T) {
	// What makes the job safe to run every hour, and safe to run in more than
	// one replica at once: every predicate tests `redacted_at is null`, so a
	// row already done is invisible to the next pass.
	store := agentStore(t)
	org, owner := seedRetentionOrg(t)

	seedInvitationAndMessage(t, org, owner,
		"expired-"+uuid.NewString()[:8]+"@example.invalid", "-1 day",
		"Accept at https://example.test/i/tok-expired")

	if _, err := store.ReclaimOutbox(t.Context(), deliveredBodyRetention, 100); err != nil {
		t.Fatalf("first reclaim: %v", err)
	}

	// Counted against this organisation rather than against the return value,
	// which is deployment-wide and would make this test depend on what every
	// other test happened to leave behind.
	conn := migratorConn(t)
	var before time.Time
	if err := conn.QueryRow(t.Context(),
		`select redacted_at from transactional_outbox where org_id = $1`,
		org).Scan(&before); err != nil {
		t.Fatalf("reading the first redaction: %v", err)
	}

	if _, err := store.ReclaimOutbox(t.Context(), deliveredBodyRetention, 100); err != nil {
		t.Fatalf("second reclaim: %v", err)
	}

	var after time.Time
	if err := conn.QueryRow(t.Context(),
		`select redacted_at from transactional_outbox where org_id = $1`,
		org).Scan(&after); err != nil {
		t.Fatalf("reading the second redaction: %v", err)
	}
	if !after.Equal(before) {
		t.Errorf("a redacted row was redacted again: %s then %s", before, after)
	}
}
