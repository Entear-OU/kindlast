package schedule

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// DeliverNotificationWorkflow rings one finding's doorbell for everybody who
// wants to hear it, when they want to hear it (ENT-209 on Temporal, ENT-256
// part three).
//
// # THE LOOP, AND WHY IT IS A LOOP
//
// Plan, send to whoever is due, sleep until the earliest hold ends, plan
// again; settle when nobody is left. The plan is asked again after every
// sleep rather than computed once, because a sleep here can be nine hours
// and people change their preferences, leave organisations and get their
// addresses verified in nine hours. What the workflow remembers across rounds
// is only who it has already sent to, so a person is told once however many
// rounds it takes for their colleagues.
//
// # THE SLEEP IS THE WHOLE POINT
//
// `workflow.Sleep` is a durable timer: it survives this process restarting,
// the engine restarting, and the deployment being upgraded in between, and it
// fires once. The loop this replaces could only drop a notification that fell
// inside quiet hours, with the reason on the row, because holding one needs
// exactly this and there was no scheduler (§17.5). Now "inside quiet hours"
// is a time, and the time is kept by the engine.
//
// # WHAT A RESTART COSTS, STATED RATHER THAN HIDDEN
//
// The set of people already sent to lives in this workflow's state. If the
// run fails terminally (a refused credential, the operator fixes it, the
// relay starts a fresh run for the still-pending row), the fresh run sends to
// everybody the plan names, including anybody the failed run reached. A
// duplicate doorbell is a nuisance; a finding nobody hears about is the
// failure this product exists to prevent. The per-recipient delivery record
// §17.3 describes would close that, and arrives with the channels that need
// it.
func DeliverNotificationWorkflow(ctx workflow.Context, notificationID string) (NotificationResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// A plan is one read; a send is a few emails. Two minutes covers
		// either on a busy stack.
		StartToCloseTimeout: 2 * time.Minute,
		// The same policy as a message delivery, for the same reasons: every
		// reason a send fails resolves on its own, and the thing that ends a
		// notification is the row being settled, not a count.
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Minute,
		},
	})

	sent := map[string]bool{}
	var result NotificationResult

	// Bounded, so a plan that keeps answering "hold" for a reason nobody
	// anticipated (a clock that never reaches the end of a window, say) ends
	// in a visible failure rather than a workflow that sleeps forever. A
	// notification with recipients in every zone on Earth and the longest
	// window anybody can set needs a handful of rounds; a hundred is not a
	// budget anyone hits honestly.
	const maxRounds = 100
	for round := 1; round <= maxRounds; round++ {
		var plan NotificationPlan
		if err := workflow.ExecuteActivity(ctx, PlanNotificationActivityName, notificationID).Get(ctx, &plan); err != nil {
			return result, err
		}
		if plan.Settled {
			// Sent or skipped by an earlier run, or gone. Nothing to do and
			// nothing to record: the row already says what happened.
			result.Settled = true
			return result, nil
		}

		var due []string
		var holdUntil time.Time
		for _, r := range plan.Recipients {
			if sent[r.UserID] {
				continue
			}
			switch r.Decision {
			case decisionSend:
				due = append(due, r.UserID)
			case decisionHold:
				if holdUntil.IsZero() || r.HoldUntil.Before(holdUntil) {
					holdUntil = r.HoldUntil
				}
				result.Held++
				result.Reason = r.Reason
			default:
				result.Reason = r.Reason
			}
		}

		if len(due) > 0 {
			var n int32
			if err := workflow.ExecuteActivity(ctx, NotifyRecipientsActivityName, notificationID, due).Get(ctx, &n); err != nil {
				return result, err
			}
			for _, u := range due {
				sent[u] = true
			}
			result.Sent += int(n)
		}

		if holdUntil.IsZero() {
			break
		}
		result.Held = 0
		// Never a zero or negative sleep, and never a sub-minute one: the
		// plan's clock and the engine's can differ by a little, and a round
		// that re-plans every few milliseconds until they agree is a hot loop
		// in a workflow history.
		wait := holdUntil.Sub(workflow.Now(ctx))
		if wait < time.Minute {
			wait = time.Minute
		}
		result.Rounds = round
		if err := workflow.Sleep(ctx, wait); err != nil {
			return result, err
		}
	}

	outcome := settleSkipped
	if result.Sent > 0 {
		outcome = settleSent
	}
	var settled bool
	if err := workflow.ExecuteActivity(ctx, SettleNotificationActivityName, notificationID, outcome, result.Reason).Get(ctx, &settled); err != nil {
		return result, err
	}
	result.Settled = settled
	return result, nil
}

// NotificationPlan is core-api's answer to "who, and when", as the workflow
// keeps it: user ids, decisions and times. No addresses (§16.3).
type NotificationPlan struct {
	Settled    bool
	Recipients []PlannedRecipient
}

// PlannedRecipient is one person's decision as of the plan.
type PlannedRecipient struct {
	UserID    string
	Decision  string
	HoldUntil time.Time
	Reason    string
}

// The decisions, as strings in the history so a person reading it in the UI
// sees "hold" rather than 2.
const (
	decisionSend = "send"
	decisionHold = "hold"
	decisionSkip = "skip"
)

// The settle outcomes, likewise.
const (
	settleSent    = "sent"
	settleSkipped = "skipped"
)

// NotificationResult is what the history keeps about one notification.
type NotificationResult struct {
	// Sent is how many emails left, across every round.
	Sent int
	// Held is how many recipients were still being held when the run ended,
	// which is zero unless the round budget ran out.
	Held int
	// Rounds is how many times the workflow slept and planned again.
	Rounds int
	// Reason is the last hold or skip reason seen, and is what the row
	// records when nobody wanted it.
	Reason string
	// Settled is whether the row was settled by this run (or found settled).
	Settled bool
}

// PlanNotification asks core-api who a notification goes to, and when.
func (a *Activities) PlanNotification(ctx context.Context, notificationID string) (NotificationPlan, error) {
	res, err := a.Mail.PlanNotification(ctx, connect.NewRequest(&platformv1.PlanNotificationRequest{
		NotificationId: notificationID,
	}))
	if err != nil {
		return NotificationPlan{}, badRequestOrRefusal(err)
	}
	plan := NotificationPlan{Settled: res.Msg.GetSettled()}
	for _, r := range res.Msg.GetRecipients() {
		p := PlannedRecipient{UserID: r.GetUserId(), Reason: r.GetReason()}
		switch r.GetDecision() {
		case platformv1.PlannedRecipient_DECISION_SEND:
			p.Decision = decisionSend
		case platformv1.PlannedRecipient_DECISION_HOLD:
			p.Decision = decisionHold
			if until := r.GetHoldUntil(); until != nil {
				p.HoldUntil = until.AsTime()
			}
		default:
			p.Decision = decisionSkip
		}
		plan.Recipients = append(plan.Recipients, p)
	}
	return plan, nil
}

// NotifyRecipients asks core-api to send to the named people now.
func (a *Activities) NotifyRecipients(ctx context.Context, notificationID string, userIDs []string) (int32, error) {
	res, err := a.Mail.NotifyRecipients(ctx, connect.NewRequest(&platformv1.NotifyRecipientsRequest{
		NotificationId: notificationID,
		UserIds:        userIDs,
	}))
	if err != nil {
		return 0, badRequestOrRefusal(err)
	}
	return res.Msg.GetSent(), nil
}

// SettleNotification marks the row sent or skipped.
func (a *Activities) SettleNotification(ctx context.Context, notificationID, outcome, reason string) (bool, error) {
	req := &platformv1.SettleNotificationRequest{NotificationId: notificationID, Reason: reason}
	if outcome == settleSent {
		req.Outcome = platformv1.SettleNotificationRequest_OUTCOME_SENT
	} else {
		req.Outcome = platformv1.SettleNotificationRequest_OUTCOME_SKIPPED
		if req.Reason == "" {
			// core-api refuses a skip without a reason, rightly. Reaching
			// here with none means the plan listed nobody at all.
			req.Reason = "no member of this organisation has a deliverable address"
		}
	}
	res, err := a.Mail.SettleNotification(ctx, connect.NewRequest(req))
	if err != nil {
		return false, badRequestOrRefusal(err)
	}
	return res.Msg.GetSettled(), nil
}

// badRequestOrRefusal is nonRetryableIfRefused plus the other answer that
// does not change by waiting: core-api calling the request malformed.
func badRequestOrRefusal(err error) error {
	if connect.CodeOf(err) == connect.CodeInvalidArgument {
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("core-api refused the request: %v", err), "bad-request", err)
	}
	return nonRetryableIfRefused(err)
}
