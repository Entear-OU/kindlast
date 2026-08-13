package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Provisioning, against the real database, because every interesting property
// here is a property of the database rather than of this code: a partial
// unique index, a primary key, and what two transactions do to each other.

func migratorDSN() string {
	if dsn := os.Getenv("PG_MIGRATOR_URL"); dsn != "" {
		return dsn
	}
	return "postgres://kindlast_migrator:migrator-dev-password@127.0.0.1:5433/kindlast"
}

// migratorPool opens a pool as the migrator, which bypasses RLS, for the
// fixture work that has to happen outside the policies under test.
//
// It carries the same skip-or-fail gate as testStore, and that is not
// symmetry for its own sake. A test that reached a migrator connection without
// passing through the gate would fail hard in the `go` CI job, where there is
// no stack at all and every other test skips. That is exactly what happened:
// one test called a fixture helper before testStore and turned an absent
// database into a red build.
func migratorPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), migratorDSN())
	if err == nil {
		err = pool.Ping(context.Background())
	}
	if err != nil {
		if pool != nil {
			pool.Close()
		}
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", migratorDSN(), err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", migratorDSN(), err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// cleanup removes everything a test subject created, as the migrator, which
// bypasses RLS.
//
// Before and after, not only after: the database outlives the test, and one
// interrupted run would otherwise leave a personal organisation behind and
// make every later run fail on an assertion about counts rather than the thing
// it was testing.
func cleanup(t *testing.T, subjects ...string) {
	t.Helper()

	pool := migratorPool(t)

	for _, s := range subjects {
		userID, err := subject.UUID(testIssuer, s)
		if err != nil {
			t.Fatalf("deriving the user id: %v", err)
		}
		for _, statement := range []string{
			`delete from memberships where org_id in (select id from organisations where personal_owner_id = $1)`,
			`delete from organisations where personal_owner_id = $1`,
			`delete from memberships where user_id = $1`,
			`delete from user_identities where user_id = $1`,
		} {
			if _, err := pool.Exec(context.Background(), statement, userID); err != nil {
				t.Fatalf("cleaning up: %v", err)
			}
		}
	}
}

// provision runs exactly what the handler runs, in one transaction, so the
// concurrency test exercises the real sequence rather than an approximation of
// it.
func provision(ctx context.Context, store *Store, s org.Subject) error {
	tenant, err := store.BeginTenant(ctx, s.Subject, "")
	if err != nil {
		return err
	}
	defer tenant.Rollback(ctx)

	if err := tenant.RecordIdentity(ctx, s); err != nil {
		return err
	}

	memberships, err := tenant.Memberships(ctx)
	if err != nil {
		return err
	}

	if plan := org.PlanFor(s, memberships); plan.CreatePersonalOrganisation {
		if _, err := tenant.ProvisionPersonalOrganisation(ctx, plan); err != nil {
			return err
		}
	}
	return tenant.Commit(ctx)
}

func personalOrgCount(t *testing.T, store *Store, subjectClaim string) int {
	t.Helper()

	userID, err := subject.UUID(testIssuer, subjectClaim)
	if err != nil {
		t.Fatalf("deriving the user id: %v", err)
	}

	pool := migratorPool(t)

	var count int
	err = pool.QueryRow(context.Background(),
		`select count(*) from organisations where personal_owner_id = $1`, userID).Scan(&count)
	if err != nil {
		t.Fatalf("counting personal organisations: %v", err)
	}
	return count
}

func TestFirstArrivalGetsAPersonalOrganisationAndOwnerMembership(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-first-arrival"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	s := org.Subject{
		Issuer: testIssuer, Subject: claim,
		Email: "ada.lovelace@example.com", DisplayName: "Ada Lovelace",
	}
	if err := provision(t.Context(), store, s); err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	tenant, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	defer tenant.Rollback(t.Context())

	memberships, err := tenant.Memberships(t.Context())
	if err != nil {
		t.Fatalf("reading memberships: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("memberships = %d, want exactly 1", len(memberships))
	}
	if memberships[0].Role != org.RoleOwner {
		t.Fatalf("role = %q, want owner: the organisation is theirs", memberships[0].Role)
	}
	if memberships[0].OrgName != "Ada Lovelace" {
		t.Fatalf("organisation name = %q, want it derived from the display name", memberships[0].OrgName)
	}

	// The reverse mapping, without which this uuid answers to nobody during an
	// incident and a subject access request cannot be honoured.
	var issuer, storedSubject string
	err = tenant.Tx().QueryRow(t.Context(),
		`select issuer, subject from user_identities where user_id = $1`, tenant.UserID()).
		Scan(&issuer, &storedSubject)
	if err != nil {
		t.Fatalf("reading the identity row: %v", err)
	}
	if issuer != testIssuer || storedSubject != claim {
		t.Fatalf("identity = (%q, %q), want (%q, %q)", issuer, storedSubject, testIssuer, claim)
	}
}

// The point of the issue.
//
// Two tabs, two requests, the same unseen subject, and both decide to create a
// personal organisation. A single-threaded test passes forever while this bug
// ships, which is why it is written this way and why the assertion is a count
// rather than an absence of errors.
//
// The mechanism that settles it is the partial unique index on
// organisations(personal_owner_id) added in 00003. `on conflict do nothing`
// against memberships alone would not: each transaction inserts a different
// organisation id, so neither conflicts.
func TestConcurrentFirstRequestsCreateExactlyOneOrganisation(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-two-tabs"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	s := org.Subject{
		Issuer: testIssuer, Subject: claim,
		Email: "concurrent@example.com", DisplayName: "Concurrent Arrival",
	}

	const tabs = 8
	var wg sync.WaitGroup
	errs := make([]error, tabs)

	// A barrier, so the requests genuinely overlap rather than queueing behind
	// each other's setup. Without it the test can pass by accident on a slow
	// machine, which is the failure mode it exists to prevent.
	start := make(chan struct{})

	for i := range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = provision(context.Background(), store, s)
		}()
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("tab %d failed: %v", i, err)
		}
	}

	if count := personalOrgCount(t, store, claim); count != 1 {
		t.Fatalf("%d personal organisations exist after %d concurrent first requests, want exactly 1", count, tabs)
	}

	tenant, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	defer tenant.Rollback(t.Context())

	memberships, err := tenant.Memberships(t.Context())
	if err != nil {
		t.Fatalf("reading memberships: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("memberships = %d, want exactly 1; the subject owns more than one organisation", len(memberships))
	}
}

// The partial unique index, tested directly rather than only through the
// concurrency test above.
//
// This matters because of something the breakage runs turned up: in the real
// path, RecordIdentity serialises concurrent first requests, since they all
// upsert the same user_identities primary key. The concurrency test therefore
// passes even with this index dropped, which means it proves the end-to-end
// property without proving that the constraint behind it works.
//
// The acceptance criterion is about retries, so it deserves its own assertion.
// Two separate transactions, no concurrency involved, and the second must not
// produce a second organisation.
func TestASubjectCannotOwnTwoPersonalOrganisations(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-retry"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	s := org.Subject{Issuer: testIssuer, Subject: claim, Email: "retry@example.com"}
	plan := org.PlanFor(s, nil)

	first, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	created, err := first.ProvisionPersonalOrganisation(t.Context(), plan)
	if err != nil {
		t.Fatalf("first provisioning: %v", err)
	}
	if !created {
		t.Fatal("the first call reported creating nothing")
	}
	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}

	// A retry, the way one arrives in production: the caller believes it has
	// created nothing, and tries again.
	second, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer second.Rollback(t.Context())

	created, err = second.ProvisionPersonalOrganisation(t.Context(), plan)
	if err != nil {
		t.Fatalf("the retry errored instead of being absorbed: %v", err)
	}
	if created {
		t.Fatal("the retry reported creating a second personal organisation")
	}
	if err := second.Commit(t.Context()); err != nil {
		t.Fatalf("committing the retry: %v", err)
	}

	if count := personalOrgCount(t, store, claim); count != 1 {
		t.Fatalf("%d personal organisations after a retry, want 1", count)
	}
}

// Idempotent on the subject claim, which is what makes just-in-time
// provisioning safe to run on every call rather than only the first.
func TestProvisioningTwiceChangesNothing(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-idempotent"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	s := org.Subject{Issuer: testIssuer, Subject: claim, Email: "twice@example.com"}

	for attempt := 1; attempt <= 3; attempt++ {
		if err := provision(t.Context(), store, s); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}

	if count := personalOrgCount(t, store, claim); count != 1 {
		t.Fatalf("%d personal organisations after three calls, want 1", count)
	}
}

// An invited user joins the organisation they were invited to and does NOT
// also get a personal one. The ordering is the whole point (§1.8): accept runs
// before the first GetCurrentUser, or provisioning sees a subject with no
// membership and gives them one they never asked for.
func TestAnInvitedUserJoinsTheExistingOrganisationAndGetsNoPersonalOne(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-invited"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	token := fmt.Sprintf("invitation-token-%d", time.Now().UnixNano())
	createInvitation(t, alphaOrg, "invited@example.com", org.RoleMember, token, time.Hour)
	t.Cleanup(func() { deleteInvitation(t, token) })

	// Accept first, which is the ordering that matters.
	tenant, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	orgID, orgName, role, err := tenant.AcceptInvitation(t.Context(), token)
	if err != nil {
		t.Fatalf("accepting: %v", err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}

	if orgID != alphaOrg {
		t.Fatalf("joined %q, want %q", orgID, alphaOrg)
	}
	if role != org.RoleMember {
		t.Fatalf("role = %q, want the role the invitation granted", role)
	}
	if orgName == "" {
		t.Fatal("no organisation name came back")
	}

	// Now the first /me. Provisioning must find a membership and create
	// nothing.
	s := org.Subject{Issuer: testIssuer, Subject: claim, Email: "invited@example.com"}
	if err := provision(t.Context(), store, s); err != nil {
		t.Fatalf("provisioning after accept: %v", err)
	}

	if count := personalOrgCount(t, store, claim); count != 0 {
		t.Fatalf("the invited user also got %d personal organisation(s), want 0", count)
	}

	after, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	defer after.Rollback(t.Context())

	memberships, err := after.Memberships(t.Context())
	if err != nil {
		t.Fatalf("reading memberships: %v", err)
	}
	if len(memberships) != 1 || memberships[0].OrgID != alphaOrg {
		t.Fatalf("memberships = %v, want exactly the invited organisation", memberships)
	}
}

// Accepting the same invitation twice joins once, and the second attempt is
// refused rather than silently succeeding.
func TestAnInvitationCannotBeUsedTwice(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-reuse"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	token := fmt.Sprintf("invitation-reuse-%d", time.Now().UnixNano())
	createInvitation(t, alphaOrg, "reuse@example.com", org.RoleViewer, token, time.Hour)
	t.Cleanup(func() { deleteInvitation(t, token) })

	first, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	if _, _, _, err := first.AcceptInvitation(t.Context(), token); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}

	second, err := store.BeginTenant(t.Context(), claim, "")
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer second.Rollback(t.Context())

	if _, _, _, err := second.AcceptInvitation(t.Context(), token); err == nil {
		t.Fatal("an already-accepted invitation was accepted again")
	}
}

// Expired, already used and never existed all answer the same way, so the
// endpoint cannot be used to discover which tokens are real.
func TestAnExpiredOrUnknownInvitationIsRefused(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-expired"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	expired := fmt.Sprintf("invitation-expired-%d", time.Now().UnixNano())
	createInvitation(t, alphaOrg, "expired@example.com", org.RoleMember, expired, -time.Hour)
	t.Cleanup(func() { deleteInvitation(t, expired) })

	for _, testCase := range []struct{ name, token string }{
		{"expired", expired},
		{"never existed", "no-such-invitation-token"},
		{"empty", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tenant, err := store.BeginTenant(t.Context(), claim, "")
			if err != nil {
				t.Fatalf("beginning: %v", err)
			}
			defer tenant.Rollback(t.Context())

			if _, _, _, err := tenant.AcceptInvitation(t.Context(), testCase.token); err == nil {
				t.Fatalf("%s invitation was accepted", testCase.name)
			}
		})
	}
}

// The stored token is a hash, so a database dump does not yield a working
// invitation.
func TestTheInvitationTokenIsNotStoredInTheClear(t *testing.T) {
	pool := migratorPool(t)

	token := fmt.Sprintf("invitation-secrecy-%d", time.Now().UnixNano())
	createInvitation(t, alphaOrg, "secrecy@example.com", org.RoleMember, token, time.Hour)
	t.Cleanup(func() { deleteInvitation(t, token) })

	var found int
	err := pool.QueryRow(t.Context(),
		`select count(*) from invitations where token_hash = $1`, token).Scan(&found)
	if err != nil {
		t.Fatalf("querying: %v", err)
	}
	if found != 0 {
		t.Fatal("the raw token is stored in the database; a dump would yield working invitations")
	}
}

// createInvitation writes one as the migrator, standing in for the owner-only
// invite endpoint that is build-order step 2.
func createInvitation(t *testing.T, orgID, email, role, token string, validFor time.Duration) {
	t.Helper()

	pool := migratorPool(t)

	_, err := pool.Exec(context.Background(), `
		insert into invitations (org_id, email, role, token_hash, expires_at)
		values ($1, $2, $3, $4, now() + $5::interval)
	`, orgID, email, role, HashInvitationToken(token), fmt.Sprintf("%d seconds", int(validFor.Seconds())))
	if err != nil {
		t.Fatalf("creating the invitation: %v", err)
	}
}

func deleteInvitation(t *testing.T, token string) {
	t.Helper()

	pool := migratorPool(t)

	if _, err := pool.Exec(context.Background(),
		`delete from invitations where token_hash = $1`, HashInvitationToken(token)); err != nil {
		t.Fatalf("deleting the invitation: %v", err)
	}
}
