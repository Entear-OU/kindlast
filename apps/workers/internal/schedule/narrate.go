package schedule

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Narration as the third step of a sweep (ENT-256, part five; ENT-245's "the
// half nobody scheduled", now scheduled).
//
// # WHY IT IS A STEP OF THE SWEEP AND NOT A STEP INSIDE IT
//
// narrative.go's argument stands unchanged: a model call inside the sweep's
// transaction would hold it open for minutes per finding on a local model,
// block the act path behind its locks, and lose everything on a timeout. So
// the sweep (Watcher, Analyst) stays fast and deterministic, and narration
// follows it as its own activities, after the trigger is settled and the feed
// already shows every finding with its deterministic text. Explanations arrive
// as they are drafted. What ENT-245 called "a queue's problem rather than a
// request's" is now literally a queue's: each draft is one activity with a
// retry policy, and the history says which finding took how long and why.
//
// # ONE FINDING PER ACTIVITY
//
// NarrateFindings takes a batch size, and the activity asks for one. A local
// model takes minutes per finding; one finding is one bounded, visible,
// retryable unit, and a slow or refused draft costs one activity rather than
// a batch. The loop that asks again until nothing is pending is the
// workflow's, with a cap, so an organisation with a thousand findings does not
// narrate for a day inside one run.
//
// # WHERE THIS DOES NOT GO, STATED RATHER THAN LEFT IMPLICIT
//
// §16.4 planned "Go loads, Python drafts, Go persists" as three activities
// with the draft on an `intelligence` task queue served by the Python service.
// That shape puts the draft's input into a workflow history, and an
// organisation's own provider key (ENT-236) cannot ride in it; the one
// recorded exception to "Intelligence obtains no credential" is the key
// arriving in the DraftNarrative request and nowhere else. So the model call
// stays behind core-api's NarrateFindings, which opens the key and makes the
// call, and this activity is the Go step that asks for it. Whether the local
// model path alone moves to a Python queue is a ruling for the maintainers,
// recorded in the PR and the issue rather than decided here.

// NarrateFindingsActivityName is the registered activity name.
const NarrateFindingsActivityName = "NarrateFindings"

// Narrator is what the activity needs of core-api's NarrativeService,
// declared where it is used (§21.6).
type Narrator interface {
	NarrateFindings(ctx context.Context, req *connect.Request[platformv1.NarrateFindingsRequest]) (*connect.Response[platformv1.NarrateFindingsResponse], error)
}

// maxNarrationsPerSweep bounds how many findings one sweep run narrates. The
// next sweep (the daily one, or the next trigger) picks up where this left
// off, because NarrateFindings always asks for findings that have no
// narrative yet.
const maxNarrationsPerSweep = 50

// NarrationResult is what narration did for one organisation in one run.
type NarrationResult struct {
	// Available is false when the deployment runs no Intelligence, which is a
	// supported configuration: nothing was attempted and nothing is wrong.
	Available bool
	Narrated  int32
	Refused   int32
	Failed    int32
	// Skipped is set when this organisation's model choice could not be
	// honoured (a withdrawn provider, a key that will not open): narration is
	// skipped for it, with the reason, and the sweep is not failed.
	Skipped string
}

// narrateOrganisation drafts narratives for an organisation's findings that
// have none, one per activity, until there are none or the cap is reached.
func narrateOrganisation(ctx workflow.Context, orgID string) NarrationResult {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// One draft on a local model. Ten minutes is generous for one finding
		// and short enough that a hung model call does not hold a sweep for
		// an hour.
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    15 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
			// Bounded, unlike the sweep's own activities: a draft that fails
			// five times is a model that is down, and the next sweep will ask
			// again. Narration retrying forever would hold the run open past
			// the next tick for findings whose deterministic text is already
			// in the feed.
			MaximumAttempts: 5,
		},
	})

	var result NarrationResult
	for i := 0; i < maxNarrationsPerSweep; i++ {
		var pass NarrationPass
		if err := workflow.ExecuteActivity(ctx, NarrateFindingsActivityName, orgID).Get(ctx, &pass); err != nil {
			// A refusal retrying cannot fix (the provider cannot be honoured)
			// or the retries exhausted: record it on the result and stop.
			// The sweep itself succeeded; this is the explanation step not
			// happening this run, which the feed survives.
			result.Skipped = err.Error()
			return result
		}
		result.Available = pass.Available
		if !pass.Available || pass.Attempted == 0 {
			return result
		}
		result.Narrated += pass.Narrated
		result.Refused += pass.Refused
		result.Failed += pass.Failed
		if pass.Narrated+pass.Refused == 0 {
			// No progress: the finding core-api offered failed to draft and
			// still has no narrative, so asking again now would be offered
			// the same finding. The next sweep asks again.
			return result
		}
	}
	return result
}

// NarrationPass is what one NarrateFindings call reported.
type NarrationPass struct {
	Available bool
	Attempted int32
	Narrated  int32
	Refused   int32
	Failed    int32
}

// NarrateFindings asks core-api to narrate one of an organisation's findings
// that has no narrative yet.
//
// `failed_precondition` (this organisation's provider cannot be honoured) is
// marked non-retryable: nothing changes by waiting, the guardrail is working,
// and the workflow records it and moves on. Everything else retries under the
// policy above.
func (a *Activities) NarrateFindings(ctx context.Context, orgID string) (NarrationPass, error) {
	res, err := a.Narratives.NarrateFindings(ctx, connect.NewRequest(&platformv1.NarrateFindingsRequest{
		OrgId:       orgID,
		MaxFindings: 1,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeFailedPrecondition {
			return NarrationPass{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("narration skipped for this organisation: %v", err), "provider", err)
		}
		return NarrationPass{}, badRequestOrRefusal(err)
	}
	return NarrationPass{
		Available: res.Msg.GetIntelligenceAvailable(),
		Attempted: res.Msg.GetAttempted(),
		Narrated:  res.Msg.GetNarrated(),
		Refused:   res.Msg.GetRefused(),
		Failed:    res.Msg.GetFailed(),
	}, nil
}
