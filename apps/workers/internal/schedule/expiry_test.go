package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/timestamppb"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The SDK's test environment, which is the §13 testing story for workflows:
// time is skipped rather than waited for, and an activity is stubbed at its
// registered name, so what is asserted is the workflow's behaviour (which
// activity it runs, what it returns, what it does with an error) rather than
// the engine's.

type fakeExpirer struct {
	calls int
	err   error
	count int32
}

func (f *fakeExpirer) ExpireSnoozes(
	_ context.Context, _ *connect.Request[platformv1.ExpireSnoozesRequest],
) (*connect.Response[platformv1.ExpireSnoozesResponse], error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(&platformv1.ExpireSnoozesResponse{
		Reemerged: f.count,
		RanAt:     timestamppb.New(time.Date(2026, 8, 21, 12, 10, 0, 0, time.UTC)),
	}), nil
}

func TestTheWorkflowRunsThePassOnceAndReportsWhatCameBack(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	core := &fakeExpirer{count: 3}
	activities := &Activities{CoreAPI: core}
	env.RegisterActivityWithOptions(activities.ExpireSnoozes, activityOptions(ExpireSnoozesActivityName))

	env.ExecuteWorkflow(ExpireSnoozedFindingsWorkflow)

	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the workflow failed: %v", err)
	}

	var result ExpiryResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Reemerged != 3 {
		t.Errorf("reemerged = %d, want 3", result.Reemerged)
	}
	if result.RanAt.IsZero() {
		t.Error("ran_at was lost on the way through")
	}
	if core.calls != 1 {
		t.Errorf("core-api was called %d times, want 1: the workflow is one pass, not a loop", core.calls)
	}
}

// A transient failure is retried by the policy, and the workflow succeeds
// once the activity does. This is the property that makes the schedule worth
// having over a cron line: the cron line fails once and is silent until the
// next tick, the workflow keeps trying.
func TestATransientFailureIsRetriedUntilItPasses(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	attempts := 0
	env.RegisterActivityWithOptions(func(ctx context.Context) (ExpiryResult, error) {
		attempts++
		if attempts < 3 {
			return ExpiryResult{}, connect.NewError(connect.CodeUnavailable, errors.New("edge is restarting"))
		}
		return ExpiryResult{Reemerged: 1}, nil
	}, activityOptions(ExpireSnoozesActivityName))

	env.ExecuteWorkflow(ExpireSnoozedFindingsWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the workflow failed rather than retrying: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("the activity ran %d times, want 3 (two failures, then success)", attempts)
	}
}

// A refused credential is not retried, because retrying a refusal a thousand
// times is a thousand refusals. The activity marks it non-retryable and the
// workflow fails at once, which is what makes it visible in the UI as a
// failure with a reason rather than a run that is still "trying".
func TestARefusedCredentialFailsAtOnceRatherThanRetrying(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	core := &fakeExpirer{err: connect.NewError(connect.CodePermissionDenied,
		errors.New("token does not carry internal:ingest"))}
	activities := &Activities{CoreAPI: core}
	env.RegisterActivityWithOptions(activities.ExpireSnoozes, activityOptions(ExpireSnoozesActivityName))

	env.ExecuteWorkflow(ExpireSnoozedFindingsWorkflow)

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("the workflow succeeded against a refused credential")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) || !appErr.NonRetryable() {
		t.Fatalf("the failure was not marked non-retryable: %v", err)
	}
	if core.calls != 1 {
		t.Fatalf("core-api was called %d times for a refusal, want exactly 1", core.calls)
	}
}

// The activity on its own, against the fake, for the mapping that the
// workflow tests above depend on.
func TestTheActivityMapsTheResponse(t *testing.T) {
	core := &fakeExpirer{count: 7}
	activities := &Activities{CoreAPI: core}

	result, err := activities.ExpireSnoozes(context.Background())
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if result.Reemerged != 7 {
		t.Errorf("reemerged = %d, want 7", result.Reemerged)
	}
	if result.RanAt.Year() != 2026 {
		t.Errorf("ran_at = %v, want the time core-api reported", result.RanAt)
	}
}
