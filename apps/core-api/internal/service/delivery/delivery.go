// Package delivery serves DeliveryService: the internal surface through which a
// message core-api wrote leaves the process (ENT-256, part three).
//
// On the internal chain with SweepService, for the same reason and with the
// same gate: no tenancy interceptor, because the caller is a service client
// with no membership, and the `internal:ingest` scope, which the seed issues
// to service clients and never to the browser client. A human token cannot
// reach any of this, and the interceptor test for this package proves it.
//
// # WHAT THIS IS THE OTHER HALF OF
//
// `Tenant.EnqueueMessage` writes a row into `transactional_outbox` inside the
// transaction that makes the announced fact true; that half is unchanged. This
// is the half that used to be a ticker in core-api and is now three RPCs that
// a Temporal worker calls: list what is pending, deliver one, reclaim what no
// longer needs keeping. The claim-send-record discipline inside DeliverMessage
// is the store's and is exactly what the ticker did; what moved to the engine
// is when to call, how often to retry, and the record of each attempt.
package delivery

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Outbox is what this handler needs of the agent pool, declared where it is
// used rather than exported from the store (§21.6): the transactional outbox's
// delivery half, and the doorbell path's (see doorbell.go).
type Outbox interface {
	PendingMessageIDs(ctx context.Context, limit int) ([]string, error)
	DeliverMessage(ctx context.Context, id string, deliver postgres.Deliver) (postgres.Delivery, error)
	ReclaimOutbox(ctx context.Context, bodyRetention time.Duration, batch int) (postgres.ReclaimResult, error)
	Doorbells
}

// Retention on the outbox (ENT-242).
//
// # WHY THERE IS A PERIOD AT ALL, AND WHY IT IS SHORT
//
// `body_text` holds the rendered invitation, and the rendered invitation holds
// the raw token in a path segment, because the accept link is the message.
// 00003 stores only that token's hash and says why: a database dump must not
// yield a working invitation. The outbox is the one place that rule is
// suspended, and it was meant to be suspended only until the message was
// delivered. Nothing cleared it, so every address and every message body ever
// sent was still there.
//
// # WHY SEVEN DAYS, RATHER THAN NONE AND RATHER THAN NINETY
//
// Not zero, because a delivered body answers one real question: "what did we
// actually send this person", asked when somebody reports a link that did not
// work. That complaint arrives within days.
//
// Not ninety, because after a week the body answers nothing actionable. Seven
// days is `postgres.InvitationLifetime`, so the window is exactly as long as
// the token inside it can still be used, and not one day longer. Aligning the
// two is the point rather than a coincidence: the period the body is worth
// keeping is the period the link still works.
//
// It is an upper bound rather than the usual case. The reclaim redacts at the
// earlier of this window and the invitation ceasing to be acceptable, so an
// invitation accepted ten minutes after it arrives has its body cleared on the
// next pass. A spent link is worth nothing to anybody, and holding somebody's
// address for a further week to keep it would be the wrong trade.
//
// # AND WHAT IS NOT RECLAIMED
//
// A message that has not been delivered and whose invitation can still be
// accepted is never touched, at any window. The raw token exists nowhere else,
// so clearing that body destroys an invitation somebody is waiting for and
// nobody can tell which ones need reissuing. The predicate that protects it
// lives in the database and takes no argument from here, which is deliberate:
// this constant is a decision and could be edited by anybody, and it must not
// be able to reach that case however it is edited.
const DeliveredBodyRetention = 7 * 24 * time.Hour

// Bounds on one call.
//
// The list limit bounds one relay run rather than the backlog: what is not
// listed now is listed on the next tick, a few seconds later, and a backlog
// large enough to hit it is a mail server that has been down for a while.
// Two hundred is two hundred workflow starts, which the engine takes in its
// stride and which would otherwise all race for the same pool.
//
// The reclaim batch is much larger because it is one indexed statement
// against a partial index sized to the backlog, not a network conversation per
// row.
const (
	DefaultListLimit = 200
	MaxListLimit     = 1000
	ReclaimBatch     = 500
)

// Service implements platformv1connect.DeliveryServiceHandler.
type Service struct {
	outbox Outbox
	// channels is every channel this deployment can deliver on, behind the
	// same one-Channel seam the dispatcher has always held (ENT-263). The
	// router reads the channel each row or recipient names and hands it on, so
	// there is still exactly one place a message leaves from and exactly one
	// retry policy behind it.
	channels *delivery.Router
	// baseURL is where a browser reaches the console, for the links a
	// notification carries. Empty refuses a notification send rather than
	// building a link that 404s.
	baseURL string
	// now is injectable because quiet hours are the one rule here that
	// depends on the clock, and a rule that can only be exercised by waiting
	// until 22:00 is one nobody tests.
	now func() time.Time
}

// New builds the handler.
//
// A nil channel is allowed and is the state a deployment is in before
// KINDLAST_SMTP_ADDR is set: the list, the plan, the settle and the reclaim
// still answer, and a send is refused with `failed_precondition` rather than
// the service being absent, because the rows are there and an operator asking
// "why is mail not leaving" should get an answer that names the setting. An
// empty base URL is the same for notifications.
func New(outbox Outbox, channels *delivery.Router, baseURL string) *Service {
	return &Service{outbox: outbox, channels: channels, baseURL: baseURL, now: time.Now}
}

// WithClock replaces the clock the plan reads, for tests that need to be
// inside or outside somebody's quiet hours on purpose rather than by the
// time of day the suite happens to run.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// ListUndelivered lists pending message ids, oldest first.
func (s *Service) ListUndelivered(
	ctx context.Context,
	req *connect.Request[platformv1.ListUndeliveredRequest],
) (*connect.Response[platformv1.ListUndeliveredResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	limit := int(req.Msg.GetLimit())
	switch {
	case limit <= 0:
		limit = DefaultListLimit
	case limit > MaxListLimit:
		limit = MaxListLimit
	}

	ids, err := s.outbox.PendingMessageIDs(ctx, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	bells, err := s.outbox.PendingDoorbellIDs(ctx, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.ListUndeliveredResponse{
		MessageIds:      ids,
		NotificationIds: bells,
	}), nil
}

// isUUID is the shape check the store also makes, done here so a malformed
// id is `invalid_argument` rather than a database error dressed as internal.
func isUUID(raw string) bool {
	_, err := uuid.Parse(raw)
	return err == nil
}

// DeliverMessage claims one message, sends it, and records the outcome.
//
// The error codes are the contract with the worker's retry policy and are
// worth reading as one table:
//
//	invalid_argument      the id is not a uuid; a caller bug, never retried
//	failed_precondition   no channel is configured, or none for the channel
//	                      this row names; retried, the row waits
//	unavailable           the channel refused; retried with backoff
//	internal              the database did; retried with backoff
//
// A send that fails has already been recorded on the row by the time the
// error leaves here, so the attempt count and the server's answer are in the
// table as well as in the workflow history.
func (s *Service) DeliverMessage(
	ctx context.Context,
	req *connect.Request[platformv1.DeliverMessageRequest],
) (*connect.Response[platformv1.DeliverMessageResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	if req.Msg.GetMessageId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("a delivery names one message; send message_id"))
	}
	if s.channels.Empty() {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no delivery channel is configured (neither KINDLAST_SMTP_ADDR nor "+
				"KINDLAST_TELEGRAM_BOT_TOKEN is set), so the message stays queued until one is"))
	}

	result, err := s.outbox.DeliverMessage(ctx, req.Msg.GetMessageId(), s.send)
	switch {
	case errors.Is(err, postgres.ErrBadMessageID):
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("message_id is not a uuid"))
	// Checked before ErrNotDelivered, which also wraps this, and the order is
	// the whole point (ENT-263). A row addressed to a channel this deployment
	// has not configured is not a provider that is down: no amount of backing
	// off will make it deliverable, and reporting it as `unavailable` would
	// leave it retrying with growing intervals for as long as the deployment
	// lives, with the reason buried in a workflow history nobody reads.
	//
	// It is reachable by exactly one route, which is why the sentinel exists:
	// an operator removing KINDLAST_TELEGRAM_BOT_TOKEN while rows addressed to
	// Telegram are still queued. Linking a chat is refused when the channel is
	// absent, so nothing can address one that never existed. `failed_precondition`
	// names the setting to put back, which is the only action that helps.
	case errors.Is(err, delivery.ErrChannelUnavailable):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, postgres.ErrNotDelivered):
		return nil, connect.NewError(connect.CodeUnavailable, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res := &platformv1.DeliverMessageResponse{
		Outcome:  platformv1.DeliverMessageResponse_OUTCOME_ALREADY_SETTLED,
		Attempts: result.Attempts,
	}
	if result.Sent {
		res.Outcome = platformv1.DeliverMessageResponse_OUTCOME_DELIVERED
		res.SentAt = timestamppb.New(result.SentAt)
	}
	return connect.NewResponse(res), nil
}

// ReclaimMessages clears the personal data out of messages that no longer
// need it.
func (s *Service) ReclaimMessages(
	ctx context.Context,
	_ *connect.Request[platformv1.ReclaimMessagesRequest],
) (*connect.Response[platformv1.ReclaimMessagesResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	result, err := s.outbox.ReclaimOutbox(ctx, DeliveredBodyRetention, ReclaimBatch)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.ReclaimMessagesResponse{
		Redacted:  int32(result.Redacted),
		Abandoned: int32(result.Abandoned),
		RanAt:     timestamppb.New(s.now()),
	}), nil
}

// send adapts a claimed row onto the channel.
//
// The channel is given the recipient, subject and body and nothing else. It
// deliberately does not receive the row id: a channel that knew it could update
// the row, and then two things would be recording the outcome.
// The row names its own channel and its own recipient, so this stayed one
// line longer and gained no branch: adding a channel means registering an
// adapter on the router, not editing here.
func (s *Service) send(ctx context.Context, msg postgres.PendingMessage) error {
	return s.channels.Send(ctx, delivery.Message{
		Channel:  msg.Channel,
		To:       msg.Recipient(),
		Subject:  msg.Subject,
		BodyText: msg.BodyText,
		BodyHTML: msg.BodyHTML,
	})
}
