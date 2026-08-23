package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The doorbell path's store half as the workflow drives it (ENT-256, part
// three): a finding's insert enqueues a notification by trigger (00002), the
// relay lists it by id, the plan reads it without a lock, the send and the
// settle lock it by id, and a settled row reads as pgx.ErrNoRows to all three.
//
// Against the real database because the row is created by a trigger and
// protected by the agent's policies, and a fake of either would be the thing
// under test.

// notificationFor reads the outbox row the trigger wrote for a finding.
func notificationFor(t *testing.T, findingID string) string {
	t.Helper()
	var id string
	if err := migratorPool(t).QueryRow(context.Background(),
		`select id::text from notification_outbox where finding_id = $1`, findingID).Scan(&id); err != nil {
		t.Fatalf("reading the notification the trigger should have written: %v", err)
	}
	return id
}

func TestANewFindingsNotificationIsListedReadableAndLockable(t *testing.T) {
	agent := agentStore(t)
	finding := seedSnoozedFinding(t, alphaOrg, "now() + interval '1 day'")
	id := notificationFor(t, finding)

	ids, err := agent.PendingDoorbellIDs(t.Context(), 1000)
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
		t.Fatal("the new finding's notification was not listed as pending")
	}

	bell, err := agent.Doorbell(t.Context(), id)
	if err != nil {
		t.Fatalf("reading for the plan: %v", err)
	}
	if bell.FindingID != finding || bell.OrgID != alphaOrg {
		t.Fatalf("read %+v, want the finding and its organisation", bell)
	}

	tx, err := agent.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	locked, err := agent.LockDoorbell(t.Context(), tx, id)
	if err != nil {
		t.Fatalf("locking: %v", err)
	}
	if locked.ID != id {
		t.Fatalf("locked %q, want %q", locked.ID, id)
	}
	if err := agent.MarkDoorbellSent(t.Context(), tx, id); err != nil {
		t.Fatalf("marking sent: %v", err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Settled: invisible to the plan, the lock and the list alike.
	if _, err := agent.Doorbell(t.Context(), id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a sent notification read as %v, want no rows", err)
	}
	tx2, err := agent.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx2.Rollback(t.Context()) }()
	if _, err := agent.LockDoorbell(t.Context(), tx2, id); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a sent notification locked as %v, want no rows", err)
	}
	ids, err = agent.PendingDoorbellIDs(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing again: %v", err)
	}
	for _, listed := range ids {
		if listed == id {
			t.Fatal("a sent notification is still listed as pending")
		}
	}
}
