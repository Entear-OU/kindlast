package interceptor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	dashboardservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/dashboard"
	findingsservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/findings"
	ingestservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/ingest"
	orgservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// ENT-230's acceptance criteria, through the whole chain with real delegations.
//
// # WHAT THESE ARE ACTUALLY TESTING
//
// Not "does the delegation header work". The question is whether an agent
// acting for a person can end up with authority that person does not have, and
// there are four ways it could:
//
//	the scope layer   a machine holds `internal:*` and a person does not, so a
//	                  union of the two is larger than either
//	the tenancy layer a delegation that followed the organisation header would
//	                  let whoever sets the header move the agent between tenants
//	time              a delegation that outlives its run is a standing session
//	                  for something with no session
//	membership        a person removed mid-run whose agent keeps going is an
//	                  offboarding that did not take
//
// Each has a test below, and each is written so that it fails if the property
// is removed rather than only if the plumbing breaks.
//
// PROVEN ABLE TO FAIL, by deliberate breakage, all reverted:
//
//   - Making Scope.holds union the human set with the token's own scopes turns
//     TestADelegationCannotReachThePlatformSurface red on its own.
//   - Letting the organisation header win over the delegation in
//     interceptor.open turns TestADelegationCannotCrossOrganisations red.
//   - Removing the membership check from BeginDelegatedTenant (by resolving
//     against the delegation's org without the lookup) turns
//     TestLosingYourMembershipStopsTheAgent red while the happy paths stay
//     green.
//
// The stack requirement is the same one every test in this file has: these mint
// real delegations into real Postgres and read real audit rows.

// machineScopes is what the Intelligence principal's token carries.
//
// ONE SCOPE, AND DELIBERATELY NOT ONE A PERSON HOLDS. `internal:act-on-behalf`
// is granted through client credentials and is absent from HumanScopes, which
// is what makes "this token alone can reach nothing on the console surface" a
// property of the seed rather than of a check somewhere.
const machineScopes = "internal:act-on-behalf"

// intelligenceScopes is what the same principal uses to record a finished run.
const intelligenceScopes = "internal:intelligence"

// buildDelegationChain serves the console surface behind the PRODUCTION chain,
// ActOnBehalf included, plus IngestService on the same chain.
//
// Ingest is there for one assertion and it is worth saying why, because it is
// not how the service is deployed: `IngestService` runs on the shorter internal
// chain in server.New. Mounting it here is the only way to ask "what happens
// when a delegated request reaches a platform RPC", and the answer has to be a
// refusal from the scope stage rather than a route that happens not to exist.
func buildDelegationChain(t *testing.T, a *authServer) (
	corev1connect.SessionServiceClient,
	corev1connect.OrgServiceClient,
	corev1connect.FindingsServiceClient,
	platformv1connect.IngestServiceClient,
	*stack,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)
	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		interceptor.ActOnBehalf(live.store),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(a.profiles(t)), chain))
	mux.Handle(corev1connect.NewOrgServiceHandler(
		orgservice.New("http://console.test.invalid"), chain))
	mux.Handle(corev1connect.NewFindingsServiceHandler(findingsservice.New(false), chain))
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboardservice.New(), chain))
	mux.Handle(platformv1connect.NewIngestServiceHandler(
		ingestservice.New(nil, nil, nil, live.store, nil), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return corev1connect.NewSessionServiceClient(server.Client(), server.URL),
		corev1connect.NewOrgServiceClient(server.Client(), server.URL),
		corev1connect.NewFindingsServiceClient(server.Client(), server.URL),
		platformv1connect.NewIngestServiceClient(server.Client(), server.URL),
		live
}

// mintFor mints a delegation the way a rail handler would: inside a transaction
// that is already the person's.
//
// There is no shortcut here and that is the point. The mint policy checks the
// row against the two tenancy GUCs, so a fixture that wanted to hand an agent
// somebody else's authority would have to open a transaction as them first,
// which needs their subject claim, which is the thing an attacker does not have.
func mintFor(t *testing.T, live *stack, m member, agent string) postgres.Delegation {
	t.Helper()

	tenant, err := live.store.BeginTenant(t.Context(), m.claim, m.orgID)
	if err != nil {
		t.Fatalf("opening a transaction to mint for %s: %v", m.claim, err)
	}

	minted, err := tenant.MintDelegation(t.Context(), delegation.Mint{ActingAgent: agent})
	if err != nil {
		_ = tenant.Rollback(t.Context())
		t.Fatalf("minting a delegation for %s: %v", m.claim, err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing the mint: %v", err)
	}
	return minted
}

// revoke hands a delegation back, through the same path a finished run uses.
func revoke(t *testing.T, live *stack, m member, minted postgres.Delegation) {
	t.Helper()

	tenant, err := live.store.BeginTenant(t.Context(), m.claim, m.orgID)
	if err != nil {
		t.Fatalf("opening a transaction to revoke: %v", err)
	}
	if err := tenant.RevokeDelegation(t.Context(), minted.ID); err != nil {
		_ = tenant.Rollback(t.Context())
		t.Fatalf("revoking: %v", err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing the revocation: %v", err)
	}
}

// joinExisting puts somebody who already has an organisation into a second one.
//
// Different from joinAs, which mints a new person: this is the consultant shape,
// where one human is a member of several tenants and switches between them with
// the organisation header. An invitation is redeemed by whoever holds the token
// rather than by whoever it was addressed to (00003), which is what makes this
// two calls rather than a new sign-in.
func joinExisting(
	t *testing.T, live *stack, orgs corev1connect.OrgServiceClient,
	owner, joiner member, role string,
) {
	t.Helper()

	token := fmt.Sprintf("cross-org-invite-%d", time.Now().UnixNano())

	tenant, err := live.store.BeginTenant(t.Context(), owner.claim, owner.orgID)
	if err != nil {
		t.Fatalf("opening a transaction to invite: %v", err)
	}
	if _, err := tenant.CreateInvitation(
		t.Context(), fmt.Sprintf("%s@example.invalid", joiner.claim), role, token,
	); err != nil {
		_ = tenant.Rollback(t.Context())
		t.Fatalf("creating the invitation: %v", err)
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing the invitation: %v", err)
	}

	if _, err := orgs.AcceptInvitation(t.Context(), withHeaders(
		connect.NewRequest(&corev1.AcceptInvitationRequest{Token: token}),
		joiner.headers)); err != nil {
		t.Fatalf("%s accepting into the second organisation: %v", joiner.claim, err)
	}
}

// mintExpiredFor writes an already-dead delegation, as the migrator.
//
// Backdated rather than minted-then-aged, because the table refuses both: the
// TTL constraint will not accept an expiry in the past, and the narrow-update
// trigger will not let anybody move `expires_at` afterwards. Both refusals are
// the design, so the fixture ages a legitimate row instead of writing an
// impossible one.
func mintExpiredFor(t *testing.T, a *authServer, m member) string {
	t.Helper()

	token := fmt.Sprintf("expired-delegation-%d", time.Now().UnixNano())
	person := userIDOf(t, a, m)

	if _, err := chainMigratorPool(t).Exec(t.Context(), `
		insert into act_delegations
		  (id, org_id, user_id, acting_agent, token_hash, created_at, expires_at)
		values (gen_random_uuid(), $1, $2, 'analyst', $3,
		        now() - interval '30 minutes', now() - interval '1 minute')
	`, m.orgID, person, postgres.HashDelegationToken(token)); err != nil {
		t.Fatalf("seeding an expired delegation: %v", err)
	}
	return token
}

// userIDOf is the derived user id a member's rows carry.
//
// The same one-way derivation from (issuer, subject) the tenancy interceptor
// performs, rather than a value read back from a membership row. Reading it back
// would make the assertions below true by construction; what they want to say is
// that the row names the person a token would have resolved to.
func userIDOf(t *testing.T, a *authServer, m member) string {
	t.Helper()

	id, err := subject.UUID(a.server.URL, m.claim)
	if err != nil {
		t.Fatalf("deriving the user id for %s: %v", m.claim, err)
	}
	return id.String()
}

// machineHeaders is what Intelligence sends: its own token, plus the delegation
// it was handed.
func machineHeaders(t *testing.T, a *authServer, scopes, delegationToken string) map[string]string {
	t.Helper()

	claim := fmt.Sprintf("intelligence-%d", time.Now().UnixNano())
	headers := map[string]string{
		"Authorization": "Bearer " + a.tokenWithClaims(t,
			mapClaims(a, claim, map[string]any{"scope": scopes})),
	}
	if delegationToken != "" {
		headers[interceptor.DelegationHeader] = delegationToken
	}
	return headers
}

// The headline criterion: an agent run for a viewer cannot do what a viewer
// cannot, asserted through the RPC layer with a real delegation.
//
// Inviting is the sharpest available case because the refusal is a handler
// decision rather than an empty result: a viewer sending this gets
// PermissionDenied, and so must an agent acting for them. An owner's agent
// succeeding in the same test is what stops this passing for the boring reason
// that delegated calls never work at all.
func TestAnAgentInheritsExactlyThePersonsAuthority(t *testing.T) {
	a := newAuthServer(t)
	sessions, orgs, _, _, live := buildDelegationChain(t, a)

	owner := signIn(t, a, sessions, "deleg-owner", "Delegation Owner")
	viewer := joinAs(t, a, live, orgs, owner, "deleg-viewer", "viewer")

	t.Run("an owner's agent may invite, because the owner may", func(t *testing.T) {
		minted := mintFor(t, live, owner, "analyst")
		headers := machineHeaders(t, a, machineScopes, minted.Token)

		if _, err := orgs.InviteMember(t.Context(), withHeaders(
			connect.NewRequest(&corev1.InviteMemberRequest{
				Email: fmt.Sprintf("agent-invited-%d@example.invalid", time.Now().UnixNano()),
				Role:  "viewer",
			}), headers)); err != nil {
			t.Fatalf("an agent acting for the owner could not invite: %v", err)
		}
	})

	t.Run("a viewer's agent may not, because the viewer may not", func(t *testing.T) {
		minted := mintFor(t, live, viewer, "analyst")
		headers := machineHeaders(t, a, machineScopes, minted.Token)

		_, err := orgs.InviteMember(t.Context(), withHeaders(
			connect.NewRequest(&corev1.InviteMemberRequest{
				Email: "nobody@example.invalid", Role: "viewer",
			}), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("an agent acting for a viewer invited and got %v, want PermissionDenied", got)
		}
	})

	t.Run("and the machine on its own reaches nothing", func(t *testing.T) {
		// The same token with no delegation. It carries `internal:act-on-behalf`
		// and every console RPC declares something else, so the scope stage
		// refuses before tenancy is ever asked. This is the assertion that says
		// the delegation is what grants, rather than the token.
		headers := machineHeaders(t, a, machineScopes, "")

		_, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("an undelegated machine token read the member list and got %v, "+
				"want PermissionDenied", got)
		}
	})
}

// A delegation is single-org, and the organisation header does not get a vote.
//
// # THE PERSON HERE BELONGS TO BOTH ORGANISATIONS, WHICH IS THE WHOLE TEST
//
// An earlier version used two strangers and passed for the wrong reason: the
// membership check inside BeginDelegatedTenant refused Ada in Bob's tenant, and
// it would have refused her whether or not the interceptor bound the delegation
// to its own organisation. That is exactly the consultant §20.1 describes, and
// it is the case where the two mechanisms differ: Ada is a member of both, so
// membership passes, and the ONLY thing standing between a delegation minted
// for one client and a header naming another is the check in interceptor.open.
func TestADelegationCannotCrossOrganisations(t *testing.T) {
	a := newAuthServer(t)
	sessions, orgs, _, _, live := buildDelegationChain(t, a)

	ada := signIn(t, a, sessions, "deleg-ada", "Delegation Ada")
	bob := signIn(t, a, sessions, "deleg-bob", "Delegation Bob")
	joinExisting(t, live, orgs, bob, ada, "member")

	minted := mintFor(t, live, ada, "analyst")

	t.Run("naming another organisation is refused", func(t *testing.T) {
		headers := machineHeaders(t, a, machineScopes, minted.Token)
		headers[interceptor.OrgHeader] = bob.orgID

		_, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("a delegation was used in another organisation and got %v, "+
				"want PermissionDenied", got)
		}
	})

	t.Run("naming its own is fine, because that is not a claim", func(t *testing.T) {
		headers := machineHeaders(t, a, machineScopes, minted.Token)
		headers[interceptor.OrgHeader] = ada.orgID

		res, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if err != nil {
			t.Fatalf("a delegation with a matching organisation header was refused: %v", err)
		}
		if len(res.Msg.GetMembers()) == 0 {
			t.Fatal("the delegated read returned no members, so it saw nothing at all")
		}
	})

	t.Run("and sending no header at all uses the delegation's own", func(t *testing.T) {
		headers := machineHeaders(t, a, machineScopes, minted.Token)

		res, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if err != nil {
			t.Fatalf("a delegation with no organisation header was refused: %v", err)
		}
		if len(res.Msg.GetMembers()) == 0 {
			t.Fatal("the delegated read returned no members")
		}
	})
}

// A delegation cannot outlive its run. Two mechanisms, both asserted, because
// only one of them survives a crashed run.
func TestADelegationCannotOutliveItsRun(t *testing.T) {
	a := newAuthServer(t)
	sessions, orgs, _, _, live := buildDelegationChain(t, a)

	ada := signIn(t, a, sessions, "deleg-ttl", "Delegation TTL")

	t.Run("an expired delegation is refused", func(t *testing.T) {
		token := mintExpiredFor(t, a, ada)
		headers := machineHeaders(t, a, machineScopes, token)

		_, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("an expired delegation was accepted and got %v, want PermissionDenied", got)
		}
	})

	t.Run("a revoked one is refused, and worked a moment earlier", func(t *testing.T) {
		minted := mintFor(t, live, ada, "analyst")
		headers := machineHeaders(t, a, machineScopes, minted.Token)

		// Before, so the refusal after cannot be explained by the delegation
		// never having worked.
		if _, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers)); err != nil {
			t.Fatalf("a fresh delegation was refused: %v", err)
		}

		revoke(t, live, ada, minted)

		_, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("a revoked delegation was accepted and got %v, want PermissionDenied", got)
		}
	})

	t.Run("a credential nobody minted is refused", func(t *testing.T) {
		headers := machineHeaders(t, a, machineScopes, "not-a-delegation-anybody-minted")

		_, err := orgs.ListMembers(t.Context(), withHeaders(
			connect.NewRequest(&corev1.ListMembersRequest{}), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("an invented delegation got %v, want PermissionDenied", got)
		}
	})
}

// Offboarding somebody stops their agent, not just their browser.
//
// This is the case the design would most plausibly get wrong, because the
// tempting implementation checks membership when the delegation is minted and
// trusts it for the life of the run. A person removed at 14:05 would then keep
// acting until 14:20 through something they are no longer entitled to drive.
func TestLosingYourMembershipStopsTheAgent(t *testing.T) {
	a := newAuthServer(t)
	sessions, orgs, _, _, live := buildDelegationChain(t, a)

	owner := signIn(t, a, sessions, "deleg-boss", "Delegation Boss")
	leaver := joinAs(t, a, live, orgs, owner, "deleg-leaver", "member")

	minted := mintFor(t, live, leaver, "analyst")
	headers := machineHeaders(t, a, machineScopes, minted.Token)

	if _, err := orgs.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), headers)); err != nil {
		t.Fatalf("the agent could not act while the member was still a member: %v", err)
	}

	if _, err := chainMigratorPool(t).Exec(t.Context(),
		`delete from memberships where org_id = $1 and user_id = $2`,
		owner.orgID, userIDOf(t, a, leaver)); err != nil {
		t.Fatalf("removing the membership: %v", err)
	}

	_, err := orgs.ListMembers(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ListMembersRequest{}), headers))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("the agent kept acting after its person was removed and got %v, "+
			"want PermissionDenied", got)
	}
}

// A delegation grants a person's authority and never the platform's.
//
// The token here holds `internal:act-on-behalf`, so if the human set were
// UNIONED with the token's own scopes rather than replacing them, a delegated
// caller would hold both halves and this would pass. It has to be refused.
func TestADelegationCannotReachThePlatformSurface(t *testing.T) {
	a := newAuthServer(t)
	sessions, _, _, ingest, live := buildDelegationChain(t, a)

	ada := signIn(t, a, sessions, "deleg-platform", "Delegation Platform")
	minted := mintFor(t, live, ada, "analyst")

	// Both internal scopes on one token, which is what the seeded Intelligence
	// principal actually holds. The point is that presenting a delegation
	// cannot let it keep them.
	headers := machineHeaders(t, a,
		machineScopes+" "+intelligenceScopes, minted.Token)

	_, err := ingest.RecordAgentRun(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.RecordAgentRunRequest{OrgId: ada.orgID}), headers))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("a delegated call reached the platform surface and got %v, "+
			"want PermissionDenied", got)
	}
}

// Every write made under a delegation names both the agent and the person.
func TestADelegatedWriteNamesTheAgentAndThePerson(t *testing.T) {
	a := newAuthServer(t)
	sessions, _, feed, _, live := buildDelegationChain(t, a)
	conn := seeder(t)

	ada := signIn(t, a, sessions, "deleg-audit", "Delegation Audit")
	finding := seedFinding(t, conn, ada.orgID)

	minted := mintFor(t, live, ada, "analyst")
	headers := machineHeaders(t, a, machineScopes, minted.Token)

	res, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{
			FindingId: finding, Reviewed: true,
		}), headers))
	if err != nil {
		t.Fatalf("an agent acting for the owner could not approve: %v", err)
	}
	if !res.Msg.GetApplied() {
		t.Fatal("the delegated approval reports applied=false")
	}

	var actor, role string
	var agent *string
	if err := conn.QueryRow(t.Context(), `
		select user_id::text, actor_role, acting_agent
		from audit_log where finding_id = $1
	`, finding).Scan(&actor, &role, &agent); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}

	// The person, not the machine. This is the property that makes the delegated
	// approval Ada's own Postgres fact rather than a robot's.
	if want := userIDOf(t, a, ada); actor != want {
		t.Errorf("the audit row names %q as the actor, want the delegated person %q", actor, want)
	}
	if role != "owner" {
		t.Errorf("actor_role is %q, want owner", role)
	}
	if agent == nil || *agent != "analyst" {
		t.Errorf("acting_agent is %v, want analyst", agent)
	}
}

// And a person acting for themselves names no agent, so the column means
// something when it is set.
func TestAnUndelegatedWriteNamesNoAgent(t *testing.T) {
	a := newAuthServer(t)
	sessions, _, feed, _, _ := buildDelegationChain(t, a)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "deleg-self", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	if _, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{
			FindingId: finding, Reviewed: true,
		}), ada.headers)); err != nil {
		t.Fatalf("approving: %v", err)
	}

	var agent *string
	if err := conn.QueryRow(t.Context(),
		`select acting_agent from audit_log where finding_id = $1`, finding).Scan(&agent); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if agent != nil {
		t.Errorf("a person acting for themselves produced acting_agent=%q", *agent)
	}
}

// Recording a run for a person is checked rather than believed (ENT-230).
//
// This is the hole the issue names: `on_behalf_of_user_id` arrived with ENT-218
// and was written straight to the column, so a caller holding
// `internal:intelligence` could put anybody's name on any run. These run on the
// shorter internal chain, exactly as deployed, because recording a run is
// something the machine does as itself.
func TestARunIsRecordedForAPersonOnlyWithThatPersonsDelegation(t *testing.T) {
	a := newAuthServer(t)
	sessions, _, _, _, live := buildDelegationChain(t, a)

	agentPool := requireAgentPool(t)

	scopes := realScopes(t)
	internal := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)
	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewIngestServiceHandler(
		ingestservice.New(nil, agentPool, agentPool, live.store, nil), internal))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	runs := platformv1connect.NewIngestServiceClient(server.Client(), server.URL)

	ada := signIn(t, a, sessions, "deleg-run", "Delegation Run")
	bob := signIn(t, a, sessions, "deleg-run-other", "Delegation Run Other")
	minted := mintFor(t, live, ada, "analyst")

	run := func(orgID, person, token string) *platformv1.RecordAgentRunRequest {
		now := time.Now()
		return &platformv1.RecordAgentRunRequest{
			OrgId:            orgID,
			Skill:            "analyst.narrative",
			SkillVersion:     "1.0.0",
			Model:            "Qwen3.5-4B-Q4_K_M",
			ModelVersion:     "0000000000000000000000000000000000000000000000000000000000000000",
			OnBehalfOfUserId: person,
			Delegation:       token,
			Outcome:          platformv1.AgentRunOutcome_AGENT_RUN_OUTCOME_SUCCEEDED,
			QueuedAt:         timestamppb.New(now.Add(-2 * time.Second)),
			StartedAt:        timestamppb.New(now.Add(-time.Second)),
			FinishedAt:       timestamppb.New(now),
		}
	}

	headers := machineHeaders(t, a, intelligenceScopes, "")

	t.Run("naming a person with no delegation is refused", func(t *testing.T) {
		_, err := runs.RecordAgentRun(t.Context(), withHeaders(
			connect.NewRequest(run(ada.orgID, userIDOf(t, a, ada), "")), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("an unproven claim was recorded and got %v, want PermissionDenied", got)
		}
	})

	t.Run("naming somebody else's delegation is refused", func(t *testing.T) {
		_, err := runs.RecordAgentRun(t.Context(), withHeaders(
			connect.NewRequest(run(ada.orgID, userIDOf(t, a, bob), minted.Token)), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("a run named a person its delegation does not and got %v, "+
				"want PermissionDenied", got)
		}
	})

	t.Run("recording into another organisation is refused", func(t *testing.T) {
		_, err := runs.RecordAgentRun(t.Context(), withHeaders(
			connect.NewRequest(run(bob.orgID, "", minted.Token)), headers))
		if got := codeOf(t, err); got != connect.CodePermissionDenied {
			t.Fatalf("a run was recorded against another tenant and got %v, "+
				"want PermissionDenied", got)
		}
	})

	t.Run("and the person's own delegation records the person", func(t *testing.T) {
		res, err := runs.RecordAgentRun(t.Context(), withHeaders(
			connect.NewRequest(run(ada.orgID, userIDOf(t, a, ada), minted.Token)), headers))
		if err != nil {
			t.Fatalf("recording a delegated run: %v", err)
		}

		var stored string
		if err := chainMigratorPool(t).QueryRow(t.Context(),
			`select on_behalf_of_user_id::text from agent_runs where id = $1`,
			res.Msg.GetId()).Scan(&stored); err != nil {
			t.Fatalf("reading the run back: %v", err)
		}
		if want := userIDOf(t, a, ada); stored != want {
			t.Errorf("the run was recorded for %q, want %q", stored, want)
		}
	})

	t.Run("a sweep still records a run for nobody", func(t *testing.T) {
		// The nullability of the column is load bearing: a scheduled sweep runs
		// for the organisation and for no particular person, and this path must
		// not have been closed by the checks above.
		res, err := runs.RecordAgentRun(t.Context(), withHeaders(
			connect.NewRequest(run(ada.orgID, "", "")), headers))
		if err != nil {
			t.Fatalf("recording a sweep's run: %v", err)
		}

		var stored *string
		if err := chainMigratorPool(t).QueryRow(t.Context(),
			`select on_behalf_of_user_id::text from agent_runs where id = $1`,
			res.Msg.GetId()).Scan(&stored); err != nil {
			t.Fatalf("reading the run back: %v", err)
		}
		if stored != nil {
			t.Errorf("a sweep's run was recorded for %q", *stored)
		}
	})
}

// requireAgentPool opens the producer pool, or skips the way everything else in
// this file does.
func requireAgentPool(t *testing.T) *postgres.AgentStore {
	t.Helper()

	dsn := os.Getenv("PG_AGENT_URL")
	if dsn == "" {
		dsn = "postgres://kindlast_agent:agent-dev-password@127.0.0.1:5433/kindlast"
	}
	store, err := postgres.NewAgent(t.Context(), dsn)
	if err != nil {
		unavailable(t, "agent pool not reachable at %s (%v)", dsn, err)
	}
	t.Cleanup(store.Close)
	return store
}
