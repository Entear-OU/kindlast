package schedule

import (
	"context"

	"go.temporal.io/sdk/client"
)

// Activities holds the dependencies the activities close over. Registered as
// a struct so the worker registers its methods by name once.
//
// Every field is an interface declared in this package (§21.6), satisfied by a
// generated Connect client or by the SDK's client in production and by a fake
// in the tests, so what the tests assert is this package's behaviour rather
// than the engine's or core-api's.
type Activities struct {
	// CoreAPI is SweepService: the snooze expiry pass (part two).
	CoreAPI Expirer
	// Sweeps is SweepService again, as the sweep workflows see it (part
	// four): the Watcher, the Analyst, the triggers and the targets. The same
	// generated client satisfies both; two names because two features
	// declared what they need separately, which is the §21.6 habit.
	Sweeps Sweeper
	// Narratives is NarrativeService: findings, explained, as the third step
	// of a sweep (part five).
	Narratives Narrator
	// Executions is ExecutorService: creating the record an approved finding
	// asked for (ENT-271), which used to be a database trigger.
	Executions Executor
	// Mail is DeliveryService: the transactional outbox's delivery half
	// (part three).
	Mail Deliverer
	// Starter is how the relay starts one delivery workflow per pending
	// message. The SDK client in production, set by Start; a recorder in
	// the tests.
	Starter Starter
	// TaskQueue the started workflows run on, which is this worker's own.
	TaskQueue string
}

// Starter is the one method of client.Client the relay activity uses.
//
// An activity that starts workflows is the idiomatic way for a Schedule to
// fan out into long-lived work without the fan-out being a child of a run
// that ends in seconds: each delivery has its own history, its own retry
// policy and its own line in the UI, and the relay that started it has
// already completed.
type Starter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions,
		workflow any, args ...any) (client.WorkflowRun, error)
}
