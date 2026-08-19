package interceptor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	orgservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// ENT-202's acceptance criteria, through the whole chain rather than at the
// store layer.
//
// The store tests prove the policies. These prove what a client actually
// experiences: a signed token over HTTP, through authentication, revocation,
// scope and tenancy, into the handler, and back as a Connect code. That
// distinction matters most for the role boundary, because RLS refusing a write
// and a handler refusing a request look completely different from outside. One
// is a success that changed nothing; the other is a 403 that says why.

// orgScopes is what a console token carries for this surface. Without
// org:manage the scope interceptor refuses first, which would be a correct
// refusal and a confusing test failure.
const orgScopes = "openid profile org:read org:manage"

// buildOrgChain serves SessionService and OrgService over one production
// chain, because these tests need both: /me is what provisions an
// organisation, and OrgService is what acts on it.
func buildOrgChain(t *testing.T, a *authServer) (
	corev1connect.SessionServiceClient, corev1connect.OrgServiceClient, *stack,
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
	// A real base URL: InviteMember refuses without one (ENT-219), and these
	// tests exercise inviting.
	mux.Handle(corev1connect.NewOrgServiceHandler(
		orgservice.New("http://console.test.invalid"), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return corev1connect.NewSessionServiceClient(server.Client(), server.URL),
		corev1connect.NewOrgServiceClient(server.Client(), server.URL),
		live
}

// member is one signed-in person in these tests: their subject claim, the
// organisation they landed in, and the headers that act as them.
type member struct {
	claim   string
	orgID   string
	orgSlug string
	headers map[string]string
}

// signIn mints a subject, provisions them through /me, and returns everything
// needed to act as them.
func signIn(t *testing.T, a *authServer, client corev1connect.SessionServiceClient, label, name string) member {
	t.Helper()

	claim := fmt.Sprintf("org-%s-%d", label, time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.serveUserInfo(claim, map[string]any{
		"name": name, "email": label + "@example.invalid",
	})

	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t,
			mapClaims(a, claim, map[string]any{"scope": orgScopes})),
	}

	me, err := meCall(t, client, headers)
	if err != nil {
		t.Fatalf("provisioning %s: %v", label, err)
	}
	if len(me.GetMemberships()) != 1 {
		t.Fatalf("%s landed in %d organisations, want 1", label, len(me.GetMemberships()))
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

// actingIn copies a member's headers with a different active organisation, for
// the case where someone joined an organisation that is not their own.
func (m member) actingIn(orgID string) map[string]string {
	headers := map[string]string{interceptor.OrgHeader: orgID}
	for k, v := range m.headers {
		if k != interceptor.OrgHeader {
			headers[k] = v
		}
	}
	return headers
}

func withHeaders[T any](req *connect.Request[T], headers map[string]string) *connect.Request[T] {
	for name, value := range headers {
		req.Header().Set(name, value)
	}
	return req
}

func codeOf(t *testing.T, err error) connect.Code {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error and got none")
	}
	return connect.CodeOf(err)
}

// The acceptance criterion, and the reason the slug exists as a separate
// column rather than being derived on read.
//
// Slugs live in bookmarks and in emailed capability-token links, which are
// exactly the links a compliance product has to keep working. A rename that
// moved them would break every one, silently, at the moment a customer is most
// likely to be renaming: when they have just been acquired or rebranded.
func TestRenamingAnOrganisationLeavesItsSlugAlone(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, _ := buildOrgChain(t, a)

	owner := signIn(t, a, sessionClient, "rename", "Rename Fixture")

	response, err := orgClient.UpdateOrganisation(t.Context(), withHeaders(
		connect.NewRequest(&corev1.UpdateOrganisationRequest{Name: "Renamed Entirely"}),
		owner.headers))
	if err != nil {
		t.Fatalf("renaming: %v", err)
	}

	if got := response.Msg.GetName(); got != "Renamed Entirely" {
		t.Errorf("name = %q, want the new name", got)
	}
	if got := response.Msg.GetSlug(); got != owner.orgSlug {
		t.Fatalf("slug = %q, want it unchanged at %q", got, owner.orgSlug)
	}

	// And it survives a re-read, not just the write's own answer.
	me, err := meCall(t, sessionClient, owner.headers)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if got := me.GetMemberships()[0].GetOrgSlug(); got != owner.orgSlug {
		t.Fatalf("slug after re-read = %q, want %q", got, owner.orgSlug)
	}
}

// An organisation with no owner has nobody who can invite, change roles or
// manage billing, and no way back that does not involve an operator opening a
// database session.
//
// Both halves are asserted because guarding only removal would be theatre: a
// last owner who cannot be removed can be demoted to viewer and then removed,
// arriving at the same place in two steps.
func TestTheLastOwnerCanBeNeitherRemovedNorDemoted(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, _ := buildOrgChain(t, a)

	owner := signIn(t, a, sessionClient, "lastowner", "Last Owner")
	userID := userIDFor(t, a, owner.claim)

	_, err := orgClient.RemoveMember(t.Context(), withHeaders(
		connect.NewRequest(&corev1.RemoveMemberRequest{UserId: userID}),
		owner.headers))
	if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("removing the last owner returned %v, want FailedPrecondition", got)
	}

	_, err = orgClient.UpdateMemberRole(t.Context(), withHeaders(
		connect.NewRequest(&corev1.UpdateMemberRoleRequest{
			UserId: userID, Role: "viewer",
		}), owner.headers))
	if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
		t.Errorf("demoting the last owner returned %v, want FailedPrecondition", got)
	}

	// Still an owner afterwards, because a refused write that half-applied
	// would satisfy both assertions above and still be a disaster.
	members, err := orgClient.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), owner.headers))
	if err != nil {
		t.Fatalf("listing members: %v", err)
	}
	if len(members.Msg.GetMembers()) != 1 {
		t.Fatalf("members = %d, want 1", len(members.Msg.GetMembers()))
	}
	if got := members.Msg.GetMembers()[0].GetRole(); got != "owner" {
		t.Fatalf("role = %q, want it still owner", got)
	}
}

// A client can find itself in a member list from one call (ENT-220).
//
// The gap this closes: GetCurrentUser returned the IdP subject claim, ListMembers
// returns the version 5 uuid derived from it, and the derivation is one-way, so a
// console could render an organisation's members and not know which row was the
// person reading the page. The visible cost was that an owner could not leave an
// organisation the API was perfectly willing to let them leave.
//
// Asserted by matching, not by shape. A test that only checked `user_id` was a
// uuid would pass against any uuid, including the wrong person's.
func TestACallerCanRecogniseItselfInTheMemberList(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, _ := buildOrgChain(t, a)

	ada := signIn(t, a, sessionClient, "selfid-ada", "Ada Lovelace")

	me, err := sessionClient.GetCurrentUser(t.Context(), withHeaders(
		connect.NewRequest(&corev1.GetCurrentUserRequest{}), ada.headers))
	if err != nil {
		t.Fatalf("GetCurrentUser: %v", err)
	}

	user := me.Msg.GetUser()
	if user.GetUserId() == "" {
		t.Fatal("user_id is empty, so a client still cannot identify itself")
	}
	// The two identifiers are different things and must stay so. If a later
	// change "simplifies" them into one, this is what fails.
	if user.GetUserId() == user.GetId() {
		t.Fatalf("user_id and id are both %q; they are the derived key and the "+
			"IdP subject claim and are not interchangeable", user.GetId())
	}
	if want := userIDFor(t, a, ada.claim); user.GetUserId() != want {
		t.Fatalf("user_id = %q, want the derived id %q", user.GetUserId(), want)
	}

	members, err := orgClient.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), ada.headers))
	if err != nil {
		t.Fatalf("listing members: %v", err)
	}

	var found bool
	for _, m := range members.Msg.GetMembers() {
		if m.GetUserId() == user.GetUserId() {
			found = true
		}
	}
	if !found {
		t.Fatal("no member row matches the caller's user_id, so the console " +
			"still cannot mark which row is you")
	}
}

// The capability ENT-220 unlocks: leaving.
//
// `memberships_delete_owner_or_self` has always permitted removing yourself, so
// this was never a policy gap. It was unreachable because the console could not
// name your row. Asserted through the RPC layer, with a second owner present so
// the last-owner rule is not what is being measured.
func TestAnOwnerWhoIsNotTheLastCanRemoveThemselves(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, live := buildOrgChain(t, a)

	ada := signIn(t, a, sessionClient, "leaver-ada", "Ada Leaver")
	adaID := userIDFor(t, a, ada.claim)

	// A second owner, so the last-owner rule is not what is being measured.
	bob := joinAs(t, a, live, orgClient, ada, "leaver-bob", "owner")

	if _, err := orgClient.RemoveMember(t.Context(), withHeaders(
		connect.NewRequest(&corev1.RemoveMemberRequest{UserId: adaID}),
		ada.headers)); err != nil {
		t.Fatalf("an owner could not remove themselves: %v", err)
	}

	// Actually gone. A remove that returned OK and deleted nothing would pass
	// the assertion above, which is the failure mode an RLS refusal produces.
	members, err := orgClient.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), bob.headers))
	if err != nil {
		t.Fatalf("listing members as the remaining owner: %v", err)
	}
	for _, m := range members.Msg.GetMembers() {
		if m.GetUserId() == adaID {
			t.Fatal("the owner who left is still a member")
		}
	}
	if len(members.Msg.GetMembers()) != 1 {
		t.Fatalf("members = %d, want 1", len(members.Msg.GetMembers()))
	}
}

// The members list is what the settings page renders, and a page of uuids is
// not a page. This is the visible half of 00005's policy reversal.
func TestTheMembersListNamesPeopleRatherThanUuids(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, _ := buildOrgChain(t, a)

	owner := signIn(t, a, sessionClient, "named", "Ada Lovelace")

	members, err := orgClient.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), owner.headers))
	if err != nil {
		t.Fatalf("listing members: %v", err)
	}
	if len(members.Msg.GetMembers()) != 1 {
		t.Fatalf("members = %d, want 1", len(members.Msg.GetMembers()))
	}

	got := members.Msg.GetMembers()[0]
	if got.GetDisplayName() != "Ada Lovelace" {
		t.Errorf("display name = %q, want the name userinfo returned", got.GetDisplayName())
	}
	if got.GetEmail() != "named@example.invalid" {
		t.Errorf("email = %q, want the address userinfo returned", got.GetEmail())
	}
	if got.GetJoinedAt() == nil {
		t.Error("joined_at is nil; the settings page has nothing to order or explain by")
	}
}

// An organisation's members are its own. This is the tenancy boundary at the
// RPC layer rather than at the policy layer, which is where a middleware bug
// would show up.
func TestOneOrganisationCannotListAnothersMembers(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, _ := buildOrgChain(t, a)

	ada := signIn(t, a, sessionClient, "tenancy-ada", "Tenancy Ada")
	bob := signIn(t, a, sessionClient, "tenancy-bob", "Tenancy Bob")

	// Ada asks for Bob's organisation explicitly. The tenancy interceptor
	// refuses before any handler runs, because she holds no membership there.
	_, err := orgClient.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), ada.actingIn(bob.orgID)))
	if got := codeOf(t, err); got == connect.Code(0) {
		t.Fatal("listing another organisation's members succeeded")
	}
	if got := codeOf(t, err); got != connect.CodePermissionDenied && got != connect.CodeNotFound {
		t.Errorf("cross-org list returned %v, want PermissionDenied or NotFound", got)
	}
}

// joinAs brings a second person into an existing organisation with a given
// role, by the route a real invitee takes.
//
// The invitation is created through the store rather than through
// InviteMember, and that is forced by a deliberate property of the API: the
// raw token is never returned, so a test driving the RPC has no way to learn
// the value it would need to redeem. That is the design working. The fixture
// therefore mints a token it already knows, stores its hash through the same
// function the redeem path uses, and lets the RPC layer do the acceptance.
//
// The invitee must NOT call /me first. §1.8: an invited subject who bootstraps
// before accepting is given a personal organisation alongside the one they
// were invited to, which is the whole reason AcceptInvitation shipped early.
func joinAs(
	t *testing.T,
	a *authServer,
	live *stack,
	orgClient corev1connect.OrgServiceClient,
	owner member,
	label, role string,
) member {
	t.Helper()

	claim := fmt.Sprintf("org-%s-%d", label, time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	a.serveUserInfo(claim, map[string]any{
		"name": label, "email": label + "@example.invalid",
	})

	token := fmt.Sprintf("fixture-token-%s-%d", label, time.Now().UnixNano())

	tenant, err := live.store.BeginTenant(t.Context(), owner.claim, owner.orgID)
	if err != nil {
		t.Fatalf("opening a tenant transaction to invite %s: %v", label, err)
	}
	// Addressed to the subject rather than to the label, so it matches the
	// `email` claim the token below carries. 00033 holds the caller to being
	// the person the invitation names, so a fixture whose two halves disagree
	// is a fixture inviting somebody else.
	if _, err := tenant.CreateInvitation(
		t.Context(), claim+"@example.invalid", role, token,
	); err != nil {
		_ = tenant.Rollback(t.Context())
		t.Fatalf("creating the invitation for %s: %v", label, err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing the invitation for %s: %v", label, err)
	}

	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t,
			mapClaims(a, claim, map[string]any{
				"scope": orgScopes,
				"email": claim + "@example.invalid",
			})),
	}

	// No organisation header: the invitee holds no membership anywhere yet, so
	// there is nothing to be active in. Omitted rather than empty, per §1.8.
	accepted, err := orgClient.AcceptInvitation(t.Context(), withHeaders(
		connect.NewRequest(&corev1.AcceptInvitationRequest{Token: token}), headers))
	if err != nil {
		t.Fatalf("%s accepting: %v", label, err)
	}
	if got := accepted.Msg.GetRole(); got != role {
		t.Fatalf("%s joined as %q, want %q", label, got, role)
	}

	joined := member{
		claim:   claim,
		orgID:   accepted.Msg.GetOrgId(),
		orgSlug: accepted.Msg.GetOrgSlug(),
		headers: headers,
	}
	joined.headers[interceptor.OrgHeader] = joined.orgID
	return joined
}

// The acceptance criterion, and the reason the handler checks the role at all
// when RLS would refuse the write anyway.
//
// An RLS refusal, seen from a client, is a call that succeeded and changed
// nothing. These assertions are that the caller is told no, in a way a console
// can render, rather than being left to notice that nothing happened.
func TestANonOwnerCannotManageMembers(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, live := buildOrgChain(t, a)

	owner := signIn(t, a, sessionClient, "boundary-owner", "Boundary Owner")
	viewer := joinAs(t, a, live, orgClient, owner, "boundary-viewer", "viewer")
	regular := joinAs(t, a, live, orgClient, owner, "boundary-member", "member")

	ownerID := userIDFor(t, a, owner.claim)

	t.Run("a viewer cannot invite", func(t *testing.T) {
		_, err := orgClient.InviteMember(t.Context(), withHeaders(
			connect.NewRequest(&corev1.InviteMemberRequest{
				Email: "nobody@example.invalid", Role: "viewer",
			}), viewer.headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Errorf("viewer invite returned %v, want PermissionDenied", got)
		}
	})

	t.Run("a member cannot change roles", func(t *testing.T) {
		_, err := orgClient.UpdateMemberRole(t.Context(), withHeaders(
			connect.NewRequest(&corev1.UpdateMemberRoleRequest{
				UserId: ownerID, Role: "viewer",
			}), regular.headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Errorf("member role change returned %v, want PermissionDenied", got)
		}
	})

	t.Run("a member cannot rename the organisation", func(t *testing.T) {
		_, err := orgClient.UpdateOrganisation(t.Context(), withHeaders(
			connect.NewRequest(&corev1.UpdateOrganisationRequest{Name: "Hijacked"}),
			regular.headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Errorf("member rename returned %v, want PermissionDenied", got)
		}
	})

	// And the refusals refused: nothing above changed anything. A role check
	// that returns the right code while the write lands anyway would pass
	// every assertion so far.
	t.Run("and nothing actually changed", func(t *testing.T) {
		members, err := orgClient.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), owner.headers))
		if err != nil {
			t.Fatalf("listing members: %v", err)
		}
		if len(members.Msg.GetMembers()) != 3 {
			t.Fatalf("members = %d, want 3", len(members.Msg.GetMembers()))
		}
		roles := map[string]string{}
		for _, m := range members.Msg.GetMembers() {
			roles[m.GetUserId()] = m.GetRole()
		}
		if roles[ownerID] != "owner" {
			t.Errorf("the owner is now %q", roles[ownerID])
		}
	})
}

// A viewer may read the member list. Seeing your colleagues is not an
// administrative act, which is why ListMembers declares org:read and not
// org:manage: requiring the management scope would mean a viewer could not
// render the page they are entitled to see.
func TestAViewerMayStillReadTheMemberList(t *testing.T) {
	a := newAuthServer(t)
	sessionClient, orgClient, live := buildOrgChain(t, a)

	owner := signIn(t, a, sessionClient, "readonly-owner", "Readonly Owner")
	viewer := joinAs(t, a, live, orgClient, owner, "readonly-viewer", "viewer")

	members, err := orgClient.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), viewer.headers))
	if err != nil {
		t.Fatalf("a viewer could not read the member list: %v", err)
	}
	if len(members.Msg.GetMembers()) != 2 {
		t.Fatalf("members = %d, want 2", len(members.Msg.GetMembers()))
	}
}

// userIDFor derives the uuid the API takes from the subject claim the IdP
// issues, through the same one-way function provisioning used.
func userIDFor(t *testing.T, a *authServer, claim string) string {
	t.Helper()

	id, err := subject.UUID(a.server.URL, claim)
	if err != nil {
		t.Fatalf("deriving the user id: %v", err)
	}
	return id.String()
}
