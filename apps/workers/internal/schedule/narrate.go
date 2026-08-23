package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Narration as the third step of a sweep (ENT-256, part five): Go loads,
// Python drafts, Go persists (§16.4), each retrying on its own.
//
// # THE THREE ACTIVITIES, AND WHERE EACH RUNS
//
//	NextFindingToNarrate   Go, on `core`: asks core-api for the next finding
//	                       with no narrative, with its draft request built
//	                       (the signal, the one obligation it may cite, the
//	                       provider and model names for the run record).
//	DraftNarrative         Python, on `intelligence`: the harness, the
//	                       guardrail ring, the citation validator and the
//	                       `agent_runs` record, exactly as the RPC of the same
//	                       name, with every model call going back through
//	                       core-api's CompletionService so the worker holds no
//	                       endpoint and no key.
//	RecordNarrative        Go, on `core`: writes the narrative or the refusal
//	                       against the finding through core-api.
//
// Two task queues served by two languages, which is the payoff §16.4 named:
// a draft that fails retries as a draft, a record that fails retries as a
// record, and neither holds the other's pool.
//
// # WHY IT IS A STEP OF THE SWEEP AND NOT A STEP INSIDE IT
//
// narrative.go's argument stands unchanged: a model call inside the sweep's
// transaction would hold it open for minutes per finding on a local model,
// block the act path behind its locks, and lose everything on a timeout. So
// the sweep (Watcher, Analyst) stays fast and deterministic, and narration
// follows it, after the trigger is settled and the feed already shows every
// finding with its deterministic text. Explanations arrive as they are
// drafted.
//
// # WHAT CROSSES INTO THE HISTORY
//
// The draft request: the finding's own words and the obligation it may cite,
// which is the customer's compliance exposure, and the draft: the narrative.
// §16.3 accepted exactly this when it chose the three-activity shape and made
// the namespace retention a personal-data decision, and it is why the
// retention is set deliberately short. What does NOT cross is any credential:
// since the completions hardening (#239) the Python worker asks core-api for
// every model call, and a key rides in no activity input anywhere.
//
// # A DEPLOYMENT WITHOUT A PYTHON WORKER
//
// Two cases, told apart. A deployment with no Intelligence at all (no model
// profile) answers `intelligence_available: false` at the load step and costs
// one activity. A deployment that runs Intelligence but whose worker is down
// has nobody polling `intelligence`: the draft activity's schedule-to-start
// timeout fires, narration is recorded as skipped with that reason, and the
// sweep is not failed. The next sweep asks again.

// Registered activity names. DraftNarrativeActivityName is the name the
// Python worker registers; it is pinned here and in
// apps/intelligence/src/kindlast_intelligence/worker.py, and the two must
// agree, which the end-to-end run is what proves.
const (
	NextFindingToNarrateActivityName = "NextFindingToNarrate"
	DraftNarrativeActivityName       = "DraftNarrative"
	RecordNarrativeActivityName      = "RecordNarrative"
)

// IntelligenceTaskQueue is the queue the Python worker polls (§16.4). Pinned
// rather than configured: the two workers name it from their own
// configuration, and a mismatch would be a draft that nobody ever picks up,
// which the schedule-to-start timeout would report but a constant prevents.
const IntelligenceTaskQueue = "intelligence"

// Narrator is what the Go activities need of core-api's NarrativeService,
// declared where it is used (§21.6).
type Narrator interface {
	NextFindingToNarrate(ctx context.Context, req *connect.Request[platformv1.NextFindingToNarrateRequest]) (*connect.Response[platformv1.NextFindingToNarrateResponse], error)
	RecordNarrative(ctx context.Context, req *connect.Request[platformv1.RecordNarrativeRequest]) (*connect.Response[platformv1.RecordNarrativeResponse], error)
}

// maxNarrationsPerSweep bounds how many findings one sweep run narrates. The
// next sweep (the daily one, or the next trigger) picks up where this left
// off, because core-api always offers findings that have no narrative yet.
const maxNarrationsPerSweep = 50

// NarrationResult is what narration did for one organisation in one run.
type NarrationResult struct {
	// Available is false when the deployment runs no Intelligence, which is a
	// supported configuration: nothing was attempted and nothing is wrong.
	Available bool
	Narrated  int32
	Refused   int32
	Failed    int32
	// Skipped is set when narration stopped for a reason that is not a
	// finding's own: this organisation's model choice could not be honoured,
	// or no Python worker is polling the `intelligence` queue. The sweep is
	// not failed; the next one asks again.
	Skipped string
}

// NarrationCandidate is the load step's answer, as the workflow keeps it.
type NarrationCandidate struct {
	Available bool
	Found     bool
	FindingID string
	Draft     *platformv1.DraftNarrativeRequest
}

// narrateOrganisation drafts narratives for an organisation's findings that
// have none, one per draft activity, until there are none or the cap is
// reached.
func narrateOrganisation(ctx workflow.Context, orgID string) NarrationResult {
	loadCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    5,
		},
	})
	draftCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// THE ONLY ACTIVITY IN THIS BINARY THAT RUNS SOMEWHERE ELSE. The
		// Python worker polls this queue; nothing in `workers` registers this
		// name.
		TaskQueue: IntelligenceTaskQueue,
		// If nobody picks the draft up in two minutes, nobody is polling:
		// the Python worker is down or this deployment never ran one. Fail
		// fast with a reason rather than holding the sweep for ten minutes
		// per finding waiting for a worker that is not coming.
		ScheduleToStartTimeout: 2 * time.Minute,
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
	recordCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})

	var result NarrationResult
	for i := 0; i < maxNarrationsPerSweep; i++ {
		var candidate NarrationCandidate
		if err := workflow.ExecuteActivity(loadCtx, NextFindingToNarrateActivityName, orgID).Get(ctx, &candidate); err != nil {
			// A refusal retrying cannot fix (the provider cannot be honoured)
			// or the retries exhausted: record it and stop. The sweep itself
			// succeeded; this is the explanation step not happening this run.
			result.Skipped = err.Error()
			return result
		}
		result.Available = candidate.Available
		if !candidate.Available || !candidate.Found {
			return result
		}

		var draft platformv1.DraftNarrativeResponse
		if err := workflow.ExecuteActivity(draftCtx, DraftNarrativeActivityName, candidate.Draft).Get(ctx, &draft); err != nil {
			var timeout *temporal.TimeoutError
			if errors.As(err, &timeout) && timeout.TimeoutType() == enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START {
				result.Skipped = "no Intelligence worker is polling the " + IntelligenceTaskQueue + " task queue"
			} else {
				result.Skipped = err.Error()
			}
			return result
		}

		var recorded bool
		if err := workflow.ExecuteActivity(recordCtx, RecordNarrativeActivityName, orgID, candidate.FindingID, &draft).Get(ctx, &recorded); err != nil {
			result.Skipped = err.Error()
			return result
		}
		switch draft.GetOutcome() {
		case platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED:
			result.Narrated++
		case platformv1.DraftOutcome_DRAFT_OUTCOME_REFUSED:
			result.Refused++
		default:
			result.Failed++
		}
	}
	return result
}

// NextFindingToNarrate asks core-api for the next finding to narrate, with
// its draft request built.
//
// `failed_precondition` (this organisation's provider cannot be honoured) is
// marked non-retryable: nothing changes by waiting, the guardrail is working,
// and the workflow records it and moves on.
func (a *Activities) NextFindingToNarrate(ctx context.Context, orgID string) (NarrationCandidate, error) {
	res, err := a.Narratives.NextFindingToNarrate(ctx, connect.NewRequest(&platformv1.NextFindingToNarrateRequest{
		OrgId: orgID,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeFailedPrecondition {
			return NarrationCandidate{}, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("narration skipped for this organisation: %v", err), "provider", err)
		}
		return NarrationCandidate{}, badRequestOrRefusal(err)
	}
	return NarrationCandidate{
		Available: res.Msg.GetIntelligenceAvailable(),
		Found:     res.Msg.GetFound(),
		FindingID: res.Msg.GetFindingId(),
		Draft:     res.Msg.GetDraft(),
	}, nil
}

// RecordNarrative writes what the Python worker's draft produced.
func (a *Activities) RecordNarrative(ctx context.Context, orgID, findingID string, draft *platformv1.DraftNarrativeResponse) (bool, error) {
	res, err := a.Narratives.RecordNarrative(ctx, connect.NewRequest(&platformv1.RecordNarrativeRequest{
		OrgId:     orgID,
		FindingId: findingID,
		Draft:     draft,
	}))
	if err != nil {
		return false, badRequestOrRefusal(err)
	}
	return res.Msg.GetRecorded(), nil
}
