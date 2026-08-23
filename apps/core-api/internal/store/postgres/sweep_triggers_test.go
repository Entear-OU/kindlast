package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/onboarding"
)

// The trigger ENT-212 shipped without (00035), proved against a real Postgres
// rather than assumed from the migration's own comments.
//
// # WHY A FRESH ORGANISATION RATHER THAN THE SHARED FIXTURES
//
// `alphaOrg`/`betaOrg` are used by onboarding tests elsewhere in this package,
// and the drain test below claims *whichever* row is oldest and pending. Reusing
// a shared fixture would make this test's outcome depend on what another test
// left behind, in whichever order the suite happens to run. A fresh
// organisation makes both tests' assertions exact rather than "at least one".

// seedSweepTestOrg makes an organisation with an owner and removes it after.
// The cascade from `organisations` takes any `sweep_triggers` row with it, the
// same cleanup `seedRetentionOrg` relies on for `transactional_outbox`.
func seedSweepTestOrg(t *testing.T) (org, owner uuid.UUID) {
	t.Helper()

	conn := migratorConn(t)
	org = uuid.New()
	owner = uuid.New()

	if _, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "sweep-trigger-test-"+org.String()[:8], "Sweep trigger test"); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	if _, err := conn.Exec(t.Context(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org, owner); err != nil {
		t.Fatalf("seeding an owner: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, org)
	})
	return org, owner
}

// confirmWithMinimalFacts drives one organisation through the interview far
// enough to confirm, the same call ConfirmProfile makes.
func confirmWithMinimalFacts(t *testing.T, tenant *Tenant) string {
	t.Helper()

	session, _, err := tenant.StartOnboardingSession(t.Context())
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	facts := map[string]string{
		memory.KeyIndustry:   `"a bakery"`,
		memory.KeyStaffCount: `4`,
	}
	profileID, err := tenant.ConfirmOnboarding(t.Context(), session.ID, facts)
	if err != nil {
		t.Fatalf("confirming: %v", err)
	}
	return profileID
}

// TestConfirmingOnboardingEnqueuesASweepTrigger proves the write half: the row
// lands, in the same transaction, before anything drains it.
//
// # THE COMMIT MATTERS HERE MORE THAN IN MOST TESTS IN THIS PACKAGE
//
// Several sibling tests read their own writes back through the same
// uncommitted transaction and never call Commit, which is fine for asserting
// what a transaction contains. This one commits deliberately, because the
// claim this test makes is that the row is durable and visible to a *different*
// connection once the request that wrote it finishes, which is exactly the
// property TestATriggerIsListedRunAndSettledAcrossTheConnectionBoundary
// depends on and an uncommitted row would not prove.
func TestConfirmingOnboardingEnqueuesASweepTrigger(t *testing.T) {
	store := testStore(t)
	org, owner := seedSweepTestOrg(t)

	tenant, err := store.BeginTenant(t.Context(), owner.String(), org.String())
	if err != nil {
		t.Fatalf("beginning the tenant transaction: %v", err)
	}
	confirmWithMinimalFacts(t, tenant)
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}

	conn := migratorConn(t)
	var reason, status string
	var doneAt *time.Time
	err = conn.QueryRow(t.Context(), `
		select reason, status, done_at
		  from sweep_triggers
		 where org_id = $1
	`, org).Scan(&reason, &status, &doneAt)
	if err != nil {
		t.Fatalf("reading the sweep trigger: %v", err)
	}
	if reason != onboarding.ReasonOnboardingConfirmed {
		t.Errorf("reason = %q, want %q", reason, onboarding.ReasonOnboardingConfirmed)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending: nothing has run it yet", status)
	}
	if doneAt != nil {
		t.Errorf("done_at is set on a row nothing has processed")
	}

	var count int
	if err := conn.QueryRow(t.Context(),
		`select count(*) from sweep_triggers where org_id = $1`, org,
	).Scan(&count); err != nil {
		t.Fatalf("counting sweep triggers: %v", err)
	}
	if count != 1 {
		t.Errorf("%d sweep triggers for one confirmation, want 1", count)
	}
}

// TestATriggerIsListedRunAndSettledAcrossTheConnectionBoundary proves the
// read half, and proves it across the connection boundary the migration exists
// to respect: the facts are committed by an app-pool transaction, and the
// sweep that reads them runs on a completely separate agent-pool connection,
// driven the way the workflow drives it: list, Watcher, Analyst, settle.
//
// This is the seam AGENTS.md asks to be driven once against the real stack
// rather than trusted from a fake: ConfirmOnboarding and the agent's sweep
// have unit-level coverage each, and have never met before this test.
func TestATriggerIsListedRunAndSettledAcrossTheConnectionBoundary(t *testing.T) {
	store := testStore(t)
	agent := agentStore(t)
	org, owner := seedSweepTestOrg(t)

	tenant, err := store.BeginTenant(t.Context(), owner.String(), org.String())
	if err != nil {
		t.Fatalf("beginning the tenant transaction: %v", err)
	}
	confirmWithMinimalFacts(t, tenant)
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}

	// Listed. Other suites in this package enqueue their own triggers against
	// their own organisations, so what this test owns is the row for *its*
	// organisation being among them.
	triggers, err := agent.PendingSweepTriggers(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var mine *SweepTrigger
	for i := range triggers {
		if triggers[i].OrgID == org.String() {
			mine = &triggers[i]
		}
	}
	if mine == nil {
		t.Fatalf("the trigger confirming %s's onboarding wrote was not listed as pending", org)
	}
	if mine.Reason != onboarding.ReasonOnboardingConfirmed {
		t.Errorf("reason = %q, want %q", mine.Reason, onboarding.ReasonOnboardingConfirmed)
	}

	// And the daily schedule would visit this organisation too: it has a
	// profile now.
	targets, err := agent.SweepTargets(t.Context())
	if err != nil {
		t.Fatalf("listing targets: %v", err)
	}
	found := false
	for _, id := range targets {
		if id == org.String() {
			found = true
		}
	}
	if !found {
		t.Fatal("an organisation with a confirmed profile is not a sweep target")
	}

	// The Watcher, then the Analyst, as two calls on the agent pool: the two
	// activities of the workflow.
	if _, err := agent.RunSweep(t.Context(), mine.OrgID, true); err != nil {
		t.Fatalf("the watcher: %v", err)
	}
	if _, err := agent.RunAnalyst(t.Context(), mine.OrgID); err != nil {
		t.Fatalf("the analyst: %v", err)
	}

	// A failed attempt first, to prove it is recorded and leaves the row
	// pending; then done.
	if settled, err := agent.SettleSweepTrigger(t.Context(), mine.ID, errNoSuchThing); err != nil || !settled {
		t.Fatalf("recording a failed attempt: settled=%v err=%v", settled, err)
	}
	conn := migratorConn(t)
	var status string
	var attempts int
	var lastError *string
	var doneAt *time.Time
	read := func() {
		if err := conn.QueryRow(t.Context(), `
			select status, attempts, last_error, done_at from sweep_triggers where id = $1::uuid
		`, mine.ID).Scan(&status, &attempts, &lastError, &doneAt); err != nil {
			t.Fatalf("reading the sweep trigger: %v", err)
		}
	}
	read()
	if status != "pending" || attempts != 1 || lastError == nil || *lastError != errNoSuchThing.Error() {
		t.Fatalf("after a failed attempt: status=%s attempts=%d last_error=%v; want pending, 1, the cause", status, attempts, lastError)
	}

	if settled, err := agent.SettleSweepTrigger(t.Context(), mine.ID, nil); err != nil || !settled {
		t.Fatalf("marking done: settled=%v err=%v", settled, err)
	}
	read()
	if status != "done" || doneAt == nil || attempts != 2 || lastError != nil {
		t.Fatalf("after done: status=%s attempts=%d last_error=%v done_at=%v; want done, 2, nil, set", status, attempts, lastError, doneAt)
	}

	// Settled twice is a no-op that says so, and the row is no longer listed.
	if settled, err := agent.SettleSweepTrigger(t.Context(), mine.ID, nil); err != nil || settled {
		t.Fatalf("settling a done row: settled=%v err=%v; want false, nil", settled, err)
	}
	triggers, err = agent.PendingSweepTriggers(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing again: %v", err)
	}
	for _, tr := range triggers {
		if tr.ID == mine.ID {
			t.Fatal("a done trigger is still listed as pending")
		}
	}
}

var errNoSuchThing = errors.New("sweep: the watcher fell over")
