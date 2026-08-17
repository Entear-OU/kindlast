// Package notifications serves NotificationService.
//
// Preferences only. There is no RPC to send a notification and none to read the
// outbox, deliberately: a caller that could trigger delivery could use this
// service to mail an arbitrary address on a timer, and a queue of pending
// messages is operational detail rather than a customer surface (ENT-209).
package notifications

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
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
}

// Service implements corev1connect.NotificationServiceHandler.
type Service struct {
	// smtpConfigured decides whether the email channel is reported as
	// available. Read from configuration rather than probed, because a mail
	// server that happens to be down is not the same as a deployment that has
	// not been told where to submit mail, and only the second is worth telling a
	// person about on a settings page.
	smtpConfigured bool
}

func New(smtpConfigured bool) *Service {
	return &Service{smtpConfigured: smtpConfigured}
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
		Id:          "email",
		DisplayName: "Email",
		Available:   s.smtpConfigured,
	}
	if !s.smtpConfigured {
		email.UnavailableReason = "This deployment has no mail server configured, " +
			"so notifications are queued rather than delivered."
	}

	// Only the channels that exist. Telegram, Slack and WhatsApp are in the
	// design and are not built, and listing them as unavailable would be a
	// roadmap rendered as a settings page: a person cannot act on it, and it
	// makes the product look broken rather than unfinished (§18.3).
	return connect.NewResponse(&corev1.GetNotificationCapabilitiesResponse{
		Channels: []*corev1.NotificationChannel{email},
	}), nil
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
	}
}
