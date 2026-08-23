package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Narration as the third step of a sweep (ENT-256, part five), in the SDK's
// test environment: Go loads, Python drafts, Go persists. The Python activity
// is stubbed under the name the Python worker registers, which is what lets
// the chain be asserted without a Python process; what is asserted is the
// chain's behaviour (the order, what crosses each boundary, what each answer
// does), not the draft.

// fakeNarrator is core-api's NarrativeService minus the model: a list of
// findings awaiting narrative per organisation, offered one at a time until
// something is recorded for each.
type fakeNarrator struct {
	mu          sync.Mutex
	pending     map[string][]string
	unavailable bool
	refuse      map[string]error
	loads       int
	recorded    []*platformv1.RecordNarrativeRequest
}

func (f *fakeNarrator) NextFindingToNarrate(
	_ context.Context, req *connect.Request[platformv1.NextFindingToNarrateRequest],
) (*connect.Response[platformv1.NextFindingToNarrateResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org := req.Msg.GetOrgId()
	f.loads++
	if err := f.refuse[org]; err != nil {
		return nil, err
	}
	if f.unavailable {
		return connect.NewResponse(&platformv1.NextFindingToNarrateResponse{IntelligenceAvailable: false}), nil
	}
	left := f.pending[org]
	if len(left) == 0 {
		return connect.NewResponse(&platformv1.NextFindingToNarrateResponse{IntelligenceAvailable: true}), nil
	}
	return connect.NewResponse(&platformv1.NextFindingToNarrateResponse{
		IntelligenceAvailable: true,
		Found:                 true,
		FindingId:             left[0],
		Draft: &platformv1.DraftNarrativeRequest{
			OrgId:  org,
			Signal: "finding " + left[0],
			Obligations: []*platformv1.ObligationContext{{
				Slug: "gdpr-art-30-ropa", Title: "ROPA", Summary: "Keep a record.",
			}},
			ModelEndpoint: &platformv1.ModelEndpoint{Provider: "instance", Model: "local"},
		},
	}), nil
}

func (f *fakeNarrator) RecordNarrative(
	_ context.Context, req *connect.Request[platformv1.RecordNarrativeRequest],
) (*connect.Response[platformv1.RecordNarrativeResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, req.Msg)
	// Recorded: no longer pending.
	org := req.Msg.GetOrgId()
	var rest []string
	for _, id := range f.pending[org] {
		if id != req.Msg.GetFindingId() {
			rest = append(rest, id)
		}
	}
	f.pending[org] = rest
	return connect.NewResponse(&platformv1.RecordNarrativeResponse{Recorded: true}), nil
}

// fakeDrafter stands in for the Python worker's DraftNarrative activity: it
// answers with an outcome per finding and records what it was handed.
type fakeDrafter struct {
	mu       sync.Mutex
	outcomes map[string]platformv1.DraftOutcome
	handed   []*platformv1.DraftNarrativeRequest
}

func (d *fakeDrafter) draft(_ context.Context, req *platformv1.DraftNarrativeRequest) (*platformv1.DraftNarrativeResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handed = append(d.handed, req)
	outcome := platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED
	if o, ok := d.outcomes[req.GetSignal()]; ok {
		outcome = o
	}
	res := &platformv1.DraftNarrativeResponse{Outcome: outcome, AgentRunId: "run-" + req.GetSignal()}
	if outcome == platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED {
		res.Narrative = "Because you hold personal data."
	} else {
		res.OutcomeDetail = "the model cited an article it was not offered"
	}
	return res, nil
}

func registerNarration(env *testsuite.TestWorkflowEnvironment, a *Activities, drafter *fakeDrafter) {
	registerSweeps(env, a)
	env.RegisterActivityWithOptions(a.NextFindingToNarrate, activityOptions(NextFindingToNarrateActivityName))
	env.RegisterActivityWithOptions(a.RecordNarrative, activityOptions(RecordNarrativeActivityName))
	if drafter != nil {
		// The Python activity, by its registered name. In production nothing
		// in this binary registers it; the test environment does not route
		// by task queue, so a stub under the name is what stands in.
		env.RegisterActivityWithOptions(drafter.draft, activityOptions(DraftNarrativeActivityName))
	}
}

func TestATriggeredSweepNarratesAfterSettlingGoLoadsPythonDraftsGoPersists(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{}
	narrator := &fakeNarrator{pending: map[string][]string{"org-a": {"f1", "f2", "f3"}}}
	drafter := &fakeDrafter{outcomes: map[string]platformv1.DraftOutcome{
		"finding f2": platformv1.DraftOutcome_DRAFT_OUTCOME_REFUSED,
	}}
	registerNarration(env, &Activities{Sweeps: sweeps, Narratives: narrator}, drafter)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if !result.Narration.Available || result.Narration.Narrated != 2 || result.Narration.Refused != 1 {
		t.Fatalf("narration = %+v, want 2 narrated, 1 refused", result.Narration)
	}
	// The trigger was settled before any of it.
	if len(sweeps.settled) != 1 {
		t.Fatalf("settled %v, want the trigger settled", sweeps.settled)
	}
	// Three drafts, each handed the request core-api built: the finding's
	// words, its one obligation, the provider name, and no key anywhere.
	if len(drafter.handed) != 3 {
		t.Fatalf("the Python activity was handed %d drafts, want 3", len(drafter.handed))
	}
	for _, req := range drafter.handed {
		if req.GetOrgId() != "org-a" || len(req.GetObligations()) != 1 || req.GetModelEndpoint().GetProvider() != "instance" {
			t.Fatalf("the draft request was %+v", req)
		}
		if req.GetModelEndpoint().GetApiKey() != "" || req.GetModelEndpoint().GetBaseUrl() != "" { //nolint:staticcheck // asserting the deprecated fields stay empty
			t.Fatal("an endpoint or key crossed into the draft activity's input")
		}
	}
	// Three records, the refusal among them with its reason, and four loads
	// (three findings and one "nothing left").
	if len(narrator.recorded) != 3 || narrator.loads != 4 {
		t.Fatalf("recorded %d, loaded %d; want 3 and 4", len(narrator.recorded), narrator.loads)
	}
	var refusals int
	for _, r := range narrator.recorded {
		if r.GetDraft().GetOutcome() == platformv1.DraftOutcome_DRAFT_OUTCOME_REFUSED && r.GetDraft().GetOutcomeDetail() != "" {
			refusals++
		}
	}
	if refusals != 1 {
		t.Fatalf("recorded %d refusals with a reason, want 1", refusals)
	}
}

func TestADeploymentWithoutIntelligenceCostsOneActivityAndNothingIsWrong(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	narrator := &fakeNarrator{unavailable: true}
	drafter := &fakeDrafter{}
	registerNarration(env, &Activities{Sweeps: &fakeSweeper{}, Narratives: narrator}, drafter)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed on a stack without Intelligence: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Narration.Available || result.Narration.Skipped != "" || narrator.loads != 1 || len(drafter.handed) != 0 {
		t.Fatalf("narration = %+v after %d loads and %d drafts; want unavailable, one load, no draft", result.Narration, narrator.loads, len(drafter.handed))
	}
}

// An organisation whose provider cannot be honoured (failed_precondition at
// the load step) is recorded as skipped and the sweep still succeeds.
func TestAProviderThatCannotBeHonouredSkipsNarrationWithoutFailingTheSweep(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	narrator := &fakeNarrator{refuse: map[string]error{
		"org-a": connect.NewError(connect.CodeFailedPrecondition, errors.New("this organisation provider key cannot be opened")),
	}}
	drafter := &fakeDrafter{}
	registerNarration(env, &Activities{Sweeps: &fakeSweeper{}, Narratives: narrator}, drafter)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a skipped narration failed the sweep: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Narration.Skipped == "" || len(drafter.handed) != 0 || narrator.loads != 1 {
		t.Fatalf("narration = %+v, loads %d, drafts %d; want skipped with the reason after one load", result.Narration, narrator.loads, len(drafter.handed))
	}
}

// Nobody polling the `intelligence` queue: the draft's schedule-to-start
// timeout fires, narration is skipped with that reason, and the sweep is not
// failed. The finding stays un-narrated and the next sweep asks again.
func TestNoPythonWorkerIsSkippedWithTheReasonRatherThanHangingTheSweep(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	narrator := &fakeNarrator{pending: map[string][]string{"org-a": {"f1"}}}
	activities := &Activities{Sweeps: &fakeSweeper{}, Narratives: narrator}
	registerNarration(env, activities, nil)
	// The stub answers as the engine does when nobody picked the task up in
	// time: a timeout error of the schedule-to-start kind.
	env.RegisterActivityWithOptions(func(context.Context, *platformv1.DraftNarrativeRequest) (*platformv1.DraftNarrativeResponse, error) {
		return nil, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START, nil)
	}, activityOptions(DraftNarrativeActivityName))

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a missing worker failed the sweep: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Narration.Skipped == "" || len(narrator.recorded) != 0 {
		t.Fatalf("narration = %+v with %d records; want skipped with a reason and nothing recorded", result.Narration, len(narrator.recorded))
	}
	if result.Narration.Skipped != "no Intelligence worker is polling the intelligence task queue" {
		t.Fatalf("reason = %q", result.Narration.Skipped)
	}
}

func TestTheDailySweepNarratesEachSweptOrganisationAfterTheFanOut(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{targets: []string{"org-a", "org-b"}}
	narrator := &fakeNarrator{pending: map[string][]string{"org-a": {"a1", "a2"}, "org-b": {"b1"}}}
	drafter := &fakeDrafter{}
	registerNarration(env, &Activities{Sweeps: sweeps, Narratives: narrator}, drafter)

	env.ExecuteWorkflow(DailySweepWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the daily sweep failed: %v", err)
	}
	var result DailySweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Swept != 2 || result.Narrated != 3 || len(result.NarrationSkipped) != 0 {
		t.Fatalf("result = %+v, want 2 swept, 3 narrated, none skipped", result)
	}
	if len(drafter.handed) != 3 || len(narrator.recorded) != 3 {
		t.Fatalf("drafted %d, recorded %d; want 3 and 3", len(drafter.handed), len(narrator.recorded))
	}
}
