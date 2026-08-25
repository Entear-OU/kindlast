package interceptor_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	deliveryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// DeliveryService's gate (ENT-256, part three), on the same chain as the
// sweep and tested the same way: the verifier is real, the outbox is a
// recorder, and what is asserted is that a human token cannot reach any of the
// three RPCs, that a service token can, and that the error codes the worker's
// retry policy keys on are the ones the handler actually returns.
//
// The recorder's Deliver calls the supplied send function, so the test also
// sees that the channel is the thing asked to send and that nothing but the
// recipient, subject and body reaches it.

type recordingOutbox struct {
	pending  []string
	lists    int
	delivers []string
	reclaims int
	// sent is what the channel was handed, via the handler's adapter.
	sent []delivery.Message
	// settled makes Deliver report "nothing pending by that id".
	settled bool

	// The doorbell half.
	commits      int
	locks        int
	tokens       int
	delegations  int
	bellSettled  bool
	bellOutcome  string
	bellFailures []string
}

func (r *recordingOutbox) PendingMessageIDs(context.Context, int) ([]string, error) {
	r.lists++
	return r.pending, nil
}

func (r *recordingOutbox) DeliverMessage(
	ctx context.Context, id string, deliver postgres.Deliver,
) (postgres.Delivery, error) {
	r.delivers = append(r.delivers, id)
	if id == "not-a-uuid" {
		return postgres.Delivery{}, postgres.ErrBadMessageID
	}
	if r.settled {
		return postgres.Delivery{}, nil
	}
	msg := postgres.PendingMessage{
		ID: id, Kind: "invitation", RecipientEmail: "invitee@example.invalid",
		Subject: "You are invited", BodyText: "Accept at https://example.invalid/i/tok", Attempts: 1,
	}
	if err := deliver(ctx, msg); err != nil {
		return postgres.Delivery{Attempts: 2}, errors.Join(postgres.ErrNotDelivered, err)
	}
	return postgres.Delivery{Sent: true, Attempts: 2, SentAt: time.Unix(0, 0).UTC()}, nil
}

func (r *recordingOutbox) ReclaimOutbox(
	_ context.Context, _ time.Duration, _ int,
) (postgres.ReclaimResult, error) {
	r.reclaims++
	return postgres.ReclaimResult{Redacted: 3, Abandoned: 1}, nil
}

// The doorbell half (ENT-256, part three). One pending notification with
// three candidate recipients, chosen so that at the pinned clock (noon UTC)
// one is due now, one is inside quiet hours, and one is below their floor.
const bellID = "22222222-2222-2222-2222-222222222222"

var bellRecipients = []postgres.Recipient{
	{UserID: "due", Email: "due@example.invalid", EmailVerified: true,
		MinSeverity: "low", FindingSeverity: "high", Timezone: "UTC", OrgSlug: "acme", OrgName: "Acme"},
	{UserID: "asleep", Email: "asleep@example.invalid",
		MinSeverity: "low", FindingSeverity: "high", Timezone: "UTC",
		QuietHoursStart: "11:00", QuietHoursEnd: "14:00", OrgSlug: "acme", OrgName: "Acme"},
	{UserID: "uninterested", Email: "u@example.invalid",
		MinSeverity: "critical", FindingSeverity: "high", Timezone: "UTC", OrgSlug: "acme", OrgName: "Acme"},
}

// fakeTx is the transaction the handler is handed and commits or rolls back;
// nothing else on it is called, and calling it would panic, which is the
// point of embedding the interface unimplemented.
type fakeTx struct {
	pgx.Tx
	outbox *recordingOutbox
}

func (f fakeTx) Commit(context.Context) error   { f.outbox.commits++; return nil }
func (f fakeTx) Rollback(context.Context) error { return nil }

func (r *recordingOutbox) Begin(context.Context) (pgx.Tx, error) { return fakeTx{outbox: r}, nil }
func (r *recordingOutbox) PendingDoorbellIDs(context.Context, int) ([]string, error) {
	if r.bellSettled {
		return nil, nil
	}
	return []string{bellID}, nil
}
func (r *recordingOutbox) Doorbell(_ context.Context, id string) (postgres.Doorbell, error) {
	if r.bellSettled || id != bellID {
		return postgres.Doorbell{}, pgx.ErrNoRows
	}
	return postgres.Doorbell{ID: bellID, OrgID: "33333333-3333-3333-3333-333333333333",
		FindingID: "44444444-4444-4444-4444-444444444444"}, nil
}
func (r *recordingOutbox) LockDoorbell(ctx context.Context, _ pgx.Tx, id string) (postgres.Doorbell, error) {
	r.locks++
	return r.Doorbell(ctx, id)
}
func (r *recordingOutbox) FindingCounts(context.Context, pgx.Tx, string, string) (int32, int64, error) {
	return 0, 1, nil
}

func (r *recordingOutbox) Recipients(context.Context, pgx.Tx, string) ([]postgres.Recipient, error) {
	return bellRecipients, nil
}
func (r *recordingOutbox) MintCapabilityToken(context.Context, pgx.Tx, string, string, string, string, string) error {
	r.tokens++
	return nil
}
func (r *recordingOutbox) MintApprovalDelegation(context.Context, pgx.Tx, string, string, string, time.Duration) (bool, error) {
	r.delegations++
	return true, nil
}
func (r *recordingOutbox) MarkDoorbellSent(context.Context, pgx.Tx, string) error {
	r.bellSettled = true
	r.bellOutcome = "sent"
	return nil
}
func (r *recordingOutbox) MarkDoorbellSkipped(_ context.Context, _ pgx.Tx, _, reason string) error {
	r.bellSettled = true
	r.bellOutcome = "skipped: " + reason
	return nil
}
func (r *recordingOutbox) MarkDoorbellFailed(_ context.Context, _ pgx.Tx, _ string, cause error) error {
	r.bellFailures = append(r.bellFailures, cause.Error())
	return nil
}

// recordingChannel is the mail server, minus the mail server.
type recordingChannel struct {
	outbox *recordingOutbox
	refuse error
}

func (c *recordingChannel) Name() string { return "recording" }
func (c *recordingChannel) Send(_ context.Context, msg delivery.Message) error {
	if c.refuse != nil {
		return c.refuse
	}
	c.outbox.sent = append(c.outbox.sent, msg)
	return nil
}

func buildDeliveryChain(t *testing.T, a *authServer, channel delivery.Channel) (
	platformv1connect.DeliveryServiceClient, *recordingOutbox,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)

	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)

	outbox := &recordingOutbox{pending: []string{"11111111-1111-1111-1111-111111111111"}}
	if rc, ok := channel.(*recordingChannel); ok {
		rc.outbox = outbox
	}
	// Noon UTC, pinned: "asleep" above is inside 11:00 to 14:00 and the
	// other two are not in any window.
	noon := func() time.Time { return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC) }
	// A nil channel is a router with nothing on it, which is what a deployment
	// that has configured neither SMTP nor a bot token has (ENT-263). The
	// failed_precondition cases below turn on exactly that.
	channels := delivery.NewRouter()
	channels.Register(delivery.ChannelEmail, channel)
	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewDeliveryServiceHandler(
		deliveryservice.New(outbox, channels, "http://localhost:3000").WithClock(noon), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return platformv1connect.NewDeliveryServiceClient(server.Client(), server.URL), outbox
}

const humanScopes = "openid profile email findings:read findings:act dashboard:read org:read org:manage notifications:read notifications:write"

func TestNoDeliveryRPCIsReachableWithAHumanToken(t *testing.T) {
	a := newAuthServer(t)
	client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
	human := sweepHeaders(t, a, humanScopes, "")

	_, err := client.ListUndelivered(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListUndeliveredRequest{}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("list: got %v, want permission_denied", got)
	}
	_, err = client.DeliverMessage(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("deliver: got %v, want permission_denied", got)
	}
	_, err = client.ReclaimMessages(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ReclaimMessagesRequest{}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("reclaim: got %v, want permission_denied", got)
	}

	if outbox.lists+len(outbox.delivers)+outbox.reclaims != 0 {
		t.Fatalf("the outbox was reached by a human token: %+v", outbox)
	}
}

func TestAServiceTokenListsDeliversAndReclaims(t *testing.T) {
	a := newAuthServer(t)
	client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
	service := sweepHeaders(t, a, "internal:ingest", "")

	listed, err := client.ListUndelivered(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListUndeliveredRequest{}), service))
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if got := listed.Msg.GetMessageIds(); len(got) != 1 || got[0] != outbox.pending[0] {
		t.Fatalf("listed %v, want the one pending id", got)
	}

	delivered, err := client.DeliverMessage(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
	if err != nil {
		t.Fatalf("delivering: %v", err)
	}
	if delivered.Msg.GetOutcome() != platformv1.DeliverMessageResponse_OUTCOME_DELIVERED {
		t.Fatalf("outcome = %v, want delivered", delivered.Msg.GetOutcome())
	}
	if delivered.Msg.GetAttempts() != 2 || delivered.Msg.GetSentAt() == nil {
		t.Errorf("attempts and sent_at did not survive the round trip: %+v", delivered.Msg)
	}
	if len(outbox.sent) != 1 || outbox.sent[0].To != "invitee@example.invalid" {
		t.Fatalf("the channel was handed %+v, want the one message", outbox.sent)
	}

	reclaimed, err := client.ReclaimMessages(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ReclaimMessagesRequest{}), service))
	if err != nil {
		t.Fatalf("reclaiming: %v", err)
	}
	if reclaimed.Msg.GetRedacted() != 3 || reclaimed.Msg.GetAbandoned() != 1 {
		t.Errorf("counts did not survive the round trip: %+v", reclaimed.Msg)
	}
	if outbox.reclaims != 1 {
		t.Errorf("the reclaim ran %d times, want 1", outbox.reclaims)
	}
}

// The codes the worker's retry policy keys on, each one produced by the
// state that should produce it. A wrong code here is not a failing request,
// it is a message retried forever or given up on at once.
func TestDeliveryErrorCodesAreTheRetryContract(t *testing.T) {
	a := newAuthServer(t)
	service := sweepHeaders(t, a, "internal:ingest", "")

	t.Run("a refused send is unavailable, and was recorded", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a,
			&recordingChannel{refuse: errors.New("451 try again later")})
		_, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
		if got := codeOf(t, err); got != connect.CodeUnavailable {
			t.Fatalf("got %v, want unavailable", got)
		}
		if len(outbox.delivers) != 1 {
			t.Fatalf("the store was asked %d times, want 1: the attempt is recorded there", len(outbox.delivers))
		}
	})

	t.Run("no channel is failed_precondition, and the store is not asked", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, nil)
		_, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
		if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
			t.Fatalf("got %v, want failed_precondition", got)
		}
		if len(outbox.delivers) != 0 {
			t.Fatal("a row was claimed with no channel to send it on")
		}
	})

	t.Run("a settled row is success, not an error", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
		outbox.settled = true
		res, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
		if err != nil {
			t.Fatalf("a settled row failed the call: %v", err)
		}
		if res.Msg.GetOutcome() != platformv1.DeliverMessageResponse_OUTCOME_ALREADY_SETTLED {
			t.Fatalf("outcome = %v, want already settled", res.Msg.GetOutcome())
		}
		if len(outbox.sent) != 0 {
			t.Fatal("a settled row was sent again")
		}
	})

	t.Run("a bad id is invalid_argument", func(t *testing.T) {
		client, _ := buildDeliveryChain(t, a, &recordingChannel{})
		_, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: "not-a-uuid"}), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Fatalf("got %v, want invalid_argument", got)
		}
		_, err = client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{}), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Fatalf("empty id: got %v, want invalid_argument", got)
		}
	})
}

// The doorbell RPCs (ENT-256, part three) sit behind the same gate, and the
// three verbs the workflow speaks answer as the contract says.
func TestNoDoorbellRPCIsReachableWithAHumanToken(t *testing.T) {
	a := newAuthServer(t)
	client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
	human := sweepHeaders(t, a, humanScopes, "")

	_, err := client.PlanNotification(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.PlanNotificationRequest{NotificationId: bellID}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("plan: got %v, want permission_denied", got)
	}
	_, err = client.NotifyRecipients(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.NotifyRecipientsRequest{NotificationId: bellID, UserIds: []string{"due"}}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("notify: got %v, want permission_denied", got)
	}
	_, err = client.SettleNotification(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleNotificationRequest{
			NotificationId: bellID, Outcome: platformv1.SettleNotificationRequest_OUTCOME_SENT}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("settle: got %v, want permission_denied", got)
	}
	if outbox.locks != 0 || len(outbox.sent) != 0 || outbox.bellSettled {
		t.Fatalf("a human token reached the doorbell path: %+v", outbox)
	}
}

func TestTheWorkflowsThreeVerbsPlanNotifyAndSettle(t *testing.T) {
	a := newAuthServer(t)
	client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
	service := sweepHeaders(t, a, "internal:ingest", "")

	// The relay lists it.
	listed, err := client.ListUndelivered(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListUndeliveredRequest{}), service))
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if got := listed.Msg.GetNotificationIds(); len(got) != 1 || got[0] != bellID {
		t.Fatalf("listed notifications %v, want the one pending id", got)
	}

	// PLAN: one due, one held until 14:00 UTC, one skipped below the floor.
	plan, err := client.PlanNotification(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.PlanNotificationRequest{NotificationId: bellID}), service))
	if err != nil {
		t.Fatalf("planning: %v", err)
	}
	if plan.Msg.GetSettled() {
		t.Fatal("a pending notification planned as settled")
	}
	decisions := map[string]*platformv1.PlannedRecipient{}
	for _, r := range plan.Msg.GetRecipients() {
		decisions[r.GetUserId()] = r
	}
	if d := decisions["due"]; d == nil || d.GetDecision() != platformv1.PlannedRecipient_DECISION_SEND {
		t.Fatalf("due = %v, want send", d)
	}
	if d := decisions["asleep"]; d == nil || d.GetDecision() != platformv1.PlannedRecipient_DECISION_HOLD ||
		!d.GetHoldUntil().AsTime().Equal(time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("asleep = %v, want held until 14:00 UTC", d)
	}
	if d := decisions["uninterested"]; d == nil || d.GetDecision() != platformv1.PlannedRecipient_DECISION_SKIP || d.GetReason() == "" {
		t.Fatalf("uninterested = %v, want skipped with a reason", d)
	}

	// NOTIFY the due one only: one email, one unsubscribe token, one approve
	// link (the address is verified), the row locked and the transaction
	// committed, and NOT settled.
	sent, err := client.NotifyRecipients(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.NotifyRecipientsRequest{NotificationId: bellID, UserIds: []string{"due"}}), service))
	if err != nil {
		t.Fatalf("notifying: %v", err)
	}
	if sent.Msg.GetSent() != 1 || sent.Msg.GetSettled() {
		t.Fatalf("notify = %+v, want one sent, not settled", sent.Msg)
	}
	if len(outbox.sent) != 1 || outbox.sent[0].To != "due@example.invalid" {
		t.Fatalf("the channel was handed %+v, want one message to the due recipient", outbox.sent)
	}
	if outbox.tokens != 1 || outbox.delegations != 1 || outbox.locks != 1 || outbox.commits != 1 {
		t.Fatalf("tokens=%d delegations=%d locks=%d commits=%d, want 1 each", outbox.tokens, outbox.delegations, outbox.locks, outbox.commits)
	}
	if outbox.bellSettled {
		t.Fatal("a send settled the row; that is the workflow's call, once nobody is left")
	}

	// SETTLE as sent. A second settle is a no-op that says so.
	settled, err := client.SettleNotification(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleNotificationRequest{
			NotificationId: bellID, Outcome: platformv1.SettleNotificationRequest_OUTCOME_SENT}), service))
	if err != nil {
		t.Fatalf("settling: %v", err)
	}
	if !settled.Msg.GetSettled() || outbox.bellOutcome != "sent" {
		t.Fatalf("settle = %+v, outcome %q; want settled as sent", settled.Msg, outbox.bellOutcome)
	}
	again, err := client.SettleNotification(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.SettleNotificationRequest{
			NotificationId: bellID, Outcome: platformv1.SettleNotificationRequest_OUTCOME_SENT}), service))
	if err != nil || again.Msg.GetSettled() {
		t.Fatalf("settling twice: err=%v settled=%v; want no error and settled=false", err, again.Msg.GetSettled())
	}
	// And the plan now says settled.
	plan, err = client.PlanNotification(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.PlanNotificationRequest{NotificationId: bellID}), service))
	if err != nil || !plan.Msg.GetSettled() {
		t.Fatalf("planning a settled row: err=%v settled=%v", err, plan.Msg.GetSettled())
	}
}

func TestDoorbellErrorCodesAreTheRetryContract(t *testing.T) {
	a := newAuthServer(t)
	service := sweepHeaders(t, a, "internal:ingest", "")

	t.Run("a refused send is unavailable and recorded on the row", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, &recordingChannel{refuse: errors.New("451 try again later")})
		_, err := client.NotifyRecipients(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.NotifyRecipientsRequest{NotificationId: bellID, UserIds: []string{"due"}}), service))
		if got := codeOf(t, err); got != connect.CodeUnavailable {
			t.Fatalf("got %v, want unavailable", got)
		}
		if len(outbox.bellFailures) != 1 || outbox.commits != 1 {
			t.Fatalf("failures=%v commits=%d; want the attempt recorded and committed", outbox.bellFailures, outbox.commits)
		}
	})

	t.Run("no channel is failed_precondition before anything is locked", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, nil)
		_, err := client.NotifyRecipients(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.NotifyRecipientsRequest{NotificationId: bellID, UserIds: []string{"due"}}), service))
		if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
			t.Fatalf("got %v, want failed_precondition", got)
		}
		if outbox.locks != 0 {
			t.Fatal("a row was locked with no channel to send on")
		}
	})

	t.Run("a skip without a reason is refused", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
		_, err := client.SettleNotification(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.SettleNotificationRequest{
				NotificationId: bellID, Outcome: platformv1.SettleNotificationRequest_OUTCOME_SKIPPED}), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Fatalf("got %v, want invalid_argument", got)
		}
		if outbox.bellSettled {
			t.Fatal("a skip with no reason was recorded")
		}
	})

	t.Run("a bad id is invalid_argument on all three", func(t *testing.T) {
		client, _ := buildDeliveryChain(t, a, &recordingChannel{})
		if _, err := client.PlanNotification(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.PlanNotificationRequest{NotificationId: "nope"}), service)); codeOf(t, err) != connect.CodeInvalidArgument {
			t.Fatalf("plan: got %v, want invalid_argument", codeOf(t, err))
		}
		if _, err := client.NotifyRecipients(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.NotifyRecipientsRequest{NotificationId: "nope", UserIds: []string{"x"}}), service)); codeOf(t, err) != connect.CodeInvalidArgument {
			t.Fatalf("notify: got %v, want invalid_argument", codeOf(t, err))
		}
		if _, err := client.SettleNotification(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.SettleNotificationRequest{NotificationId: "nope",
				Outcome: platformv1.SettleNotificationRequest_OUTCOME_SENT}), service)); codeOf(t, err) != connect.CodeInvalidArgument {
			t.Fatalf("settle: got %v, want invalid_argument", codeOf(t, err))
		}
	})
}
