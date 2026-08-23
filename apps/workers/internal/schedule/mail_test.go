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

	// The doorbell half: pending notification ids, and a scripted plan per
	// round (plans[0] is answered to the first PlanNotification call, and so
	// on; the last one repeats).
	pendingBells []string
	plans        []*platformv1.PlanNotificationResponse
	planCalls    int
	notified     [][]string
	settledWith  *platformv1.SettleNotificationRequest
}

func (f *fakeDeliverer) PlanNotification(
	_ context.Context, _ *connect.Request[platformv1.PlanNotificationRequest],
) (*connect.Response[platformv1.PlanNotificationResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.planCalls
	if i >= len(f.plans) {
		i = len(f.plans) - 1
	}
	f.planCalls++
	return connect.NewResponse(f.plans[i]), nil
}

func (f *fakeDeliverer) NotifyRecipients(
	_ context.Context, req *connect.Request[platformv1.NotifyRecipientsRequest],
) (*connect.Response[platformv1.NotifyRecipientsResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil && len(f.notified) < f.refuseFor {
		f.notified = append(f.notified, nil)
		return nil, f.refuse
	}
	f.notified = append(f.notified, req.Msg.GetUserIds())
	return connect.NewResponse(&platformv1.NotifyRecipientsResponse{Sent: int32(len(req.Msg.GetUserIds()))}), nil
}

func (f *fakeDeliverer) SettleNotification(
	_ context.Context, req *connect.Request[platformv1.SettleNotificationRequest],
) (*connect.Response[platformv1.SettleNotificationResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settledWith = req.Msg
	return connect.NewResponse(&platformv1.SettleNotificationResponse{Settled: true}), nil
}

func planned(userID string, decision platformv1.PlannedRecipient_Decision, holdUntil time.Time, reason string) *platformv1.PlannedRecipient {
	p := &platformv1.PlannedRecipient{UserId: userID, Decision: decision, Reason: reason}
	if !holdUntil.IsZero() {
		p.HoldUntil = timestamppb.New(holdUntil)
	}
	return p
}

func (f *fakeDeliverer) ListUndelivered(
	_ context.Context, _ *connect.Request[platformv1.ListUndeliveredRequest],
) (*connect.Response[platformv1.ListUndeliveredResponse], error) {
	return connect.NewResponse(&platformv1.ListUndeliveredResponse{
		MessageIds:      f.pending,
		NotificationIds: f.pendingBells,
	}), nil
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
	env.RegisterActivityWithOptions(a.PlanNotification, activityOptions(PlanNotificationActivityName))
	env.RegisterActivityWithOptions(a.NotifyRecipients, activityOptions(NotifyRecipientsActivityName))
	env.RegisterActivityWithOptions(a.SettleNotification, activityOptions(SettleNotificationActivityName))
}

func TestTheRelayStartsOneDeliveryPerPendingMessageOnItsOwnId(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{pending: []string{"aaa", "bbb", "ccc"}, pendingBells: []string{"n1", "n2"}}
	starter := &fakeStarter{running: map[string]bool{
		deliveryWorkflowID("bbb"):    true,
		notificationWorkflowID("n2"): true,
	}}
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
	if result.Pending != 5 || result.Started != 3 || result.AlreadyRunning != 2 {
		t.Fatalf("result = %+v, want 5 pending, 3 started, 2 already running", result)
	}

	// THE PROPERTY THE WHOLE DESIGN RESTS ON: the workflow id is the row id,
	// so the engine will never run two deliveries of one message or one
	// notification at once, and a row already being delivered (or held
	// through somebody's quiet hours) is left to the run that has it.
	if len(starter.started) != 3 {
		t.Fatalf("started %d workflows, want 3", len(starter.started))
	}
	for i, want := range []string{deliveryWorkflowID("aaa"), deliveryWorkflowID("ccc"), notificationWorkflowID("n1")} {
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
		SweepRelayInterval:    15 * time.Second,
		SweepSchedule:         "0 6 * * *",
		ExecutorRelayInterval: 15 * time.Second,
	})
	want := []string{SnoozeExpiryScheduleID, OutboxRelayScheduleID, OutboxReclaimScheduleID,
		SweepTriggerRelayScheduleID, ExecutorRelayScheduleID, DailySweepScheduleID}
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

// The doorbell path (ENT-209 on Temporal). The first three are the three
// outcomes a plan can produce, and the middle one is the reason the path
// moved: a recipient inside quiet hours is held on a durable timer and told
// when the window ends, rather than dropped.

func TestANotificationGoesToEverybodyDueAndSettlesAsSent(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{{
		Recipients: []*platformv1.PlannedRecipient{
			planned("ada", platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""),
			planned("bob", platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""),
			planned("cy", platformv1.PlannedRecipient_DECISION_SKIP, time.Time{}, "severity low is below the high floor"),
		},
	}}}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the notification failed: %v", err)
	}
	var result NotificationResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Sent != 2 || !result.Settled || result.Rounds != 0 {
		t.Fatalf("result = %+v, want 2 sent, settled, no rounds slept", result)
	}
	if len(mail.notified) != 1 || len(mail.notified[0]) != 2 {
		t.Fatalf("notified %v, want one call to the two due recipients", mail.notified)
	}
	if mail.settledWith == nil || mail.settledWith.GetOutcome() != platformv1.SettleNotificationRequest_OUTCOME_SENT {
		t.Fatalf("settled with %v, want sent", mail.settledWith)
	}
}

// THE TEST THE PATH MOVED FOR. Bob is inside quiet hours at the first plan.
// The workflow sends to Ada now, sleeps until Bob's window ends (the test
// environment skips the clock forward rather than waiting), plans again,
// sends to Bob and not to Ada again, and settles as sent.
func TestARecipientInsideQuietHoursIsHeldAndToldWhenTheWindowEnds(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	start := env.Now()
	windowEnds := start.Add(7 * time.Hour)
	mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{
		{Recipients: []*platformv1.PlannedRecipient{
			planned("ada", platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""),
			planned("bob", platformv1.PlannedRecipient_DECISION_HOLD, windowEnds, "inside quiet hours"),
		}},
		{Recipients: []*platformv1.PlannedRecipient{
			planned("ada", platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""),
			planned("bob", platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""),
		}},
	}}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the notification failed: %v", err)
	}
	var result NotificationResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if result.Sent != 2 || result.Rounds != 1 || !result.Settled {
		t.Fatalf("result = %+v, want 2 sent over one hold, settled", result)
	}
	if len(mail.notified) != 2 {
		t.Fatalf("notified %v, want two calls: Ada now, Bob after the window", mail.notified)
	}
	if len(mail.notified[0]) != 1 || mail.notified[0][0] != "ada" {
		t.Fatalf("first send went to %v, want Ada alone", mail.notified[0])
	}
	if len(mail.notified[1]) != 1 || mail.notified[1][0] != "bob" {
		t.Fatalf("second send went to %v, want Bob alone: Ada was already told", mail.notified[1])
	}
	if mail.planCalls != 2 {
		t.Fatalf("planned %d times, want 2: once now, once after the window", mail.planCalls)
	}
	// And the workflow clock really did move past the window: the second
	// plan happened no earlier than when Bob's quiet hours ended.
	if env.Now().Before(windowEnds) {
		t.Fatalf("the workflow finished at %v, before the window ended at %v", env.Now(), windowEnds)
	}
}

func TestANotificationNobodyWantsIsSettledAsSkippedWithTheReason(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{{
		Recipients: []*platformv1.PlannedRecipient{
			planned("cy", platformv1.PlannedRecipient_DECISION_SKIP, time.Time{}, "severity low is below the high floor"),
		},
	}}}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the notification failed: %v", err)
	}
	if len(mail.notified) != 0 {
		t.Fatalf("notified %v for a notification nobody wanted", mail.notified)
	}
	if mail.settledWith == nil ||
		mail.settledWith.GetOutcome() != platformv1.SettleNotificationRequest_OUTCOME_SKIPPED ||
		mail.settledWith.GetReason() != "severity low is below the high floor" {
		t.Fatalf("settled with %v, want skipped with the floor reason", mail.settledWith)
	}
}

func TestANotificationWithNoRecipientsAtAllIsSkippedWithAReason(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{{}}}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the notification failed: %v", err)
	}
	if mail.settledWith == nil || mail.settledWith.GetReason() == "" {
		t.Fatalf("settled with %v, want skipped with a reason core-api will accept", mail.settledWith)
	}
}

func TestASettledNotificationIsLeftAlone(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{plans: []*platformv1.PlanNotificationResponse{{Settled: true}}}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a settled notification failed the workflow: %v", err)
	}
	if len(mail.notified) != 0 || mail.settledWith != nil {
		t.Fatal("a settled notification was sent or settled again")
	}
}

// A refused send is retried by the policy, as for a message, and the
// notification settles once it goes.
func TestARefusedNotificationSendIsRetriedUntilItLeaves(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	mail := &fakeDeliverer{
		plans: []*platformv1.PlanNotificationResponse{{
			Recipients: []*platformv1.PlannedRecipient{
				planned("ada", platformv1.PlannedRecipient_DECISION_SEND, time.Time{}, ""),
			},
		}},
		refuse:    connect.NewError(connect.CodeUnavailable, errors.New("451 try again later")),
		refuseFor: 2,
	}
	registerMail(env, &Activities{Mail: mail})

	env.ExecuteWorkflow(DeliverNotificationWorkflow, "n1")

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the notification failed rather than retrying: %v", err)
	}
	if len(mail.notified) != 3 {
		t.Fatalf("core-api was asked %d times, want 3 (two refusals, then success)", len(mail.notified))
	}
	if mail.settledWith == nil || mail.settledWith.GetOutcome() != platformv1.SettleNotificationRequest_OUTCOME_SENT {
		t.Fatalf("settled with %v, want sent", mail.settledWith)
	}
}
