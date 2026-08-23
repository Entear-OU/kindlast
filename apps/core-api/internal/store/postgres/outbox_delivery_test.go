package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Delivering one message by id (ENT-256, part three), against the real
// database, because the properties that matter are the row's: that a send
// marks it sent in the same transaction, that a failed send records the
// attempt and leaves it pending, and that a second delivery of a settled row
// sends nothing and says so. The Temporal side retries on the strength of
// those three, and a fake store would be testing the fake.

// pendingMessageID reads back the id of the one message seeded for an
// organisation.
func pendingMessageID(t *testing.T, org uuid.UUID) string {
	t.Helper()
	var id string
	if err := migratorConn(t).QueryRow(t.Context(),
		`select id::text from transactional_outbox where org_id = $1`, org).Scan(&id); err != nil {
		t.Fatalf("reading the seeded message id: %v", err)
	}
	return id
}

func TestASentMessageIsMarkedSentAndNotSentAgain(t *testing.T) {
	store := agentStore(t)
	org, owner := seedRetentionOrg(t)
	seedInvitationAndMessage(t, org, owner,
		"deliver-"+uuid.NewString()[:8]+"@example.invalid", "7 days", "Accept at https://example.test/i/tok")
	id := pendingMessageID(t, org)

	var handed []PendingMessage
	send := func(_ context.Context, msg PendingMessage) error {
		handed = append(handed, msg)
		return nil
	}

	first, err := store.DeliverMessage(t.Context(), id, send)
	if err != nil {
		t.Fatalf("delivering: %v", err)
	}
	if !first.Sent || first.Attempts != 1 || first.SentAt.IsZero() {
		t.Fatalf("first delivery = %+v, want sent on attempt 1 with a time", first)
	}
	if len(handed) != 1 || handed[0].RecipientEmail == "" || handed[0].BodyText == "" {
		t.Fatalf("the channel was handed %+v, want the rendered message", handed)
	}

	// The retry Temporal will make if the activity timed out after the mail
	// left: nothing pending by that id, success, and the channel not asked.
	second, err := store.DeliverMessage(t.Context(), id, send)
	if err != nil {
		t.Fatalf("redelivering a sent message: %v", err)
	}
	if second.Sent {
		t.Fatal("a sent message was reported sent again")
	}
	if len(handed) != 1 {
		t.Fatalf("the channel was asked %d times, want 1: a settled row is never resent", len(handed))
	}

	var status string
	var attempts int
	if err := migratorConn(t).QueryRow(t.Context(),
		`select status, attempts from transactional_outbox where id = $1::uuid`, id).
		Scan(&status, &attempts); err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if status != "sent" || attempts != 1 {
		t.Fatalf("row is %s after %d attempts, want sent after 1", status, attempts)
	}
}

func TestAFailedSendIsRecordedAndLeftPending(t *testing.T) {
	store := agentStore(t)
	org, owner := seedRetentionOrg(t)
	seedInvitationAndMessage(t, org, owner,
		"refuse-"+uuid.NewString()[:8]+"@example.invalid", "7 days", "Accept at https://example.test/i/tok")
	id := pendingMessageID(t, org)

	refused := errors.New("451 4.3.0 try again later")
	result, err := store.DeliverMessage(t.Context(), id,
		func(context.Context, PendingMessage) error { return refused })

	if !errors.Is(err, ErrNotDelivered) || !errors.Is(err, refused) {
		t.Fatalf("err = %v, want ErrNotDelivered wrapping the channel's answer", err)
	}
	if result.Sent || result.Attempts != 1 {
		t.Fatalf("result = %+v, want not sent, attempt 1 recorded", result)
	}

	var status, lastError string
	var attempts int
	if err := migratorConn(t).QueryRow(t.Context(),
		`select status, attempts, coalesce(last_error, '') from transactional_outbox where id = $1::uuid`, id).
		Scan(&status, &attempts, &lastError); err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if status != "pending" {
		t.Fatalf("status = %s, want pending: giving up is the reclaim's call, not the sender's", status)
	}
	if attempts != 1 || lastError != refused.Error() {
		t.Fatalf("attempts = %d, last_error = %q; want 1 and the server's answer", attempts, lastError)
	}

	// And it is still listed, so the relay offers it again.
	ids, err := store.PendingMessageIDs(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	found := false
	for _, listed := range ids {
		if listed == id {
			found = true
		}
	}
	if !found {
		t.Fatal("a message that failed to send is no longer listed as pending")
	}
}

func TestPendingMessageIDsListsOldestFirstAndIdsOnly(t *testing.T) {
	store := agentStore(t)
	org, owner := seedRetentionOrg(t)
	seedInvitationAndMessage(t, org, owner,
		"first-"+uuid.NewString()[:8]+"@example.invalid", "7 days", "first")
	// A second row a moment later. The clock in the container is fine-grained
	// enough that two inserts in sequence order by created_at.
	time.Sleep(5 * time.Millisecond)
	seedInvitationAndMessage(t, org, owner,
		"second-"+uuid.NewString()[:8]+"@example.invalid", "7 days", "second")

	ids, err := store.PendingMessageIDs(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}

	var mine []string
	for _, id := range ids {
		var body string
		if err := migratorConn(t).QueryRow(t.Context(),
			`select body_text from transactional_outbox where id = $1::uuid and org_id = $2`,
			id, org).Scan(&body); err == nil {
			mine = append(mine, body)
		}
	}
	if len(mine) != 2 || mine[0] != "first" || mine[1] != "second" {
		t.Fatalf("listed in order %v, want first then second", mine)
	}
}

func TestABadIdIsRefusedBeforeTheDatabaseIsAsked(t *testing.T) {
	store := agentStore(t)
	_, err := store.DeliverMessage(t.Context(), "not-a-uuid",
		func(context.Context, PendingMessage) error { t.Fatal("sent for a bad id"); return nil })
	if !errors.Is(err, ErrBadMessageID) {
		t.Fatalf("err = %v, want ErrBadMessageID", err)
	}
}
