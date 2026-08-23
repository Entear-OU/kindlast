package schedule

import (
	"context"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/testsuite"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The agentic Watcher as a step of a sweep (ENT-258, PR 2), in the SDK's test
// environment. The Python activity is stubbed under the name the Python worker
// registers, which is what lets the chain be asserted without a Python
// process; what is asserted is this package's behaviour, not the agent's.
//
// The switch is set with `t.Setenv` rather than faked, because the thing worth
// testing is that it reaches the workflow through `workflow.SideEffect` and
// that a deployment which sets nothing gets the default (ENT-258, PR 3: on).

type fakeWatcherAPI struct {
	mu          sync.Mutex
	unavailable bool
	noProfile   bool
	refuse      error
	calls       int
}

func (f *fakeWatcherAPI) WatcherContext(
	_ context.Context, req *connect.Request[platformv1.WatcherContextRequest],
) (*connect.Response[platformv1.WatcherContextResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.refuse != nil {
		return nil, f.refuse
	}
	if f.noProfile {
		return connect.NewResponse(&platformv1.WatcherContextResponse{HasProfile: false}), nil
	}
	return connect.NewResponse(&platformv1.WatcherContextResponse{
		HasProfile:            true,
		IntelligenceAvailable: !f.unavailable,
		Facts: []*platformv1.ProfileFact{
			{Key: "has_dpo", ValueJson: `"no"`, Source: "onboarding"},
		},
		Obligations: []*platformv1.CitableObligation{
			{Slug: "gdpr-art-37-dpo", Title: "DPO", Summary: "Appoint one."},
		},
		ModelProvider: "instance",
		ModelName:     "local",
	}), nil
}

// fakeAgent stands in for the Python worker's Watch activity.
type fakeAgent struct {
	mu       sync.Mutex
	response *platformv1.WatchResponse
	handed   []*platformv1.WatchRequest
}

func (a *fakeAgent) watch(_ context.Context, req *platformv1.WatchRequest) (*platformv1.WatchResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handed = append(a.handed, req)
	if a.response != nil {
		return a.response, nil
	}
	return &platformv1.WatchResponse{
		Outcome:    platformv1.WatchOutcome_WATCH_OUTCOME_SUCCEEDED,
		AgentRunId: "run-1",
		Signals: []*platformv1.RaisedSignal{
			{SignalId: "s1", DedupKey: "profile_gap:has_dpo", Title: "No DPO", Severity: "medium", Raised: true},
			{SignalId: "s2", DedupKey: "profile_gap:has_ropa", Title: "No ROPA", Severity: "medium", Raised: false},
		},
	}, nil
}

func registerWatch(env *testsuite.TestWorkflowEnvironment, a *Activities, agent *fakeAgent) {
	registerNarration(env, a, &fakeDrafter{})
	env.RegisterActivityWithOptions(a.LoadWatchContext, activityOptions(LoadWatchContextActivityName))
	if agent != nil {
		env.RegisterActivityWithOptions(agent.watch, activityOptions(WatchActivityName))
	}
}

func TestTheAgenticWatcherRunsBetweenTheDetectorsAndTheAnalyst(t *testing.T) {
	// Nothing set: the default, which is what every deployment gets.
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	watchers := &fakeWatcherAPI{}
	agent := &fakeAgent{}
	registerWatch(env, &Activities{
		Sweeps: &fakeSweeper{}, Narratives: &fakeNarrator{pending: map[string][]string{}},
		Watchers: watchers,
	}, agent)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}

	if !result.Watch.Ran {
		t.Fatalf("watch = %+v, want it to have run", result.Watch)
	}
	// BOTH COUNTS, and this is the assertion that matters. A run whose every
	// observation was already open is a run that worked, so "one new, one
	// already known" must not collapse into "one signal".
	if result.Watch.Raised != 1 || result.Watch.Repeated != 1 {
		t.Fatalf("watch = %+v, want 1 raised and 1 repeated", result.Watch)
	}
	if len(agent.handed) != 1 {
		t.Fatalf("the Python activity was handed %d requests, want 1", len(agent.handed))
	}
	// The request carries the context core-api assembled and the model it
	// resolved, and no credential: the endpoint fields that once carried one
	// are deprecated and must stay empty.
	handed := agent.handed[0]
	if handed.GetOrgId() != "org-a" || len(handed.GetContext().GetFacts()) != 1 {
		t.Fatalf("the watch request was %+v", handed)
	}
	if len(handed.GetContext().GetObligations()) != 1 {
		t.Fatal("the run was offered no obligations, so it could cite nothing")
	}
	if handed.GetModelEndpoint().GetProvider() != "instance" {
		t.Fatalf("the model endpoint was %+v", handed.GetModelEndpoint())
	}
	if handed.GetModelEndpoint().GetApiKey() != "" || handed.GetModelEndpoint().GetBaseUrl() != "" { //nolint:staticcheck // asserting the deprecated fields stay empty
		t.Fatal("an endpoint or key crossed into the watch activity's input")
	}
}

func TestTurningItOffLeavesTheSweepExactlyAsItWas(t *testing.T) {
	// The off switch, and the property is that it costs nothing at all: not an
	// activity, not a call to core-api, not a line in the history. An operator
	// who turns this off because a local model is too slow gets back precisely
	// the sweep they had before, rather than a cheaper version of the new one.
	t.Setenv("KINDLAST_WATCHER_AGENT", "0")
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	watchers := &fakeWatcherAPI{}
	agent := &fakeAgent{}
	registerWatch(env, &Activities{
		Sweeps: &fakeSweeper{}, Narratives: &fakeNarrator{pending: map[string][]string{}},
		Watchers: watchers,
	}, agent)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Watch.Ran {
		t.Fatal("the agentic Watcher ran with KINDLAST_WATCHER_AGENT=0")
	}
	if watchers.calls != 0 || len(agent.handed) != 0 {
		t.Fatalf("the step cost %d loads and %d watches, want none", watchers.calls, len(agent.handed))
	}
}

func TestADeploymentWithNoIntelligenceCostsOneActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	watchers := &fakeWatcherAPI{unavailable: true}
	agent := &fakeAgent{}
	registerWatch(env, &Activities{
		Sweeps: &fakeSweeper{}, Narratives: &fakeNarrator{pending: map[string][]string{}},
		Watchers: watchers,
	}, agent)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	var result SweepResult
	_ = env.GetWorkflowResult(&result)
	if result.Watch.Ran || len(agent.handed) != 0 {
		t.Fatalf("watch = %+v, want it not to have run", result.Watch)
	}
	if watchers.calls != 1 {
		t.Fatalf("the load step ran %d times, want exactly 1", watchers.calls)
	}
}

func TestAnOrganisationPartWayThroughOnboardingIsNotWatched(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	watchers := &fakeWatcherAPI{noProfile: true}
	agent := &fakeAgent{}
	registerWatch(env, &Activities{
		Sweeps: &fakeSweeper{}, Narratives: &fakeNarrator{pending: map[string][]string{}},
		Watchers: watchers,
	}, agent)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the sweep failed: %v", err)
	}
	if len(agent.handed) != 0 {
		t.Fatal("an organisation with no profile was handed to the agent")
	}
}

func TestARefusedWatchIsRecordedAndTheSweepStillSucceeds(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	agent := &fakeAgent{response: &platformv1.WatchResponse{
		Outcome:       platformv1.WatchOutcome_WATCH_OUTCOME_REFUSED,
		OutcomeDetail: `tool 'create_finding' is not in this skill's allow-list [raise_signal]`,
		AgentRunId:    "run-1",
		Signals: []*platformv1.RaisedSignal{
			{SignalId: "s1", DedupKey: "k1", Title: "Something", Severity: "low", Raised: true},
		},
	}}
	registerWatch(env, &Activities{
		Sweeps: &fakeSweeper{}, Narratives: &fakeNarrator{pending: map[string][]string{}},
		Watchers: &fakeWatcherAPI{},
	}, agent)

	env.ExecuteWorkflow(TriggeredSweepWorkflow, Trigger{ID: "t1", OrgID: "org-a"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a refused watch failed the sweep: %v", err)
	}
	var result SweepResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if !result.Watch.Refused {
		t.Fatalf("watch = %+v, want it marked refused", result.Watch)
	}
	// WHAT IT WROTE BEFORE IT WAS STOPPED IS STILL REPORTED. A refusal three
	// steps in has already raised whatever the first two steps raised, and a
	// result saying only "refused" would describe less than what happened.
	if result.Watch.Raised != 1 {
		t.Fatalf("watch = %+v, want the signal it raised before it was stopped", result.Watch)
	}
	if result.Watch.Detail == "" {
		t.Fatal("a refusal with no reason in the history is a refusal nobody can act on")
	}
}

// EVERY VALUE THE OFF SWITCH ACCEPTS, AND EVERYTHING ELSE MEANING ON.
//
// Written out because the default is on and the helper reads the negative,
// which is the shape somebody skim-reading gets backwards. A deployment that
// sets KINDLAST_WATCHER_AGENT=true meaning "yes please" must not be switched
// off by a helper that only understood "1".
func TestWhatTheOffSwitchAccepts(t *testing.T) {
	for _, c := range []struct {
		value string
		off   bool
	}{
		{"", false},
		{"0", true},
		{"false", true},
		{"FALSE", true},
		{" no ", true},
		{"off", true},
		{"1", false},
		{"true", false},
		{"yes", false},
		{"anything else", false},
	} {
		if got := falsy(c.value); got != c.off {
			t.Errorf("falsy(%q) = %v, want %v", c.value, got, c.off)
		}
	}
}
