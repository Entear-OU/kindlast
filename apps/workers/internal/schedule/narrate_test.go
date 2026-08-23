package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/testsuite"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Narration as the third step of a sweep (ENT-256, part five), in the SDK's
// test environment. What is asserted: it runs after the trigger is settled,
// one finding per activity until none is pending, it stops at once on a
// deployment without Intelligence, a provider that cannot be honoured is
// recorded without failing the sweep, and the daily run narrates each swept
// organisation once.

// fakeNarrator is core-api's NarrativeService minus the model: it holds a
// number of findings awaiting narrative per organisation and drafts one per
// call.
type fakeNarrator struct {
	mu          sync.Mutex
	pending     map[string]int32
	unavailable bool
	refuse      map[string]error
	calls       []string
	maxAsked    int32
}

func (f *fakeNarrator) NarrateFindings(
	_ context.Context, req *connect.Request[platformv1.NarrateFindingsRequest],
) (*connect.Response[platformv1.NarrateFindingsResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org := req.Msg.GetOrgId()
	f.calls = append(f.calls, org)
	if req.Msg.GetMaxFindings() > f.maxAsked {
		f.maxAsked = req.Msg.GetMaxFindings()
	}
	if err := f.refuse[org]; err != nil {
		return nil, err
	}
	if f.unavailable {
		return connect.NewResponse(&platformv1.NarrateFindingsResponse{IntelligenceAvailable: false}), nil
	}
	left := f.pending[org]
	if left == 0 {
		return connect.NewResponse(&platformv1.NarrateFindingsResponse{IntelligenceAvailable: true}), nil
	}
	f.pending[org] = left - 1
	return connect.NewResponse(&platformv1.NarrateFindingsResponse{
		IntelligenceAvailable: true, Attempted: 1, Narrated: 1,
	}), nil
}

func registerNarration(env *testsuite.TestWorkflowEnvironment, a *Activities) {
	registerSweeps(env, a)
	env.RegisterActivityWithOptions(a.NarrateFindings, activityOptions(NarrateFindingsActivityName))
}

func TestATriggeredSweepNarratesAfterSettlingOneFindingPerActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{}
	narrator := &fakeNarrator{pending: map[string]int32{"org-a": 3}}
	registerNarration(env, &Activities{Sweeps: sweeps, Narratives: narrator})

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if !result.Narration.Available || result.Narration.Narrated != 3 {
		t.Fatalf("narration = %+v, want 3 narrated", result.Narration)
	}
	// Three drafts and one "nothing left" answer, one finding asked for each
	// time.
	if len(narrator.calls) != 4 || narrator.maxAsked != 1 {
		t.Fatalf("narrate was called %d times asking for at most %d, want 4 calls of 1", len(narrator.calls), narrator.maxAsked)
	}
	// And the trigger was settled before any of it.
	if len(sweeps.settled) != 1 {
		t.Fatalf("settled %v, want the trigger settled", sweeps.settled)
	}
}

func TestADeploymentWithoutIntelligenceCostsOneActivityAndNothingIsWrong(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	narrator := &fakeNarrator{unavailable: true}
	registerNarration(env, &Activities{Sweeps: &fakeSweeper{}, Narratives: narrator})

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed on a stack without Intelligence: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Narration.Available || result.Narration.Skipped != "" || len(narrator.calls) != 1 {
		t.Fatalf("narration = %+v after %d calls; want unavailable, not skipped, one call", result.Narration, len(narrator.calls))
	}
}

// An organisation whose provider cannot be honoured (failed_precondition) is
// recorded as skipped and the sweep still succeeds: the guardrail working is
// not a failed sweep.
func TestAProviderThatCannotBeHonouredSkipsNarrationWithoutFailingTheSweep(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	narrator := &fakeNarrator{refuse: map[string]error{
		"org-a": connect.NewError(connect.CodeFailedPrecondition, errors.New("this organisation provider key cannot be opened")),
	}}
	registerNarration(env, &Activities{Sweeps: &fakeSweeper{}, Narratives: narrator})

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a skipped narration failed the sweep: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Narration.Skipped == "" || result.Narration.Narrated != 0 {
		t.Fatalf("narration = %+v, want skipped with the reason", result.Narration)
	}
	if len(narrator.calls) != 1 {
		t.Fatalf("narrate was called %d times for a refusal, want exactly 1", len(narrator.calls))
	}
}

func TestTheDailySweepNarratesEachSweptOrganisationAfterTheFanOut(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{targets: []string{"org-a", "org-b"}}
	narrator := &fakeNarrator{pending: map[string]int32{"org-a": 2, "org-b": 1}}
	registerNarration(env, &Activities{Sweeps: sweeps, Narratives: narrator})

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
	// Two drafts for a, one for b, and one "nothing left" answer each: five
	// calls, every one asking for a single finding.
	if len(narrator.calls) != 5 || narrator.maxAsked != 1 {
		t.Fatalf("narrate was called %d times asking for at most %d, want 5 calls of 1", len(narrator.calls), narrator.maxAsked)
	}
}
