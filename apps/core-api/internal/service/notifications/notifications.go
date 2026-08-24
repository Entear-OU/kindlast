// Package notifications serves NotificationService.
//
// Preferences and the caller's own linked channels. There is still no RPC to
// send a notification and none to read the outbox, deliberately: a caller that
// could trigger delivery could use this service to mail an arbitrary address on
// a timer, and a queue of pending messages is operational detail rather than a
// customer surface (ENT-209).
//
// # WHAT LINKING A CHANNEL IS ALLOWED TO REACH, AND WHAT IT IS NOT (ENT-263)
//
// The bot token is not in this package, in any message this package returns, or
// in any table these handlers read. It is an operator secret of the same class
// as the SMTP password: read from core-api's configuration by the dispatcher,
// held in `internal/delivery`, and never on a path a browser can call. What
// this package knows about it is one boolean, through the router, which is
// whether the deployment has a Telegram channel at all.
//
// The verification code is not returned either. LinkTelegramChat mints one,
// hands the hash to the store and the plaintext to the outbox, and lets neither
// back out through a response: a code that came back in the RPC that created it
// would prove nothing about who holds the chat, which is the only thing it
// exists to prove.
package notifications

import (
	"context"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// preferring is what these handlers need of the request's transaction,
// declared where it is used rather than exported from the store (§21.6).
//
// Note what is absent: no user id parameter anywhere. The caller is always the
// subject, and the row is found by the policy's own `user_id =
// app.current_user_id`. Offering a parameter would invite a client to pass
// somebody else's and make this handler the thing that refuses, when the
// policy already does.
type preferring interface {
	Preferences(ctx context.Context) (notify.Preferences, error)
	SavePreferences(ctx context.Context, p notify.Preferences) error

	// The linked-channel half (ENT-263). Note that none of these takes a user
	// either, for a sharper reason: a linked chat is somebody's messaging
	// identity, and a method that could name a colleague would be the shape of
	// an endpoint for enumerating which co-workers are reachable and where.
	LinkedChannels(ctx context.Context) ([]postgres.LinkedChannel, error)
	LinkTelegramChat(ctx context.Context, chatID, codeHash string, expiresAt time.Time) error
	VerifyTelegramChat(ctx context.Context, code string) error
	UnlinkTelegramChat(ctx context.Context) (bool, error)

	// The verification code rides the same outbox every other message rides,
	// written inside the request's own transaction alongside the pending code
	// it carries. That is the whole of ENT-263's constraint in one line: a
	// handler that called Telegram directly here would have its own retry, its
	// own failure log, and a window in which the row and the message disagree.
	EnqueueMessage(ctx context.Context, msg notify.Message) error
	OrganisationName(ctx context.Context) (string, error)
}

// Service implements corev1connect.NotificationServiceHandler.
type Service struct {
	// channels is what this deployment can actually deliver on, read from the
	// same router the dispatcher sends through (ENT-263).
	//
	// The router rather than a pair of booleans, so the settings page and the
	// dispatch path cannot disagree about whether a channel exists. Read from
	// configuration rather than probed: a mail server that happens to be down
	// is not the same as a deployment that has never been told where to submit
	// mail, and only the second is worth telling a person about.
	channels *delivery.Router
}

func New(channels *delivery.Router) *Service {
	return &Service{channels: channels}
}

func tenantFor(ctx context.Context) (preferring, error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	store, ok := tenant.(preferring)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot read notification preferences"))
	}
	return store, nil
}

func (s *Service) GetNotificationPreferences(
	ctx context.Context,
	_ *connect.Request[corev1.GetNotificationPreferencesRequest],
) (*connect.Response[corev1.GetNotificationPreferencesResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	prefs, err := store.Preferences(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetNotificationPreferencesResponse{
		Preferences: toProto(prefs),
	}), nil
}

func (s *Service) UpdateNotificationPreferences(
	ctx context.Context,
	req *connect.Request[corev1.UpdateNotificationPreferencesRequest],
) (*connect.Response[corev1.UpdateNotificationPreferencesResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	// Normalised and validated before it reaches the database, so a bad
	// timezone is `invalid_argument` naming the field rather than a constraint
	// violation naming a column, and so an unloadable zone is caught now rather
	// than hours later as a notification that did not arrive.
	prefs, err := fromProto(req.Msg.GetPreferences()).Normalise()
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	if err := store.SavePreferences(ctx, prefs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Re-read rather than echoed, so the response describes what the database
	// holds rather than what was sent.
	saved, err := store.Preferences(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.UpdateNotificationPreferencesResponse{
		Preferences: toProto(saved),
	}), nil
}

func (s *Service) GetNotificationCapabilities(
	ctx context.Context,
	_ *connect.Request[corev1.GetNotificationCapabilitiesRequest],
) (*connect.Response[corev1.GetNotificationCapabilitiesResponse], error) {
	// Tenancy is not consulted: what a deployment can deliver on is the same
	// answer for every organisation in it. The scope check has already run, so
	// this is not an unauthenticated endpoint.
	if _, err := tenantFor(ctx); err != nil {
		return nil, err
	}

	email := &corev1.NotificationChannel{
		Id:          notify.ChannelEmail,
		DisplayName: "Email",
		Available:   s.channels.Has(notify.ChannelEmail),
	}
	if !email.GetAvailable() {
		email.UnavailableReason = "This deployment has no mail server configured, " +
			"so notifications are queued rather than delivered."
	}

	// Telegram is reported unavailable rather than omitted on a deployment that
	// has set no bot token (ENT-263), which is the difference §18.3 asks for: a
	// console can say why the switch is not offered instead of silently lacking
	// it, and an operator reading the settings page learns that the missing
	// piece is theirs to configure.
	//
	// Slack and WhatsApp are still absent entirely, because they are a design
	// and not a build. Listing those would be a roadmap rendered as a settings
	// page: a person cannot act on it, and it makes the product look broken
	// rather than unfinished.
	telegram := &corev1.NotificationChannel{
		Id:          notify.ChannelTelegram,
		DisplayName: "Telegram",
		Available:   s.channels.Has(notify.ChannelTelegram),
	}
	if !telegram.GetAvailable() {
		telegram.UnavailableReason = "This deployment has no Telegram bot configured, " +
			"so a chat cannot be linked."
	}

	return connect.NewResponse(&corev1.GetNotificationCapabilitiesResponse{
		Channels: []*corev1.NotificationChannel{email, telegram},
	}), nil
}

// ListLinkedChannels returns the caller's own channels, verified or pending.
func (s *Service) ListLinkedChannels(
	ctx context.Context,
	_ *connect.Request[corev1.ListLinkedChannelsRequest],
) (*connect.Response[corev1.ListLinkedChannelsResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	linked, err := store.LinkedChannels(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res := &corev1.ListLinkedChannelsResponse{}
	for _, c := range linked {
		channel := &corev1.LinkedChannel{
			Kind:     c.Kind,
			ChatId:   c.ChatID,
			Verified: c.Verified,
			LinkedAt: timestamppb.New(c.CreatedAt),
		}
		if !c.PendingUntil.IsZero() {
			channel.CodeExpiresAt = timestamppb.New(c.PendingUntil)
		}
		res.Channels = append(res.Channels, channel)
	}
	return connect.NewResponse(res), nil
}

// LinkTelegramChat records a claim and queues the code that will prove it.
//
// # WHY THE CODE IS QUEUED RATHER THAN SENT
//
// Because sending it here would be the second dispatch path this issue exists
// to avoid. `EnqueueMessage` writes it into `transactional_outbox` inside the
// same transaction that stores the hash, so the row carrying the code and the
// row expecting it commit together or neither does, and the existing relay
// delivers it with the existing retry policy and the existing record of what
// was attempted. The cost is a second or two of latency; the alternative is a
// second answer to "did this message go out".
func (s *Service) LinkTelegramChat(
	ctx context.Context,
	req *connect.Request[corev1.LinkTelegramChatRequest],
) (*connect.Response[corev1.LinkTelegramChatResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	// Refused before anything is written, so a deployment with no bot token
	// cannot accumulate claims for codes that will never be delivered. This is
	// also the half of the airgap property that lives in Go: with no token
	// there is no Telegram channel on the router, so nothing here ever
	// constructs a message addressed to one.
	if !s.channels.Has(notify.ChannelTelegram) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this deployment has no Telegram bot configured "+
				"(KINDLAST_TELEGRAM_BOT_TOKEN is not set), so a chat cannot be linked"))
	}

	chatID := strings.TrimSpace(req.Msg.GetChatId())
	if !notify.ValidChatID(chatID) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("chat_id is the numeric id of a Telegram chat, digits with an "+
				"optional leading minus"))
	}

	code, err := notify.NewVerificationCode()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	expiresAt := time.Now().Add(notify.VerificationCodeLifetime)

	if err := store.LinkTelegramChat(ctx, chatID, notify.HashVerificationCode(code), expiresAt); err != nil {
		if errors.Is(err, postgres.ErrChatAlreadyLinked) {
			// Named plainly rather than obscured. The chat id is one the
			// caller just supplied, so this tells them nothing they did not
			// already have, and the alternative is somebody retyping a correct
			// id repeatedly against a silent failure.
			return nil, connect.NewError(connect.CodeAlreadyExists,
				errors.New("that chat is already linked by another member of this organisation"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	orgName, err := store.OrganisationName(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if err := store.EnqueueMessage(ctx, notify.TelegramVerification(code, orgName, chatID)); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.LinkTelegramChatResponse{
		CodeExpiresAt: timestamppb.New(expiresAt),
	}), nil
}

// VerifyTelegramChat spends one attempt against the pending code.
//
// Wrong, expired, already spent and never issued are one answer, for the reason
// the proto comment gives. Running out of attempts is the exception, because
// "ask for a new code" is the right advice for the first four and useless for
// the fifth: starting again with the same spent row fails the same way.
func (s *Service) VerifyTelegramChat(
	ctx context.Context,
	req *connect.Request[corev1.VerifyTelegramChatRequest],
) (*connect.Response[corev1.VerifyTelegramChatResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	code := strings.TrimSpace(req.Msg.GetCode())
	if code == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("verifying names the code that arrived in the chat; send code"))
	}

	switch err := store.VerifyTelegramChat(ctx, code); {
	case errors.Is(err, postgres.ErrTooManyVerificationAttempts):
		return nil, connect.NewError(connect.CodeResourceExhausted,
			errors.New("too many attempts against that code; link the chat again to get a new one"))
	case errors.Is(err, postgres.ErrNoPendingChannel):
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("that code is not usable; link the chat again to get a new one"))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.VerifyTelegramChatResponse{Verified: true}), nil
}

// UnlinkTelegramChat removes the caller's chat.
func (s *Service) UnlinkTelegramChat(
	ctx context.Context,
	_ *connect.Request[corev1.UnlinkTelegramChatRequest],
) (*connect.Response[corev1.UnlinkTelegramChatResponse], error) {
	store, err := tenantFor(ctx)
	if err != nil {
		return nil, err
	}

	// Not gated on the channel being configured, unlike linking. Somebody whose
	// operator has just removed the bot token still has a row, and refusing to
	// let them delete it would leave them unable to undo a link they made.
	unlinked, err := store.UnlinkTelegramChat(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&corev1.UnlinkTelegramChatResponse{Unlinked: unlinked}), nil
}

func toProto(p notify.Preferences) *corev1.NotificationPreferences {
	return &corev1.NotificationPreferences{
		Email:                 p.Email,
		MinSeverityForEmail:   p.MinSeverityForEmail,
		WeeklyBriefingEnabled: p.WeeklyBriefingEnabled,
		DeadlineAlertsEnabled: p.DeadlineAlertsEnabled,
		Timezone:              p.Timezone,
		QuietHoursStart:       p.QuietHoursStart,
		QuietHoursEnd:         p.QuietHoursEnd,
		FindingChannel:        p.FindingChannel,
	}
}

func fromProto(p *corev1.NotificationPreferences) notify.Preferences {
	if p == nil {
		// An absent message is not the same as an empty one, and this is the
		// safer reading: a client that sent nothing gets the defaults rather
		// than every switch silently turned off by proto3's zero values.
		return notify.Defaults()
	}
	return notify.Preferences{
		Email:                 p.GetEmail(),
		MinSeverityForEmail:   p.GetMinSeverityForEmail(),
		WeeklyBriefingEnabled: p.GetWeeklyBriefingEnabled(),
		DeadlineAlertsEnabled: p.GetDeadlineAlertsEnabled(),
		Timezone:              p.GetTimezone(),
		QuietHoursStart:       p.GetQuietHoursStart(),
		QuietHoursEnd:         p.GetQuietHoursEnd(),
		FindingChannel:        p.GetFindingChannel(),
	}
}
