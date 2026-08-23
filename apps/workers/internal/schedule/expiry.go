// Package schedule is the worker half of this binary: the schedules that used
// to be pg_cron jobs and Vercel cron routes, run as Temporal workflows whose
// activities call core-api (ENT-256, core-api-surface §16).
//
// # WHAT LIVES HERE AND WHAT DOES NOT
//
// Workflows and activities, a worker that polls the `core` task queue, and
// the code that registers each Schedule with the engine at boot. No database
// handle, no Postgres role, no persistence: the gateway half of this binary
// holds none of those by design (§21.4) and the worker half inherits the rule.
// "An activity" here means "an RPC on core-api's internal surface", made with
// the same service credential Intelligence presents, and core-api does the
// work on whatever pool the work belongs to.
//
// That is why the first schedule is snooze expiry. It is the simplest thing
// the design names: one RPC, no input, idempotent, and its effect is visible
// in a console (a deferred finding comes back to "needs a decision"), so it
// proves the whole path from Schedule to database before anything harder
// rides it.
//
// # WHY THE WORKFLOW IS ONE ACTIVITY AND NOT A LOOP
//
// A workflow that iterated organisations would need to list them, and neither
// this process nor the producer role may (00008). The database function is a
// single cross-tenant pass, bounded by its own body (00034), so the workflow
// asks for it once and records what came back. When a schedule needs per-
// organisation fan-out, the Watcher chain in part four, it will be a
// different workflow rather than this one grown.
package schedule

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// ExpireSnoozedFindingsWorkflow is the workflow behind the
// `expire-snoozed-findings` Schedule.
//
// Named as the Temporal workflow type, which is how the UI and the CLI refer
// to it, and the name is deliberately the function's: an operator reading
// "ExpireSnoozedFindingsWorkflow failed" in the UI should be able to find the
// thing that failed without a glossary.
func ExpireSnoozedFindingsWorkflow(ctx workflow.Context) (ExpiryResult, error) {
	options := workflow.ActivityOptions{
		// Long enough for core-api to take a pool connection on a busy stack,
		// short enough that a hung call does not hold the schedule past its
		// next tick. The pass itself is one UPDATE.
		StartToCloseTimeout: 2 * time.Minute,
		// Retry, and keep retrying, with backoff. The activity is idempotent
		// (a second pass finds nothing to do), core-api restarting is ordinary,
		// and a schedule whose one activity gives up after three tries on a
		// Sunday night is a schedule that silently did not run. Capped by the
		// workflow's own timeout below rather than by an attempt count.
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result ExpiryResult
	if err := workflow.ExecuteActivity(ctx, ExpireSnoozesActivityName).Get(ctx, &result); err != nil {
		return ExpiryResult{}, err
	}
	return result, nil
}

// ExpiryResult is what one pass reports, and what the workflow history keeps.
//
// The count and the time, and nothing that identifies a finding or an
// organisation: a workflow history is kept for the namespace's retention and
// is readable in the UI, so it carries the minimum (§16.3).
type ExpiryResult struct {
	Reemerged int32
	RanAt     time.Time
}

// ExpireSnoozesActivityName is the registered activity name, pinned so the
// workflow and the registration cannot drift apart.
const ExpireSnoozesActivityName = "ExpireSnoozes"

// Expirer is what the activity needs of core-api, declared where it is used
// (§21.6). The generated SweepService client satisfies it; a test's fake does
// too.
type Expirer interface {
	ExpireSnoozes(ctx context.Context, req *connect.Request[platformv1.ExpireSnoozesRequest]) (*connect.Response[platformv1.ExpireSnoozesResponse], error)
}

// ExpireSnoozes calls core-api's pass and returns what it reported.
//
// Errors are returned as they are, which is the retry policy working rather
// than this code being lazy about them; see nonRetryableIfRefused for the one
// exception and why.
func (a *Activities) ExpireSnoozes(ctx context.Context) (ExpiryResult, error) {
	res, err := a.CoreAPI.ExpireSnoozes(ctx, connect.NewRequest(&platformv1.ExpireSnoozesRequest{}))
	if err != nil {
		return ExpiryResult{}, nonRetryableIfRefused(err)
	}
	result := ExpiryResult{Reemerged: res.Msg.GetReemerged()}
	if ranAt := res.Msg.GetRanAt(); ranAt != nil {
		result.RanAt = ranAt.AsTime()
	}
	return result, nil
}
