package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The DSAR trail (ENT-226).
//
// The database suite in `db/tests/dsar-trail.test.ts` owns the boundary: RLS,
// the composite foreign key, the append-only trigger, the grants. These tests
// own what Go decided instead, which is the other half of §14.5's split:
//
//   - the future-occurrence refusal, which is a rule and not a constraint,
//     because `now()` is not immutable and a check cannot see it
//   - the action vocabulary, refused with a sentence rather than a constraint
//     name
//   - provenance validated against what the caller can see, which the foreign
//     key alone cannot do because referential checks do not run under row
//     security
//   - the audit row, in the same transaction as the entry
//
// Everything runs inside a transaction that is rolled back, so none of it
// survives. `refused` (records_write_test.go) wraps a call expected to fail, so
// a raise inside the database does not poison the rest of the test.

// seedDsar logs one request directly, without going through LogDsar, so a
// failure in these tests points at the trail rather than at the register.
func seedDsar(t *testing.T, tenant *Tenant, ctx context.Context) string {
	t.Helper()

	received := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	var id string
	err := tenant.Tx().QueryRow(ctx, `
		insert into dsars (org_id, created_by, subject_name, request_type,
		                   status, received_at, response_due_at)
		values ($1, $2, 'A Data Subject', 'access', 'open', $3, $4)
		returning id::text
	`, alphaOrg, adaUser, received, received.AddDate(0, 1, 0)).Scan(&id)
	if err != nil {
		t.Fatalf("seeding a dsar: %v", err)
	}
	return id
}

func TestATrailEntryIsAppendedWithItsAuditRow(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	searched := time.Date(2026, time.August, 5, 9, 0, 0, 0, time.UTC)
	entry, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
		Source:     "  Salesforce  ",
		Action:     records.TrailFound,
		Detail:     "Contact record, opened 2019",
		OccurredAt: searched,
	})
	if err != nil {
		t.Fatalf("appending a trail entry: %v", err)
	}

	// Trimmed on the way in, because "Salesforce" and "Salesforce " would
	// otherwise be two stores in a count of how many were searched.
	if entry.Source != "Salesforce" {
		t.Fatalf("source %q, want it trimmed to Salesforce", entry.Source)
	}
	if !entry.OccurredAt.Equal(searched) {
		t.Fatalf("occurred_at %s, want %s", entry.OccurredAt, searched)
	}
	// The two timestamps are different facts and the row keeps both. A trail
	// that only knew when it was typed up could not show that the search
	// happened inside the statutory month.
	if !entry.RecordedAt.After(entry.OccurredAt) {
		t.Fatalf("recorded_at %s is not after occurred_at %s", entry.RecordedAt, entry.OccurredAt)
	}
	// Taken from the session, never from the request.
	if entry.CreatedBy != adaUser {
		t.Fatalf("created_by %q, want %q", entry.CreatedBy, adaUser)
	}

	// An access to a data subject's data in service of a DSAR is still an
	// access, and the audit row lands in the same transaction as the entry
	// rather than as a second statement that can fail on its own.
	var audited int
	if err := tenant.Tx().QueryRow(ctx, `
		select count(*) from audit_log
		 where target_table = 'dsar_trail_entries'
		   and target_id = $1
		   and action_type = 'add_dsar_trail_entry'
		   and user_id = $2
	`, entry.ID, adaUser).Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 1 {
		t.Fatalf("got %d audit rows for the entry, want 1", audited)
	}
}

// The trail is what makes `responded_at` checkable, so the register has to
// carry how much of it there is. A request with an empty trail and a response
// date is an assertion with nothing behind it, and that is a state the console
// shows rather than hides.
func TestTheRegisterCarriesHowManyTrailEntriesStandBehindARequest(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	before, err := tenant.Dsar(ctx, dsarID)
	if err != nil {
		t.Fatalf("reading the request: %v", err)
	}
	if before.TrailEntryCount != 0 {
		t.Fatalf("a fresh request has %d trail entries, want 0", before.TrailEntryCount)
	}

	for _, action := range []string{records.TrailSearched, records.TrailNoneFound} {
		if _, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
			Source: "HR system", Action: action,
		}); err != nil {
			t.Fatalf("appending %s: %v", action, err)
		}
	}

	after, err := tenant.Dsar(ctx, dsarID)
	if err != nil {
		t.Fatalf("re-reading the request: %v", err)
	}
	if after.TrailEntryCount != 2 {
		t.Fatalf("got %d trail entries, want 2", after.TrailEntryCount)
	}

	// And on the list, not only the read: that is where a handler decides
	// whether a request is ready to be marked answered.
	page, err := tenant.Dsars(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("listing the register: %v", err)
	}
	var found bool
	for _, d := range page.Items {
		if d.ID == dsarID {
			found = true
			if d.TrailEntryCount != 2 {
				t.Fatalf("the list reports %d trail entries, want 2", d.TrailEntryCount)
			}
		}
	}
	if !found {
		t.Fatal("the seeded request is not in the register listing")
	}
}

// Chronological, and by when the work happened rather than by when it was
// written up. A trail entered out of order is the normal case: somebody
// remembers on Friday that they also searched the archive on Tuesday.
func TestTheTrailReadsForwardByWhenTheWorkHappened(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	day := func(d int) time.Time {
		return time.Date(2026, time.August, d, 9, 0, 0, 0, time.UTC)
	}
	// Written up in one order, done in another. An implementation that ordered
	// by `recorded_at` would return the insertion order and pass a weaker test.
	for _, seed := range []struct {
		source string
		on     time.Time
	}{
		{"CRM", day(6)},
		{"Archive", day(2)},
		{"HR system", day(4)},
	} {
		if _, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
			Source: seed.source, Action: records.TrailSearched, OccurredAt: seed.on,
		}); err != nil {
			t.Fatalf("appending %s: %v", seed.source, err)
		}
	}

	page, err := tenant.DsarTrail(ctx, dsarID, "", 10)
	if err != nil {
		t.Fatalf("reading the trail: %v", err)
	}
	want := []string{"Archive", "HR system", "CRM"}
	if len(page.Items) != len(want) {
		t.Fatalf("got %d entries, want %d", len(page.Items), len(want))
	}
	for i := range want {
		if page.Items[i].Source != want[i] {
			t.Fatalf("entry %d is %q, want %q", i, page.Items[i].Source, want[i])
		}
	}
}

// The cursor ascends, like the DSAR log's and unlike the other two registers'.
// A cursor built with the wrong comparison returns the same page forever or
// skips the rest, and both look like a working endpoint until there is more
// than one page.
func TestTheTrailCursorAscendsAndVisitsEveryEntryOnce(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	const total = 5
	for i := range total {
		if _, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
			Source: "Store", Action: records.TrailSearched,
			OccurredAt: time.Date(2026, time.August, 2+i, 9, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("appending entry %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for range total {
		page, err := tenant.DsarTrail(ctx, dsarID, cursor, 2)
		if err != nil {
			t.Fatalf("paging the trail: %v", err)
		}
		for _, e := range page.Items {
			if seen[e.ID] {
				t.Fatalf("entry %s came back twice", e.ID)
			}
			seen[e.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != total {
		t.Fatalf("paged over %d entries, want %d", len(seen), total)
	}
}

// A trail for a request the caller cannot see is not an empty trail, and the
// difference matters: a console renders "no such request" and "nothing recorded
// yet" completely differently.
func TestATrailForAnotherOrganisationsRequestIsNotFound(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	ada, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer ada.Rollback(ctx)

	// Bob's request, in Beta, committed so Ada's separate transaction can
	// attempt to reach it. Seeded and removed by the same test rather than left
	// behind.
	bob, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	var betaDsar string
	received := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	if err := bob.Tx().QueryRow(ctx, `
		insert into dsars (org_id, created_by, subject_name, request_type,
		                   status, received_at, response_due_at)
		values ($1, $2, 'Beta Subject', 'access', 'open', $3, $4)
		returning id::text
	`, betaOrg, bobUser, received, received.AddDate(0, 1, 0)).Scan(&betaDsar); err != nil {
		t.Fatalf("seeding Beta's request: %v", err)
	}
	if err := bob.Commit(ctx); err != nil {
		t.Fatalf("committing Beta's request: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := store.BeginTenant(context.Background(), bobUser, betaOrg)
		if err != nil {
			t.Fatalf("cleanup transaction: %v", err)
		}
		defer cleanup.Rollback(context.Background())
		if _, err := cleanup.Tx().Exec(context.Background(),
			`delete from dsars where id = $1`, betaDsar); err != nil {
			t.Fatalf("removing Beta's request: %v", err)
		}
		if err := cleanup.Commit(context.Background()); err != nil {
			t.Fatalf("committing the cleanup: %v", err)
		}
	})

	if _, err := ada.DsarTrail(ctx, betaDsar, "", 10); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("reading Beta's trail as Ada: got %v, want pgx.ErrNoRows", err)
	}

	// And the same for the write, which is the half that would otherwise file
	// Alpha's search results against Beta's request.
	err = refused(t, ada, ctx, func() error {
		_, err := ada.AddTrailEntry(ctx, betaDsar, records.TrailEntry{
			Source: "Alpha CRM", Action: records.TrailSearched,
		})
		return err
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("appending to Beta's trail as Ada: got %v, want pgx.ErrNoRows", err)
	}
}

// Refused rather than clamped, exactly as a future receipt date is. The point of
// `occurred_at` is to record when a search actually happened, and a value the
// database quietly rewrote is a worse record than a refusal somebody can fix.
func TestAnEntryCannotHaveHappenedInTheFuture(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	err = refused(t, tenant, ctx, func() error {
		_, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
			Source: "CRM", Action: records.TrailSearched,
			OccurredAt: time.Now().Add(48 * time.Hour),
		})
		return err
	})
	if !errors.Is(err, ErrFutureOccurrence) {
		t.Fatalf("got %v, want ErrFutureOccurrence", err)
	}
}

// The action vocabulary is a check constraint AND a Go check. The constraint is
// what makes it true whoever writes; this is what makes the refusal a sentence
// somebody can act on rather than a constraint name.
func TestAnActionOutsideTheVocabularyIsRefusedBeforeItReachesTheConstraint(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	// No savepoint, deliberately: if this reached the database the transaction
	// would be poisoned and every later statement in this test would fail with
	// 25P02. That it does not is the assertion.
	if _, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
		Source: "CRM", Action: "had-a-look",
	}); err == nil {
		t.Fatal("an unknown action was accepted")
	}

	if _, err := tenant.Dsar(ctx, dsarID); err != nil {
		t.Fatalf("the transaction is unusable after the refusal: %v", err)
	}
}

// Provenance is checked against what the caller can see, not merely against the
// table. A foreign key would accept a run in another organisation, because
// referential checks do not run under row security, and an entry claiming to
// come from somebody else's agent run is provenance nobody can verify.
func TestAgentRunProvenanceMustResolveInTheCallersOwnOrganisation(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	dsarID := seedDsar(t, tenant, ctx)

	for _, claimed := range []struct {
		name string
		id   string
	}{
		{"not a uuid at all", "not-a-uuid"},
		{"a well-formed id nothing answers to", "c0000000-0000-4000-8000-00000000ffff"},
	} {
		t.Run(claimed.name, func(t *testing.T) {
			err := refused(t, tenant, ctx, func() error {
				_, err := tenant.AddTrailEntry(ctx, dsarID, records.TrailEntry{
					Source: "CRM", Action: records.TrailSearched,
					AgentRunID: claimed.id,
				})
				return err
			})
			if !errors.Is(err, ErrUnknownAgentRun) {
				t.Fatalf("got %v, want ErrUnknownAgentRun", err)
			}
		})
	}
}
