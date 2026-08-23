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

// The Executor as a workflow (ENT-271), in the SDK's test environment: the
// relay starts one execution per pending job on its own id, and the execution
// asks core-api to create the record.

type fakeExecutor struct {
	mu       sync.Mutex
	pending  []*platformv1.ExecutorJob
	executed []string
	refuse   error
	settled  bool
}

func (f *fakeExecutor) ListPendingJobs(
	_ context.Context, _ *connect.Request[platformv1.ListPendingJobsRequest],
) (*connect.Response[platformv1.ListPendingJobsResponse], error) {
	return connect.NewResponse(&platformv1.ListPendingJobsResponse{Jobs: f.pending}), nil
}

func (f *fakeExecutor) ExecuteJob(
	_ context.Context, req *connect.Request[platformv1.ExecuteJobRequest],
) (*connect.Response[platformv1.ExecuteJobResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.executed = append(f.executed, req.Msg.GetJobId())
	if f.refuse != nil && len(f.executed) <= 2 {
		return nil, f.refuse
	}
	if f.settled {
		return connect.NewResponse(&platformv1.ExecuteJobResponse{}), nil
	}
	return connect.NewResponse(&platformv1.ExecuteJobResponse{
		Settled: true, RecordId: "record-for-" + req.Msg.GetJobId(), RecordTable: "processing_activities",
	}), nil
}

func registerExecutor(env *testsuite.TestWorkflowEnvironment, a *Activities) {
	env.RegisterActivityWithOptions(a.ListExecutorJobs, activityOptions(ListExecutorJobsActivityName))
	env.RegisterActivityWithOptions(a.StartExecutions, activityOptions(StartExecutionsActivityName))
	env.RegisterActivityWithOptions(a.ExecuteJob, activityOptions(ExecuteJobActivityName))
}

func TestTheRelayStartsOneExecutionPerJobOnItsOwnId(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	executor := &fakeExecutor{pending: []*platformv1.ExecutorJob{
		{JobId: "j1", OrgId: "org-a", FindingId: "f1", ActionType: "create_ropa"},
		{JobId: "j2", OrgId: "org-a", FindingId: "f2", ActionType: "create_dsar"},
	}}
	starter := &fakeStarter{running: map[string]bool{executionWorkflowID("j2"): true}}
	registerExecutor(env, &Activities{Executions: executor, Starter: starter, TaskQueue: "core"})

	env.ExecuteWorkflow(RelayExecutorJobsWorkflow)

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
	// One workflow per job id: an approval is executed by at most one run at
	// a time, which is what stops two records for one decision.
	if len(starter.started) != 1 || starter.started[0].ID != executionWorkflowID("j1") {
		t.Fatalf("started %+v, want execute/j1 alone", starter.started)
	}
}

func TestAnExecutionAsksCoreAPIAndReportsWhatExists(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	executor := &fakeExecutor{}
	registerExecutor(env, &Activities{Executions: executor})

	env.ExecuteWorkflow(ExecuteApprovalWorkflow, ExecutorJob{ID: "j1", OrgID: "org-a", FindingID: "f1", ActionType: "create_ropa"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the execution failed: %v", err)
	}
	var result ExecutionResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if !result.Settled || result.RecordID != "record-for-j1" || result.RecordTable != "processing_activities" {
		t.Fatalf("result = %+v", result)
	}
	if len(executor.executed) != 1 {
		t.Fatalf("core-api was asked %d times, want 1", len(executor.executed))
	}
}

// A record is owed to somebody who approved a finding, so a transient failure
// retries rather than giving up: the alternative is a compliance record that
// silently never appears.
func TestAFailedExecutionIsRetriedUntilTheRecordExists(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	executor := &fakeExecutor{refuse: connect.NewError(connect.CodeUnavailable, errors.New("the pool is full"))}
	registerExecutor(env, &Activities{Executions: executor})

	env.ExecuteWorkflow(ExecuteApprovalWorkflow, ExecutorJob{ID: "j1", ActionType: "create_ropa"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the execution gave up: %v", err)
	}
	if len(executor.executed) != 3 {
		t.Fatalf("core-api was asked %d times, want 3 (two failures, then success)", len(executor.executed))
	}
}

// A job already settled (an earlier run created the record and the retry
// arrived anyway) completes without a second record.
func TestASettledJobCompletesWithoutCreatingASecondRecord(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	executor := &fakeExecutor{settled: true}
	registerExecutor(env, &Activities{Executions: executor})

	env.ExecuteWorkflow(ExecuteApprovalWorkflow, ExecutorJob{ID: "j1", ActionType: "create_ropa"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a settled job failed the execution: %v", err)
	}
	var result ExecutionResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Settled {
		t.Fatal("a job settled earlier was reported as settled by this run")
	}
}

// A malformed job id is this binary's bug and does not change by waiting.
func TestAMalformedJobIdIsNotRetried(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	executor := &fakeExecutor{refuse: connect.NewError(connect.CodeInvalidArgument, errors.New("job_id is not a uuid"))}
	registerExecutor(env, &Activities{Executions: executor})

	env.ExecuteWorkflow(ExecuteApprovalWorkflow, ExecutorJob{ID: "nope", ActionType: "create_ropa"})

	err := env.GetWorkflowError()
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("err = %v, want a non-retryable failure", err)
	}
	if len(executor.executed) != 1 {
		t.Fatalf("core-api was asked %d times for a caller bug, want 1", len(executor.executed))
	}
}
