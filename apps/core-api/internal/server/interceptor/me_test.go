package interceptor_test

import (
	"context"
	"fmt"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// ENT-196 through the whole chain rather than at the store layer.
//
// The store tests prove provisioning against real policies; these prove the
// path a client actually takes: a signed token over HTTP, through
// authentication, revocation, scope and tenancy, into the handler, and back
// out as a response. Everything in between is the real thing.

// migratorDSNForChain honours PG_MIGRATOR_URL like every other connection
// helper here. Hardcoding it would mean this one fixture path silently ignored
// the environment CI and a non-default local setup both use.
func migratorDSNForChain() string {
	if dsn := os.Getenv("PG_MIGRATOR_URL"); dsn != "" {
		return dsn
	}
	return "postgres://kindlast_migrator:migrator-dev-password@127.0.0.1:5433/kindlast"
}

// forget removes a test subject's rows, as the migrator, so a run starts and
// ends from nothing.
func forget(t *testing.T, issuer, claim string) {
	t.Helper()

	userID, err := subject.UUID(issuer, claim)
	if err != nil {
		t.Fatalf("deriving the user id: %v", err)
	}

	pool := chainMigratorPool(t)

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

// meCall is GetCurrentUser with the response, which `call` deliberately drops.
func meCall(
	t *testing.T,
	client corev1connect.SessionServiceClient,
	headers map[string]string,
) (*corev1.GetCurrentUserResponse, error) {
	t.Helper()

	request := connect.NewRequest(&corev1.GetCurrentUserRequest{})
	for name, value := range headers {
		request.Header().Set(name, value)
	}

	response, err := client.GetCurrentUser(t.Context(), request)
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

// A subject nobody has ever seen signs in and lands with somewhere to be.
//
// Note what is NOT sent: no organisation header. A client on its very first
// call cannot know an organisation id, because none exists yet, and an
// endpoint that required one would be unreachable exactly when it matters.
func TestANewSubjectIsProvisionedThroughTheWholeChain(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-newcomer-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	me, err := meCall(t, client, map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, map[string]any{
			"email": "newcomer@example.com", "name": "New Comer",
		})),
	})
	if err != nil {
		t.Fatalf("a new subject could not complete its first call: %v", err)
	}

	if len(me.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d, want exactly 1; a new user landed with nowhere to be", len(me.GetMemberships()))
	}
	if got := me.GetMemberships()[0].GetRole(); got != "owner" {
		t.Fatalf("role = %q, want owner", got)
	}
	if got := me.GetMemberships()[0].GetOrgName(); got != "New Comer" {
		t.Fatalf("organisation name = %q, want it derived from the token's name claim", got)
	}
	if me.GetActiveOrgId() != me.GetMemberships()[0].GetOrgId() {
		t.Fatalf("active org = %q but the only membership is %q; the console would open on nothing",
			me.GetActiveOrgId(), me.GetMemberships()[0].GetOrgId())
	}
	// A brand-new organisation has no subscription row, and the answer must be
	// a usable plan rather than an empty string the console has to guess at.
	if me.GetPlan() != "free" {
		t.Fatalf("plan = %q, want free for a newly provisioned organisation", me.GetPlan())
	}
	if me.GetUser().GetEmail() != "newcomer@example.com" {
		t.Fatalf("email = %q, want it from the verified token claims", me.GetUser().GetEmail())
	}
}

// Calling twice changes nothing, which is what makes it safe for this to be
// the first call every authenticated page makes.
func TestCallingMeTwiceIsIdempotent(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-repeat-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, map[string]any{
			"email": "repeat@example.com",
		})),
	}

	first, err := meCall(t, client, headers)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := meCall(t, client, headers)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if first.GetActiveOrgId() != second.GetActiveOrgId() {
		t.Fatalf("the active organisation changed between calls: %q then %q",
			first.GetActiveOrgId(), second.GetActiveOrgId())
	}
	if len(second.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d after two calls, want 1", len(second.GetMemberships()))
	}
}

// A seeded user with an existing organisation gets it back, plan included, and
// nothing is provisioned for them.
func TestAnExistingUserGetsTheirOrganisationAndPlan(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	me, err := meCall(t, client, map[string]string{
		"Authorization":       "Bearer " + a.token(t, adaUser, "openid profile"),
		interceptor.OrgHeader: alphaOrg,
	})
	if err != nil {
		t.Fatalf("an existing user could not read their session: %v", err)
	}

	if me.GetActiveOrgId() != alphaOrg {
		t.Fatalf("active org = %q, want the one the header asked for", me.GetActiveOrgId())
	}
	if me.GetPlan() != "pro" {
		t.Fatalf("plan = %q, want pro from the seeded subscription; the plan gate reads this", me.GetPlan())
	}
}

// The §1.8 ordering, through the chain: accept, then the first /me, and the
// invited user must not also acquire a personal organisation.
func TestAcceptingAnInvitationBeforeTheFirstMeYieldsOneOrganisation(t *testing.T) {
	a := newAuthServer(t)
	live := requireStack(t, a.server.URL)

	scopes := realScopes(t)
	orgClient := serveOrg(t,
		interceptorsFor(t, a, live, scopes)...,
	)
	sessionClient, _ := buildChain(t, a, scopes)

	claim := fmt.Sprintf("chain-invited-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	token := fmt.Sprintf("chain-invitation-%d", time.Now().UnixNano())
	insertInvitation(t, alphaOrg, "chain-invited@example.com", "member", token)
	t.Cleanup(func() { removeInvitation(t, token) })

	bearer := "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, map[string]any{
		"email": "chain-invited@example.com",
	}))

	accepted, err := orgClient.AcceptInvitation(t.Context(),
		requestWith(&corev1.AcceptInvitationRequest{Token: token}, map[string]string{
			"Authorization": bearer,
		}))
	if err != nil {
		t.Fatalf("accepting through the chain: %v", err)
	}
	if accepted.Msg.GetOrgId() != alphaOrg {
		t.Fatalf("joined %q, want %q", accepted.Msg.GetOrgId(), alphaOrg)
	}

	me, err := meCall(t, sessionClient, map[string]string{"Authorization": bearer})
	if err != nil {
		t.Fatalf("first /me after accepting: %v", err)
	}

	if len(me.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d, want 1; the invited user was also given a personal organisation",
			len(me.GetMemberships()))
	}
	if me.GetMemberships()[0].GetOrgId() != alphaOrg {
		t.Fatalf("membership = %q, want the invited organisation", me.GetMemberships()[0].GetOrgId())
	}
}

// An invitation token nobody issued is not found, and says no more than that.
func TestAnUnknownInvitationIsNotFoundThroughTheChain(t *testing.T) {
	a := newAuthServer(t)
	live := requireStack(t, a.server.URL)

	orgClient := serveOrg(t, interceptorsFor(t, a, live, realScopes(t))...)

	claim := fmt.Sprintf("chain-unknown-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	_, err := orgClient.AcceptInvitation(t.Context(),
		requestWith(&corev1.AcceptInvitationRequest{Token: "no-such-token"}, map[string]string{
			"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
		}))

	assertCode(t, err, connect.CodeNotFound, "an unknown invitation token was accepted")
}
