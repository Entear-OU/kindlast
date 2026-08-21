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
	w := worker.New(c, opts.TaskQueue, worker.Options{})
	w.RegisterWorkflow(ExpireSnoozedFindingsWorkflow)
	w.RegisterActivityWithOptions(opts.Activities.ExpireSnoozes,
		activityOptions(ExpireSnoozesActivityName))

	if err := w.Start(); err != nil {
		return nil, fmt.Errorf("schedule: starting the worker: %w", err)
	}

	if err := ensureSnoozeExpirySchedule(ctx, c, opts); err != nil {
		w.Stop()
		return nil, err
	}

	opts.Logger.Info("temporal worker started",
		"task_queue", opts.TaskQueue,
		"namespace", opts.Namespace,
		"schedule", SnoozeExpiryScheduleID,
		"cron", opts.SnoozeExpirySchedule)
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

// ensureSnoozeExpirySchedule creates the Schedule, or brings an existing one
// into line with the configured cron and action.
//
// Create-then-update rather than delete-and-recreate, because a Schedule has
// state worth keeping: its history of runs, whether an operator paused it,
// and the backfill it may be part way through. Changing the cron in
// configuration should change the cron and nothing else.
func ensureSnoozeExpirySchedule(ctx context.Context, c client.Client, opts Options) error {
	spec := client.ScheduleSpec{
		CronExpressions: []string{opts.SnoozeExpirySchedule},
		// A missed tick (the engine was down) runs once when it is back,
		// rather than once per missed hour. The pass is idempotent, so
		// running it ten times for ten missed hours would be ten UPDATEs
		// that do the work of one; running it once is the same outcome.
		Jitter: 0,
	}
	action := &client.ScheduleWorkflowAction{
		ID:        SnoozeExpiryScheduleID + "-run",
		Workflow:  ExpireSnoozedFindingsWorkflow,
		TaskQueue: opts.TaskQueue,
		// The whole run, retries included, has to finish before the next
		// tick or it is overlapping itself for no reason. Under an hour is
		// the constraint that makes the overlap policy below never fire in
		// practice.
		WorkflowExecutionTimeout: 50 * time.Minute,
	}

	handle := c.ScheduleClient().GetHandle(ctx, SnoozeExpiryScheduleID)
	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:     SnoozeExpiryScheduleID,
		Spec:   spec,
		Action: action,
		// If a run is still going when the next tick comes, skip the tick:
		// the pass is idempotent and the running one will do the work.
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		// After an outage, one catch-up run rather than one per missed tick.
		CatchupWindow: time.Hour,
		Memo: map[string]any{
			"owner": "workers (ENT-256)",
			"what":  "brings deferred findings back when their date passes",
		},
	})
	if err == nil {
		opts.Logger.Info("schedule created", "id", SnoozeExpiryScheduleID)
		return nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("schedule: creating %s: %w", SnoozeExpiryScheduleID, err)
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
		return fmt.Errorf("schedule: updating %s: %w", SnoozeExpiryScheduleID, err)
	}
	opts.Logger.Info("schedule exists, configuration applied", "id", SnoozeExpiryScheduleID)
	return nil
}
