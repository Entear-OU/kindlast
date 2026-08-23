package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/audit"
)

// The audit log's read path (ENT-223).
//
// Everything here seeds and asserts inside one transaction that is rolled back,
// for the reason records_test.go's header gives: an earlier suite in this
// package leaked 42 fixture rows when its cleanup silently did nothing.
//
// What is under test is this package's own code, not RLS. The isolation suite
// asserts the policies structurally over every table with an org_id. What
// cannot be asserted structurally is that the ordering is stable when two rows
// share a timestamp, which is the normal case here rather than an edge one: an
// act writes the decision and the record it created in ONE transaction, so both
// rows carry the same `occurred_at` exactly.

// seedAuditRows inserts audit entries through the app role.
//
// Through the app role deliberately, so a missing insert policy or grant fails
// here rather than in production. The policy binds `user_id` to the GUC user, so
// every row a tenant transaction can write names that tenant's own user; that
// constraint is the reason the actor-filter test below uses two transactions.
func seedAuditRows(
	t *testing.T, tx pgx.Tx, ctx context.Context,
	orgID, userID, role string, rows []auditFixture,
) []string {
	t.Helper()

	var ids []string
	for _, row := range rows {
		var id string
		err := tx.QueryRow(ctx, `
			insert into audit_log (
				org_id, user_id, approving_user_id, actor_role,
				action_type, target_table, target_id, before, after, occurred_at
			)
			values ($1, $2, $2, $3, $4, $5, null, $6::jsonb, $7::jsonb, $8)
			returning id::text
		`, orgID, userID, role, row.action, row.target,
			nullableJSON(row.before), nullableJSON(row.after), row.at).Scan(&id)
		if err != nil {
			t.Fatalf("seeding an audit row (%s): %v", row.action, err)
		}
		ids = append(ids, id)
	}
	return ids
}

type auditFixture struct {
	action string
	target string
	before string
	after  string
	at     time.Time
}

func nullableJSON(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var auditBase = time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)

// auditOrg gives one test an organisation of its own to write audit rows in.
//
// # WHY THESE TESTS CANNOT SHARE alphaOrg
//
// Every test in this file asserts "given exactly these rows, the read path does
// X": the cursor visits six rows once each, the export returns all seventy, the
// action-type list holds each value once. All of that is a statement about the
// whole of one organisation's audit log, so it is only true while this test is
// the only thing that has written to it.
//
// That used to hold by accident rather than by design. The rows each test seeds
// live in a transaction it rolls back, so tests could not see each other's, and
// nothing else in the package committed an audit row into `alphaOrg`. Both
// halves were load-bearing and only the first was written down.
//
// ENT-268 removed the second half. Accepting an invitation now writes an audit
// row, and `provision_test.go` has three tests that accept an invitation into
// `alphaOrg` and COMMIT, because what they are testing is behaviour across
// transactions. One committed row, and four tests here counted one too many.
//
// Bumping those four numbers would have been the wrong fix twice over: it would
// encode however many acceptances the package happens to commit today, and it
// would re-break the next time somebody audits a new action. The five tests
// that still passed were passing by luck, because they filter by action type or
// date range and `accept_invitation` did not match. So the fix is to give these
// tests the exclusive log they were always assuming, rather than to teach them
// which neighbours to expect.
//
// The organisation is created and dropped as the migrator, outside the tenant
// transaction, because `BeginTenant` verifies membership before the transaction
// is usable. Ada stays the actor: her having no `user_identities` row is the
// subject of one of the tests below, and only the tenancy scope moves.
func auditOrg(t *testing.T) string {
	t.Helper()

	id := uuid.NewString()
	pool := migratorPool(t)
	ctx := context.Background()

	name := "Audit Read Path " + id[:8]
	if _, err := pool.Exec(ctx, `
		insert into organisations (id, name, slug) values ($1, $2, org_slug($2))
	`, id, name); err != nil {
		t.Fatalf("creating the audit fixture organisation: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')
	`, id, adaUser); err != nil {
		t.Fatalf("seeding the audit fixture membership: %v", err)
	}

	t.Cleanup(func() {
		// audit_log first: it forbids UPDATE by trigger and says nothing about
		// DELETE, so the migrator can take fixture rows back out. Rows only
		// reach it here if a test committed, which none currently do, and
		// clearing it anyway is what stops this fixture becoming the next
		// accidental assumption.
		for _, statement := range []string{
			`delete from audit_log where org_id = $1`,
			`delete from memberships where org_id = $1`,
			`delete from organisations where id = $1`,
		} {
			if _, err := pool.Exec(context.Background(), statement, id); err != nil {
				t.Fatalf("cleaning up the audit fixture: %v", err)
			}
		}
	})

	return id
}

func TestTheAuditLogPagesNewestFirstAndTheCursorVisitsEveryRowOnce(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// FOUR ROWS SHARING ONE TIMESTAMP, which is the normal case rather than a
	// contrived one: an approval writes the decision and the record it created
	// in a single transaction, so both carry the transaction timestamp. Without
	// `id` as a tie-break the planner picks their relative order, and a keyset
	// cursor can then skip a row or return one forever.
	shared := auditBase.Add(time.Hour)
	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{action: "approve_finding", target: "findings", at: shared},
		{action: "create_ropa", target: "processing_activities", at: shared},
		{action: "approve_finding", target: "findings", at: shared},
		{action: "create_ai_system", target: "ai_systems", at: shared},
		{action: "reject_finding", target: "findings", at: auditBase},
		{action: "snooze_finding", target: "findings", at: auditBase.Add(2 * time.Hour)},
	})

	seen := map[string]int{}
	cursor := ""
	var order []time.Time
	for page := 0; page < 10; page++ {
		entries, next, err := tenant.AuditEntries(ctx, audit.Filter{}, cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, entry := range entries {
			seen[entry.ID]++
			order = append(order, entry.OccurredAt)
		}
		if next == "" {
			break
		}
		cursor = next
	}

	if len(seen) != 6 {
		t.Fatalf("walked the cursor and saw %d distinct rows, want 6", len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("row %s was returned %d times", id, count)
		}
	}

	// Newest first, and never going back up.
	for i := 1; i < len(order); i++ {
		if order[i].After(order[i-1]) {
			t.Fatalf("ordering went backwards at %d: %v then %v", i, order[i-1], order[i])
		}
	}
	if !order[0].Equal(auditBase.Add(2 * time.Hour)) {
		t.Fatalf("newest row is not first: %v", order[0])
	}
}

func TestTheUpperBoundIsExclusiveSoConsecutiveRangesDoNotOverlap(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	boundary := auditBase.Add(time.Hour)
	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{action: "reject_finding", target: "findings", at: auditBase},
		{action: "approve_finding", target: "findings", at: boundary},
		{action: "snooze_finding", target: "findings", at: boundary.Add(time.Hour)},
	})

	// An auditor pulling one period and then the next must not see the boundary
	// row twice. A duplicated decision in an audit file is a question nobody
	// wants to have to answer.
	first, _, err := tenant.AuditEntries(ctx,
		audit.Filter{Since: auditBase, Until: boundary}, "", 50)
	if err != nil {
		t.Fatalf("first range: %v", err)
	}
	second, _, err := tenant.AuditEntries(ctx,
		audit.Filter{Since: boundary, Until: boundary.Add(2 * time.Hour)}, "", 50)
	if err != nil {
		t.Fatalf("second range: %v", err)
	}

	if len(first) != 1 {
		t.Fatalf("first range returned %d rows, want 1", len(first))
	}
	if len(second) != 2 {
		t.Fatalf("second range returned %d rows, want 2", len(second))
	}
	for _, a := range first {
		for _, b := range second {
			if a.ID == b.ID {
				t.Fatalf("row %s appeared in both ranges", a.ID)
			}
		}
	}
}

func TestFreeTextSearchesForWhatWasTypedRatherThanAsAPattern(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{action: "approve_finding", target: "findings", at: auditBase},
		{action: "create_ropa", target: "processing_activities", at: auditBase},
	})

	// A bare `%` must not match everything. Somebody searching for a per cent
	// sign gets nothing, which is the honest answer, rather than the whole log,
	// which looks like the filter is broken.
	wildcard, _, err := tenant.AuditEntries(ctx, audit.Filter{Query: "%"}, "", 50)
	if err != nil {
		t.Fatalf("wildcard query: %v", err)
	}
	if len(wildcard) != 0 {
		t.Fatalf("a bare %% matched %d rows", len(wildcard))
	}

	// An underscore is a literal too, which matters because every action type
	// in this schema contains one.
	underscore, _, err := tenant.AuditEntries(ctx, audit.Filter{Query: "approve_finding"}, "", 50)
	if err != nil {
		t.Fatalf("underscore query: %v", err)
	}
	if len(underscore) != 1 {
		t.Fatalf("searching for approve_finding matched %d rows, want 1", len(underscore))
	}

	// And it still matches on the target table, not only the action.
	target, _, err := tenant.AuditEntries(ctx, audit.Filter{Query: "processing_act"}, "", 50)
	if err != nil {
		t.Fatalf("target query: %v", err)
	}
	if len(target) != 1 {
		t.Fatalf("searching a target table matched %d rows, want 1", len(target))
	}
}

func TestFreeTextDoesNotSearchThePayloads(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// `before` and `after` hold whatever the acted-on row contained, which for a
	// DSAR includes a data subject's name. Searching across them would make the
	// audit log a search engine over the personal data it exists to account for,
	// which is the wrong direction for a GDPR product to point its own tooling.
	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{
			action: "log_dsar", target: "dsars", at: auditBase,
			after: `{"subject_name":"Wilhelmina Nightingale"}`,
		},
	})

	found, _, err := tenant.AuditEntries(ctx, audit.Filter{Query: "Nightingale"}, "", 50)
	if err != nil {
		t.Fatalf("payload query: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("free text reached into the payload and matched %d rows", len(found))
	}

	// The row is still there and still readable; it is the SEARCH that stops at
	// the payload boundary, not the record.
	all, _, err := tenant.AuditEntries(ctx, audit.Filter{}, "", 50)
	if err != nil {
		t.Fatalf("unfiltered: %v", err)
	}
	if len(all) != 1 || all[0].AfterJSON == "" {
		t.Fatalf("the payload is missing from the record itself: %+v", all)
	}
}

func TestTheActionTypeFilterTakesSeveralValues(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{action: "approve_finding", target: "findings", at: auditBase},
		{action: "reject_finding", target: "findings", at: auditBase},
		{action: "snooze_finding", target: "findings", at: auditBase},
		{action: "create_ropa", target: "processing_activities", at: auditBase},
	})

	// "Every decision a human made" is three action types. Making a client issue
	// three requests and merge them would also break the ordering across them.
	filter := audit.Filter{ActionTypes: []string{
		"approve_finding", "reject_finding", "snooze_finding",
	}}
	entries, _, err := tenant.AuditEntries(ctx, filter, "", 50)
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d rows, want 3", len(entries))
	}
	for _, entry := range entries {
		if entry.ActionType == "create_ropa" {
			t.Fatal("an excluded action type came back")
		}
	}
}

func TestAnEntryKeepsItsActorEvenWithNoIdentityRecord(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{action: "approve_finding", target: "findings", at: auditBase},
	})

	entries, _, err := tenant.AuditEntries(ctx, audit.Filter{}, "", 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d rows, want 1", len(entries))
	}

	// The join is a LEFT join. An audit log that dropped rows when somebody was
	// offboarded would be defeatable by offboarding somebody.
	if entries[0].Actor.UserID != adaUser {
		t.Fatalf("actor id: got %q, want %q", entries[0].Actor.UserID, adaUser)
	}
	// The role AS RECORDED, not resolved now. This is why the column exists: a
	// page resolving it at render time would relabel a past act every time
	// somebody's role changed.
	if entries[0].Actor.Role != "owner" {
		t.Fatalf("actor role: got %q, want %q", entries[0].Actor.Role, "owner")
	}
	if entries[0].Actor.Kind != audit.ActorHuman {
		t.Fatalf("actor kind: got %q", entries[0].Actor.Kind)
	}
}

func TestTheExportIsNotPagedAndReportsWhetherItWasCut(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// More rows than a default page, so an export that quietly reused the list's
	// paging fails here. That is the specific bug this test exists for: an export
	// that stops at a page boundary is a valid CSV that simply ends, and the
	// auditor cannot see the truncation.
	var rows []auditFixture
	for i := 0; i < audit.DefaultPageSize+10; i++ {
		rows = append(rows, auditFixture{
			action: "approve_finding",
			target: "findings",
			at:     auditBase.Add(time.Duration(i) * time.Minute),
		})
	}
	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", rows)

	entries, truncated, err := tenant.AuditEntriesForExport(ctx, audit.Filter{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(entries) != len(rows) {
		t.Fatalf("export returned %d rows, want %d", len(entries), len(rows))
	}
	// Well under the cap, so this must not claim truncation. A false positive
	// would send an auditor narrowing a range that was already complete.
	if truncated {
		t.Fatal("a complete export reported itself truncated")
	}
}

func TestTheActionTypeListOffersOnlyValuesThatExist(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedAuditRows(t, tenant.Tx(), ctx, org, adaUser, "owner", []auditFixture{
		{action: "approve_finding", target: "findings", at: auditBase},
		{action: "approve_finding", target: "findings", at: auditBase},
		{action: "create_ropa", target: "processing_activities", at: auditBase},
	})

	types, err := tenant.AuditActionTypes(ctx)
	if err != nil {
		t.Fatalf("action types: %v", err)
	}

	// Distinct, and drawn from the rows rather than from a constant list. A
	// dropdown offering values the schema could theoretically hold sends a
	// person hunting for rows that were never written.
	counts := map[string]int{}
	for _, actionType := range types {
		counts[actionType]++
	}
	if counts["approve_finding"] != 1 {
		t.Fatalf("approve_finding appears %d times: %v", counts["approve_finding"], types)
	}
	if counts["create_ropa"] != 1 {
		t.Fatalf("create_ropa missing: %v", types)
	}
}

func TestAPageTokenFromTheFeedIsRefusedRatherThanMisread(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	org := auditOrg(t)

	tenant, err := store.BeginTenant(ctx, adaUser, org)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	if _, _, err := tenant.AuditEntries(ctx, audit.Filter{}, "not-a-cursor", 50); err == nil {
		t.Fatal("a malformed cursor was accepted")
	}
}
