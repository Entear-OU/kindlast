package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Tenancy proved against a real Postgres, as two different users, per §13.3
// and §14.1.
//
// Not a mock, and the reason is the whole point of ENT-192. A mocked
// membership check asserts that this package's own `if` statements work.
// Isolation is enforced by policies in the database, under a role that owns
// nothing and cannot bypass them, and the only way to know those policies are
// on is to ask the database as that role. The failure this guards against is
// silent: no error, no warning, every test green, and tenant isolation simply
// absent.
//
// Needs the compose stack:
//
//	docker compose -f deploy/compose.yaml up -d

const testIssuer = "http://localhost:8300"

func testStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("PG_APP_URL")
	if dsn == "" {
		dsn = "postgres://kindlast_app:app-dev-password@127.0.0.1:5433/kindlast"
	}

	store, err := New(t.Context(), dsn, testIssuer)
	if err != nil {
		// Skips on a laptop, fails in CI. A self-skipping suite that reports
		// green while testing nothing is how a security boundary stops being
		// covered without anyone deciding to stop covering it.
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", dsn, err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", dsn, err)
	}
	t.Cleanup(store.Close)
	return store
}

// The seeded fixtures: Ada owns Alpha, Bob owns Beta, and neither belongs to
// the other's organisation.
const (
	alphaOrg = "a0000000-0000-4000-8000-000000000001"
	adaUser  = "a0000000-0000-4000-8000-0000000000aa"
	betaOrg  = "b0000000-0000-4000-8000-000000000001"
	bobUser  = "b0000000-0000-4000-8000-0000000000ba"
)

// The assertion ENT-192 said to write before porting a single policy, now made
// through the code path that will actually serve requests.
//
// If this ever fails, nothing else in this package is worth reading: the
// application is connecting as a role that bypasses row level security, and
// every policy in the schema is a no-op.
func TestTheApplicationRoleCannotBypassRLS(t *testing.T) {
	store := testStore(t)

	tenant, err := store.BeginTenant(t.Context(), adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(t.Context())

	var isSuperuser string
	if err := tenant.Tx().QueryRow(t.Context(), "select current_setting('is_superuser')").Scan(&isSuperuser); err != nil {
		t.Fatalf("reading is_superuser: %v", err)
	}
	if isSuperuser != "off" {
		t.Fatalf("is_superuser = %q, want off; superusers bypass RLS entirely and every policy is a no-op", isSuperuser)
	}

	var bypassRLS bool
	if err := tenant.Tx().QueryRow(t.Context(),
		"select rolbypassrls from pg_roles where rolname = current_user").Scan(&bypassRLS); err != nil {
		t.Fatalf("reading rolbypassrls: %v", err)
	}
	if bypassRLS {
		t.Fatal("the application role has BYPASSRLS; policies are enforced for everyone except the one role that matters")
	}
}

// A cross-organisation read returns zero rows rather than an error, which is
// the §0.5 rule: tenancy is answered by the absence of rows, never by a
// refusal. A refusal would leak that the row exists.
func TestACrossOrganisationReadReturnsZeroRowsRatherThanAnError(t *testing.T) {
	store := testStore(t)

	// Ada, acting in her own organisation, asks for Bob's.
	ada, err := store.BeginTenant(t.Context(), adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer ada.Rollback(t.Context())

	var visibleToAda int
	err = ada.Tx().QueryRow(t.Context(),
		"select count(*) from subscriptions where org_id = $1", betaOrg).Scan(&visibleToAda)
	if err != nil {
		t.Fatalf("a cross-organisation read errored instead of returning nothing: %v", err)
	}
	if visibleToAda != 0 {
		t.Fatalf("Ada can see %d of Beta's subscription rows, want 0", visibleToAda)
	}

	// The same query is not vacuous: Bob sees the row Ada cannot. Without
	// this half, a policy that hid everything from everyone would pass.
	bob, err := store.BeginTenant(t.Context(), bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer bob.Rollback(t.Context())

	var visibleToBob int
	err = bob.Tx().QueryRow(t.Context(),
		"select count(*) from subscriptions where org_id = $1", betaOrg).Scan(&visibleToBob)
	if err != nil {
		t.Fatalf("Bob reading his own organisation: %v", err)
	}
	if visibleToBob != 1 {
		t.Fatalf("Bob sees %d of his own subscription rows, want 1; "+
			"the isolation above may be hiding everything from everyone", visibleToBob)
	}
}

// Asking to act inside someone else's organisation is refused before any query
// runs, rather than quietly producing an empty view of it.
func TestActingInAnotherOrganisationIsRefused(t *testing.T) {
	store := testStore(t)

	_, err := store.BeginTenant(t.Context(), adaUser, betaOrg)
	if !errors.Is(err, ErrNotAMember) {
		t.Fatalf("error = %v, want ErrNotAMember; Ada was allowed to act inside Bob's organisation", err)
	}
}

// With no organisation header, the caller's own organisation is resolved.
func TestTheDefaultOrganisationIsResolvedWhenNoHeaderIsSent(t *testing.T) {
	store := testStore(t)

	tenant, err := store.BeginTenant(t.Context(), adaUser, "")
	if err != nil {
		t.Fatalf("resolving the default organisation: %v", err)
	}
	defer tenant.Rollback(t.Context())

	if tenant.OrgID() != alphaOrg {
		t.Fatalf("default organisation = %q, want %q", tenant.OrgID(), alphaOrg)
	}
	if tenant.Role() != "owner" {
		t.Fatalf("role = %q, want owner", tenant.Role())
	}
}

// A verified subject with no membership anywhere is not an error: it is what
// arrives on first sign-in, and ENT-196 provisions from exactly this state.
//
// The GUC still has to hold a real uuid, because every policy casts it. This
// asserts the state is usable rather than merely reachable.
func TestASubjectWithNoMembershipGetsAUsableEmptyTenancy(t *testing.T) {
	store := testStore(t)

	tenant, err := store.BeginTenant(t.Context(), "c0000000-0000-4000-8000-00000000000c", "")
	if err != nil {
		t.Fatalf("a brand-new subject was refused: %v", err)
	}
	defer tenant.Rollback(t.Context())

	if tenant.OrgID() != noOrganisation {
		t.Fatalf("org = %q, want the nil uuid", tenant.OrgID())
	}

	// The query must run and return nothing. If app.current_org_id were unset
	// or empty, this would raise instead, and the caller would see a server
	// error where they should see an empty console.
	var visible int
	if err := tenant.Tx().QueryRow(t.Context(), "select count(*) from subscriptions").Scan(&visible); err != nil {
		t.Fatalf("reading with no active organisation errored: %v\n"+
			"every policy casts current_setting('app.current_org_id')::uuid, so the GUC must always hold a real uuid", err)
	}
	if visible != 0 {
		t.Fatalf("a subject with no membership can see %d rows, want 0", visible)
	}
}

// A Zitadel subject is a snowflake integer, not a uuid, and it must still
// resolve to a stable identity rather than crashing on the cast.
func TestANonUUIDSubjectIsAccepted(t *testing.T) {
	store := testStore(t)

	tenant, err := store.BeginTenant(t.Context(), "386089961457188867", "")
	if err != nil {
		t.Fatalf("a Zitadel-shaped subject was refused: %v", err)
	}
	defer tenant.Rollback(t.Context())

	if tenant.UserID() == "" {
		t.Fatal("no user id was derived for a non-uuid subject")
	}
	if tenant.OrgID() != noOrganisation {
		t.Fatalf("org = %q, want the nil uuid for a subject with no membership", tenant.OrgID())
	}
}

// The GUCs are transaction-scoped, and that is what makes them safe under a
// connection pool.
//
// Set at session level instead, request B would inherit request A's
// organisation whenever it borrowed the same pooled connection, and would read
// A's rows. The loop runs more times than the pool holds connections, so every
// connection is checked rather than whichever one came back first.
func TestTheTenancyGUCsDoNotSurviveTheTransaction(t *testing.T) {
	store := testStore(t)

	tenant, err := store.BeginTenant(t.Context(), adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}

	for i := range 20 {
		var leaked string
		err := store.pool.QueryRow(context.Background(),
			"select coalesce(current_setting('app.current_org_id', true), '')").Scan(&leaked)
		if err != nil {
			t.Fatalf("checking for a leaked setting: %v", err)
		}
		if leaked != "" {
			t.Fatalf("connection %d still carries app.current_org_id = %q after the transaction committed; "+
				"the next request on this connection would act inside that organisation", i, leaked)
		}
	}
}
