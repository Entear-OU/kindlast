package hands_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	handsservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/hands"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// HandsService, without a database and without a model (ENT-261).
//
// The acceptance criterion this issue exists for is a negative one, so the
// tests are arranged around making the caller TRY: name a column the register
// does not have, attribute a value to a fact the organisation never recorded,
// arrive after the approval. Each is refused here rather than by a prompt.

const (
	org     = "11111111-1111-1111-1111-111111111111"
	finding = "22222222-2222-2222-2222-222222222222"
)

// approvals is the agent pool, faked.
//
// It CANNOT approve and it CANNOT create a record, which is not a
// simplification of the real store: `postgres.AgentStore` has no method that
// approves a finding and none that writes a register, and the interface this
// service declares names two methods, both of which are here.
type approvals struct {
	context postgres.ApprovalContext
	err     error

	written  []postgres.Plan
	writeErr error
}

func (a *approvals) ApprovalContextFor(
	_ context.Context, _, _ string,
) (postgres.ApprovalContext, error) {
	return a.context, a.err
}

func (a *approvals) PrepareRecord(
	_ context.Context, _, _ string, plan postgres.Plan,
) (postgres.Plan, error) {
	if a.writeErr != nil {
		return postgres.Plan{}, a.writeErr
	}
	a.written = append(a.written, plan)
	return plan, nil
}

type explainer struct {
	seen     *platformv1.ExplainApprovalRequest
	response *platformv1.ExplainApprovalResponse
	err      error
}

func (e *explainer) ExplainApproval(
	_ context.Context,
	req *connect.Request[platformv1.ExplainApprovalRequest],
) (*connect.Response[platformv1.ExplainApprovalResponse], error) {
	e.seen = req.Msg
	if e.err != nil {
		return nil, e.err
	}
	return connect.NewResponse(e.response), nil
}

func ropaContext() postgres.ApprovalContext {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	return postgres.ApprovalContext{
		Finding: postgres.ApprovalFinding{
			ID:              finding,
			Status:          "pending",
			Severity:        "high",
			Detected:        "You have no record of processing activities.",
			ProposedAction:  "Create an entry for payroll.",
			ActionType:      findings.ActionCreateROPA,
			ObligationSlug:  "gdpr-art-30-ropa",
			ObligationTitle: "Records of processing activities",
			CitationLabel:   "GDPR Art. 30(1)",
		},
		Register: register,
		Facts: []postgres.WatchedFact{
			{Key: "industry", ValueJSON: `"payroll services"`, Source: "onboarding"},
			{Key: "data_categories", ValueJSON: `["names"]`, Source: "onboarding"},
		},
	}
}

// verified is the context every handler expects: the internal chain has already
// checked the token, and a handler reached without one is a wiring bug it
// reports rather than trusts.
func verified() context.Context {
	return interceptor.WithClaims(context.Background(),
		&oidc.Claims{Subject: "the-intelligence-service"})
}

func service(a *approvals, e *explainer) *handsservice.Service {
	var explain handsservice.Explainer
	if e != nil {
		explain = e
	}
	return handsservice.New(a, explain, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// --------------------------------------------------------------------------
// Never decides: the acceptance criterion

// TestNothingOnThisSurfaceCanApproveAFindingOrCreateARecord is the structural
// half of the criterion, and it is deliberately a test about the CONTRACT
// rather than about a handler.
//
// A handler test proves that this implementation does not approve. This proves
// that no implementation could, because the service declares no method that
// would, and the ones that do live behind scopes this principal does not hold.
//
// ENT-245 is why it walks the descriptor rather than the Go type: a test that
// walks a list proves the members, not the list, so what has to be pinned is
// the set of RPCs the service declares at all.
func TestNothingOnThisSurfaceCanApproveAFindingOrCreateARecord(t *testing.T) {
	methods := platformv1.File_kindlast_platform_v1_hands_proto.
		Services().ByName("HandsService").Methods()

	declared := map[string]bool{}
	for i := 0; i < methods.Len(); i++ {
		declared[string(methods.Get(i).Name())] = true
	}

	want := map[string]bool{"ExplainApproval": true, "PrepareRecord": true}
	if len(declared) != len(want) {
		t.Fatalf("HandsService declares %v; want exactly %v.\n\nThe Hands "+
			"explains and prepares, and a third RPC here is a third thing an "+
			"agent can reach. Adding one is a decision, not a detail.",
			keys(declared), keys(want))
	}
	for name := range want {
		if !declared[name] {
			t.Errorf("HandsService no longer declares %s", name)
		}
	}

	// And the names themselves, because a method called `ApproveAndPrepare`
	// would satisfy the count above.
	for i := 0; i < methods.Len(); i++ {
		name := strings.ToLower(string(methods.Get(i).Name()))
		for _, forbidden := range []string{"approve", "reject", "execute", "create"} {
			if strings.Contains(name, forbidden) && name != "explainapproval" {
				t.Errorf("HandsService declares %s, which names %q. The Hands "+
					"never decides: approving is findings:act and creating a "+
					"record is ExecutorService.ExecuteJob, and neither belongs "+
					"here", methods.Get(i).Name(), forbidden)
			}
		}
	}
}

// TestEveryRPCHereIsInternalAndNoneOfThemIsTheActPath pins the scopes.
//
// `findings:act` is the capability that approves, and it is issued only to a
// human's token. An RPC here declaring it would hand a machine principal the
// one thing this whole design exists to keep away from it, and would do so
// without any handler changing.
func TestEveryRPCHereIsInternalAndNoneOfThemIsTheActPath(t *testing.T) {
	methods := platformv1.File_kindlast_platform_v1_hands_proto.
		Services().ByName("HandsService").Methods()

	for i := 0; i < methods.Len(); i++ {
		method := methods.Get(i)
		scope := requiredScope(method)
		if scope != "internal:ingest" {
			t.Errorf("%s declares %q; every RPC here is internal:ingest",
				method.Name(), scope)
		}
		if strings.HasPrefix(scope, "findings:") {
			t.Errorf("%s declares %q, a human scope on the console surface",
				method.Name(), scope)
		}
	}
}

// TestPreparingWritesAPlanAndNothingElse is the behavioural half.
//
// The fake store records every call it received. A prepare that had somehow
// approved would have had to call a method that does not exist on the
// interface, which is why the assertion is about the plan being the only
// write rather than about a status not having changed.
func TestPreparingWritesAPlanAndNothingElse(t *testing.T) {
	store := &approvals{context: ropaContext()}
	svc := service(store, nil)

	_, err := svc.PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{
			OrgId:       org,
			FindingId:   finding,
			Explanation: "Approving this adds one entry to your record.",
			Fields: []*platformv1.PreparedField{{
				Name: "data_categories", Values: []string{"names"},
				FromFact: "data_categories",
			}},
			LeftForYou: []*platformv1.LeftForYou{{
				Name: "retention_period",
				Why:  "You have not told us how long you keep payroll records.",
			}},
		}))
	if err != nil {
		t.Fatalf("preparing a record: %v", err)
	}

	if len(store.written) != 1 {
		t.Fatalf("wrote %d plans; want 1", len(store.written))
	}
	plan := store.written[0]
	if plan.Register.Name != records.RegisterProcessingActivities {
		t.Errorf("the plan names %q; want the register the finding's action "+
			"type names", plan.Register.Name)
	}
	if len(plan.Fields) != 1 || len(plan.LeftForYou) != 1 {
		t.Errorf("the plan is %+v; want one filled column and one left", plan)
	}
}

func TestTheResponseCountsBothWhatWasFilledAndWhatWasLeft(t *testing.T) {
	// A response reporting only what was filled would let a caller render a
	// record that reads as complete, which is the failure this agent exists to
	// fix rather than to reproduce.
	store := &approvals{context: ropaContext()}
	res, err := service(store, nil).PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{
			OrgId: org, FindingId: finding,
			Fields: []*platformv1.PreparedField{{
				Name: "data_categories", Values: []string{"names"},
				FromFact: "data_categories",
			}},
			LeftForYou: []*platformv1.LeftForYou{
				{Name: "purpose", Why: "You have not said what payroll is for."},
				{Name: "retention_period", Why: "You have not said how long."},
			},
		}))
	if err != nil {
		t.Fatalf("preparing: %v", err)
	}
	if res.Msg.GetFilled() != 1 || res.Msg.GetLeft() != 2 {
		t.Errorf("filled %d and left %d; want 1 and 2",
			res.Msg.GetFilled(), res.Msg.GetLeft())
	}
}

// --------------------------------------------------------------------------
// What a plan may say

func TestAColumnTheRegisterDoesNotHaveIsRefused(t *testing.T) {
	store := &approvals{context: ropaContext()}
	_, err := service(store, nil).PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{
			OrgId: org, FindingId: finding,
			Fields: []*platformv1.PreparedField{{
				Name: "annual_revenue", Values: []string{"a lot"}, FromFact: "industry",
			}},
		}))

	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v; want invalid_argument", err)
	}
	if len(store.written) != 0 {
		t.Error("a refused plan was written anyway")
	}
}

func TestAValueFromAFactThisOrganisationDoesNotHoldIsRefused(t *testing.T) {
	// The invariant, checked against the organisation's own rows. The harness
	// checks the same key against what the RUN was offered, which is the
	// guardrail, and the two refuse different things.
	store := &approvals{context: ropaContext()}
	_, err := service(store, nil).PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{
			OrgId: org, FindingId: finding,
			Fields: []*platformv1.PreparedField{{
				Name: "purpose", Values: []string{"Paying people"},
				FromFact: "staff_count",
			}},
		}))

	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v; want invalid_argument", err)
	}
	if !strings.Contains(err.Error(), "staff_count") {
		t.Errorf("the refusal does not name the fact: %v", err)
	}
	if len(store.written) != 0 {
		t.Error("a plan citing a fact that does not exist was written anyway")
	}
}

// TestTheRegisterIsReadFromTheFindingAndNotTakenFromTheCaller is the reason
// PrepareRecord re-reads the context it was just asked about.
//
// A caller that could name its own register would be choosing which columns it
// is allowed to write, which turns the whole vocabulary into a suggestion. The
// request message deliberately carries no register field, and this asserts that
// the one used comes from the finding's action type.
func TestTheRegisterIsReadFromTheFindingAndNotTakenFromTheCaller(t *testing.T) {
	aiRegister, _ := records.RegisterFor(findings.ActionCreateAISystem)
	ctx := ropaContext()
	ctx.Finding.ActionType = findings.ActionCreateAISystem
	ctx.Register = aiRegister

	store := &approvals{context: ctx}

	// `data_categories` is a ROPA column and is not one of the AI register's.
	_, err := service(store, nil).PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{
			OrgId: org, FindingId: finding,
			Fields: []*platformv1.PreparedField{{
				Name: "data_categories", Values: []string{"names"},
				FromFact: "data_categories",
			}},
		}))

	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v; want invalid_argument", err)
	}
}

// TestAPlanArrivingAfterTheApprovalIsRefused is the "cannot change what a
// person approved" half.
//
// The store refuses once an `executor_jobs` row exists, which is the instant
// the approval became true (00036). Reported as failed_precondition rather
// than internal, because nothing broke: a person decided while the run was
// thinking, and the guardrail held.
func TestAPlanArrivingAfterTheApprovalIsRefused(t *testing.T) {
	store := &approvals{
		context:  ropaContext(),
		writeErr: postgres.ErrAlreadyEnqueued,
	}
	_, err := service(store, nil).PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{
			OrgId: org, FindingId: finding,
			Fields: []*platformv1.PreparedField{{
				Name: "data_categories", Values: []string{"names"},
				FromFact: "data_categories",
			}},
		}))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v; want failed_precondition", err)
	}
}

func TestAFindingWhoseApprovalCreatesNothingIsRefusedBeforeAModelIsCalled(t *testing.T) {
	// A `review` finding is approved and creates nothing. Running a model over
	// it would produce a description of a record that will not exist, which is
	// worse than no explanation at all.
	e := &explainer{response: &platformv1.ExplainApprovalResponse{}}
	store := &approvals{err: postgres.ErrNothingToPrepare}

	_, err := service(store, e).ExplainApproval(verified(), connect.NewRequest(
		&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding}))

	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v; want invalid_argument", err)
	}
	if e.seen != nil {
		t.Error("a model was asked about a finding that creates no record")
	}
}

func TestAnUnknownFindingIsTheSameAnswerAsOneInAnotherOrganisation(t *testing.T) {
	// One answer for both, deliberately: the difference is exactly what
	// probing for a tenancy leak looks like.
	store := &approvals{err: postgres.ErrNoSuchFinding}
	_, err := service(store, &explainer{}).ExplainApproval(
		verified(), connect.NewRequest(
			&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding}))

	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v; want invalid_argument", err)
	}
}

// --------------------------------------------------------------------------
// What the agent is shown, and what it is not

func TestTheContextOffersTheRegistersColumnsAndTheOrganisationsFacts(t *testing.T) {
	// The offered set is what the harness validates against, so what is put in
	// it is load bearing rather than presentational: a fact absent here is a
	// fact the run may not claim to have used, whatever the database holds.
	e := &explainer{response: &platformv1.ExplainApprovalResponse{
		Outcome: platformv1.ExplainOutcome_EXPLAIN_OUTCOME_SUCCEEDED,
	}}
	store := &approvals{context: ropaContext()}

	if _, err := service(store, e).ExplainApproval(
		verified(), connect.NewRequest(
			&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding}),
	); err != nil {
		t.Fatalf("explaining: %v", err)
	}

	got := e.seen.GetContext()
	if got.GetRegister() != records.RegisterProcessingActivities {
		t.Errorf("the context names %q as the register", got.GetRegister())
	}
	if len(got.GetFields()) == 0 {
		t.Error("the context offers no columns, so the run can fill nothing")
	}
	if len(got.GetFacts()) != 2 {
		t.Errorf("the context offers %d facts; want the two the store held",
			len(got.GetFacts()))
	}
	if got.GetFinding().GetFindingId() != finding {
		t.Error("the context does not name the finding it is about")
	}
}

func TestADeploymentWithNoIntelligenceSaysSoRatherThanExplainingNothing(t *testing.T) {
	// An empty explanation would read as "there is nothing to say about
	// approving this", which is a claim about the finding rather than about
	// the deployment.
	store := &approvals{context: ropaContext()}
	_, err := service(store, nil).ExplainApproval(verified(), connect.NewRequest(
		&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding}))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v; want failed_precondition", err)
	}
}

func TestIntelligenceBeingUnreachableIsNotReportedAsARefusedRun(t *testing.T) {
	// A transport failure is not a run that was refused: there may be no run
	// at all, and reporting one as REFUSED would put a guardrail's name on a
	// network problem in the column a customer reads to decide whether to
	// trust the result.
	e := &explainer{err: errors.New("connection refused")}
	store := &approvals{context: ropaContext()}

	res, err := service(store, e).ExplainApproval(verified(), connect.NewRequest(
		&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding}))

	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("got %v; want unavailable", err)
	}
	if res != nil {
		t.Error("a response was returned alongside a transport error")
	}
}

func TestARefusedRunComesBackAsAnOutcomeAndNotAnError(t *testing.T) {
	// §26.3: a refusal is what a working guardrail produces. Returning an
	// error code for it would tell the caller the harness broke when it did
	// precisely what it was built to do.
	e := &explainer{response: &platformv1.ExplainApprovalResponse{
		Outcome:       platformv1.ExplainOutcome_EXPLAIN_OUTCOME_REFUSED,
		OutcomeDetail: "tool 'approve_finding' is not in this skill's allow-list",
		AgentRunId:    "33333333-3333-3333-3333-333333333333",
	}}
	store := &approvals{context: ropaContext()}

	res, err := service(store, e).ExplainApproval(verified(), connect.NewRequest(
		&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding}))
	if err != nil {
		t.Fatalf("a refusal arrived as an error: %v", err)
	}
	if res.Msg.GetOutcome() != platformv1.HandsOutcome_HANDS_OUTCOME_REFUSED {
		t.Errorf("got outcome %v; want REFUSED", res.Msg.GetOutcome())
	}
	// AND THE RUN IS STILL NAMED (ENT-277). A refused run leaves an
	// `agent_runs` row like any other, and a caller that cannot reach it
	// cannot answer "what happened".
	if res.Msg.GetAgentRunId() == "" {
		t.Error("a refused run came back with no agent_runs id")
	}
}

func TestAHandlerReachedWithNoVerifiedIdentityRefuses(t *testing.T) {
	store := &approvals{context: ropaContext()}
	svc := service(store, &explainer{})

	if _, err := svc.ExplainApproval(context.Background(), connect.NewRequest(
		&platformv1.HandsExplainRequest{OrgId: org, FindingId: finding},
	)); connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("ExplainApproval: got %v; want internal", err)
	}
	if _, err := svc.PrepareRecord(context.Background(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{OrgId: org, FindingId: finding},
	)); connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("PrepareRecord: got %v; want internal", err)
	}
}

func TestBothRPCsRequireAnOrganisationAndAFinding(t *testing.T) {
	svc := service(&approvals{context: ropaContext()}, &explainer{})

	if _, err := svc.ExplainApproval(verified(), connect.NewRequest(
		&platformv1.HandsExplainRequest{OrgId: org},
	)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("ExplainApproval with no finding: got %v", err)
	}
	if _, err := svc.PrepareRecord(verified(), connect.NewRequest(
		&platformv1.PrepareRecordRequest{FindingId: finding},
	)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("PrepareRecord with no organisation: got %v", err)
	}
}

func requiredScope(method protoreflect.MethodDescriptor) string {
	options := method.Options()
	if options == nil {
		return ""
	}
	// Rendered rather than read through the extension type, because the
	// extension lives in `kindlast/options/v1` and this test only needs to
	// know which string was declared.
	text := options.(interface{ String() string }).String()
	const marker = `required_scope]:"`
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	rest := text[start+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}
