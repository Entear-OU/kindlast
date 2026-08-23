package schedule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The Watcher-to-Analyst chain as a workflow (ENT-256, part four; design §16.2,
// §20.3). This is what step 8 was for.
//
// # TWO THINGS START A SWEEP
//
// Confirming onboarding writes a `sweep_triggers` row in the same transaction
// as the profile (00035). The `relay-sweep-triggers` Schedule lists pending
// rows every few seconds and starts one TriggeredSweepWorkflow per row, id
// `sweep/{trigger id}`, so a trigger is run by at most one workflow at a time
// and the console shows findings within seconds of "confirm" without anybody
// calling RunSweep by hand. And the `sweep-every-organisation` Schedule runs
// DailySweepWorkflow once a day: list every organisation with a profile, sweep
// each, bounded concurrency, one organisation's failure never failing the
// rest.
//
// # THE ANALYST IS THE NEXT STEP, NOT THE NEXT SCHEDULE
//
// pg_cron used to run the Watcher at 06:00 and the Analyst at 06:05 and hope.
// Here a sweep is two activities in sequence, Watcher then Analyst, each with
// its own retry, ordered by success rather than by clock. Part five puts the
// Python activity (the narrative) between them on its own task queue, which is
// why they are two activities rather than RunSweep doing both.
//
// # "SWEEP EVERYONE", WRITTEN DELIBERATELY
//
// RunSweep refuses to sweep more than one organisation because the blast
// radius is every customer at once. The daily workflow is the loop sweep.proto
// says somebody should have to write: it is visible, pausable and readable in
// the UI, one organisation per activity pair, and a schedule an operator can
// trigger by hand after an upgrade to see the whole estate sweep.

// Schedule ids.
const (
	SweepTriggerRelayScheduleID = "relay-sweep-triggers"
	DailySweepScheduleID        = "sweep-every-organisation"
)

// Registered activity names.
const (
	ListSweepTriggersActivityName    = "ListSweepTriggers"
	StartTriggeredSweepsActivityName = "StartTriggeredSweeps"
	RunWatcherActivityName           = "RunWatcher"
	RunAnalystActivityName           = "RunAnalyst"
	SettleSweepTriggerActivityName   = "SettleSweepTrigger"
	ListSweepTargetsActivityName     = "ListSweepTargets"
)

// triggeredSweepWorkflowID is the workflow id for one trigger, and the thing
// that makes a trigger run by at most one workflow at a time.
func triggeredSweepWorkflowID(triggerID string) string {
	return "sweep/" + triggerID
}

// Sweeper is what the activities need of core-api's SweepService, declared
// where it is used (§21.6).
type Sweeper interface {
	RunSweep(ctx context.Context, req *connect.Request[platformv1.RunSweepRequest]) (*connect.Response[platformv1.RunSweepResponse], error)
	RunAnalyst(ctx context.Context, req *connect.Request[platformv1.RunAnalystRequest]) (*connect.Response[platformv1.RunAnalystResponse], error)
	ListSweepTriggers(ctx context.Context, req *connect.Request[platformv1.ListSweepTriggersRequest]) (*connect.Response[platformv1.ListSweepTriggersResponse], error)
	SettleSweepTrigger(ctx context.Context, req *connect.Request[platformv1.SettleSweepTriggerRequest]) (*connect.Response[platformv1.SettleSweepTriggerResponse], error)
	ListSweepTargets(ctx context.Context, req *connect.Request[platformv1.ListSweepTargetsRequest]) (*connect.Response[platformv1.ListSweepTargetsResponse], error)
}

// OrgHeader is the header core-api reads the organisation from on the sweep
// surface, repeated here rather than imported from core-api's interceptor
// package because this module does not depend on core-api's internals.
const OrgHeader = "Kindlast-Org-Id"

// Trigger is one pending sweep trigger as the relay lists it.
type Trigger struct {
	ID    string
	OrgID string
}

// RelaySweepTriggersWorkflow is the workflow behind `relay-sweep-triggers`:
// list what is pending, start a sweep for each. Same bounds and reasons as
// RelayOutboxWorkflow.
func RelaySweepTriggersWorkflow(ctx workflow.Context) (RelayResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var triggers []Trigger
	if err := workflow.ExecuteActivity(ctx, ListSweepTriggersActivityName).Get(ctx, &triggers); err != nil {
		return RelayResult{}, err
	}
	if len(triggers) == 0 {
		return RelayResult{}, nil
	}
	var started RelayResult
	if err := workflow.ExecuteActivity(ctx, StartTriggeredSweepsActivityName, triggers).Get(ctx, &started); err != nil {
		return RelayResult{}, err
	}
	return started, nil
}

// sweepOptions are the activity options for the Watcher and the Analyst.
//
// A sweep over one organisation is seconds of SQL today. Five minutes covers
// a profile-heavy organisation on a busy stack; the retry backs off and keeps
// going, because every reason a sweep fails (core-api restarting, the pool
// full) resolves on its own and both functions are idempotent (signals are
// deduplicated, the Analyst works over unprocessed signals). Capped by the
// enclosing workflow's own timeout rather than by an attempt count.
func sweepOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    5 * time.Minute,
		},
	})
}

// SweepResult is what one organisation's sweep did.
type SweepResult struct {
	OrgID    string
	Signals  int32
	Findings int32
	RanAt    time.Time
	// Narration is the third step, after the findings exist (part five).
	Narration NarrationResult
	// Watch is what the agentic Watcher did, when an operator has turned it
	// on (ENT-258). Zero-valued and `Ran: false` otherwise, which is every
	// deployment today.
	Watch WatchResult
}

// sweepOrganisation runs the Watcher, then the Analyst, for one organisation:
// the chain itself, shared by the triggered and the daily workflows.
func sweepOrganisation(ctx workflow.Context, orgID string) (SweepResult, error) {
	sweepCtx := sweepOptions(ctx)
	var watched SweepResult
	if err := workflow.ExecuteActivity(sweepCtx, RunWatcherActivityName, orgID).Get(ctx, &watched); err != nil {
		return SweepResult{}, err
	}

	// THE AGENT BETWEEN THE TWO (ENT-258).
	//
	// After the detectors so it is shown what they already raised, which is
	// most of what stops it repeating them. Before the Analyst so a signal it
	// raises becomes a finding in this sweep rather than tomorrow's.
	//
	// Read from the workflow's own configuration rather than an env lookup
	// here: a workflow that read the environment would decide differently on
	// replay after an operator changed it, which is the determinism rule
	// Temporal enforces by refusing to replay.
	var agentic WatchResult
	if agenticWatcherEnabled(ctx) {
		agentic = watchOrganisation(ctx, orgID)
	}

	var analysed SweepResult
	if err := workflow.ExecuteActivity(sweepCtx, RunAnalystActivityName, orgID).Get(ctx, &analysed); err != nil {
		return SweepResult{}, err
	}
	return SweepResult{
		OrgID:    orgID,
		Signals:  watched.Signals,
		Findings: analysed.Findings,
		RanAt:    analysed.RanAt,
		Watch:    agentic,
	}, nil
}

// agenticWatcherEnabled reports whether this deployment runs the agentic
// Watcher as part of a sweep.
//
// # ON UNLESS SOMEBODY TURNS IT OFF (ENT-258, PR 3)
//
// It shipped off, because a PR that had not run the comparison should not have
// turned an unproven agent loose on every customer's daily sweep. The
// comparison runs in CI now, against a real model on a real stack, and what it
// gates is the part that matters: every signal the fixed detectors raised
// survives the agent untouched, nothing is written outside the vocabulary or
// citing an obligation the run was not offered, and no finding is written.
//
// So the default moved. `KINDLAST_WATCHER_AGENT=0` turns it off, and the
// reason to reach for that is cost rather than safety: a watch is several
// model calls where a draft is one, so a deployment running a local model on
// modest hardware pays minutes per organisation per sweep.
//
// # WHY A SIDE EFFECT AND NOT os.Getenv
//
// A workflow must produce the same decisions when it is replayed, and an
// environment read is not a decision Temporal recorded: an operator who
// changes the variable between a run and its replay would make the replay take
// a different path, and the SDK fails the workflow with a non-determinism
// error rather than silently diverging. `workflow.SideEffect` records the
// value in the history the first time and hands back the recorded one ever
// after, which is exactly the property wanted here.
func agenticWatcherEnabled(ctx workflow.Context) bool {
	var enabled bool
	if err := workflow.SideEffect(ctx, func(workflow.Context) any {
		return !falsy(os.Getenv("KINDLAST_WATCHER_AGENT"))
	}).Get(&enabled); err != nil {
		// A side effect that cannot be recorded is a broken history, not a
		// reason to change what the deployment does. Default to the default.
		return true
	}
	return enabled
}

// falsy reads the off switch, and reads ONLY the off switch.
//
// Unset means on, so this asks whether somebody said no rather than whether
// somebody said yes. Written this way round on purpose: a `truthy` helper with
// an inverted default is the shape where somebody eventually reads
// `truthy(os.Getenv(...))` at a glance and concludes the opposite of what the
// code does.
func falsy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// TriggeredSweepWorkflow sweeps one organisation because somebody asked
// (today: they confirmed onboarding), and settles the trigger.
//
// The chain retries with backoff and no attempt limit, the same "retry
// forever, visibly" the outbox has, so a transient failure never reaches the
// settle-as-failed path. What does is a failure retrying cannot fix (a refused
// credential, a request core-api calls malformed): the trigger then records
// the attempt and the reason, stays pending, and the relay starts a fresh
// workflow for it once this one has closed and an operator has fixed the
// cause.
func TriggeredSweepWorkflow(ctx workflow.Context, trigger Trigger) (SweepResult, error) {
	result, sweepErr := sweepOrganisation(ctx, trigger.OrgID)

	settleCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    time.Minute,
		},
	})
	var cause string
	if sweepErr != nil {
		cause = sweepErr.Error()
	}
	var settled bool
	if err := workflow.ExecuteActivity(settleCtx, SettleSweepTriggerActivityName, trigger.ID, cause).Get(ctx, &settled); err != nil {
		return result, err
	}
	if sweepErr != nil {
		return result, sweepErr
	}

	// Settled first, narrated second, on purpose. The trigger says done the
	// moment findings exist and the feed shows them with their deterministic
	// text; the explanations arrive as a local model drafts them, which can
	// be minutes each, and a person who just confirmed onboarding should not
	// wait on that to see the first finding.
	result.Narration = narrateOrganisation(ctx, trigger.OrgID)
	return result, nil
}

// DailySweepResult is what one daily run did, as the history keeps it:
// counts, and the organisations that failed (ids only) so an operator can
// find their histories.
type DailySweepResult struct {
	Organisations int
	Swept         int
	Failed        []string
	Signals       int32
	Findings      int32
	// Narrated is how many findings got their explanation across the estate
	// this run; NarrationSkipped lists the organisations whose model choice
	// could not be honoured, by id.
	Narrated         int32
	NarrationSkipped []string
}

// dailyConcurrency bounds how many organisations are swept at once.
//
// Each sweep is one organisation's worth of SQL on the producer pool, and the
// pool is shared with every other activity and the agent run path. Four at a
// time finishes a thousand organisations in minutes and leaves the pool
// breathing; the number is a tuning knob rather than a correctness one.
const dailyConcurrency = 4

// DailySweepWorkflow is the workflow behind `sweep-every-organisation`.
//
// List, then fan out, with a semaphore rather than all at once. A failed
// organisation is recorded and the rest continue: the whole point of one
// activity pair per organisation is that one tenant's broken profile cannot
// stop the estate from being swept.
//
// When the corpus ingest becomes a workflow of its own (§20.3), it runs
// before this fans out, and "ordered by success rather than by clock" applies
// one level up. Today the corpus is loaded by a job at deploy and this runs
// against whatever is there.
func DailySweepWorkflow(ctx workflow.Context) (DailySweepResult, error) {
	listCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
		},
	})
	var orgs []string
	if err := workflow.ExecuteActivity(listCtx, ListSweepTargetsActivityName).Get(ctx, &orgs); err != nil {
		return DailySweepResult{}, err
	}

	result := DailySweepResult{Organisations: len(orgs)}
	if len(orgs) == 0 {
		return result, nil
	}

	// A Temporal-safe fan-out: workflow.Go coroutines, a buffered channel as
	// the semaphore, and a WaitGroup. Deterministic on replay because the
	// coroutines are scheduled by the workflow, not by the Go runtime.
	semaphore := workflow.NewBufferedChannel(ctx, dailyConcurrency)
	wg := workflow.NewWaitGroup(ctx)
	var swept []string
	for _, orgID := range orgs {
		wg.Add(1)
		workflow.Go(ctx, func(ctx workflow.Context) {
			defer wg.Done()
			semaphore.Send(ctx, struct{}{})
			defer semaphore.Receive(ctx, nil)

			sweep, err := sweepOrganisation(ctx, orgID)
			if err != nil {
				result.Failed = append(result.Failed, orgID)
				return
			}
			result.Swept++
			result.Signals += sweep.Signals
			result.Findings += sweep.Findings
			swept = append(swept, orgID)
		})
	}
	wg.Wait(ctx)

	// Narration afterwards, and one organisation at a time. One
	// `llama-server` serves one request at a time, so drafting four
	// organisations' findings in parallel moves the queue rather than
	// shortening it and turns one slow finding into a pile of timeouts; the
	// sweeps were parallel because they are seconds of SQL, and this is not.
	// The first organisation answers whether Intelligence exists at all, and
	// an estate without it costs one activity rather than one per tenant.
	for _, orgID := range swept {
		narration := narrateOrganisation(ctx, orgID)
		result.Narrated += narration.Narrated
		if narration.Skipped != "" {
			result.NarrationSkipped = append(result.NarrationSkipped, orgID)
		}
		if !narration.Available && narration.Skipped == "" {
			break
		}
	}
	return result, nil
}

// ListSweepTriggers asks core-api which sweeps are waiting.
func (a *Activities) ListSweepTriggers(ctx context.Context) ([]Trigger, error) {
	res, err := a.Sweeps.ListSweepTriggers(ctx, connect.NewRequest(&platformv1.ListSweepTriggersRequest{}))
	if err != nil {
		return nil, nonRetryableIfRefused(err)
	}
	var out []Trigger
	for _, t := range res.Msg.GetTriggers() {
		out = append(out, Trigger{ID: t.GetTriggerId(), OrgID: t.GetOrgId()})
	}
	return out, nil
}

// StartTriggeredSweeps starts one TriggeredSweepWorkflow per trigger, and
// counts, idempotent on the id exactly as StartDeliveries is.
func (a *Activities) StartTriggeredSweeps(ctx context.Context, triggers []Trigger) (RelayResult, error) {
	result := RelayResult{Pending: len(triggers)}
	for _, t := range triggers {
		_, err := a.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:                       triggeredSweepWorkflowID(t.ID),
			TaskQueue:                a.TaskQueue,
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		}, TriggeredSweepWorkflow, t)
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		switch {
		case err == nil:
			result.Started++
		case errors.As(err, &already):
			result.AlreadyRunning++
		default:
			return result, fmt.Errorf("starting the sweep for trigger %s: %w", t.ID, err)
		}
	}
	return result, nil
}

// RunWatcher asks core-api to run the Watcher for one organisation: RunSweep
// with detect_only, which is the half that raises signals.
func (a *Activities) RunWatcher(ctx context.Context, orgID string) (SweepResult, error) {
	req := connect.NewRequest(&platformv1.RunSweepRequest{DetectOnly: true})
	req.Header().Set(OrgHeader, orgID)
	res, err := a.Sweeps.RunSweep(ctx, req)
	if err != nil {
		return SweepResult{}, badRequestOrRefusal(err)
	}
	result := SweepResult{OrgID: orgID, Signals: res.Msg.GetSignals()}
	if ranAt := res.Msg.GetRanAt(); ranAt != nil {
		result.RanAt = ranAt.AsTime()
	}
	return result, nil
}

// RunAnalyst asks core-api to run the Analyst for one organisation.
func (a *Activities) RunAnalyst(ctx context.Context, orgID string) (SweepResult, error) {
	req := connect.NewRequest(&platformv1.RunAnalystRequest{})
	req.Header().Set(OrgHeader, orgID)
	res, err := a.Sweeps.RunAnalyst(ctx, req)
	if err != nil {
		return SweepResult{}, badRequestOrRefusal(err)
	}
	result := SweepResult{OrgID: orgID, Findings: res.Msg.GetFindings()}
	if ranAt := res.Msg.GetRanAt(); ranAt != nil {
		result.RanAt = ranAt.AsTime()
	}
	return result, nil
}

// SettleSweepTrigger records what a triggered sweep did. An empty cause is
// done; anything else is a failed attempt with that reason.
func (a *Activities) SettleSweepTrigger(ctx context.Context, triggerID, cause string) (bool, error) {
	req := &platformv1.SettleSweepTriggerRequest{TriggerId: triggerID}
	if cause == "" {
		req.Outcome = platformv1.SettleSweepTriggerRequest_OUTCOME_DONE
	} else {
		req.Outcome = platformv1.SettleSweepTriggerRequest_OUTCOME_FAILED
		req.Error = cause
	}
	res, err := a.Sweeps.SettleSweepTrigger(ctx, connect.NewRequest(req))
	if err != nil {
		return false, badRequestOrRefusal(err)
	}
	return res.Msg.GetSettled(), nil
}

// ListSweepTargets asks core-api which organisations the daily sweep visits.
func (a *Activities) ListSweepTargets(ctx context.Context) ([]string, error) {
	res, err := a.Sweeps.ListSweepTargets(ctx, connect.NewRequest(&platformv1.ListSweepTargetsRequest{}))
	if err != nil {
		return nil, nonRetryableIfRefused(err)
	}
	return res.Msg.GetOrgIds(), nil
}
