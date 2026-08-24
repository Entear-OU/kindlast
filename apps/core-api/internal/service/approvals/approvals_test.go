package approvals

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

const orgID = "1961c05f-5e88-4f2f-92a1-d26600e0bcd0"

// The customer-facing half of the Hands (ENT-278).
//
// What these assert is the seam, because the run itself is already covered
// where it lives: `service/hands` proves what the agent may and may not do, and
// `store/postgres/hands_test.go` proves it against a database. What is new here
// is a browser reaching it, so these are about the three things that changes.
//
// THE ORGANISATION IS NEVER TAKEN FROM THE CALLER. The platform surface lets a
// machine name its own organisation because `internal:ingest` never reaches a
// browser. This request carries no organisation at all, and the handler must
// take it from the tenancy interceptor.
//
// A FINDING SOMEBODY CANNOT SEE ANSWERS AS ABSENT, before a model is asked
// anything, so this cannot become a way to spend another organisation's budget
// or to learn whether one of their findings exists.
//
// AND A REFUSAL IS NOT A FAULT, which is the distinction the whole surface is
// drawn around.

type fakeTenant struct {
	finding domain.Finding
	err     error
	asked   []string
}

func (f *fakeTenant) OrgID() string                  { return orgID }
func (f *fakeTenant) Role() string                   { return "member" }
func (f *fakeTenant) UserID() string                 { return "user-1" }
func (f *fakeTenant) Commit(context.Context) error   { return nil }
func (f *fakeTenant) Rollback(context.Context) error { return nil }

func (f *fakeTenant) Finding(
	_ context.Context, findingID string,
) (domain.Finding, []domain.SupportingChunk, error) {
	f.asked = append(f.asked, findingID)
	if f.err != nil {
		return domain.Finding{}, nil, f.err
	}
	return f.finding, nil, nil
}

// fakeHands is the platform service, as much of it as this caller uses.
type fakeHands struct {
	response *platformv1.HandsExplainResponse
	err      error
	requests []*platformv1.HandsExplainRequest
}

func (f *fakeHands) ExplainApproval(
	_ context.Context,
	req *connect.Request[platformv1.HandsExplainRequest],
) (*connect.Response[platformv1.HandsExplainResponse], error) {
	f.requests = append(f.requests, req.Msg)
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(f.response), nil
}

func aFinding() domain.Finding {
	return domain.Finding{
		ID:             "f-1",
		Status:         "pending",
		Severity:       "high",
		ActionType:     "create_ropa",
		Detected:       "No record of processing activities exists for payroll.",
		ProposedAction: "Create a record covering payroll.",
	}
}

func anExplanation() *platformv1.HandsExplainResponse {
	return &platformv1.HandsExplainResponse{
		Outcome: platformv1.HandsOutcome_HANDS_OUTCOME_SUCCEEDED,
		Explanation: "Approving this adds one entry to your Article 30 record, " +
			"covering the payroll you run in house.",
		Prepared: []*platformv1.PreparedField{{
			Name:     "legal_basis",
			Values:   []string{"legal obligation"},
			FromFact: "payroll.legal_basis",
		}},
		LeftForYou: []*platformv1.LeftForYou{{
			Name: "retention_period",
			Why:  "you have not told us how long you keep payroll records",
		}},
		AgentRunId: "11111111-1111-4111-8111-111111111111",
	}
}

func ctxWith(tenant interceptor.Tenant) context.Context {
	ctx := interceptor.WithClaims(context.Background(), &oidc.Claims{Subject: "user-1"})
	return interceptor.WithTenant(ctx, tenant)
}

func explain(
	t *testing.T, service *Service, tenant interceptor.Tenant,
) (*corev1.ExplainApprovalResponse, error) {
	t.Helper()
	response, err := service.ExplainApproval(ctxWith(tenant), connect.NewRequest(
		&corev1.ExplainApprovalRequest{FindingId: "f-1"},
	))
	if err != nil {
		return nil, err
	}
	return response.Msg, nil
}

func TestTheOrganisationComesFromTheTenancyAndNotFromTheCaller(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}
	hands := &fakeHands{response: anExplanation()}

	if _, err := explain(t, New(hands, nil), tenant); err != nil {
		t.Fatalf("explaining: %v", err)
	}

	if len(hands.requests) != 1 {
		t.Fatalf("asked the Hands %d times, want 1", len(hands.requests))
	}
	// The request the browser sent carries no organisation, and this one does.
	// It came from the tenancy interceptor, which resolved it from the header
	// against the caller's own memberships.
	if got := hands.requests[0].GetOrgId(); got != orgID {
		t.Errorf("ran for org %q, want %q", got, orgID)
	}
	if got := hands.requests[0].GetFindingId(); got != "f-1" {
		t.Errorf("ran for finding %q, want f-1", got)
	}
}

func TestAFindingTheCallerCannotSeeIsAbsentAndCostsNoRun(t *testing.T) {
	tenant := &fakeTenant{err: pgx.ErrNoRows}
	hands := &fakeHands{response: anExplanation()}

	_, err := explain(t, New(hands, nil), tenant)
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("got %v, want not_found", err)
	}
	// The read happens through the caller's own transaction, so a finding in
	// another organisation is the same answer as one that never existed. And
	// nothing was asked of a model, which is what stops this being a way to
	// spend somebody else's budget by guessing ids.
	if len(hands.requests) != 0 {
		t.Errorf("asked the Hands about a finding the caller cannot see")
	}
}

func TestAFindingThatCreatesNoRecordIsRefusedWithAReason(t *testing.T) {
	finding := aFinding()
	finding.ActionType = "review"
	tenant := &fakeTenant{finding: finding}
	hands := &fakeHands{response: anExplanation()}

	_, err := explain(t, New(hands, nil), tenant)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v, want failed_precondition", err)
	}
	// Approving a review finding records the decision and creates nothing, so
	// there is no record to explain. A run would have produced an explanation
	// of a record that will not exist, which is worse than no explanation.
	if len(hands.requests) != 0 {
		t.Errorf("asked the Hands to explain a record that will not be created")
	}
}

func TestADeploymentWithNoIntelligenceSaysSoRatherThanFailing(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}

	msg, err := explain(t, New(nil, nil), tenant)
	if err != nil {
		t.Fatalf("explaining without Intelligence: %v", err)
	}
	// Not an error. The model sits behind a compose profile, so a stack can run
	// without it, and "this deployment runs no model" is a different sentence
	// from "the Hands would not explain" with a different thing to do about it.
	if msg.GetIntelligenceAvailable() {
		t.Errorf("claimed a model this deployment does not run")
	}
	if msg.GetExplanation() != "" {
		t.Errorf("explained something with no model to explain it with")
	}
}

func TestASuccessCarriesThePlanTheRunProducedAndTheRunItLeft(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}

	msg, err := explain(t, New(&fakeHands{response: anExplanation()}, nil), tenant)
	if err != nil {
		t.Fatalf("explaining: %v", err)
	}

	if msg.GetOutcome() != corev1.ExplainOutcome_EXPLAIN_OUTCOME_SUCCEEDED {
		t.Fatalf("outcome %v, want succeeded", msg.GetOutcome())
	}
	if !msg.GetIntelligenceAvailable() {
		t.Errorf("a run happened and the response says there is no model")
	}
	if msg.GetExplanation() == "" {
		t.Errorf("no explanation on a successful run")
	}
	// The id, because `agent_runs` has no read path and this is the reference a
	// support conversation quotes.
	if msg.GetAgentRunId() != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("run id %q", msg.GetAgentRunId())
	}

	// BOTH HALVES OF THE PLAN. A response listing only what was filled lets a
	// console draw a record that reads as complete, which is the failure this
	// agent exists to fix.
	if len(msg.GetPrepared()) != 1 || len(msg.GetLeftForYou()) != 1 {
		t.Fatalf("plan carried %d filled and %d left, want 1 and 1",
			len(msg.GetPrepared()), len(msg.GetLeftForYou()))
	}
	if got := msg.GetPrepared()[0].GetFromFact(); got != "payroll.legal_basis" {
		t.Errorf("prepared field cites %q, want the fact it came from", got)
	}
}

func TestEveryColumnIsNamedInWordsCoreAPIWroteRatherThanTheModel(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}

	msg, err := explain(t, New(&fakeHands{response: anExplanation()}, nil), tenant)
	if err != nil {
		t.Fatalf("explaining: %v", err)
	}

	// The register and its columns are authored in `domain/records`, so the
	// sentence a customer reads about what approving does is one this product
	// wrote. A console left to render `legal_basis` would be showing a column
	// name, and a model asked to name the register would be stating what the
	// product does, which it is not the authority on.
	if msg.GetRegisterLabel() != "your Article 30 record of processing activities" {
		t.Errorf("register label %q", msg.GetRegisterLabel())
	}
	if got := msg.GetPrepared()[0].GetLabel(); got != "the lawful basis you rely on" {
		t.Errorf("prepared field label %q", got)
	}
	if got := msg.GetLeftForYou()[0].GetLabel(); got != "how long you keep it" {
		t.Errorf("left field label %q", got)
	}
}

func TestARefusalIsAnOutcomeAndNotAnError(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}
	hands := &fakeHands{response: &platformv1.HandsExplainResponse{
		Outcome:       platformv1.HandsOutcome_HANDS_OUTCOME_REFUSED,
		OutcomeDetail: "the run asked for a tool it was not offered",
		AgentRunId:    "22222222-2222-4222-8222-222222222222",
	}}

	msg, err := explain(t, New(hands, nil), tenant)
	if err != nil {
		t.Fatalf("a refusal arrived as a transport error: %v", err)
	}
	// §26.3: a refusal is what a working guardrail produces. Returning it as an
	// error code would make a console draw the product's most important
	// behaviour as a fault.
	if msg.GetOutcome() != corev1.ExplainOutcome_EXPLAIN_OUTCOME_REFUSED {
		t.Errorf("outcome %v, want refused", msg.GetOutcome())
	}
	if msg.GetOutcomeDetail() == "" {
		t.Errorf("a refusal with no reason leaves the person nothing to read")
	}
	// A refused run is still a run, and showing it is what makes the refusal
	// checkable rather than a sentence to take on faith.
	if msg.GetAgentRunId() == "" {
		t.Errorf("a refusal with no run record")
	}
	if msg.GetExplanation() != "" {
		t.Errorf("a refused explanation reached the caller")
	}
}

func TestTheHandsBeingUnreachableIsNotDrawnAsARefusal(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}
	hands := &fakeHands{err: errors.New("dial tcp: connection refused")}

	_, err := explain(t, New(hands, nil), tenant)
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("got %v, want unavailable", err)
	}
	// A transport failure may be no run at all. Reporting it as REFUSED would
	// put a guardrail's name on a network problem, in the column a customer
	// reads to decide whether to trust the result.
}

func TestAnUnhonourableModelChoiceRefusesRatherThanFallingBack(t *testing.T) {
	tenant := &fakeTenant{finding: aFinding()}
	hands := &fakeHands{err: connect.NewError(connect.CodeFailedPrecondition,
		errors.New("this organisation chose a provider with no key"))}

	_, err := explain(t, New(hands, nil), tenant)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v, want failed_precondition", err)
	}
	// Nothing broke, and the reason is worth reading: an organisation whose
	// chosen provider cannot be honoured must not have its memory processed on
	// a model it did not choose.
}
