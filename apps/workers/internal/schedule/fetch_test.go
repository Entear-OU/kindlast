package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The scheduled fetch as a workflow (ENT-279), in the SDK's test environment.
//
// What these prove is the half the workflow is responsible for: one fetch per
// target on its own id so a hanging endpoint never accumulates concurrent
// fetches, a recorded refusal or failure completing the workflow rather than
// failing it, and a deployment with no gateway reading as "nothing to fetch"
// rather than as a fault. What happens inside core-api (the plan, the
// credential, the deposit) is that service's tests.

type fakeFetcher struct {
	mu      sync.Mutex
	targets []*platformv1.FetchTarget
	listErr error

	fetched []*platformv1.RunScheduledFetchRequest
	answer  *platformv1.RunScheduledFetchResponse
	err     error
}

func (f *fakeFetcher) ListFetchTargets(
	_ context.Context, _ *connect.Request[platformv1.ListFetchTargetsRequest],
) (*connect.Response[platformv1.ListFetchTargetsResponse], error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return connect.NewResponse(&platformv1.ListFetchTargetsResponse{Targets: f.targets}), nil
}

func (f *fakeFetcher) RunScheduledFetch(
	_ context.Context, req *connect.Request[platformv1.RunScheduledFetchRequest],
) (*connect.Response[platformv1.RunScheduledFetchResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, req.Msg)
	if f.err != nil {
		return nil, f.err
	}
	if f.answer != nil {
		return connect.NewResponse(f.answer), nil
	}
	return connect.NewResponse(&platformv1.RunScheduledFetchResponse{
		Outcome: "succeeded", FetchId: "fetch-1", EvidenceId: "evidence-1", EvidenceIsNew: true,
	}), nil
}

func registerFetch(env *testsuite.TestWorkflowEnvironment, a *Activities) {
	env.RegisterActivityWithOptions(a.ListFetchTargets, activityOptions(ListFetchTargetsActivityName))
	env.RegisterActivityWithOptions(a.StartFetches, activityOptions(StartFetchesActivityName))
	env.RegisterActivityWithOptions(a.RunScheduledFetch, activityOptions(RunScheduledFetchActivityName))
}

func TestTheFetchRelayStartsOneFetchPerTargetOnItsOwnId(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetcher := &fakeFetcher{targets: []*platformv1.FetchTarget{
		{OrgId: "org-a", IntegrationId: "conn-1", Tool: "list_records"},
		{OrgId: "org-a", IntegrationId: "conn-1", Tool: "search_tickets"},
		{OrgId: "org-b", IntegrationId: "conn-2", Tool: "list_records"},
	}}
	// conn-1's search_tickets is already being fetched: the endpoint hung on
	// the last tick and its workflow is still running. The relay must count it
	// and leave it alone rather than queue a second fetch behind it.
	starter := &fakeStarter{running: map[string]bool{
		fetchWorkflowID(FetchTarget{IntegrationID: "conn-1", Tool: "search_tickets"}): true,
	}}
	registerFetch(env, &Activities{Fetches: fetcher, Starter: starter, TaskQueue: "core"})

	env.ExecuteWorkflow(RelayEvidenceFetchesWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the relay failed: %v", err)
	}
	var result RelayResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Pending != 3 || result.Started != 2 || result.AlreadyRunning != 1 {
		t.Fatalf("result = %+v, want 3 pending, 2 started, 1 already running", result)
	}
	for _, started := range starter.started {
		if started.ID == fetchWorkflowID(FetchTarget{IntegrationID: "conn-1", Tool: "search_tickets"}) {
			t.Fatal("a second fetch was queued against a target whose fetch is still running")
		}
	}
}

// A deployment with no gateway serves no FetchService, and the mounted-nothing
// answer is `unimplemented`. That is a configuration, not a fault: the relay
// reports an empty tick rather than failing every interval forever on a stack
// that deliberately connects nothing.
func TestADeploymentWithNoGatewayFetchesNothingQuietly(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetcher := &fakeFetcher{listErr: connect.NewError(connect.CodeUnimplemented,
		errors.New("this deployment connects nothing"))}
	registerFetch(env, &Activities{Fetches: fetcher, Starter: &fakeStarter{}, TaskQueue: "core"})

	env.ExecuteWorkflow(RelayEvidenceFetchesWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("an unconfigured deployment failed the relay: %v", err)
	}
	var result RelayResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Pending != 0 || result.Started != 0 {
		t.Fatalf("result = %+v, want an empty tick", result)
	}
}

// A customer's endpoint being down is a RECORDED OUTCOME, not a workflow
// failure. core-api answers the RPC successfully with `failed` and a detail,
// the workflow completes, and Temporal never retries somebody else's outage on
// our schedule; the next attempt is the next time the target goes stale.
func TestAFailedFetchIsARecordedOutcomeNotACrash(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetcher := &fakeFetcher{answer: &platformv1.RunScheduledFetchResponse{
		Outcome: "failed",
		Detail:  "the endpoint did not answer usefully",
		FetchId: "fetch-9",
	}}
	registerFetch(env, &Activities{Fetches: fetcher})

	env.ExecuteWorkflow(FetchEvidenceWorkflow,
		FetchTarget{OrgID: "org-a", IntegrationID: "conn-1", Tool: "list_records"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed fetch failed the workflow: %v", err)
	}
	var result FetchResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Outcome != "failed" || result.Detail == "" || result.FetchID != "fetch-9" {
		t.Fatalf("result = %+v, want the recorded failure with its reason", result)
	}
	if len(fetcher.fetched) != 1 {
		t.Fatalf("core-api was asked %d times, want exactly 1: a recorded failure must not retry", len(fetcher.fetched))
	}
}

// An egress refusal is the same shape: the gateway declined with zero bytes on
// the wire, core-api recorded `refused`, and the workflow's job is to complete
// with that answer rather than retry a control that will say no again.
func TestARefusedFetchCompletesWithTheRefusalRecorded(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetcher := &fakeFetcher{answer: &platformv1.RunScheduledFetchResponse{
		Outcome: "refused",
		Detail:  "\"tools.example.com\" is not on this deployment's egress allow-list",
		FetchId: "fetch-10",
	}}
	registerFetch(env, &Activities{Fetches: fetcher})

	env.ExecuteWorkflow(FetchEvidenceWorkflow,
		FetchTarget{OrgID: "org-a", IntegrationID: "conn-1", Tool: "list_records"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a refusal failed the workflow: %v", err)
	}
	var result FetchResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Outcome != "refused" || result.FetchID != "fetch-10" {
		t.Fatalf("result = %+v, want the recorded refusal", result)
	}
	if len(fetcher.fetched) != 1 {
		t.Fatalf("core-api was asked %d times, want 1", len(fetcher.fetched))
	}
}

// The activity sends the connection and the tool, and DOES NOT send the
// organisation. Whose authority a fetch runs under comes from the connection's
// own consent record inside core-api; a caller that could name it would be
// able to reach a customer's systems in somebody else's name.
func TestAFetchNamesTheConnectionAndToolAndNothingElse(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetcher := &fakeFetcher{}
	registerFetch(env, &Activities{Fetches: fetcher})

	env.ExecuteWorkflow(FetchEvidenceWorkflow,
		FetchTarget{OrgID: "org-a", IntegrationID: "conn-1", Tool: "list_records"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the fetch failed: %v", err)
	}
	if len(fetcher.fetched) != 1 {
		t.Fatalf("core-api was asked %d times, want 1", len(fetcher.fetched))
	}
	sent := fetcher.fetched[0]
	if sent.GetIntegrationId() != "conn-1" || sent.GetTool() != "list_records" {
		t.Fatalf("sent %+v, want the connection and the tool", sent)
	}
	// The request message itself carries no organisation field, so this is
	// pinned by the contract; what is asserted here is that the activity did
	// not smuggle it anywhere else either.
	if sent.ProtoReflect().Descriptor().Fields().ByName("org_id") != nil {
		t.Fatal("RunScheduledFetchRequest grew an org_id field; the whole point is that the caller cannot name one")
	}

	var result FetchResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Outcome != "succeeded" || !result.EvidenceIsNew {
		t.Fatalf("result = %+v", result)
	}
}

// A connection that vanished between the listing and the fetch (the
// organisation was erased) is nothing to retry: the activity fails
// non-retryably and the workflow reports it once.
func TestAVanishedConnectionIsNotRetried(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	fetcher := &fakeFetcher{err: connect.NewError(connect.CodeNotFound,
		errors.New("no such connection"))}
	registerFetch(env, &Activities{Fetches: fetcher})

	env.ExecuteWorkflow(FetchEvidenceWorkflow,
		FetchTarget{OrgID: "org-a", IntegrationID: "conn-gone", Tool: "list_records"})

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("a vanished connection reported success")
	}
	var applicationErr *temporal.ApplicationError
	if !errors.As(err, &applicationErr) || applicationErr.Type() != "no-connection" {
		t.Fatalf("failed as %v, want the non-retryable no-connection error", err)
	}
	if len(fetcher.fetched) != 1 {
		t.Fatalf("core-api was asked %d times, want 1: erased means erased, not retried", len(fetcher.fetched))
	}
}
