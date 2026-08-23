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

// The Executor as a workflow (ENT-271, ENT-225 phase 2).
//
// Approving a finding used to fire a database trigger that created the record
// inside the approving transaction. Now the approval writes an `executor_jobs`
// row in that transaction (00036), the `relay-executor-jobs` Schedule lists
// what is pending every few seconds and starts one ExecuteApprovalWorkflow per
// row with the job id as the workflow id, and the workflow asks core-api to
// execute it: create the record, write the audit row, settle the job, in one
// transaction as the approver.
//
// # WHY THE RETRY POLICY HAS NO ATTEMPT LIMIT
//
// A person approved a finding and a record is owed. Every reason an execution
// fails (core-api restarting, the pool full, a constraint the payload trips)
// is either transient or something an operator fixes, and a job given up on
// after five tries is a compliance record that silently never appeared. So it
// retries with backoff for as long as the run lives, and the run is bounded by
// the workflow's own timeout; a job still pending after that is listed again
// by the next relay tick. What is NOT retried is a job id core-api calls
// malformed, which is this binary's bug and does not change by waiting.

// ExecutorRelayScheduleID is the Schedule's id in the engine.
const ExecutorRelayScheduleID = "relay-executor-jobs"

// Registered activity names.
const (
	ListExecutorJobsActivityName = "ListExecutorJobs"
	StartExecutionsActivityName  = "StartExecutions"
	ExecuteJobActivityName       = "ExecuteJob"
)

// executionWorkflowID is the workflow id for one job, and the thing that makes
// a job executed by at most one run at a time.
func executionWorkflowID(jobID string) string { return "execute/" + jobID }

// Executor is what the activities need of core-api's ExecutorService.
type Executor interface {
	ListPendingJobs(ctx context.Context, req *connect.Request[platformv1.ListPendingJobsRequest]) (*connect.Response[platformv1.ListPendingJobsResponse], error)
	ExecuteJob(ctx context.Context, req *connect.Request[platformv1.ExecuteJobRequest]) (*connect.Response[platformv1.ExecuteJobResponse], error)
}

// ExecutorJob is one pending execution as the relay lists it.
type ExecutorJob struct {
	ID         string
	OrgID      string
	FindingID  string
	ActionType string
}

// RelayExecutorJobsWorkflow is the workflow behind `relay-executor-jobs`.
func RelayExecutorJobsWorkflow(ctx workflow.Context) (RelayResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var jobs []ExecutorJob
	if err := workflow.ExecuteActivity(ctx, ListExecutorJobsActivityName).Get(ctx, &jobs); err != nil {
		return RelayResult{}, err
	}
	if len(jobs) == 0 {
		return RelayResult{}, nil
	}
	var started RelayResult
	if err := workflow.ExecuteActivity(ctx, StartExecutionsActivityName, jobs).Get(ctx, &started); err != nil {
		return RelayResult{}, err
	}
	return started, nil
}

// ExecutionResult is what one execution did, as the history keeps it: whether
// this run settled the job, and what exists for the finding now. Ids, never a
// customer's proposed record.
type ExecutionResult struct {
	Settled     bool
	RecordID    string
	RecordTable string
}

// ExecuteApprovalWorkflow creates the record one approved finding asked for.
func ExecuteApprovalWorkflow(ctx workflow.Context, job ExecutorJob) (ExecutionResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// One insert plus one audit row. Two minutes covers a busy pool.
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Minute,
		},
	})

	var result ExecutionResult
	if err := workflow.ExecuteActivity(ctx, ExecuteJobActivityName, job.ID).Get(ctx, &result); err != nil {
		return ExecutionResult{}, err
	}
	return result, nil
}

// ListExecutorJobs asks core-api which approvals are waiting for their record.
func (a *Activities) ListExecutorJobs(ctx context.Context) ([]ExecutorJob, error) {
	res, err := a.Executions.ListPendingJobs(ctx, connect.NewRequest(&platformv1.ListPendingJobsRequest{}))
	if err != nil {
		return nil, nonRetryableIfRefused(err)
	}
	var out []ExecutorJob
	for _, job := range res.Msg.GetJobs() {
		out = append(out, ExecutorJob{
			ID: job.GetJobId(), OrgID: job.GetOrgId(),
			FindingID: job.GetFindingId(), ActionType: job.GetActionType(),
		})
	}
	return out, nil
}

// StartExecutions starts one ExecuteApprovalWorkflow per job, idempotent on
// the id exactly as StartDeliveries is.
func (a *Activities) StartExecutions(ctx context.Context, jobs []ExecutorJob) (RelayResult, error) {
	result := RelayResult{Pending: len(jobs)}
	for _, job := range jobs {
		_, err := a.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:                       executionWorkflowID(job.ID),
			TaskQueue:                a.TaskQueue,
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		}, ExecuteApprovalWorkflow, job)
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		switch {
		case err == nil:
			result.Started++
		case errors.As(err, &already):
			result.AlreadyRunning++
		default:
			return result, fmt.Errorf("starting the execution of %s: %w", job.ID, err)
		}
	}
	return result, nil
}

// ExecuteJob asks core-api to create the record and settle the job.
func (a *Activities) ExecuteJob(ctx context.Context, jobID string) (ExecutionResult, error) {
	res, err := a.Executions.ExecuteJob(ctx, connect.NewRequest(&platformv1.ExecuteJobRequest{
		JobId: jobID,
	}))
	if err != nil {
		return ExecutionResult{}, badRequestOrRefusal(err)
	}
	return ExecutionResult{
		Settled:     res.Msg.GetSettled(),
		RecordID:    res.Msg.GetRecordId(),
		RecordTable: res.Msg.GetRecordTable(),
	}, nil
}
