package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// The three registers, read through the code path that will serve requests.
//
// MOST OF THIS RUNS INSIDE A TRANSACTION THAT IS ROLLED BACK
//
// Seeding and asserting in one transaction leaks nothing, which matters here:
// an earlier suite in this package left 42 fixture rows behind when its cleanup
// silently did nothing, and nobody noticed until they showed up in a count.
//
// The exception is the cross-tenant test, and the reason is worth stating
// because it is a trap. Uncommitted rows are invisible to another transaction
// whatever the policies say, so a cross-tenant assertion against rolled-back
// fixtures passes with RLS switched off entirely. That one commits, and cleans
// up by primary key afterwards.
//
// RLS itself is asserted structurally by db/tests (force-rls, rls-isolation)
// over every table carrying an org_id. What is under test here is this
// package's own code: the orderings, the keyset cursors, and the quota.

// seedRegisters inserts the onboarding session and compliance profile the three
// registers hang off, and returns both ids.
//
// BOTH ids, not just the profile, and that is not tidiness. The profile
// cascades the records below it, so deleting the profile looks like a complete
// cleanup and leaves the session behind. The first version of this returned only
// the profile id and leaked two sessions, found by counting rows after the suite
// rather than by trusting the cleanup to have worked.
//
// Everything is inserted through the app role, so a missing INSERT policy or
// grant fails here rather than in production.
func seedRegisters(t *testing.T, tx pgx.Tx, ctx context.Context, orgID, userID string) (profileID, sessionID string) {
	t.Helper()

	err := tx.QueryRow(ctx, `
		insert into onboarding_sessions (org_id, created_by, status)
		values ($1, $2, 'completed')
		returning id::text
	`, orgID, userID).Scan(&sessionID)
	if err != nil {
		t.Fatalf("seeding an onboarding session: %v", err)
	}

	err = tx.QueryRow(ctx, `
		insert into compliance_profiles (
			session_id, org_id, created_by, industry,
			has_dpo, has_ropa, transfers_outside_eu
		)
		values ($1, $2, $3, 'Testing', 'no', 'no', 'no')
		returning id::text
	`, sessionID, orgID, userID).Scan(&profileID)
	if err != nil {
		t.Fatalf("seeding a compliance profile: %v", err)
	}

	return profileID, sessionID
}

func TestTheArticle30RegisterPagesNewestFirstAndTheCursorDoesNotSkip(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	profile, _ := seedRegisters(t, tenant.Tx(), ctx, alphaOrg, adaUser)

	// Five entries with distinct, deliberately out-of-insertion-order created_at
	// values, so an implementation that returned insertion order rather than
	// the declared ordering fails.
	base := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	offsets := []int{2, 0, 4, 1, 3}
	for i, off := range offsets {
		_, err := tenant.Tx().Exec(ctx, `
			insert into processing_activities (org_id, profile_id, name, created_at)
			values ($1, $2, $3, $4)
		`, alphaOrg, profile, names[i], base.Add(time.Duration(off)*time.Hour))
		if err != nil {
			t.Fatalf("seeding activity %d: %v", i, err)
		}
	}

	// Two pages of two, then the remainder. Walking the cursor must visit all
	// five exactly once: that is the property a keyset exists for.
	seen := []string{}
	cursor := ""
	for page := 0; page < 5; page++ {
		got, err := tenant.ProcessingActivities(ctx, cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, item := range got.Items {
			seen = append(seen, item.Name)
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}

	if len(seen) != 5 {
		t.Fatalf("walked the cursor and saw %d entries, want 5: %v", len(seen), seen)
	}

	// Newest first. offsets[2] is the largest, so names[2] leads.
	want := []string{names[2], names[4], names[0], names[3], names[1]}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("ordering wrong at %d: got %v, want %v", i, seen, want)
		}
	}
}

var names = []string{"Alpha activity", "Bravo activity", "Charlie activity", "Delta activity", "Echo activity"}

func TestTheDsarLogOrdersBySoonestDeadlineNotByCreation(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	received := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	// Inserted in one order, due in another. An implementation that ordered by
	// creation would return the insertion order and pass a weaker test.
	due := []int{30, 5, 60, 1, 15}
	for i, days := range due {
		_, err := tenant.Tx().Exec(ctx, `
			insert into dsars (org_id, created_by, subject_name, request_type, received_at, response_due_at)
			values ($1, $2, $3, 'access', $4, $5)
		`, alphaOrg, adaUser, names[i], received, received.AddDate(0, 0, days))
		if err != nil {
			t.Fatalf("seeding dsar %d: %v", i, err)
		}
	}

	page, err := tenant.Dsars(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("listing dsars: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("got %d requests, want 5", len(page.Items))
	}

	// Soonest first: 1, 5, 15, 30, 60 days out.
	want := []string{names[3], names[1], names[4], names[0], names[2]}
	for i := range want {
		if page.Items[i].SubjectName != want[i] {
			t.Fatalf("ordering wrong at %d: got %s, want %s", i, page.Items[i].SubjectName, want[i])
		}
	}
}

// The DSAR cursor ascends where the other two descend. A cursor built with the
// wrong comparison returns the same page forever or skips the rest of the list,
// and both look like a working endpoint until someone has more than one page.
func TestTheDsarCursorAscendsAndVisitsEveryRequestOnce(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	received := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	for i, days := range []int{30, 5, 60, 1, 15} {
		_, err := tenant.Tx().Exec(ctx, `
			insert into dsars (org_id, created_by, subject_name, request_type, received_at, response_due_at)
			values ($1, $2, $3, 'access', $4, $5)
		`, alphaOrg, adaUser, names[i], received, received.AddDate(0, 0, days))
		if err != nil {
			t.Fatalf("seeding dsar %d: %v", i, err)
		}
	}

	seen := map[string]int{}
	cursor := ""
	for page := 0; page < 5; page++ {
		got, err := tenant.Dsars(ctx, "", cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, d := range got.Items {
			seen[d.SubjectName]++
		}
		if got.NextCursor == "" {
			break
		}
		cursor = got.NextCursor
	}

	if len(seen) != 5 {
		t.Fatalf("saw %d distinct requests, want 5: %v", len(seen), seen)
	}
	for name, count := range seen {
		if count != 1 {
			t.Fatalf("%s appeared %d times; the cursor is repeating rows", name, count)
		}
	}
}

func TestAPageTokenFromAnotherListIsRefusedRatherThanMisread(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	for _, cursor := range []string{"not base64 at all!!", "bm8tc2VwYXJhdG9y", "MjAyNi0wMS0wMXxub3QtYS11dWlk"} {
		if _, err := tenant.ProcessingActivities(ctx, cursor, 10); !errors.Is(err, ErrBadCursor) {
			t.Fatalf("cursor %q: want ErrBadCursor, got %v", cursor, err)
		}
		if _, err := tenant.Dsars(ctx, "", cursor, 10); !errors.Is(err, ErrBadCursor) {
			t.Fatalf("cursor %q on dsars: want ErrBadCursor, got %v", cursor, err)
		}
	}
}

// The quota counts manual entries only. A record the Executor created on an
// approved finding is part of the compliance record and is never withheld
// behind a plan, so counting it would let approving findings exhaust the
// customer's own allowance.
func TestTheManualQuotaIgnoresExecutorCreatedRecords(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	profile, _ := seedRegisters(t, tenant.Tx(), ctx, alphaOrg, adaUser)

	before, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("reading the quota: %v", err)
	}

	// Two manual entries.
	for i := 0; i < 2; i++ {
		if _, err := tenant.Tx().Exec(ctx, `
			insert into processing_activities (org_id, profile_id, name)
			values ($1, $2, $3)
		`, alphaOrg, profile, names[i]); err != nil {
			t.Fatalf("seeding a manual activity: %v", err)
		}
	}

	// One that came from a finding.
	//
	// The finding id is a literal rather than a seeded row, for two reasons.
	// The app role cannot insert into `watcher_findings` at all (its RLS insert
	// policy is for the agent role, which is correct and was confirmed by this
	// test failing that way first), and `processing_activities.finding_id`
	// carries no foreign key, so the column can be exercised directly. What is
	// under test is the `finding_id is null` predicate, not referential
	// integrity.
	if _, err := tenant.Tx().Exec(ctx, `
		insert into processing_activities (org_id, profile_id, name, finding_id)
		values ($1, $2, 'executor created', $3)
	`, alphaOrg, profile, "f1000000-0000-4000-8000-00000000000f"); err != nil {
		t.Fatalf("seeding an executor activity: %v", err)
	}

	after, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("re-reading the quota: %v", err)
	}

	if got := after.Used - before.Used; got != 2 {
		t.Fatalf("quota counted %d new entries, want 2; the executor-created row is being counted", got)
	}
}

// A malformed id names no record, which is the same answer as an id naming a
// record in another organisation. It must not reach SQL and come back as a cast
// error from inside a policy.
func TestAMalformedIdReadsAsNotFoundRatherThanErroring(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	for _, id := range []string{"", "not-a-uuid", "12345"} {
		if _, err := tenant.ProcessingActivity(ctx, id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("activity %q: want ErrNoRows, got %v", id, err)
		}
		if _, err := tenant.AiSystem(ctx, id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("system %q: want ErrNoRows, got %v", id, err)
		}
		if _, err := tenant.Dsar(ctx, id); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("dsar %q: want ErrNoRows, got %v", id, err)
		}
	}
}

// The one test that has to commit, and the one that would otherwise pass for
// the wrong reason. See the file comment.
func TestAnotherOrganisationsRecordIsNotFoundRatherThanForbidden(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	ada, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}

	profile, session := seedRegisters(t, ada.Tx(), ctx, alphaOrg, adaUser)

	var activityID string
	err = ada.Tx().QueryRow(ctx, `
		insert into processing_activities (org_id, profile_id, name)
		values ($1, $2, 'Ada''s private activity')
		returning id::text
	`, alphaOrg, profile).Scan(&activityID)
	if err != nil {
		ada.Rollback(ctx)
		t.Fatalf("seeding Ada's activity: %v", err)
	}

	if err := ada.Commit(ctx); err != nil {
		t.Fatalf("committing Ada's fixture: %v", err)
	}

	// Delete by primary key, as Ada, and fail loudly rather than leaving rows
	// behind.
	//
	// The session is deleted as well as the profile, and that is the whole
	// lesson of this block. The profile cascades the activity under it, so
	// deleting only the profile looks complete and silently leaves the session:
	// that is exactly what the first version of this test did, and it was found
	// by counting rows afterwards rather than by the cleanup reporting anything.
	// Every delete asserts its row count for the same reason.
	t.Cleanup(func() {
		ctx := context.Background()

		cleanup, err := store.BeginTenant(ctx, adaUser, alphaOrg)
		if err != nil {
			t.Errorf("CLEANUP FAILED to open a transaction, fixtures remain: %v", err)
			return
		}
		defer cleanup.Rollback(ctx)

		for _, target := range []struct {
			table string
			query string
			id    string
		}{
			{"compliance_profiles", "delete from compliance_profiles where id = $1", profile},
			{"onboarding_sessions", "delete from onboarding_sessions where id = $1", session},
		} {
			tag, err := cleanup.Tx().Exec(ctx, target.query, target.id)
			if err != nil {
				t.Errorf("CLEANUP FAILED to delete %s %s: %v", target.table, target.id, err)
				return
			}
			if tag.RowsAffected() != 1 {
				t.Errorf("CLEANUP deleted %d rows from %s, want 1; fixtures may remain",
					tag.RowsAffected(), target.table)
				return
			}
		}

		if err := cleanup.Commit(ctx); err != nil {
			t.Errorf("CLEANUP FAILED to commit, fixtures remain: %v", err)
		}
	})

	// Ada can read it, so the fixture is real and the assertion below is not
	// vacuous.
	ada2, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's second transaction: %v", err)
	}
	defer ada2.Rollback(ctx)

	if _, err := ada2.ProcessingActivity(ctx, activityID); err != nil {
		t.Fatalf("Ada cannot read her own committed activity: %v", err)
	}

	// Bob cannot, and the answer is not-found rather than an error.
	bob, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer bob.Rollback(ctx)

	if _, err := bob.ProcessingActivity(ctx, activityID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("Bob reading Ada's activity: want ErrNoRows, got %v", err)
	}

	// And it is absent from his list rather than merely unreadable by id.
	page, err := bob.ProcessingActivities(ctx, "", 100)
	if err != nil {
		t.Fatalf("Bob listing his own register: %v", err)
	}
	for _, item := range page.Items {
		if item.ID == activityID {
			t.Fatalf("Ada's activity appears in Bob's register")
		}
	}
}
