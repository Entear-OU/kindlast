package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The scheduled fetch that deposits evidence (ENT-279).
//
// # WHAT WAS BROKEN, WHICH WAS NOT THE CODE
//
// ENT-274 gave the Watcher `read_evidence`: it reads observations a fetch has
// already deposited for one connection and one granted tool. Nothing deposited
// any on a schedule, so stored evidence was whatever a person happened to
// click Fetch for on the Integrations page, and the tool usually found
// nothing. The feature was real and inert.
//
// # THE SHAPE: A SCHEDULED FETCH DEPOSITS, A SWEEP READS
//
// Not "the agent fetches during a sweep", and probably permanently not. A
// sweep is a twenty minute activity with three retries and nobody waiting on
// it: a synchronous fetch inside one would put a customer's latency on the
// sweep's critical path and could dial them three times per sweep, once per
// attempt. And letting an agent cause a fetch would mean the role that runs
// models holding what it takes to reach a customer's systems, which is ENT-279
// unresolved half and a decision nobody has taken.
//
// So the fetch runs on its own clock, on this worker, and what the sweep sees
// is whatever it has already deposited.
//
// # THE FAN-OUT IS THE SAME RELAY THE OUTBOX AND THE EXECUTOR USE
//
// List what is due, start one workflow per target with the target as its
// workflow id, conflict policy FAIL. That last part is doing real work here
// rather than being idiom: a customer endpoint that hangs for three hours must
// not accumulate three concurrent fetches against it, and the id is what makes
// the second tick find the first still running and leave it alone.

// EvidenceFetchRelayScheduleID is the Schedule's id in the engine, which is
// what an operator pauses.
//
// PAUSING IT IS THE OFF SWITCH, and it is deliberately the only one. This is
// the process that reaches into customers' systems unattended, so the control
// should be the one an operator can see, pause and read the history of, rather
// than an environment variable whose current value nobody can tell from the
// outside. The deployment-level switch is core-api's: without a gateway it
// serves no FetchService and this relay does nothing.
const EvidenceFetchRelayScheduleID = "fetch-evidence-for-every-connection"

// Registered activity names.
const (
	ListFetchTargetsActivityName  = "ListFetchTargets"
	StartFetchesActivityName      = "StartFetches"
	RunScheduledFetchActivityName = "RunScheduledFetch"
)

// fetchWorkflowID is the workflow id for one connection and tool, and the
// thing that makes a target fetched by at most one run at a time.
func fetchWorkflowID(target FetchTarget) string {
	return "fetch/" + target.IntegrationID + "/" + target.Tool
}

// Fetcher is what the activities need of core-api's FetchService, declared
// where it is used (§21.6).
type Fetcher interface {
	ListFetchTargets(ctx context.Context, req *connect.Request[platformv1.ListFetchTargetsRequest]) (*connect.Response[platformv1.ListFetchTargetsResponse], error)
	RunScheduledFetch(ctx context.Context, req *connect.Request[platformv1.RunScheduledFetchRequest]) (*connect.Response[platformv1.RunScheduledFetchResponse], error)
}

// FetchTarget is one connection and one tool whose evidence has gone stale.
//
// Ids and a tool name, because this travels into a workflow history that
// anybody who can read the engine can read. No endpoint and no credential ever
// leave core-api.
type FetchTarget struct {
	OrgID         string
	IntegrationID string
	Tool          string
}

// FetchResult is what one scheduled fetch did, as the history keeps it.
type FetchResult struct {
	// Outcome is `succeeded`, `refused` or `failed`, the same closed set the
	// `integration_fetches` row holds.
	//
	// A REFUSAL AND A FAILURE ARE BOTH SUCCESSFUL ACTIVITIES. A customer's
	// endpoint being down is a recorded outcome rather than an error, so
	// Temporal never retries somebody else's outage on our schedule; the next
	// attempt is the next time the target comes up stale, which is a day
	// later. What the retry policy is for is core-api being unreachable.
	Outcome string
	Detail  string
	FetchID string
	// EvidenceID is the observation this fetch is linked to, and
	// EvidenceIsNew says whether it was written now or was already there
	// because the endpoint returned exactly what it returned last time.
	EvidenceID    string
	EvidenceIsNew bool
}

// RelayEvidenceFetchesWorkflow is the workflow behind
// `fetch-evidence-for-every-connection`: list what is stale, start one fetch
// for each.
func RelayEvidenceFetchesWorkflow(ctx workflow.Context) (RelayResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var targets []FetchTarget
	if err := workflow.ExecuteActivity(ctx, ListFetchTargetsActivityName).Get(ctx, &targets); err != nil {
		return RelayResult{}, err
	}
	if len(targets) == 0 {
		return RelayResult{}, nil
	}
	var started RelayResult
	if err := workflow.ExecuteActivity(ctx, StartFetchesActivityName, targets).Get(ctx, &started); err != nil {
		return RelayResult{}, err
	}
	return started, nil
}

// FetchEvidenceWorkflow fetches one granted read-only tool on one connection.
//
// # THE RETRY POLICY IS BOUNDED, UNLIKE THE EXECUTOR'S
//
// The Executor retries forever because a person approved a finding and a
// record is owed. Nobody is owed this. Evidence that could not be collected
// this hour is collected the next time the target comes up stale, and the
// activity that would be retried is one that has already decided to record a
// failure rather than raise one, so a retry here can only mean core-api itself
// is unreachable. Five attempts over a few minutes rides out a restart; more
// than that is an operator's problem rather than a schedule's, and the tick an
// hour later will list the same target anyway.
func FetchEvidenceWorkflow(ctx workflow.Context, target FetchTarget) (FetchResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// Longer than anything downstream. core-api bounds its own gateway
		// call, the gateway bounds its outbound call, and the outbound client
		// bounds the socket, so the innermost deadline is the one that fires
		// and the recorded detail names the endpoint rather than us. This is
		// the backstop for all three having failed to fire.
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    15 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    5,
		},
	})

	var result FetchResult
	if err := workflow.ExecuteActivity(ctx, RunScheduledFetchActivityName, target).Get(ctx, &result); err != nil {
		return FetchResult{}, err
	}
	return result, nil
}

// ListFetchTargets asks core-api which connection and tool is due a fetch.
//
// A deployment with no gateway serves no FetchService, so the route is not
// mounted and Connect answers `unimplemented`. That is a configuration rather
// than a fault: this stack connects nothing, there is nothing to fetch, and
// saying so every hour in a failed activity would be noise an operator learns
// to ignore.
func (a *Activities) ListFetchTargets(ctx context.Context) ([]FetchTarget, error) {
	res, err := a.Fetches.ListFetchTargets(ctx,
		connect.NewRequest(&platformv1.ListFetchTargetsRequest{}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeUnimplemented {
			return nil, nil
		}
		return nil, nonRetryableIfRefused(err)
	}
	var out []FetchTarget
	for _, target := range res.Msg.GetTargets() {
		out = append(out, FetchTarget{
			OrgID:         target.GetOrgId(),
			IntegrationID: target.GetIntegrationId(),
			Tool:          target.GetTool(),
		})
	}
	return out, nil
}

// StartFetches starts one FetchEvidenceWorkflow per target, idempotent on the
// id exactly as StartExecutions is.
func (a *Activities) StartFetches(ctx context.Context, targets []FetchTarget) (RelayResult, error) {
	result := RelayResult{Pending: len(targets)}
	for _, target := range targets {
		_, err := a.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:        fetchWorkflowID(target),
			TaskQueue: a.TaskQueue,
			// FAIL rather than USE_EXISTING, so a target already being fetched
			// is counted and skipped. A customer's endpoint that hangs is the
			// case this exists for: without it, an hourly tick against a
			// three-hour hang would have three fetches queued behind each
			// other on one connection.
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		}, FetchEvidenceWorkflow, target)
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		switch {
		case err == nil:
			result.Started++
		case errors.As(err, &already):
			result.AlreadyRunning++
		default:
			return result, fmt.Errorf("starting the fetch of %s on %s: %w",
				target.Tool, target.IntegrationID, err)
		}
	}
	return result, nil
}

// RunScheduledFetch asks core-api to fetch one tool and record what happened.
//
// The target's organisation is deliberately not sent. core-api reads it, and
// whose consent the fetch runs under, from the connection's own rows: a caller
// that could name either would be able to reach a customer's systems in
// somebody else's name.
func (a *Activities) RunScheduledFetch(ctx context.Context, target FetchTarget) (FetchResult, error) {
	res, err := a.Fetches.RunScheduledFetch(ctx, connect.NewRequest(&platformv1.RunScheduledFetchRequest{
		IntegrationId: target.IntegrationID,
		Tool:          target.Tool,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			// The connection is gone, which happens when an organisation was
			// erased between the listing and this call. Nothing to retry and
			// nothing to fix.
			return FetchResult{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("core-api knows no connection %s", target.IntegrationID),
				"no-connection", err)
		}
		return FetchResult{}, badRequestOrRefusal(err)
	}
	return FetchResult{
		Outcome:       res.Msg.GetOutcome(),
		Detail:        res.Msg.GetDetail(),
		FetchID:       res.Msg.GetFetchId(),
		EvidenceID:    res.Msg.GetEvidenceId(),
		EvidenceIsNew: res.Msg.GetEvidenceIsNew(),
	}, nil
}
