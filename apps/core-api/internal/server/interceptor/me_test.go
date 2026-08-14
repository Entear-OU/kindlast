package interceptor_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"

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

// The defect this closes, reproduced at the level it was found.
//
// Every other provisioning test above mints a token carrying `name` and
// `email`, so every one of them passes while the shipped stack does not: the
// bundled Zitadel puts neither claim in an access token, provisioning falls
// through to the `sub`, and a real person's first organisation is named after
// a snowflake integer. Observed on the running stack as an organisation called
// `386250729179840515`.
//
// So the token here carries what Zitadel actually issues, which is nothing
// describing the human, and the name has to come from userinfo instead.
func TestAnOrganisationIsNamedFromUserInfoWhenTheTokenCarriesNoProfile(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-nameless-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.serveUserInfo(claim, map[string]any{
		"name":  "Ada Lovelace",
		"email": "ada@example.com",
	})

	me, err := meCall(t, client, map[string]string{
		// No name claim and no email claim, exactly as Zitadel mints them.
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
	})
	if err != nil {
		t.Fatalf("a subject with no profile claims could not complete its first call: %v", err)
	}

	if len(me.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d, want exactly 1", len(me.GetMemberships()))
	}
	if got := me.GetMemberships()[0].GetOrgName(); got != "Ada Lovelace" {
		t.Fatalf("organisation name = %q, want it derived from userinfo; "+
			"a name equal to the subject claim is the ENT-198 blocker this test exists for", got)
	}
}

// Provisioning must not depend on the authorization server being reachable.
//
// The whole reason verification is local (§1.4) is that a page render survives
// `auth` being down. A userinfo call that could fail the request would hand
// that property back on the one request where a user has no organisation yet,
// which is the worst moment to fail: they would be signed in with nowhere to
// be, and a retry loop against a service that is already unwell.
func TestProvisioningSurvivesUserInfoBeingUnreachable(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-nouserinfo-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.takeUserInfoDown()

	me, err := meCall(t, client, map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
	})
	if err != nil {
		t.Fatalf("provisioning failed because userinfo was down: %v", err)
	}

	if len(me.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d, want exactly 1 even with no profile available", len(me.GetMemberships()))
	}
	// Degraded, not broken: the subject claim is a poor label and it is still
	// better than an organisation with no name at all.
	if got := me.GetMemberships()[0].GetOrgName(); got != claim {
		t.Fatalf("organisation name = %q, want the subject claim as the last-resort fallback", got)
	}
}

// An organisation named after a subject claim repairs itself on the next
// sign-in.
//
// This is the ordering constraint in §20.1 rather than a cosmetic tidy-up.
// Slugs are immutable once minted, so a name that is really an identifier has
// to be corrected BEFORE ENT-198 derives a URL from it; afterwards the ugly
// name is permanent and the rename is no longer defensible, because a slug in
// a bookmark and an emailed approval link would both stop resolving.
//
// A migration cannot do this: userinfo needs the caller's own access token and
// there is no caller during a migration. So it happens here, once, for each
// affected person, the next time they arrive.
//
// The sequence below is the real history: an organisation created while no
// profile was available, then the same person coming back.
func TestAnOrganisationNamedAfterTheSubjectIsRenamedOnTheNextSignIn(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-rename-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	// First arrival, with nothing available to name it after.
	a.takeUserInfoDown()
	first, err := meCall(t, client, map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if got := first.GetMemberships()[0].GetOrgName(); got != claim {
		t.Fatalf("organisation name = %q, want the subject claim; "+
			"this test needs the defect present before it can prove the repair", got)
	}

	// The same person, later, on a stack where userinfo now answers.
	a.bringUserInfoUp()
	a.serveUserInfo(claim, map[string]any{"name": "Ada Lovelace", "email": "ada@example.com"})

	second, err := meCall(t, client, map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
	})
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if got := second.GetMemberships()[0].GetOrgName(); got != "Ada Lovelace" {
		t.Fatalf("organisation name = %q, want it repaired to the person's name", got)
	}
	if len(second.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d, want the same one renamed rather than a second created",
			len(second.GetMemberships()))
	}
	if second.GetMemberships()[0].GetOrgId() != first.GetMemberships()[0].GetOrgId() {
		t.Fatal("the organisation id changed; a rename must not create a new organisation")
	}

	// The identity row is repaired in the same pass, and this is the only
	// chance it gets: the profile is fetched once for this person and never
	// again after the rename. Users who predate the fix would otherwise keep a
	// row that maps a derived uuid back to nobody, which is what a subject
	// access request has to be answered from.
	if got := recordedEmailFor(t, a.server.URL, claim); got != "ada@example.com" {
		t.Fatalf("recorded email = %q, want the repair to have restored it", got)
	}
}

// The rename condition is evaluated on every sign-in forever, so it must cost
// nothing when there is nothing to repair.
//
// The assertion counts requests at the server rather than checking the
// response, and that distinction is the whole test. Every failure on this path
// degrades quietly by design, so a wasted round trip to the authorization
// server and no call at all produce byte-identical responses. Asserting on the
// outcome would pass just as happily against an implementation that consulted
// userinfo on every page render for every user, forever.
func TestANormallyNamedOrganisationNeverConsultsUserInfoAgain(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-norename-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.serveUserInfo(claim, map[string]any{"name": "Grace Hopper", "email": "grace@example.com"})
	if _, err := meCall(t, client, map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
	}); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// One fetch, for provisioning. Everything after this must add none.
	afterProvisioning := a.userInfoFetchCount()

	for range 3 {
		second, err := meCall(t, client, map[string]string{
			"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, nil)),
		})
		if err != nil {
			t.Fatalf("returning call: %v", err)
		}
		if got := second.GetMemberships()[0].GetOrgName(); got != "Grace Hopper" {
			t.Fatalf("organisation name = %q, want it left alone", got)
		}
	}

	if got := a.userInfoFetchCount(); got != afterProvisioning {
		t.Fatalf("userinfo fetches went from %d to %d across three returning calls; "+
			"a caller with nothing to repair must not reach the authorization server at all",
			afterProvisioning, got)
	}
}

// A token that does carry the claims must not trigger a network call at all.
//
// This is the property that keeps userinfo off the hot path: a conformant
// provider that puts `name` in the access token should never cause this
// service to talk to the authorization server during a request.
func TestUserInfoIsNotConsultedWhenTheTokenAlreadyCarriesAProfile(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-hasprofile-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	// Down, so consulting it would fail the assertion below rather than
	// silently succeed.
	a.takeUserInfoDown()

	me, err := meCall(t, client, map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t, mapClaims(a, claim, map[string]any{
			"name": "Grace Hopper", "email": "grace@example.com",
		})),
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	if got := me.GetMemberships()[0].GetOrgName(); got != "Grace Hopper" {
		t.Fatalf("organisation name = %q, want it from the token's own claims", got)
	}
}
