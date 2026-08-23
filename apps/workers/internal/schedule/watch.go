package schedule

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The agentic Watcher as a step of a sweep (ENT-258, PR 2).
//
// # WHERE IT SITS, AND WHY THERE
//
// A sweep is Watcher, Analyst, then narration. This goes between the first two:
//
//	RunWatcher   the three deterministic detectors, unchanged
//	LoadWatchContext + Watch   this
//	RunAnalyst   turns every open signal into findings
//	narrate      explains them
//
// After the detectors, because the agent is shown what is already open and
// deciding "that is already known" is most of what stops it repeating itself.
// Before the Analyst, because a signal the agent raises should become a finding
// in the same sweep rather than waiting for tomorrow's.
//
// # TWO ACTIVITIES, NOT THREE, AND THAT IS THE DIFFERENCE FROM NARRATION
//
// Narration is Go loads, Python drafts, Go persists, because a draft is one
// value the caller can take and write. A watch is not: the model decides DURING
// the run whether to raise, and what a raise did changes what it decides next.
// So the write is a tool inside the loop, reached through
// `WatcherService.RaiseSignal`, and there is nothing left for Go to persist.
//
// What keeps that within the rule is that the tool is a core-api RPC like every
// other: it validates the vocabulary, requires a deduplication key, resolves
// the citation and writes under the producer role's policies. The Python worker
// gains one call it may make, not a database handle.
//
// # OFF UNLESS AN OPERATOR TURNS IT ON
//
// `KINDLAST_WATCHER_AGENT=1`. The deterministic detectors are what every
// deployment runs today and they stay the baseline ENT-258 compares against;
// turning an unproven agent loose on every customer's daily sweep is not
// something a PR that has not run the comparison yet should do. PR 3 runs the
// comparison in CI and is where the default changes.
//
// A deployment with the flag on and no Intelligence costs one activity and does
// nothing: the load step answers `intelligence_available: false` and the step
// returns.

// Registered activity names. WatchActivityName is the name the Python worker
// registers; it is pinned here and in
// apps/intelligence/src/kindlast_intelligence/worker.py, and the two must
// agree, which the end-to-end run is what proves.
const (
	LoadWatchContextActivityName = "LoadWatchContext"
	WatchActivityName            = "Watch"
)

// Watcher is what the load activity needs of core-api's WatcherService,
// declared where it is used (§21.6).
type Watcher interface {
	WatcherContext(ctx context.Context, req *connect.Request[platformv1.WatcherContextRequest]) (*connect.Response[platformv1.WatcherContextResponse], error)
}

// WatchCandidate is the load step's answer, as the workflow keeps it.
type WatchCandidate struct {
	// Available is false when the deployment runs no Intelligence, or when the
	// organisation has no compliance profile. Two reasons, one answer, because
	// what the workflow does about either is the same: nothing, quietly.
	Available bool
	Request   *platformv1.WatchRequest
}

// WatchResult is what the agentic Watcher did for one organisation.
type WatchResult struct {
	// Ran is false when the step was switched off, when the deployment has no
	// Intelligence, or when the organisation has nothing to watch yet.
	Ran bool
	// Raised counts the signals that were new; Repeated counts the ones the
	// deduplication key already covered. Both, because a run whose every
	// observation was already open is a run that worked and a count of zero
	// new signals on its own reads like a run that saw nothing.
	Raised   int32
	Repeated int32
	Refused  bool
	// Detail is why, when the run did not succeed, or why the step did not
	// run. For an operator reading a history.
	Detail string
}

// watchOrganisation runs the agentic Watcher over one organisation.
//
// Never returns an error. The agent is an addition to a sweep that already
// works: a model that is down, a worker that is not polling, or a run the
// guardrails refused must not fail a sweep whose deterministic half has
// already produced findings. Everything that went wrong is in the result, and
// the history has the activity's own failure beside it.
func watchOrganisation(ctx workflow.Context, orgID string) WatchResult {
	loadCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    5,
		},
	})
	watchCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// Runs on the Python worker's queue; nothing in this binary registers
		// this name.
		TaskQueue: IntelligenceTaskQueue,
		// If nobody picks it up in two minutes, nobody is polling. Same
		// reasoning and same number as the draft step.
		ScheduleToStartTimeout: 2 * time.Minute,
		// A watch is several model calls rather than one, so it gets longer
		// than a draft. Twenty minutes is generous for one organisation on a
		// local model and short enough that a hung call does not hold a sweep
		// for an hour; the harness's own budget refuses long before this on a
		// healthy stack.
		StartToCloseTimeout: 20 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    15 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
			// BOUNDED, AND FOR A SHARPER REASON THAN THE DRAFT'S.
			//
			// A retry re-runs a loop that has already written signals. That is
			// safe because a signal is deduplicated on its key, so the second
			// attempt updates rows the first created rather than duplicating
			// them, which is the same property that lets the daily sweep run
			// every day. Safe is not free: three attempts is enough to ride
			// out a restarting worker and few enough that a genuinely broken
			// model does not spend an hour re-deciding.
			MaximumAttempts: 3,
		},
	})

	var candidate WatchCandidate
	if err := workflow.ExecuteActivity(loadCtx, LoadWatchContextActivityName, orgID).Get(ctx, &candidate); err != nil {
		return WatchResult{Detail: err.Error()}
	}
	if !candidate.Available {
		return WatchResult{Detail: "no Intelligence, or nothing to watch yet"}
	}

	var watched platformv1.WatchResponse
	if err := workflow.ExecuteActivity(watchCtx, WatchActivityName, candidate.Request).Get(ctx, &watched); err != nil {
		var timeout *temporal.TimeoutError
		if errors.As(err, &timeout) && timeout.TimeoutType() == enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START {
			return WatchResult{Detail: "no Intelligence worker is polling the " + IntelligenceTaskQueue + " task queue"}
		}
		return WatchResult{Detail: err.Error()}
	}

	result := WatchResult{Ran: true, Detail: watched.GetOutcomeDetail()}
	for _, signal := range watched.GetSignals() {
		if signal.GetRaised() {
			result.Raised++
			continue
		}
		result.Repeated++
	}
	// REFUSED IS RECORDED AND IS NOT A FAILURE (§26.3). A run stopped by its
	// own guardrail may still have raised something before it stopped, which
	// is why the counts above are read either way.
	result.Refused = watched.GetOutcome() == platformv1.WatchOutcome_WATCH_OUTCOME_REFUSED
	return result
}

// LoadWatchContext asks core-api for everything one watch reasons over, and
// builds the request the Python activity is handed.
//
// `failed_precondition` (this organisation chose a provider core-api cannot
// honour) is marked non-retryable: nothing changes by waiting, the guardrail is
// working, and the workflow records it and moves on.
func (a *Activities) LoadWatchContext(ctx context.Context, orgID string) (WatchCandidate, error) {
	res, err := a.Watchers.WatcherContext(ctx, connect.NewRequest(&platformv1.WatcherContextRequest{
		OrgId: orgID,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeFailedPrecondition {
			return WatchCandidate{}, temporal.NewNonRetryableApplicationError(
				"the agentic Watcher was skipped for this organisation: "+err.Error(), "provider", err)
		}
		return WatchCandidate{}, badRequestOrRefusal(err)
	}

	// An organisation part way through onboarding has no profile, so there is
	// nothing to watch and nowhere to hang a signal. Not an error: it is the
	// normal state of an organisation that signed up an hour ago.
	if !res.Msg.GetHasProfile() || !res.Msg.GetIntelligenceAvailable() {
		return WatchCandidate{}, nil
	}

	return WatchCandidate{
		Available: true,
		Request: &platformv1.WatchRequest{
			OrgId:   orgID,
			Context: res.Msg,
			// Built here from the two names the context carries. The message
			// lives in `intelligence.proto`, which imports `watcher.proto`,
			// so the context could not name it without a cycle; see the
			// field's comment there for why that was not solved by moving it.
			ModelEndpoint: &platformv1.ModelEndpoint{
				Provider: res.Msg.GetModelProvider(),
				Model:    res.Msg.GetModelName(),
			},
		},
	}, nil
}
