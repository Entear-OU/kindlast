package interceptor_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	dashboardservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/dashboard"
	findingsservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
)

// ENT-203's acceptance criteria through the whole chain, with real signed
// tokens, rather than at the store layer.
//
// The database suite proves the policies and the audit writes. These prove what
// a client experiences: a token over HTTP, through authentication, revocation,
// scope and tenancy, into the handler, and back as a Connect code. The
// distinction carries real weight on the act path, because RLS refusing a write
// and a handler refusing a request look nothing alike from outside. One is a
// success that changed nothing; the other says why.
//
// Note what these tests do NOT need: ENT-221. No human token in the deployed
// stack carries findings:read, because deploy/seed/seed.sh grants its project
// roles to nobody. These tests mint their own tokens against the harness's
// JWKS, so the RPC layer is fully exercisable while that is still true.
//
// PROVEN ABLE TO FAIL, and not by synthetic breakage: two of these caught real
// bugs in the implementation they were written against, which is the better
// evidence.
//
//   - TestApprovingTwiceThroughTheAPIWritesNoSecondRow failed because the store
//     decided `applied` by reading the status AFTER calling approve_finding. A
//     second approval leaves the finding approved, so every repeat call
//     reported applied=true. The status is now read before acting.
//   - TestTheFeedCarriesTheStoredCitation and TestAReadTokenIsRefusedTheActPath
//     failed on a NULL quoted_text from finding_supporting_chunks, which took
//     the whole detail view down with an internal error rather than showing a
//     citation with no body.
//
// The plan-gating pair proves itself in both directions without any edit: a
// gate that never fires fails the billing-enabled test, and one that always
// fires fails the billing-disabled test.

// readScopes is a console token that may look and not touch. The whole point of
// splitting the two scopes: an integration that renders the feed should not be
// one credential leak away from approving on a human's behalf.
const readScopes = "openid profile findings:read dashboard:read"

// actScopes adds the act path.
const actScopes = "openid profile findings:read findings:act dashboard:read"

func migratorDSN() string {
	if dsn := os.Getenv("PG_MIGRATOR_URL"); dsn != "" {
		return dsn
	}
	return "postgres://kindlast_migrator:migrator-dev-password@127.0.0.1:5433/kindlast"
}

// seeder is a migrator connection.
//
// Findings cannot be created as kindlast_app at all: `findings` has select and
// update policies and deliberately no insert policy, because the Analyst runs
// on a maintenance connection (00002's header). So a fixture finding needs this
// rather than the application pool, and that asymmetry is itself the design
// working.
func seeder(t *testing.T) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(t.Context(), migratorDSN())
	if err != nil {
		unavailable(t, "migrator connection not reachable (%v)", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// buildFindingsChain serves SessionService, FindingsService and DashboardService
// over one production interceptor chain. Session is needed because /me is what
// provisions an organisation.
func buildFindingsChain(t *testing.T, a *authServer, billingEnabled bool) (
	corev1connect.SessionServiceClient,
	corev1connect.FindingsServiceClient,
	corev1connect.DashboardServiceClient,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)
	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(a.profiles(t)), chain))
	mux.Handle(corev1connect.NewFindingsServiceHandler(findingsservice.New(billingEnabled), chain))
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboardservice.New(), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return corev1connect.NewSessionServiceClient(server.Client(), server.URL),
		corev1connect.NewFindingsServiceClient(server.Client(), server.URL),
		corev1connect.NewDashboardServiceClient(server.Client(), server.URL)
}

// signInWith provisions a person carrying the scopes given.
func signInWith(
	t *testing.T, a *authServer, client corev1connect.SessionServiceClient, label, scopes string,
) member {
	t.Helper()

	claim := fmt.Sprintf("find-%s-%d", label, time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.serveUserInfo(claim, map[string]any{
		"name": label, "email": label + "@example.invalid",
	})

	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t,
			mapClaims(a, claim, map[string]any{"scope": scopes})),
	}

	me, err := meCall(t, client, headers)
	if err != nil {
		t.Fatalf("provisioning %s: %v", label, err)
	}

	m := member{
		claim:   claim,
		orgID:   me.GetMemberships()[0].GetOrgId(),
		orgSlug: me.GetMemberships()[0].GetOrgSlug(),
		headers: headers,
	}
	m.headers[interceptor.OrgHeader] = m.orgID
	return m
}

// seedFinding creates a pending finding in an organisation and returns its id.
func seedFinding(t *testing.T, conn *pgx.Conn, orgID string) string {
	t.Helper()
	ctx := t.Context()

	var actor string
	if err := conn.QueryRow(ctx,
		`select user_id::text from memberships where org_id = $1 limit 1`, orgID,
	).Scan(&actor); err != nil {
		t.Fatalf("finding the organisation's owner: %v", err)
	}

	slug := fmt.Sprintf("rpc-fixture-%d", time.Now().UnixNano())
	var obligation string
	if err := conn.QueryRow(ctx, `
		insert into obligations
		  (slug, title, summary, citation_celex, citation_kind, citation_article)
		values ($1, 'Records of processing activities', $2, '32016R0679', 'article', 30)
		returning id::text
	`, slug,
		"A fixture obligation standing in for a real one, long enough to satisfy the "+
			"hundred character floor the schema places on an obligation summary.",
	).Scan(&obligation); err != nil {
		t.Fatalf("seeding an obligation: %v", err)
	}
	// Cleans up the organisation as well as the obligation, and reports rather
	// than swallows.
	//
	// The first version deleted only the obligation and discarded the error
	// with `_, _ =`. Forty-two fixture obligations were found accumulated in a
	// development database because of it. The cause was never established,
	// which is exactly the problem: a cleanup that cannot fail visibly gives
	// you no way to find out why it did not work.
	//
	// So this does two things differently. It removes the organisation first,
	// which cascades through profiles, signals and findings and leaves nothing
	// referencing the obligation (findings.obligation_id is ON DELETE RESTRICT,
	// so an ordering mistake there would refuse rather than cascade). And it
	// logs both failures, so the next leak comes with a reason attached.
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := conn.Exec(ctx,
			`delete from organisations where id = $1`, orgID); err != nil {
			t.Logf("cleanup: removing the fixture organisation: %v", err)
		}
		if _, err := conn.Exec(ctx,
			`delete from obligations where id = $1`, obligation); err != nil {
			t.Logf("cleanup: removing the fixture obligation: %v", err)
		}
	})

	var session, profile, signal, finding string
	if err := conn.QueryRow(ctx, `
		insert into onboarding_sessions (org_id, created_by) values ($1, $2) returning id::text
	`, orgID, actor).Scan(&session); err != nil {
		t.Fatalf("seeding an onboarding session: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into compliance_profiles
		  (org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
		values ($1, $2, $3, 'saas', 'no', 'no', 'no')
		returning id::text
	`, orgID, actor, session).Scan(&profile); err != nil {
		t.Fatalf("seeding a compliance profile: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into watcher_findings (org_id, profile_id, kind, title, dedup_key)
		values ($1, $2, 'profile_gap', 'Fixture signal', $3)
		returning id::text
	`, orgID, profile, slug).Scan(&signal); err != nil {
		t.Fatalf("seeding a signal: %v", err)
	}
	if err := conn.QueryRow(ctx, `
		insert into findings
		  (org_id, profile_id, watcher_finding_id, obligation_id, obligation_slug,
		   detected, proposed_action, severity, regulatory_obligation, citation_url)
		values ($1, $2, $3, $4, $5, 'No record of processing activities', 'Create one',
		        'critical', 'GDPR Art. 30', 'https://eur-lex.europa.eu/fixture')
		returning id::text
	`, orgID, profile, signal, obligation, slug).Scan(&finding); err != nil {
		t.Fatalf("seeding a finding: %v", err)
	}

	return finding
}

// The headline acceptance criterion, asserted through the RPC layer with a
// real token rather than by calling the SQL function directly.
func TestApprovingThroughTheAPIWritesOneAuditRowNamingTheHuman(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	res, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{FindingId: finding, Reviewed: true}),
		ada.headers))
	if err != nil {
		t.Fatalf("approving: %v", err)
	}
	if !res.Msg.GetApplied() {
		t.Fatal("the approval reports applied=false")
	}

	var actor, role, action string
	var rows int
	if err := conn.QueryRow(t.Context(), `
		select count(*)::int from audit_log where finding_id = $1
	`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("approving wrote %d audit rows, want exactly 1", rows)
	}

	if err := conn.QueryRow(t.Context(), `
		select user_id::text, actor_role, action_type
		from audit_log where finding_id = $1
	`, finding).Scan(&actor, &role, &action); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}

	// The actor is the signed-in human, resolved from the token's subject by
	// the tenancy interceptor, never a request field.
	if role != "owner" {
		t.Errorf("actor_role is %q, want owner", role)
	}
	if action != "approve_finding" {
		t.Errorf("action_type is %q, want approve_finding", action)
	}
	if actor == "" {
		t.Error("the audit row names no actor")
	}
}

func TestApprovingTwiceThroughTheAPIWritesNoSecondRow(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	approve := func() *connect.Response[corev1.ApproveFindingResponse] {
		t.Helper()
		res, err := feed.ApproveFinding(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ApproveFindingRequest{FindingId: finding}), ada.headers))
		if err != nil {
			t.Fatalf("approving: %v", err)
		}
		return res
	}

	approve()
	second := approve()

	// The second call is not an error. A double submit or a retry is the usual
	// cause and neither is a problem worth showing a customer.
	if second.Msg.GetApplied() {
		t.Error("the second approval reports applied=true")
	}

	var rows int
	if err := conn.QueryRow(t.Context(),
		`select count(*)::int from audit_log where finding_id = $1`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("two approvals wrote %d audit rows, want exactly 1", rows)
	}
}

// The scope split, which is only worth having if it is enforced.
func TestAReadTokenIsRefusedTheActPath(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", readScopes)
	finding := seedFinding(t, conn, ada.orgID)

	// It can read.
	if _, err := feed.GetFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetFindingRequest{FindingId: finding}), ada.headers,
	)); err != nil {
		t.Fatalf("a findings:read token cannot read a finding: %v", err)
	}

	// It cannot act, on any of the three.
	for _, act := range []struct {
		name string
		call func() error
	}{
		{"approve", func() error {
			_, err := feed.ApproveFinding(t.Context(), withHeaders(
				connect.NewRequest(&corev1.ApproveFindingRequest{FindingId: finding}), ada.headers))
			return err
		}},
		{"reject", func() error {
			_, err := feed.RejectFinding(t.Context(), withHeaders(
				connect.NewRequest(&corev1.RejectFindingRequest{FindingId: finding}), ada.headers))
			return err
		}},
		{"snooze", func() error {
			_, err := feed.SnoozeFinding(t.Context(), withHeaders(
				connect.NewRequest(&corev1.SnoozeFindingRequest{FindingId: finding}), ada.headers))
			return err
		}},
	} {
		t.Run(act.name, func(t *testing.T) {
			if got := codeOf(t, act.call()); got != connect.CodePermissionDenied {
				t.Fatalf("got %v, want permission_denied", got)
			}
		})
	}

	// And nothing happened. A refusal that still wrote would be the worst of
	// both: a 403 the caller believes and a change the record keeps.
	var rows int
	if err := conn.QueryRow(t.Context(),
		`select count(*)::int from audit_log where finding_id = $1`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("a refused act wrote %d audit rows, want 0", rows)
	}
}

// §13.3, through the whole chain with two subjects' tokens.
func TestOneOrganisationCannotSeeOrActOnAnothersFindings(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", actScopes)
	bob := signInWith(t, a, sessions, "bob", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	// Bob's feed does not contain it.
	list, err := feed.ListFindings(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListFindingsRequest{}), bob.headers))
	if err != nil {
		t.Fatalf("listing as bob: %v", err)
	}
	for _, f := range list.Msg.GetFindings() {
		if f.GetFindingId() == finding {
			t.Fatal("bob's feed contains a finding from another organisation")
		}
	}

	// Reading it directly is not_found, not permission_denied: telling Bob it
	// exists but is not his is the disclosure this is meant to prevent.
	_, err = feed.GetFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetFindingRequest{FindingId: finding}), bob.headers))
	if got := codeOf(t, err); got != connect.CodeNotFound {
		t.Fatalf("reading another organisation's finding gave %v, want not_found", got)
	}

	// Acting on it changes nothing and says nothing.
	res, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{FindingId: finding}), bob.headers))
	if err != nil {
		t.Fatalf("approving as bob returned an error rather than applied=false: %v", err)
	}
	if res.Msg.GetApplied() {
		t.Fatal("bob approved another organisation's finding")
	}

	var status string
	if err := conn.QueryRow(t.Context(),
		`select status from findings where id = $1`, finding).Scan(&status); err != nil {
		t.Fatalf("reading the finding: %v", err)
	}
	if status != "pending" {
		t.Fatalf("the finding is %q after a cross-org approval, want pending", status)
	}

	var rows int
	if err := conn.QueryRow(t.Context(),
		`select count(*)::int from audit_log where finding_id = $1`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("a cross-org act wrote %d audit rows, want 0", rows)
	}
}

// ENT-161, end to end. The state a brand-new organisation is actually in.
func TestANewOrganisationIsNotAssessedRatherThanGreen(t *testing.T) {
	a := newAuthServer(t)
	sessions, _, board := buildFindingsChain(t, a, false)

	ada := signInWith(t, a, sessions, "ada", readScopes)

	res, err := board.GetDashboard(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetDashboardRequest{}), ada.headers))
	if err != nil {
		t.Fatalf("reading the dashboard: %v", err)
	}

	if got := res.Msg.GetPosture(); got != "not_assessed" {
		t.Fatalf("a brand-new organisation reports posture %q, want not_assessed", got)
	}
	if res.Msg.GetPipeline().GetWatcherLastRunAt() != nil {
		t.Error("the Watcher has never run, but a last-run time was returned")
	}
	if res.Msg.GetPipeline().GetProfileExists() {
		t.Error("onboarding has not happened, but a profile was reported")
	}
}

// The self-hosted default (§18.1). Billing off means the act path is ungated,
// and that is not an oversight: a self-hoster has no subscription and never
// will, so gating them out of the Executor would make the self-hosted build a
// demo.
func TestTheActPathIsUngatedWhenBillingIsDisabled(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	res, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{FindingId: finding}), ada.headers))
	if err != nil {
		t.Fatalf("a self-hosted approval was refused: %v", err)
	}
	if !res.Msg.GetApplied() {
		t.Fatal("a self-hosted approval reports applied=false")
	}
}

// And with billing on, a free organisation is refused. Proves the gate is real
// rather than a branch that always falls through.
func TestAFreeOrganisationIsRefusedTheActPathWhenBillingIsEnabled(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, true)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	_, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{FindingId: finding}), ada.headers))

	// resource_exhausted rather than permission_denied, because they mean
	// different things to the person reading the console: one sends an owner to
	// their permissions, the other is a sentence with a button under it.
	if got := codeOf(t, err); got != connect.CodeResourceExhausted {
		t.Fatalf("got %v, want resource_exhausted", got)
	}

	var status string
	if err := conn.QueryRow(t.Context(),
		`select status from findings where id = $1`, finding).Scan(&status); err != nil {
		t.Fatalf("reading the finding: %v", err)
	}
	if status != "pending" {
		t.Fatalf("a plan-refused approval still changed the finding to %q", status)
	}
}

// The feed reads the citation the Analyst recorded, and the handler does not
// reassemble it. If a renderer ever needs the label, this is where it comes
// from.
func TestTheFeedCarriesTheStoredCitation(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "ada", readScopes)
	finding := seedFinding(t, conn, ada.orgID)

	res, err := feed.GetFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetFindingRequest{FindingId: finding}), ada.headers))
	if err != nil {
		t.Fatalf("reading the finding: %v", err)
	}

	citation := res.Msg.GetFinding().GetCitation()
	if got := citation.GetLabel(); got != "GDPR Art. 30" {
		t.Errorf("label is %q, want the stored %q", got, "GDPR Art. 30")
	}
	if got := citation.GetCelex(); got != "32016R0679" {
		t.Errorf("celex is %q, want 32016R0679", got)
	}
	if got := citation.GetArticle(); got != 30 {
		t.Errorf("article is %d, want 30", got)
	}
}
