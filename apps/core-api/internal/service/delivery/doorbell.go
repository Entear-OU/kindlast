package delivery

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// The doorbell path (ENT-209), now driven from Temporal (ENT-256, part three).
//
// # WHAT MOVED AND WHAT DID NOT
//
// The in-process loop this replaces did four things per notification in one
// transaction: claim the oldest pending row, work out who wants it from
// memberships and preferences, mint each person's links and send to each, and
// mark the row. The deciding, the minting and the sending are here, unchanged
// in substance. What is gone is the claim-the-oldest and the one-shot "nobody
// wants it right now, so skip it", because the workflow now owns the sequence:
// it PLANS (who is due, who is held until when, who is skipped), SENDS to the
// due, sleeps until the earliest hold ends, plans again, and SETTLES the row
// when nobody is left. Quiet hours become a durable timer rather than a
// dropped message, which §17.5 recorded as the limitation of dispatching on a
// plain ticker.
//
// # WHY THIS SHARES THE CHANNEL AND NOT THE MESSAGE PATH
//
// `transactional_outbox` carries one rendered message with one recipient
// decided at mint; a notification is a fact whose recipients are decided now,
// from memberships and preferences. Same delivery seam, different resolution
// (§23.6): the channel is shared, the rows are not, and forcing a recipient
// into the notification row at enqueue is exactly what ENT-192's as-built note
// warns against.

// UnsubscribeLifetime is how long a link in an email stays usable.
//
// Longer than an invitation's seven days, because people read compliance email
// late and an expired unsubscribe link is worse than an expired invitation: the
// person is trying to stop mail they did not want, and failing them teaches
// them to use their mail client's spam button instead, which costs the sender's
// domain reputation rather than one message.
const UnsubscribeLifetime = "30 days"

// ApprovalLifetime is how long §8's one-tap approve link stays usable.
//
// An hour, which is 00021's ceiling for any delegation, and this is the one
// place in the design where two things pull against each other hard enough to
// be worth writing down rather than resolving quietly.
//
// People read compliance mail late. That argument is why an unsubscribe token
// lives for thirty days, and it applies just as well to an approve link. But an
// unsubscribe token stops mail and an approve link makes a regulatory decision,
// and 00021's ceiling is a claim the schema makes to a customer: no delegation
// is ever long-lived, and it binds the migrator so that the claim is true
// rather than merely intended. Widening it for the mailbox case would be
// weakening a security boundary to buy convenience.
//
// So the ceiling wins and the cost lands on the interstitial, which tells
// somebody arriving with an expired link exactly that and points them at the
// finding in the console. One more click, rather than a credential with a
// month's life sitting in a mailbox.
const ApprovalLifetime = time.Hour

// Doorbells is the store half of the doorbell path, declared where it is
// used (§21.6).
type Doorbells interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	PendingDoorbellIDs(ctx context.Context, limit int) ([]string, error)
	Doorbell(ctx context.Context, id string) (postgres.Doorbell, error)
	LockDoorbell(ctx context.Context, tx pgx.Tx, id string) (postgres.Doorbell, error)
	Recipients(ctx context.Context, tx pgx.Tx, outboxID string) ([]postgres.Recipient, error)
	MintCapabilityToken(ctx context.Context, tx pgx.Tx, orgID, userID, kind, tokenHash, lifetime string) error
	// MintApprovalDelegation returns false when the mint was declined rather
	// than when it failed, which is why it is a boolean and not an error. See
	// the store method for the two races that produce it.
	MintApprovalDelegation(ctx context.Context, tx pgx.Tx, outboxID, userID, tokenHash string, lifetime time.Duration) (bool, error)
	MarkDoorbellSent(ctx context.Context, tx pgx.Tx, id string) error
	MarkDoorbellSkipped(ctx context.Context, tx pgx.Tx, id, reason string) error
	MarkDoorbellFailed(ctx context.Context, tx pgx.Tx, id string, cause error) error
}

// PlanNotification works out who a notification goes to, and when, as of now.
func (s *Service) PlanNotification(
	ctx context.Context,
	req *connect.Request[platformv1.PlanNotificationRequest],
) (*connect.Response[platformv1.PlanNotificationResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	id, err := notificationID(req.Msg.GetNotificationId())
	if err != nil {
		return nil, err
	}

	if _, err := s.outbox.Doorbell(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return connect.NewResponse(&platformv1.PlanNotificationResponse{Settled: true}), nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// The recipients function is SECURITY DEFINER and wants a transaction to
	// run in like every other call into it; this one is read only and is
	// rolled back.
	tx, err := s.outbox.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	recipients, err := s.outbox.Recipients(ctx, tx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res := &platformv1.PlanNotificationResponse{}
	for _, r := range recipients {
		// The decision, in Go, over rows the database merely fetched.
		d := notify.Decide(r.FindingSeverity, r.MinSeverity,
			s.localTime(r.Timezone), r.QuietHoursStart, r.QuietHoursEnd)
		planned := &platformv1.PlannedRecipient{UserId: r.UserID, Reason: d.Reason}
		switch {
		case d.Send:
			planned.Decision = platformv1.PlannedRecipient_DECISION_SEND
		case d.Held():
			planned.Decision = platformv1.PlannedRecipient_DECISION_HOLD
			planned.HoldUntil = timestamppb.New(d.HoldUntil)
		default:
			planned.Decision = platformv1.PlannedRecipient_DECISION_SKIP
		}
		res.Recipients = append(res.Recipients, planned)
	}
	return connect.NewResponse(res), nil
}

// NotifyRecipients sends one notification to the named people, now.
func (s *Service) NotifyRecipients(
	ctx context.Context,
	req *connect.Request[platformv1.NotifyRecipientsRequest],
) (*connect.Response[platformv1.NotifyRecipientsResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	id, err := notificationID(req.Msg.GetNotificationId())
	if err != nil {
		return nil, err
	}
	if len(req.Msg.GetUserIds()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("a notification goes to somebody; send user_ids"))
	}
	if s.channel == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no mail channel is configured (KINDLAST_SMTP_ADDR is not set), "+
				"so the notification stays queued until one is"))
	}
	if s.baseURL == "" {
		// Every notification carries a link into `/o/{slug}/`, and an email
		// whose only actionable content is broken is worse than one that has
		// not been sent.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no console address is configured (KINDLAST_APP_BASE_URL is not set), "+
				"so the notification stays queued until one is"))
	}

	tx, err := s.outbox.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bell, err := s.outbox.LockDoorbell(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return connect.NewResponse(&platformv1.NotifyRecipientsResponse{Settled: true}), nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	recipients, err := s.outbox.Recipients(ctx, tx, bell.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	// Only who the plan named, and only if they are still a recipient. The
	// plan is the decision; this is the delivery, and the same person is not
	// re-decided here, or a recipient whose quiet hours start between the
	// plan and the send would be held twice.
	asked := make(map[string]bool, len(req.Msg.GetUserIds()))
	for _, u := range req.Msg.GetUserIds() {
		asked[u] = true
	}
	var wanted []postgres.Recipient
	for _, r := range recipients {
		if asked[r.UserID] {
			wanted = append(wanted, r)
		}
	}

	if sendErr := s.sendAll(ctx, tx, bell, wanted); sendErr != nil {
		if err := s.outbox.MarkDoorbellFailed(ctx, tx, bell.ID, sendErr); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, connect.NewError(connect.CodeInternal,
				fmt.Errorf("committing a failed notification: %w", err))
		}
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("the notification was not delivered: %w", sendErr))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("committing a notification: %w", err))
	}
	return connect.NewResponse(&platformv1.NotifyRecipientsResponse{
		Sent: int32(len(wanted)),
	}), nil
}

// SettleNotification marks a notification sent or skipped.
func (s *Service) SettleNotification(
	ctx context.Context,
	req *connect.Request[platformv1.SettleNotificationRequest],
) (*connect.Response[platformv1.SettleNotificationResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	id, err := notificationID(req.Msg.GetNotificationId())
	if err != nil {
		return nil, err
	}
	outcome := req.Msg.GetOutcome()
	if outcome == platformv1.SettleNotificationRequest_OUTCOME_UNSPECIFIED {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("settling names an outcome; send sent or skipped"))
	}
	reason := req.Msg.GetReason()
	if outcome == platformv1.SettleNotificationRequest_OUTCOME_SKIPPED && reason == "" {
		// An operator asking "why did nothing go out" deserves an answer
		// better than silence, so a skip without one is refused rather than
		// recorded.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("a skipped notification records why; send reason"))
	}

	tx, err := s.outbox.Begin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	bell, err := s.outbox.LockDoorbell(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return connect.NewResponse(&platformv1.SettleNotificationResponse{Settled: false}), nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if outcome == platformv1.SettleNotificationRequest_OUTCOME_SENT {
		err = s.outbox.MarkDoorbellSent(ctx, tx, bell.ID)
	} else {
		err = s.outbox.MarkDoorbellSkipped(ctx, tx, bell.ID, reason)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("committing a settled notification: %w", err))
	}
	return connect.NewResponse(&platformv1.SettleNotificationResponse{Settled: true}), nil
}

// notificationID validates the id a request names. Not a uuid is a caller
// bug and invalid_argument, which the worker does not retry.
func notificationID(raw string) (string, error) {
	if raw == "" {
		return "", connect.NewError(connect.CodeInvalidArgument,
			errors.New("a notification names one row; send notification_id"))
	}
	if !isUUID(raw) {
		return "", connect.NewError(connect.CodeInvalidArgument,
			errors.New("notification_id is not a uuid"))
	}
	return raw, nil
}

// sendAll mints one unsubscribe link per recipient and sends to each.
//
// One token per person, never one per notification. An unsubscribe link is a
// bearer credential that turns off somebody's email, so a shared one would let
// any recipient unsubscribe every other recipient, and the schema would record
// it as their own choice.
//
// The first failure stops the batch and the whole batch is retried. That means
// a second attempt can re-send to somebody who already received it, which is
// the same trade the transactional outbox makes and the same direction: a
// duplicate doorbell is a nuisance, a finding nobody hears about is the
// failure this product exists to prevent.
func (s *Service) sendAll(
	ctx context.Context, tx pgx.Tx, bell postgres.Doorbell, recipients []postgres.Recipient,
) error {
	for _, r := range recipients {
		// Built per recipient from the row's own organisation, so the link
		// lands in the organisation the notification is about rather than
		// wherever the reader's session last pointed. §8 names the failure that
		// prevents: a consultant with three clients acting against the wrong
		// company from a stale link.
		findingURL := fmt.Sprintf("%s/o/%s/feed/%s", s.baseURL, r.OrgSlug, bell.FindingID)

		token, err := newCapabilityToken()
		if err != nil {
			return err
		}
		if err := s.outbox.MintCapabilityToken(ctx, tx, bell.OrgID, r.UserID,
			"unsubscribe", postgres.HashInvitationToken(token), UnsubscribeLifetime); err != nil {
			return err
		}

		approveURL, err := s.approveLink(ctx, tx, bell, r)
		if err != nil {
			return err
		}

		msg := notify.FindingNotification(notify.Doorbell{
			RecipientEmail: r.Email,
			OrgName:        r.OrgName,
			Severity:       r.FindingSeverity,
			FindingURL:     findingURL,
			UnsubscribeURL: fmt.Sprintf("%s/unsubscribe/%s", s.baseURL, token),
			ApproveURL:     approveURL,
		})

		if err := s.channel.Send(ctx, delivery.Message{
			To:       msg.RecipientEmail,
			Subject:  msg.Subject,
			BodyText: msg.BodyText,
		}); err != nil {
			return err
		}
	}
	return nil
}

// approveLink mints §8's one-tap approval for one recipient, or nothing.
//
// # THE ADDRESS GATE IS ASKED HERE AND ENFORCED IN THE SCHEMA
//
// §1.8 gates acting on a finding behind an address somebody proved they
// control, and 00027 refuses the row for anybody else, including for this
// caller, which runs as the schema owner and bypasses RLS. Asking first is not
// a duplicate of that: an exception from inside the delivery transaction would
// abort the whole notification and retry it forever, so the person with an
// unverified address would stop receiving mail entirely rather than receiving
// mail without a link.
//
// # ONE LINK PER PERSON, LIKE THE UNSUBSCRIBE TOKEN AND FOR A SHARPER REASON
//
// The unsubscribe token is per person so nobody can unsubscribe everybody else.
// This is per person because it carries their authority to approve: a shared
// link would let whichever recipient clicked first have the approval recorded
// against a colleague, in a customer's own compliance record.
//
// # THE URL NAMES THE FINDING AS WELL AS CARRYING THE TOKEN
//
// Both are needed to redeem it (00027). A token recovered on its own, from a
// mail relay's logs or a truncated URL, approves nothing.
func (s *Service) approveLink(
	ctx context.Context, tx pgx.Tx, bell postgres.Doorbell, r postgres.Recipient,
) (string, error) {
	if !r.EmailVerified {
		return "", nil
	}

	token, err := newCapabilityToken()
	if err != nil {
		return "", err
	}

	minted, err := s.outbox.MintApprovalDelegation(ctx, tx, bell.ID, r.UserID,
		postgres.HashDelegationToken(token), ApprovalLifetime)
	if err != nil {
		return "", err
	}
	if !minted {
		// Declined rather than failed: the row went away, or this person is no
		// longer a member. The doorbell still rings, without a link.
		return "", nil
	}

	return fmt.Sprintf("%s/approve/%s/%s", s.baseURL, bell.FindingID, token), nil
}

// localTime is `now` in somebody's zone, falling back to UTC.
//
// A zone that will not load falls back rather than failing the delivery. The
// preferences write already refuses an unknown zone, so reaching this means the
// tzdata on this host differs from the one that accepted it, and dropping a
// compliance notification over a timezone database mismatch is the wrong trade.
func (s *Service) localTime(zone string) time.Time {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return s.now().UTC()
	}
	return s.now().In(loc)
}

// newCapabilityToken mints a bearer capability, the same shape as an invitation
// token: 32 bytes from crypto/rand, base64url unpadded so it survives a URL
// path segment untouched.
func newCapabilityToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating a capability token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
