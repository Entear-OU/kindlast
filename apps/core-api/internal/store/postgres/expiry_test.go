package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Snooze expiry on the producer pool (ENT-256, part two).
//
// Against the real database, because what is under test is partly 00034: that
// `expire_snoozed_findings()` is now SECURITY DEFINER, executable by the agent
// role, and reaches findings in every organisation with no GUC set. A fake
// would prove the Go wrapper calls a function and nothing about whether the
// function may run.
//
// The fixture rows are written as the migrator and deleted in cleanup, because
// this test commits (the expiry is its own transaction on the agent pool, so a
// rolled-back test transaction would not be visible to it).

// seedSnoozedFinding writes a finding in `org` that was deferred until
// `snoozedUntilSQL` (an SQL expression, so the test can say "an hour ago" in
// the database's clock rather than this process's).
func seedSnoozedFinding(t *testing.T, org, snoozedUntilSQL string) string {
	t.Helper()

	pool := migratorPool(t)
	ensureProfile(t, org)
	id := uuid.NewString()
	signal := uuid.NewString()

	if _, err := pool.Exec(context.Background(), `
		insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
		select $1, $2, p.id, 'profile_gap', 'expiry fixture', $3
		  from compliance_profiles p where p.org_id = $2 limit 1
	`, signal, org, "expiry-"+signal); err != nil {
		t.Fatalf("seeding a signal: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into findings (id, org_id, profile_id, watcher_finding_id, obligation_id,
		                      detected, proposed_action, status, snoozed_until)
		select $1, $2, p.id, $3, o.id, 'expiry fixture', 'nothing', 'snoozed', `+snoozedUntilSQL+`
		  from compliance_profiles p, obligations o
		 where p.org_id = $2 limit 1
	`, id, org, signal); err != nil {
		t.Fatalf("seeding a snoozed finding: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from findings where id = $1`, id)
		_, _ = pool.Exec(context.Background(), `delete from watcher_findings where id = $1`, signal)
	})
	return id
}

// ensureProfile gives the fixture organisation a compliance profile to hang
// findings off, if the seed did not. The seed creates the two organisations
// and their members and nothing else, so a test that needs a profile makes
// one, as the migrator, and removes it again.
func ensureProfile(t *testing.T, org string) {
	t.Helper()
	pool := migratorPool(t)

	var existing int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from compliance_profiles where org_id = $1`, org).Scan(&existing); err != nil {
		t.Fatalf("counting profiles: %v", err)
	}
	if existing > 0 {
		return
	}

	session := uuid.NewString()
	profile := uuid.NewString()
	if _, err := pool.Exec(context.Background(), `
		insert into onboarding_sessions (id, org_id, created_by) values ($1, $2, $3)
	`, session, org, adaUser); err != nil {
		t.Fatalf("seeding a session: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into compliance_profiles
		  (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
		values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')
	`, profile, org, adaUser, session); err != nil {
		t.Fatalf("seeding a profile: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `delete from compliance_profiles where id = $1`, profile)
		_, _ = pool.Exec(context.Background(), `delete from onboarding_sessions where id = $1`, session)
	})
}

func findingStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	if err := migratorPool(t).QueryRow(context.Background(),
		`select status from findings where id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("reading the finding: %v", err)
	}
	return status
}

func TestExpiringSnoozesBringsBackEveryOrganisationsDueFindings(t *testing.T) {
	agent := agentStore(t)

	// One due finding in each fixture organisation, and one in the first that
	// is not due yet. The pass sets no GUC, so reaching both organisations is
	// the thing 00034 exists for, and leaving the third alone is the bound.
	dueAlpha := seedSnoozedFinding(t, alphaOrg, "now() - interval '1 hour'")
	dueBeta := seedSnoozedFinding(t, betaOrg, "now() - interval '1 day'")
	notYet := seedSnoozedFinding(t, alphaOrg, "now() + interval '1 day'")

	result, err := agent.ExpireSnoozes(t.Context())
	if err != nil {
		t.Fatalf("expiring: %v", err)
	}

	// At least two, not exactly two: another test, or a real deferral on a
	// shared development stack, may have rows due as well. The three fixture
	// rows are what this asserts on.
	if result.Reemerged < 2 {
		t.Fatalf("reemerged = %d, want at least the two due fixtures", result.Reemerged)
	}
	if result.RanAt.IsZero() {
		t.Fatal("no ran_at came back")
	}

	if got := findingStatus(t, dueAlpha); got != "pending" {
		t.Errorf("alpha's due finding is %q, want pending", got)
	}
	if got := findingStatus(t, dueBeta); got != "pending" {
		t.Errorf("beta's due finding is %q, want pending", got)
	}
	if got := findingStatus(t, notYet); got != "snoozed" {
		t.Errorf("the finding not yet due is %q, want still snoozed", got)
	}
}

// Idempotent, which is what lets a scheduler retry it without anybody
// reasoning about it: the second pass finds the fixtures already back and
// counts nothing for them.
func TestExpiringSnoozesTwiceMovesNothingTwice(t *testing.T) {
	agent := agentStore(t)
	due := seedSnoozedFinding(t, alphaOrg, "now() - interval '1 hour'")

	if _, err := agent.ExpireSnoozes(t.Context()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if got := findingStatus(t, due); got != "pending" {
		t.Fatalf("after the first pass the finding is %q, want pending", got)
	}

	// Mark it so a second pass touching it would be visible: a pending
	// finding with a snoozed_until is not a state the function produces, so if
	// the second pass rewrote the row this would be cleared.
	if _, err := migratorPool(t).Exec(context.Background(),
		`update findings set snoozed_until = now() - interval '1 hour' where id = $1`, due); err != nil {
		t.Fatalf("marking: %v", err)
	}

	if _, err := agent.ExpireSnoozes(t.Context()); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	var snoozedUntil *string
	if err := migratorPool(t).QueryRow(context.Background(),
		`select snoozed_until::text from findings where id = $1`, due).Scan(&snoozedUntil); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if snoozedUntil == nil {
		t.Fatal("the second pass rewrote a finding that was already pending")
	}
}
