package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/timestamppb"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The Watcher-to-Analyst chain (ENT-256, part four), in the SDK's test
// environment: what is asserted is the shape of the chain (Watcher then
// Analyst, each naming its organisation; the trigger settled; the daily
// fan-out continuing past a failed organisation), not the engine.

// fakeSweeper is core-api's SweepService, minus core-api. It records every
// call with the organisation header it carried, and can be told to refuse
// one organisation outright.
type fakeSweeper struct {
	mu       sync.Mutex
	calls    []string // "watch:<org>" / "analyse:<org>" in order
	refuse   map[string]error
	triggers []*platformv1.SweepTrigger
	targets  []string
	settled  []*platformv1.SettleSweepTriggerRequest
}

func (f *fakeSweeper) record(kind string, org string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, kind+":"+org)
}

func (f *fakeSweeper) RunSweep(
	_ context.Context, req *connect.Request[platformv1.RunSweepRequest],
) (*connect.Response[platformv1.RunSweepResponse], error) {
	org := req.Header().Get(OrgHeader)
	f.record("watch", org)
	if err := f.refuse[org]; err != nil {
		return nil, err
	}
	if !req.Msg.GetDetectOnly() {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("the workflow's watcher step must be detect_only"))
	}
	return connect.NewResponse(&platformv1.RunSweepResponse{Signals: 3, RanAt: timestamppb.Now()}), nil
}

func (f *fakeSweeper) RunAnalyst(
	_ context.Context, req *connect.Request[platformv1.RunAnalystRequest],
) (*connect.Response[platformv1.RunAnalystResponse], error) {
	org := req.Header().Get(OrgHeader)
	f.record("analyse", org)
	return connect.NewResponse(&platformv1.RunAnalystResponse{Findings: 2, RanAt: timestamppb.Now()}), nil
}

func (f *fakeSweeper) ListSweepTriggers(
	_ context.Context, _ *connect.Request[platformv1.ListSweepTriggersRequest],
) (*connect.Response[platformv1.ListSweepTriggersResponse], error) {
	return connect.NewResponse(&platformv1.ListSweepTriggersResponse{Triggers: f.triggers}), nil
}

func (f *fakeSweeper) SettleSweepTrigger(
	_ context.Context, req *connect.Request[platformv1.SettleSweepTriggerRequest],
) (*connect.Response[platformv1.SettleSweepTriggerResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled = append(f.settled, req.Msg)
	return connect.NewResponse(&platformv1.SettleSweepTriggerResponse{Settled: true}), nil
}

func (f *fakeSweeper) ListSweepTargets(
	_ context.Context, _ *connect.Request[platformv1.ListSweepTargetsRequest],
) (*connect.Response[platformv1.ListSweepTargetsResponse], error) {
	return connect.NewResponse(&platformv1.ListSweepTargetsResponse{OrgIds: f.targets}), nil
}

func registerSweeps(env *testsuite.TestWorkflowEnvironment, a *Activities) {
	env.RegisterActivityWithOptions(a.ListSweepTriggers, activityOptions(ListSweepTriggersActivityName))
	env.RegisterActivityWithOptions(a.StartTriggeredSweeps, activityOptions(StartTriggeredSweepsActivityName))
	env.RegisterActivityWithOptions(a.RunWatcher, activityOptions(RunWatcherActivityName))
	env.RegisterActivityWithOptions(a.RunAnalyst, activityOptions(RunAnalystActivityName))
	env.RegisterActivityWithOptions(a.SettleSweepTrigger, activityOptions(SettleSweepTriggerActivityName))
	env.RegisterActivityWithOptions(a.ListSweepTargets, activityOptions(ListSweepTargetsActivityName))
}

func TestTheRelayStartsOneSweepPerTriggerOnItsOwnId(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{triggers: []*platformv1.SweepTrigger{
		{TriggerId: "t1", OrgId: "org-a"}, {TriggerId: "t2", OrgId: "org-b"},
	}}
	starter := &fakeStarter{running: map[string]bool{triggeredSweepWorkflowID("t2"): true}}
	registerSweeps(env, &Activities{Sweeps: sweeps, Starter: starter, TaskQueue: "core"})

	env.ExecuteWorkflow(RelaySweepTriggersWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the relay failed: %v", err)
	}
	var result RelayResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Pending != 2 || result.Started != 1 || result.AlreadyRunning != 1 {
		t.Fatalf("result = %+v, want 2 pending, 1 started, 1 already running", result)
	}
	if len(starter.started) != 1 || starter.started[0].ID != triggeredSweepWorkflowID("t1") {
		t.Fatalf("started %+v, want sweep/t1 alone", starter.started)
	}
}

// THE CHAIN. The Watcher, then the Analyst, each told which organisation in
// the header, then the trigger settled as done. Ordered by success: the
// Analyst is not asked until the Watcher has answered.
func TestATriggeredSweepRunsTheWatcherThenTheAnalystAndSettles(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{}
	registerSweeps(env, &Activities{Sweeps: sweeps})

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.OrgID != "org-a" || result.Signals != 3 || result.Findings != 2 {
		t.Fatalf("result = %+v, want org-a, 3 signals, 2 findings", result)
	}
	if len(sweeps.calls) != 2 || sweeps.calls[0] != "watch:org-a" || sweeps.calls[1] != "analyse:org-a" {
		t.Fatalf("calls = %v, want the watcher then the analyst, both for org-a", sweeps.calls)
	}
	if len(sweeps.settled) != 1 || sweeps.settled[0].GetOutcome() != platformv1.SettleSweepTriggerRequest_OUTCOME_DONE ||
		sweeps.settled[0].GetTriggerId() != "t1" {
		t.Fatalf("settled %v, want t1 done", sweeps.settled)
	}
}

// A transient refusal is retried until the Watcher answers, and the Analyst
// still runs after it.
func TestATransientWatcherFailureIsRetriedAndTheChainContinues(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, string) (SweepResult, error) {
		attempts++
		if attempts < 3 {
			return SweepResult{}, connect.NewError(connect.CodeUnavailable, errors.New("pool is full"))
		}
		return SweepResult{Signals: 1}, nil
	}, activityOptions(RunWatcherActivityName))
	sweeps := &fakeSweeper{}
	env.RegisterActivityWithOptions((&Activities{Sweeps: sweeps}).RunAnalyst, activityOptions(RunAnalystActivityName))
	env.RegisterActivityWithOptions((&Activities{Sweeps: sweeps}).SettleSweepTrigger, activityOptions(SettleSweepTriggerActivityName))

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed rather than retrying: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("the watcher ran %d times, want 3", attempts)
	}
	if len(sweeps.calls) != 1 || sweeps.calls[0] != "analyse:org-a" {
		t.Fatalf("after the retries the analyst calls were %v, want one for org-a", sweeps.calls)
	}
}

// A refusal retrying cannot fix settles the trigger as failed, with the
// reason on the row, and fails the workflow so it is visible.
func TestARefusedSweepSettlesTheTriggerAsFailedWithTheReason(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{refuse: map[string]error{
		"org-a": connect.NewError(connect.CodePermissionDenied, errors.New("token does not carry internal:ingest")),
	}}
	registerSweeps(env, &Activities{Sweeps: sweeps})

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("the workflow succeeded against a refusal")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("the failure was not the non-retryable refusal: %v", err)
	}
	if len(sweeps.settled) != 1 || sweeps.settled[0].GetOutcome() != platformv1.SettleSweepTriggerRequest_OUTCOME_FAILED ||
		sweeps.settled[0].GetError() == "" {
		t.Fatalf("settled %v, want t1 failed with the reason", sweeps.settled)
	}
	for _, c := range sweeps.calls {
		if c == "analyse:org-a" {
			t.Fatal("the analyst ran after the watcher was refused")
		}
	}
}

// THE ESTATE. Three organisations, one of which core-api refuses: the other
// two are swept, the refused one is listed as failed, and the run completes.
func TestTheDailySweepVisitsEveryOrganisationAndOneFailureDoesNotStopTheRest(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{
		targets: []string{"org-a", "org-b", "org-c"},
		refuse: map[string]error{
			"org-b": connect.NewError(connect.CodeInvalidArgument, errors.New("not a uuid")),
		},
	}
	registerSweeps(env, &Activities{Sweeps: sweeps})

	env.ExecuteWorkflow(DailySweepWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the daily sweep failed: %v", err)
	}
	var result DailySweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Organisations != 3 || result.Swept != 2 || len(result.Failed) != 1 || result.Failed[0] != "org-b" {
		t.Fatalf("result = %+v, want 3 organisations, 2 swept, org-b failed", result)
	}
	if result.Signals != 6 || result.Findings != 4 {
		t.Fatalf("totals = %d signals, %d findings; want 6 and 4", result.Signals, result.Findings)
	}
	analysed := map[string]bool{}
	for _, c := range sweeps.calls {
		if c == "analyse:org-a" || c == "analyse:org-c" {
			analysed[c] = true
		}
		if c == "analyse:org-b" {
			t.Fatal("the analyst ran for the organisation whose watcher was refused")
		}
	}
	if len(analysed) != 2 {
		t.Fatalf("analysed %v, want org-a and org-c", analysed)
	}
}

func TestTheDailySweepWithNoOrganisationsDoesNothing(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	sweeps := &fakeSweeper{}
	registerSweeps(env, &Activities{Sweeps: sweeps})

	env.ExecuteWorkflow(DailySweepWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an empty estate failed the run: %v", err)
	}
	if len(sweeps.calls) != 0 {
		t.Fatalf("calls = %v for an empty estate", sweeps.calls)
	}
}

// The activity on its own: the organisation rides in the header, and the
// Watcher step is detect_only, because the Analyst is the workflow's next
// step rather than RunSweep's second half.
func TestTheWatcherActivityNamesItsOrganisationAndIsDetectOnly(t *testing.T) {
	sweeps := &fakeSweeper{}
	a := &Activities{Sweeps: sweeps}
	if _, err := a.RunWatcher(context.Background(), "org-z"); err != nil {
		t.Fatalf("watcher: %v", err)
	}
	if _, err := a.RunAnalyst(context.Background(), "org-z"); err != nil {
		t.Fatalf("analyst: %v", err)
	}
	if len(sweeps.calls) != 2 || sweeps.calls[0] != "watch:org-z" || sweeps.calls[1] != "analyse:org-z" {
		t.Fatalf("calls = %v", sweeps.calls)
	}
}
