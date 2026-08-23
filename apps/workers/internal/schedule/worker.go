package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
)

// Options is what the worker needs, supplied by main.
type Options struct {
	// Addr is the engine's frontend, host:port.
	Addr string
	// Namespace the schedules live in.
	Namespace string
	// TaskQueue this worker polls; `core` on the bundled stack.
	TaskQueue string
	// SnoozeExpirySchedule is a five-field cron expression, evaluated in UTC.
	SnoozeExpirySchedule string
	// OutboxRelayInterval is how often the relay looks for pending mail.
	OutboxRelayInterval time.Duration
	// OutboxReclaimSchedule is a five-field cron expression, evaluated in UTC.
	OutboxReclaimSchedule string
	// SweepRelayInterval is how often the relay looks for sweeps somebody
	// asked for.
	SweepRelayInterval time.Duration
	// SweepSchedule is a five-field cron expression, evaluated in UTC, for
	// sweeping every organisation.
	SweepSchedule string
	// ExecutorRelayInterval is how often the relay looks for approvals whose
	// record has not been created yet.
	ExecutorRelayInterval time.Duration
	// Activities is the dependency set the activities close over.
	Activities *Activities
	Logger     *slog.Logger
}

// SnoozeExpiryScheduleID is the Schedule's id in the engine, which is what an
// operator sees in `temporal schedule list` and the UI, and what the CI step
// asks for.
const SnoozeExpiryScheduleID = "expire-snoozed-findings"

// Worker is a connected client plus a running worker, and the handle main uses
// to stop it.
type Worker struct {
	client client.Client
	worker worker.Worker
}

// Connect dials the engine, retrying until it answers or the context ends.
//
// Retried for the same reason core-api retries OIDC discovery: `temporal` and
// `workers` start together, and losing that race is ordinary rather than
// exceptional. The compose file orders them, but a compose file is one
// deployment and the binary should not depend on it.
func Connect(ctx context.Context, opts Options) (client.Client, error) {
	const attempts = 30
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		c, err := client.DialContext(ctx, client.Options{
			HostPort:  opts.Addr,
			Namespace: opts.Namespace,
			Logger:    slogAdapter{opts.Logger},
		})
		if err == nil {
			return c, nil
		}
		lastErr = err
		opts.Logger.Info("waiting for temporal", "addr", opts.Addr, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, fmt.Errorf("schedule: temporal at %s did not answer after %d attempts: %w", opts.Addr, attempts, lastErr)
}

// Start registers the workflows and activities, starts polling the task
// queue, and makes sure the schedules exist.
//
// Schedules are created here rather than by an operator, because a deployment
// where the engine is up and nobody ran the command is a deployment where
// nothing is scheduled, which looks exactly like a working one. The worker
// that serves a schedule is the thing that knows it should exist.
func Start(ctx context.Context, c client.Client, opts Options) (*Worker, error) {
	// The relay starts delivery workflows through the client it is given, on
	// the queue this worker polls. Set here rather than by main, so the two
	// cannot name different queues.
	opts.Activities.Starter = c
	opts.Activities.TaskQueue = opts.TaskQueue

	w := worker.New(c, opts.TaskQueue, worker.Options{})
	w.RegisterWorkflow(ExpireSnoozedFindingsWorkflow)
	w.RegisterWorkflow(RelayOutboxWorkflow)
	w.RegisterWorkflow(DeliverMessageWorkflow)
	w.RegisterWorkflow(ReclaimOutboxWorkflow)
	w.RegisterWorkflow(DeliverNotificationWorkflow)
	w.RegisterWorkflow(RelaySweepTriggersWorkflow)
	w.RegisterWorkflow(TriggeredSweepWorkflow)
	w.RegisterWorkflow(DailySweepWorkflow)
	w.RegisterWorkflow(RelayExecutorJobsWorkflow)
	w.RegisterWorkflow(ExecuteApprovalWorkflow)
	w.RegisterActivityWithOptions(opts.Activities.ExpireSnoozes,
		activityOptions(ExpireSnoozesActivityName))
	w.RegisterActivityWithOptions(opts.Activities.ListUndelivered,
		activityOptions(ListUndeliveredActivityName))
	w.RegisterActivityWithOptions(opts.Activities.StartDeliveries,
		activityOptions(StartDeliveriesActivityName))
	w.RegisterActivityWithOptions(opts.Activities.DeliverMessage,
		activityOptions(DeliverMessageActivityName))
	w.RegisterActivityWithOptions(opts.Activities.ReclaimMessages,
		activityOptions(ReclaimMessagesActivityName))
	w.RegisterActivityWithOptions(opts.Activities.PlanNotification,
		activityOptions(PlanNotificationActivityName))
	w.RegisterActivityWithOptions(opts.Activities.NotifyRecipients,
		activityOptions(NotifyRecipientsActivityName))
	w.RegisterActivityWithOptions(opts.Activities.SettleNotification,
		activityOptions(SettleNotificationActivityName))
	w.RegisterActivityWithOptions(opts.Activities.ListSweepTriggers,
		activityOptions(ListSweepTriggersActivityName))
	w.RegisterActivityWithOptions(opts.Activities.StartTriggeredSweeps,
		activityOptions(StartTriggeredSweepsActivityName))
	w.RegisterActivityWithOptions(opts.Activities.RunWatcher,
		activityOptions(RunWatcherActivityName))
	w.RegisterActivityWithOptions(opts.Activities.RunAnalyst,
		activityOptions(RunAnalystActivityName))
	w.RegisterActivityWithOptions(opts.Activities.SettleSweepTrigger,
		activityOptions(SettleSweepTriggerActivityName))
	w.RegisterActivityWithOptions(opts.Activities.ListSweepTargets,
		activityOptions(ListSweepTargetsActivityName))
	w.RegisterActivityWithOptions(opts.Activities.NextFindingToNarrate,
		activityOptions(NextFindingToNarrateActivityName))
	w.RegisterActivityWithOptions(opts.Activities.RecordNarrative,
		activityOptions(RecordNarrativeActivityName))
	w.RegisterActivityWithOptions(opts.Activities.ListExecutorJobs,
		activityOptions(ListExecutorJobsActivityName))
	w.RegisterActivityWithOptions(opts.Activities.StartExecutions,
		activityOptions(StartExecutionsActivityName))
	w.RegisterActivityWithOptions(opts.Activities.ExecuteJob,
		activityOptions(ExecuteJobActivityName))
	w.RegisterActivityWithOptions(opts.Activities.LoadWatchContext,
		activityOptions(LoadWatchContextActivityName))
	// DraftNarrative and Watch are deliberately NOT registered here: both run
	// on the `intelligence` task queue, served by the Python worker (§16.4).

	if err := w.Start(); err != nil {
		return nil, fmt.Errorf("schedule: starting the worker: %w", err)
	}

	for _, def := range schedules(opts) {
		if err := ensureSchedule(ctx, c, opts, def); err != nil {
			w.Stop()
			return nil, err
		}
	}

	opts.Logger.Info("temporal worker started",
		"task_queue", opts.TaskQueue,
		"namespace", opts.Namespace,
		"snooze_expiry", opts.SnoozeExpirySchedule,
		"outbox_relay", opts.OutboxRelayInterval.String(),
		"outbox_reclaim", opts.OutboxReclaimSchedule,
		"sweep_relay", opts.SweepRelayInterval.String(),
		"sweep", opts.SweepSchedule,
		"executor_relay", opts.ExecutorRelayInterval.String())
	return &Worker{client: c, worker: w}, nil
}

// Stop stops polling and closes the client.
func (w *Worker) Stop() {
	w.worker.Stop()
	w.client.Close()
}

// Client is the connected client, for the readiness probe.
func (w *Worker) Client() client.Client { return w.client }

// Ready reports whether the engine answers, which is what /readyz asks.
//
// This is the probe ENT-256 step 1 said core-api would carry. It is here
// instead because this is the process that depends on the engine, and a
// readiness check belongs to the process that would be not ready without the
// thing it checks.
func Ready(c client.Client) func(context.Context) error {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if _, err := c.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
			return fmt.Errorf("temporal: %w", err)
		}
		return nil
	}
}

// scheduleDefinition is one Schedule as this worker wants it to exist: what
// fires when, and what it starts.
type scheduleDefinition struct {
	ID   string
	Spec client.ScheduleSpec
	// Workflow is the function the schedule starts, on this worker's queue.
	Workflow any
	// ExecutionTimeout bounds one run, retries included. Under the interval
	// between ticks, so the overlap policy never fires in practice.
	ExecutionTimeout time.Duration
	// CatchupWindow is how far back a missed tick (the engine was down) is
	// made up. Every workflow here is idempotent, so one catch-up run does
	// the work of every missed one; what this bounds is how stale a tick
	// still gets run at all.
	CatchupWindow time.Duration
	Memo          map[string]any
}

// schedules is every Schedule this worker owns, in one place, so the list the
// CI step asserts against and the list the code registers are the same list.
func schedules(opts Options) []scheduleDefinition {
	return []scheduleDefinition{
		{
			ID: SnoozeExpiryScheduleID,
			// A missed tick (the engine was down) runs once when it is back,
			// rather than once per missed hour. The pass is idempotent, so
			// running it ten times for ten missed hours would be ten UPDATEs
			// that do the work of one; running it once is the same outcome.
			Spec:             client.ScheduleSpec{CronExpressions: []string{opts.SnoozeExpirySchedule}},
			Workflow:         ExpireSnoozedFindingsWorkflow,
			ExecutionTimeout: 50 * time.Minute,
			CatchupWindow:    time.Hour,
			Memo: map[string]any{
				"owner": "workers (ENT-256)",
				"what":  "brings deferred findings back when their date passes",
			},
		},
		{
			ID: OutboxRelayScheduleID,
			// An interval rather than a cron, because a cron's finest grain is
			// a minute and mail a person is waiting for should leave in
			// seconds. The catch-up window is deliberately short: a tick
			// missed while the engine was down is made up by the next tick
			// listing the same rows, so replaying old ticks would only start
			// the same deliveries a few more times and find them running.
			Spec: client.ScheduleSpec{
				Intervals: []client.ScheduleIntervalSpec{{Every: opts.OutboxRelayInterval}},
			},
			Workflow:         RelayOutboxWorkflow,
			ExecutionTimeout: 5 * time.Minute,
			CatchupWindow:    time.Minute,
			Memo: map[string]any{
				"owner": "workers (ENT-256)",
				"what":  "starts a delivery for every message and every finding notification waiting to leave",
			},
		},
		{
			ID:               OutboxReclaimScheduleID,
			Spec:             client.ScheduleSpec{CronExpressions: []string{opts.OutboxReclaimSchedule}},
			Workflow:         ReclaimOutboxWorkflow,
			ExecutionTimeout: 50 * time.Minute,
			CatchupWindow:    time.Hour,
			Memo: map[string]any{
				"owner": "workers (ENT-256)",
				"what":  "clears addresses and bodies out of delivered and abandoned messages",
			},
		},
		{
			ID: SweepTriggerRelayScheduleID,
			// The same interval shape as the outbox relay, for the same
			// reason: somebody just confirmed onboarding and is looking at an
			// empty feed.
			Spec: client.ScheduleSpec{
				Intervals: []client.ScheduleIntervalSpec{{Every: opts.SweepRelayInterval}},
			},
			Workflow:         RelaySweepTriggersWorkflow,
			ExecutionTimeout: 5 * time.Minute,
			CatchupWindow:    time.Minute,
			Memo: map[string]any{
				"owner": "workers (ENT-256)",
				"what":  "starts a sweep for every organisation that asked for one (confirmed onboarding)",
			},
		},
		{
			ID: ExecutorRelayScheduleID,
			// The same interval shape as the outbox and sweep relays, and
			// the tightest of the three reasons: somebody clicked approve
			// and is looking at Records for what they approved.
			Spec: client.ScheduleSpec{
				Intervals: []client.ScheduleIntervalSpec{{Every: opts.ExecutorRelayInterval}},
			},
			Workflow:         RelayExecutorJobsWorkflow,
			ExecutionTimeout: 5 * time.Minute,
			CatchupWindow:    time.Minute,
			Memo: map[string]any{
				"owner": "workers (ENT-271)",
				"what":  "creates the record every approved finding asked for",
			},
		},
		{
			ID:       DailySweepScheduleID,
			Spec:     client.ScheduleSpec{CronExpressions: []string{opts.SweepSchedule}},
			Workflow: DailySweepWorkflow,
			// The estate, four organisations at a time, seconds each: an hour
			// is generous for thousands. Under a day, so the overlap policy
			// never fires.
			ExecutionTimeout: 12 * time.Hour,
			CatchupWindow:    6 * time.Hour,
			Memo: map[string]any{
				"owner": "workers (ENT-256)",
				"what":  "runs the Watcher and then the Analyst over every organisation with a profile",
			},
		},
	}
}

// ensureSchedule creates the Schedule, or brings an existing one into line
// with the configured spec and action, retrying while the engine finishes
// starting.
//
// Create-then-update rather than delete-and-recreate, because a Schedule has
// state worth keeping: its history of runs, whether an operator paused it,
// and the backfill it may be part way through. Changing the cron in
// configuration should change the cron and nothing else.
//
// # WHY IT RETRIES, MEASURED RATHER THAN GUESSED
//
// The engine's healthcheck is `temporal operator cluster health`, which is
// the frontend answering. For a few seconds after a restart the frontend is
// up and the matching and history services have not yet joined its ring, and
// a schedule update in that window fails with "Not enough hosts to serve the
// request" and then the SDK's ten-second deadline. The first version of this
// returned that error, the binary exited 1, and because core-api waits on
// this service being healthy the whole stack failed to come up. It passed on
// a fresh stack, where the create path happened to land after the ring had
// formed, and failed in CI's air-gap job, which is the one place the stack is
// brought up twice in a row on the same volumes and so took the update path
// against an engine seconds old. Same class as ENT-253: a first-minute race
// that a laptop stack is always past.
//
// So: keep trying for two minutes, with the per-attempt deadline the SDK
// already applies, and only then fail, loudly, because a deployment with no
// schedule is the silent failure the CI step exists to catch.
func ensureSchedule(ctx context.Context, c client.Client, opts Options, def scheduleDefinition) error {
	const attempts = 60
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		err := ensureScheduleOnce(ctx, c, opts, def)
		if err == nil {
			return nil
		}
		lastErr = err
		opts.Logger.Info("waiting for temporal to accept the schedule",
			"id", def.ID, "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("schedule: %s could not be registered after %d attempts: %w",
		def.ID, attempts, lastErr)
}

// ensureScheduleOnce is one attempt; see the caller for why there are several.
func ensureScheduleOnce(ctx context.Context, c client.Client, opts Options, def scheduleDefinition) error {
	spec := def.Spec
	spec.Jitter = 0
	action := &client.ScheduleWorkflowAction{
		ID:                       def.ID + "-run",
		Workflow:                 def.Workflow,
		TaskQueue:                opts.TaskQueue,
		WorkflowExecutionTimeout: def.ExecutionTimeout,
	}

	handle := c.ScheduleClient().GetHandle(ctx, def.ID)
	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:     def.ID,
		Spec:   spec,
		Action: action,
		// If a run is still going when the next tick comes, skip the tick:
		// every workflow here is idempotent and the running one will do the
		// work.
		Overlap:       enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: def.CatchupWindow,
		Memo:          def.Memo,
	})
	if err == nil {
		opts.Logger.Info("schedule created", "id", def.ID)
		return nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("schedule: creating %s: %w", def.ID, err)
	}

	// It exists. Make it match the configuration, in case the cron changed.
	err = handle.Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(in client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			in.Description.Schedule.Spec = &spec
			in.Description.Schedule.Action = action
			return &client.ScheduleUpdate{Schedule: &in.Description.Schedule}, nil
		},
	})
	if err != nil {
		return fmt.Errorf("schedule: updating %s: %w", def.ID, err)
	}
	opts.Logger.Info("schedule exists, configuration applied", "id", def.ID)
	return nil
}
