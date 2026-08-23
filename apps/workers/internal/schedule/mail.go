package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The transactional outbox on Temporal (ENT-256, part three).
//
// # THE SHAPE: A RELAY SCHEDULE, AND ONE WORKFLOW PER MESSAGE
//
// core-api writes a message into `transactional_outbox` in the same
// transaction as the fact it announces, and that half does not change: a row
// written inside the commit cannot be lost between the commit and anything
// that happens after it, which is the property the table exists for (ENT-242).
// What changes is who notices the row. A ticker in core-api used to; now the
// `relay-transactional-outbox` Schedule fires every few seconds, its workflow
// asks core-api what is pending, and starts one DeliverMessageWorkflow per row,
// with the row's id as the workflow id.
//
// That id is the whole concurrency story. Temporal runs at most one workflow
// per id at a time, so a row is never being delivered twice at once, however
// many relay ticks list it while its delivery is retrying. The store adds the
// other half (`for update` on the row, see postgres.DeliverMessage) for the
// case the engine cannot see: an activity that timed out while the SMTP
// conversation was still going.
//
// # WHY EACH MESSAGE IS ITS OWN WORKFLOW, AND NOT ONE DRAIN PER TICK
//
// A drain per tick would have moved the timer and nothing else. One workflow
// per message is what §16.2 means by "an activity with a retry policy is the
// outbox": the retry policy is declared once, next to the activity, with
// backoff; every attempt and what the mail server answered is in that
// workflow's history; and a message that is not leaving is a running workflow
// in the UI with a reason, rather than a row with a counter in a table nobody
// is watching. The ticker retried every pending row every ten seconds forever
// with none of that.
//
// # WHAT CROSSES INTO A WORKFLOW HISTORY, WHICH IS WHY core-api SENDS
//
// Row ids and counts. Not the address, not the subject, not the body, which for
// an invitation holds a bearer token in the clear (00030). A history is kept
// for the namespace's retention and readable in the UI (§16.3), so the
// activity asks core-api to send rather than asking it for the message, and
// core-api, which already holds the SMTP channel, does.

// # THE DOORBELL PATH, WHICH RIDES THE SAME RELAY WITH ONE MORE VERB
//
// A finding notification (ENT-209) is a `notification_outbox` row whose
// recipients are decided at delivery from memberships and preferences, and a
// preference may say "not now": quiet hours. The in-process loop could only
// drop such a notification with the reason on the row, because holding one
// needs a scheduler (§17.5). DeliverNotificationWorkflow holds it: it asks
// core-api to PLAN (who is due, who is held until when, who is skipped),
// sends to whoever is due, sleeps on a durable timer until the earliest hold
// ends, plans again, and settles the row when nobody is left. The relay lists
// notifications beside messages and starts one of these per row, id
// `deliver-notification/{row id}`, with the same one-run-per-id guarantee.

// Schedule ids, which are what an operator sees in `temporal schedule list`
// and the UI, and what the CI step asks for.
const (
	OutboxRelayScheduleID   = "relay-transactional-outbox"
	OutboxReclaimScheduleID = "reclaim-transactional-outbox"
)

// Registered activity names, pinned so the workflows and the registrations
// cannot drift apart.
const (
	ListUndeliveredActivityName    = "ListUndelivered"
	StartDeliveriesActivityName    = "StartDeliveries"
	DeliverMessageActivityName     = "DeliverMessage"
	ReclaimMessagesActivityName    = "ReclaimMessages"
	PlanNotificationActivityName   = "PlanNotification"
	NotifyRecipientsActivityName   = "NotifyRecipients"
	SettleNotificationActivityName = "SettleNotification"
)

// deliveryWorkflowID is the workflow id for delivering one message, and the
// thing that makes a row deliverable by at most one run at a time.
func deliveryWorkflowID(messageID string) string {
	return "deliver-message/" + messageID
}

// notificationWorkflowID is the same for one finding notification.
func notificationWorkflowID(notificationID string) string {
	return "deliver-notification/" + notificationID
}

// Deliverer is what the activities need of core-api's DeliveryService,
// declared where it is used (§21.6). The generated client satisfies it; a
// test's fake does too.
type Deliverer interface {
	ListUndelivered(ctx context.Context, req *connect.Request[platformv1.ListUndeliveredRequest]) (*connect.Response[platformv1.ListUndeliveredResponse], error)
	DeliverMessage(ctx context.Context, req *connect.Request[platformv1.DeliverMessageRequest]) (*connect.Response[platformv1.DeliverMessageResponse], error)
	ReclaimMessages(ctx context.Context, req *connect.Request[platformv1.ReclaimMessagesRequest]) (*connect.Response[platformv1.ReclaimMessagesResponse], error)
	PlanNotification(ctx context.Context, req *connect.Request[platformv1.PlanNotificationRequest]) (*connect.Response[platformv1.PlanNotificationResponse], error)
	NotifyRecipients(ctx context.Context, req *connect.Request[platformv1.NotifyRecipientsRequest]) (*connect.Response[platformv1.NotifyRecipientsResponse], error)
	SettleNotification(ctx context.Context, req *connect.Request[platformv1.SettleNotificationRequest]) (*connect.Response[platformv1.SettleNotificationResponse], error)
}

// Pending is what the relay found waiting: ids and nothing else.
type Pending struct {
	MessageIDs      []string
	NotificationIDs []string
}

func (p Pending) empty() bool { return len(p.MessageIDs) == 0 && len(p.NotificationIDs) == 0 }

// RelayOutboxWorkflow is the workflow behind the `relay-transactional-outbox`
// Schedule: list what is pending, start a delivery for each.
//
// Short-lived and bounded on purpose. It runs every few seconds with the
// overlap policy set to skip, so a run that hangs would hold every tick
// behind it; the activity timeouts below are what stop that. Each activity
// retries for up to a minute and then the run fails and the next tick is the
// retry, which is long enough to ride out the ordinary case (this worker
// boots before the edge and core-api do, so the first ticks after a stack
// start have nobody to ask) and short enough that core-api being down shows
// up as failed runs within minutes rather than one run retrying forever.
func RelayOutboxWorkflow(ctx workflow.Context) (RelayResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    30 * time.Second,
		ScheduleToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Second,
		},
	})

	var pending Pending
	if err := workflow.ExecuteActivity(ctx, ListUndeliveredActivityName).Get(ctx, &pending); err != nil {
		return RelayResult{}, err
	}
	if pending.empty() {
		return RelayResult{}, nil
	}

	var started RelayResult
	if err := workflow.ExecuteActivity(ctx, StartDeliveriesActivityName, pending).Get(ctx, &started); err != nil {
		return RelayResult{}, err
	}
	return started, nil
}

// RelayResult is what one relay tick did: how many rows were pending (messages
// and notifications together), how many deliveries it started, and how many
// were already running from an earlier tick. Counts only; see the package
// comment for why.
type RelayResult struct {
	Pending        int
	Started        int
	AlreadyRunning int
}

// DeliverMessageWorkflow delivers one message, retrying until it leaves or
// the row is settled some other way.
//
// # THE RETRY POLICY, WHICH IS THE POINT OF THE MOVE
//
// Ten seconds, doubling, capped at ten minutes, with no maximum attempt count.
// No maximum because every reason a send fails (the mail server is down, the
// relay is rate limiting, core-api is restarting) is one that resolves on its
// own, and a message given up on after N tries on a Sunday night is an
// invitation that never arrives and cannot be reissued from here. What does end
// it is the row being settled: the reclaim (00030) gives up on a message whose
// invitation can no longer be accepted, and the next attempt then finds
// nothing pending and completes. So a message retries for at most the life of
// the invitation it carries, with the engine keeping the schedule and the
// record, and nothing in this binary counting.
//
// What is not retried: a refused credential, and an id core-api calls
// malformed. Both are marked non-retryable by the activity, so the workflow
// fails at once and is visible as a failure with a reason.
func DeliverMessageWorkflow(ctx workflow.Context, messageID string) (DeliveryResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// One SMTP conversation plus a pool connection on a busy stack. A
		// timeout here is retried like any other failure, and the store's
		// row lock means the retry waits for the conversation that timed out
		// rather than racing it.
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Minute,
		},
	})

	var result DeliveryResult
	if err := workflow.ExecuteActivity(ctx, DeliverMessageActivityName, messageID).Get(ctx, &result); err != nil {
		return DeliveryResult{}, err
	}
	return result, nil
}

// DeliveryResult is what the history keeps about one delivery: whether it
// left on this run or had already been settled, how many attempts the row
// records, and when it left.
type DeliveryResult struct {
	Delivered bool
	Attempts  int32
	SentAt    time.Time
}

// ReclaimOutboxWorkflow is the workflow behind `reclaim-transactional-outbox`:
// one pass, idempotent, the same shape as the snooze expiry.
func ReclaimOutboxWorkflow(ctx workflow.Context) (ReclaimResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
		},
	})

	var result ReclaimResult
	if err := workflow.ExecuteActivity(ctx, ReclaimMessagesActivityName).Get(ctx, &result); err != nil {
		return ReclaimResult{}, err
	}
	return result, nil
}

// ReclaimResult is what one reclaim pass reports.
type ReclaimResult struct {
	Redacted  int32
	Abandoned int32
	RanAt     time.Time
}

// ListUndelivered asks core-api what is pending: messages and notifications.
func (a *Activities) ListUndelivered(ctx context.Context) (Pending, error) {
	res, err := a.Mail.ListUndelivered(ctx, connect.NewRequest(&platformv1.ListUndeliveredRequest{}))
	if err != nil {
		return Pending{}, nonRetryableIfRefused(err)
	}
	return Pending{
		MessageIDs:      res.Msg.GetMessageIds(),
		NotificationIDs: res.Msg.GetNotificationIds(),
	}, nil
}

// StartDeliveries starts one DeliverMessageWorkflow per message and one
// DeliverNotificationWorkflow per notification, and counts.
//
// Starting is idempotent on the id: a row whose delivery is already running
// (it is retrying, or holding somebody through their quiet hours, and a later
// relay tick listed it again) is reported as such rather than started twice,
// because the engine refuses a second run of a running id and this reads that
// refusal as the ordinary thing it is. A row whose earlier delivery has closed
// (failed non-retryably, say, and then the operator fixed the credential) is
// started again, which is what the default id reuse policy allows and what an
// operator fixing a credential wants.
func (a *Activities) StartDeliveries(ctx context.Context, pending Pending) (RelayResult, error) {
	result := RelayResult{Pending: len(pending.MessageIDs) + len(pending.NotificationIDs)}
	start := func(id string, wf any, arg string) error {
		_, err := a.Starter.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
			ID:                       id,
			TaskQueue:                a.TaskQueue,
			WorkflowIDConflictPolicy: enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		}, wf, arg)
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		switch {
		case err == nil:
			result.Started++
		case errors.As(err, &already):
			result.AlreadyRunning++
		default:
			return fmt.Errorf("starting %s: %w", id, err)
		}
		return nil
	}
	for _, id := range pending.MessageIDs {
		if err := start(deliveryWorkflowID(id), DeliverMessageWorkflow, id); err != nil {
			return result, err
		}
	}
	for _, id := range pending.NotificationIDs {
		if err := start(notificationWorkflowID(id), DeliverNotificationWorkflow, id); err != nil {
			return result, err
		}
	}
	return result, nil
}

// DeliverMessage asks core-api to deliver one message, and maps the answer.
//
// The error mapping is the worker's half of the contract the handler states:
// `unavailable` (the channel refused) and `failed_precondition` (no channel
// yet) come back as they are and the retry policy handles them;
// `invalid_argument` and a refused credential are marked non-retryable,
// because neither changes by waiting.
func (a *Activities) DeliverMessage(ctx context.Context, messageID string) (DeliveryResult, error) {
	res, err := a.Mail.DeliverMessage(ctx, connect.NewRequest(&platformv1.DeliverMessageRequest{
		MessageId: messageID,
	}))
	if err != nil {
		return DeliveryResult{}, badRequestOrRefusal(err)
	}
	result := DeliveryResult{
		Delivered: res.Msg.GetOutcome() == platformv1.DeliverMessageResponse_OUTCOME_DELIVERED,
		Attempts:  res.Msg.GetAttempts(),
	}
	if sentAt := res.Msg.GetSentAt(); sentAt != nil {
		result.SentAt = sentAt.AsTime()
	}
	return result, nil
}

// ReclaimMessages runs core-api's retention pass and returns what it reported.
func (a *Activities) ReclaimMessages(ctx context.Context) (ReclaimResult, error) {
	res, err := a.Mail.ReclaimMessages(ctx, connect.NewRequest(&platformv1.ReclaimMessagesRequest{}))
	if err != nil {
		return ReclaimResult{}, nonRetryableIfRefused(err)
	}
	result := ReclaimResult{Redacted: res.Msg.GetRedacted(), Abandoned: res.Msg.GetAbandoned()}
	if ranAt := res.Msg.GetRanAt(); ranAt != nil {
		result.RanAt = ranAt.AsTime()
	}
	return result, nil
}

// nonRetryableIfRefused marks a permission error non-retryable and returns
// every other error as it is, which is the retry policy working rather than
// this code being lazy about them: a network error, a 503 from the edge, an
// Unavailable from core-api are all "try again in a bit", and the policy on
// the workflow side says how. What is NOT retryable is a refusal, because
// retrying a refusal a thousand times produces a thousand refusals, and the
// thing to do is tell an operator their credential no longer holds
// `internal:ingest`.
func nonRetryableIfRefused(err error) error {
	if connect.CodeOf(err) == connect.CodePermissionDenied || connect.CodeOf(err) == connect.CodeUnauthenticated {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("core-api refused the worker's credential: %v", err), "credential", err)
	}
	return err
}
