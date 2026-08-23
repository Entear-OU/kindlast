// Package executor serves ExecutorService: creating the record an approved
// finding asked for (ENT-271, ENT-225 phase 2).
//
// On the internal chain, on `internal:ingest`, like the sweep: the caller is
// the Temporal worker, and there is no membership to resolve. What replaces
// tenancy here is not nothing, and it is worth stating: the execution opens a
// tenant transaction of its own whose organisation and user come from the job
// row, so the record is created under the approver's authority and the
// caller's only input is which job to run.
package executor

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Jobs is the listing half, on the producer pool: cross-organisation, read
// only.
type Jobs interface {
	PendingExecutorJobs(ctx context.Context, limit int) ([]postgres.ExecutorJob, error)
}

// Executions is the acting half, on the application pool, as the approver.
type Executions interface {
	ExecuteJob(ctx context.Context, jobID string) (postgres.Execution, error)
	RecordFailedExecution(ctx context.Context, jobID string, cause error) error
}

// Bounds on one listing. What is not listed now is listed on the next relay
// tick a few seconds later.
const (
	DefaultListLimit = 200
	MaxListLimit     = 1000
)

// Service implements platformv1connect.ExecutorServiceHandler.
type Service struct {
	jobs       Jobs
	executions Executions
}

func New(jobs Jobs, executions Executions) *Service {
	return &Service{jobs: jobs, executions: executions}
}

// ListPendingJobs lists approvals whose record has not been created yet.
func (s *Service) ListPendingJobs(
	ctx context.Context,
	req *connect.Request[platformv1.ListPendingJobsRequest],
) (*connect.Response[platformv1.ListPendingJobsResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	limit := int(req.Msg.GetLimit())
	switch {
	case limit <= 0:
		limit = DefaultListLimit
	case limit > MaxListLimit:
		limit = MaxListLimit
	}

	jobs, err := s.jobs.PendingExecutorJobs(ctx, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := &platformv1.ListPendingJobsResponse{}
	for _, job := range jobs {
		res.Jobs = append(res.Jobs, &platformv1.ExecutorJob{
			JobId: job.ID, OrgId: job.OrgID, FindingId: job.FindingID, ActionType: job.ActionType,
		})
	}
	return connect.NewResponse(res), nil
}

// ExecuteJob creates the record and settles the job.
//
// A failure is recorded on the row before the error leaves here, so an
// operator reading `executor_jobs` sees the attempt and the reason without
// reading a workflow history, and the answer is `unavailable`, which is what
// the workflow's retry policy keys on.
func (s *Service) ExecuteJob(
	ctx context.Context,
	req *connect.Request[platformv1.ExecuteJobRequest],
) (*connect.Response[platformv1.ExecuteJobResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	jobID := req.Msg.GetJobId()
	if jobID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("an execution names one job; send job_id"))
	}

	execution, err := s.executions.ExecuteJob(ctx, jobID)
	if errors.Is(err, postgres.ErrBadJobID) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("job_id is not a uuid"))
	}
	if err != nil {
		// Recorded on the row, best effort: the execution already failed, and
		// failing to write down why is not a reason to lose the reason.
		if recordErr := s.executions.RecordFailedExecution(ctx, jobID, err); recordErr != nil {
			err = fmt.Errorf("%w (and the attempt could not be recorded: %w)", err, recordErr)
		}
		return nil, connect.NewError(connect.CodeUnavailable, err)
	}

	return connect.NewResponse(&platformv1.ExecuteJobResponse{
		Settled:     execution.Settled,
		RecordId:    execution.RecordID,
		RecordTable: execution.RecordTable,
	}), nil
}
