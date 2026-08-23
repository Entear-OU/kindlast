package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/types/known/timestamppb"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The transactional outbox on Temporal (ENT-256, part three), in the SDK's
// test environment: time is skipped rather than waited for, so a delivery
// that backs off for minutes between attempts is asserted in milliseconds, and
// what is asserted is this package's behaviour (which activity runs, with
// what, what it does with each answer) rather than the engine's.

// fakeDeliverer is core-api's DeliveryService, minus core-api.
type fakeDeliverer struct {
	mu       sync.Mutex
	pending  []string
	delivers []string
	// refuse is returned by DeliverMessage for the first `refuseFor` calls.
	refuse    error
	refuseFor int
	// settled makes DeliverMessage answer "already settled".
	settled  bool
	reclaims int
}

func (f *fakeDeliverer) ListUndelivered(
	_ context.Context, _ *connect.Request[platformv1.ListUndeliveredRequest],
) (*connect.Response[platformv1.ListUndeliveredResponse], error) {
	return connect.NewResponse(&platformv1.ListUndeliveredResponse{MessageIds: f.pending}), nil
}

func (f *fakeDeliverer) DeliverMessage(
	_ context.Context, req *connect.Request[platformv1.DeliverMessageRequest],
) (*connect.Response[platformv1.DeliverMessageResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivers = append(f.delivers, req.Msg.GetMessageId())
	if f.refuse != nil && len(f.delivers) <= f.refuseFor {
		return nil, f.refuse
	}
	if f.settled {
		return connect.NewResponse(&platformv1.DeliverMessageResponse{
			Outcome: platformv1.DeliverMessageResponse_OUTCOME_ALREADY_SETTLED,
		}), nil
	}
	return connect.NewResponse(&platformv1.DeliverMessageResponse{
		Outcome:  platformv1.DeliverMessageResponse_OUTCOME_DELIVERED,
		Attempts: int32(len(f.delivers)),
		SentAt:   timestamppb.New(time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)),
	}), nil
}

func (f *fakeDeliverer) ReclaimMessages(
	_ context.Context, _ *connect.Request[platformv1.ReclaimMessagesRequest],
) (*connect.Response[platformv1.ReclaimMessagesResponse], error) {
	f.reclaims++
	return connect.NewResponse(&platformv1.ReclaimMessagesResponse{
		Redacted: 5, Abandoned: 2, RanAt: timestamppb.Now(),
	}), nil
}

// fakeStarter records the workflows the relay asked the engine to start, and
// can answer "already running" for some of them.
type fakeStarter struct {
	started []client.StartWorkflowOptions
	running map[string]bool
}

func (s *fakeStarter) ExecuteWorkflow(
	_ context.Context, options client.StartWorkflowOptions, _ any, _ ...any,
) (client.WorkflowRun, error) {
	if s.running[options.ID] {
		return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("running", "", "")
	}
	s.started = append(s.started, options)
	return nil, nil
}

func registerMail(env *testsuite.TestWorkflowEnvironment, a *Activities) {
	env.RegisterActivityWithOptions(a.ListUndelivered, activityOptions(ListUndeliveredActivityName))
	env.RegisterActivityWithOptions(a.StartDeliveries, activityOptions(StartDeliveriesActivityName))
	env.RegisterActivityWithOptions(a.DeliverMessage, activityOptions(DeliverMessageActivityName))
	env.RegisterActivityWithOptions(a.ReclaimMessages, activityOptions(ReclaimMessagesActivityName))
}

func TestTheRelayStartsOneDeliveryPerPendingMessageOnItsOwnId(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{pending: []string{"aaa", "bbb", "ccc"}}
	starter := &fakeStarter{running: map[string]bool{deliveryWorkflowID("bbb"): true}}
	activities := &Activities{Mail: mail, Starter: starter, TaskQueue: "core"}
	registerMail(env, activities)

	env.ExecuteWorkflow(RelayOutboxWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the relay failed: %v", err)
	}
	var result RelayResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Pending != 3 || result.Started != 2 || result.AlreadyRunning != 1 {
		t.Fatalf("result = %+v, want 3 pending, 2 started, 1 already running", result)
	}

	// THE PROPERTY THE WHOLE DESIGN RESTS ON: the workflow id is the row id,
	// so the engine will never run two deliveries of one message at once, and
	// a row already being delivered is left to the run that has it.
	if len(starter.started) != 2 {
		t.Fatalf("started %d workflows, want 2", len(starter.started))
	}
	for i, want := range []string{deliveryWorkflowID("aaa"), deliveryWorkflowID("ccc")} {
		got := starter.started[i]
		if got.ID != want {
			t.Errorf("started[%d].ID = %q, want %q", i, got.ID, want)
		}
		if got.TaskQueue != "core" {
			t.Errorf("started[%d] on queue %q, want the worker's own", i, got.TaskQueue)
		}
	}
}

func TestTheRelayDoesNothingWhenNothingIsPending(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	starter := &fakeStarter{}
	registerMail(env, &Activities{Mail: &fakeDeliverer{}, Starter: starter, TaskQueue: "core"})

	env.ExecuteWorkflow(RelayOutboxWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the relay failed on an empty outbox: %v", err)
	}
	if len(starter.started) != 0 {
		t.Fatalf("started %d workflows for an empty outbox", len(starter.started))
	}
}

// The retry that makes the move worth it: the mail server refuses twice, the
// policy backs off and tries again, the third attempt sends, and the workflow
// completes with the delivery recorded. In the ticker this replaced, the same
// refusals were a hot loop at ten-second intervals with nothing to show for it
// but a counter.
func TestADeliveryRetriesARefusedSendUntilItLeaves(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{
		refuse:    connect.NewError(connect.CodeUnavailable, errors.New("451 4.3.0 try again later")),
		refuseFor: 2,
	}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverMessageWorkflow, "aaa")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the delivery failed rather than retrying: %v", err)
	}
	var result DeliveryResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if !result.Delivered || result.SentAt.IsZero() {
		t.Fatalf("result = %+v, want delivered with a time", result)
	}
	if len(mail.delivers) != 3 {
		t.Fatalf("core-api was asked %d times, want 3 (two refusals, then success)", len(mail.delivers))
	}
	for _, id := range mail.delivers {
		if id != "aaa" {
			t.Fatalf("asked to deliver %q, want aaa every time", id)
		}
	}
}

// No mail channel configured yet is retried too: the row is exactly as
// deliverable tomorrow as today, and the operator setting KINDLAST_SMTP_ADDR
// should not have to do anything else.
func TestNoChannelIsRetriedRatherThanGivenUpOn(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{
		refuse:    connect.NewError(connect.CodeFailedPrecondition, errors.New("no mail channel is configured")),
		refuseFor: 1,
	}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverMessageWorkflow, "aaa")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the delivery gave up on a missing channel: %v", err)
	}
	if len(mail.delivers) != 2 {
		t.Fatalf("core-api was asked %d times, want 2", len(mail.delivers))
	}
}

// A row that was settled while the delivery was pending (an earlier attempt
// sent it and timed out on the way back, or the reclaim gave up on it) is a
// completed delivery, not a failed one, and nothing is sent.
func TestASettledRowCompletesTheDelivery(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{settled: true}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverMessageWorkflow, "aaa")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a settled row failed the delivery: %v", err)
	}
	var result DeliveryResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Delivered {
		t.Fatal("a settled row was reported as delivered on this run")
	}
}

// Two refusals that must not be retried, because neither changes by waiting:
// a credential core-api no longer accepts, and an id it calls malformed. Both
// fail the workflow at once, which is what makes them visible as a failure
// with a reason rather than a run that is still "trying".
func TestARefusalIsNotRetried(t *testing.T) {
	for name, code := range map[string]connect.Code{
		"credential": connect.CodePermissionDenied,
		"bad id":     connect.CodeInvalidArgument,
	} {
		t.Run(name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()

			mail := &fakeDeliverer{refuse: connect.NewError(code, errors.New("refused")), refuseFor: 1000}
			registerMail(env, &Activities{Mail: mail})

			env.ExecuteWorkflow(DeliverMessageWorkflow, "aaa")

			err := env.GetWorkflowError()
			if err == nil {
				t.Fatal("the workflow succeeded against a refusal")
			}
			var appErr *temporal.ApplicationError
			if !errors.As(err, &appErr) || !appErr.NonRetryable() {
				t.Fatalf("the failure was not marked non-retryable: %v", err)
			}
			if len(mail.delivers) != 1 {
				t.Fatalf("core-api was asked %d times for a refusal, want exactly 1", len(mail.delivers))
			}
		})
	}
}

func TestTheReclaimRunsOnePassAndReportsIt(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(ReclaimOutboxWorkflow)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the reclaim failed: %v", err)
	}
	var result ReclaimResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Redacted != 5 || result.Abandoned != 2 || result.RanAt.IsZero() {
		t.Fatalf("result = %+v, want 5 redacted, 2 abandoned, with a time", result)
	}
	if mail.reclaims != 1 {
		t.Fatalf("core-api was asked %d times, want 1: the reclaim is one pass, not a loop", mail.reclaims)
	}
}

// Every Schedule this worker registers, as the CI step and the docs name
// them. The list in the code is the one place they are defined, so this is
// the test that fails when somebody renames one without renaming the other.
func TestTheScheduleListIsTheDocumentedOne(t *testing.T) {
	defs := schedules(Options{
		SnoozeExpirySchedule:  "10 * * * *",
		OutboxRelayInterval:   15 * time.Second,
		OutboxReclaimSchedule: "40 * * * *",
	})
	want := []string{SnoozeExpiryScheduleID, OutboxRelayScheduleID, OutboxReclaimScheduleID}
	if len(defs) != len(want) {
		t.Fatalf("%d schedules, want %d", len(defs), len(want))
	}
	for i, def := range defs {
		if def.ID != want[i] {
			t.Errorf("schedules[%d].ID = %q, want %q", i, def.ID, want[i])
		}
		if def.Workflow == nil || def.ExecutionTimeout <= 0 || def.CatchupWindow <= 0 {
			t.Errorf("schedules[%d] (%s) is missing a workflow, timeout or catch-up window", i, def.ID)
		}
	}
	if got := defs[1].Spec.Intervals; len(got) != 1 || got[0].Every != 15*time.Second {
		t.Errorf("the relay's interval = %+v, want the configured fifteen seconds", got)
	}
}
