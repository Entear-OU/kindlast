// Package org serves OrgService.
//
// Only AcceptInvitation, because the rest of the organisation surface is
// build-order step 2. Accept lives here now because of the ordering constraint
// in §1.8: it has to run before the invited user's first GetCurrentUser, or
// provisioning gives them a personal organisation alongside the one they were
// invited to.
package org

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	// Aliased because this package is itself called org: the service and the
	// domain rules it serves share a name, and only one of them can be `org`
	// inside this file.
	"log/slog"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// accepting is what this handler needs of the request's transaction, declared
// where it is used rather than exported from the store (§21.6).
type accepting interface {
	AcceptInvitation(ctx context.Context, token, email string) (domain.Joined, error)
}

// Profiles resolves the address an access token leaves out.
//
// The same interface `session` declares, for the same §21.6 reason and
// satisfied by the same adapter. Declared again rather than imported because
// this package says what it needs.
type Profiles interface {
	Profile(ctx context.Context, accessToken, subject string) (*oidc.Profile, error)
}

// Service implements corev1connect.OrgServiceHandler.
type Service struct {
	// appBaseURL is where a browser reaches the console, used to build the
	// invitation link at mint (ENT-219). Empty means this deployment cannot
	// send invitations, and InviteMember refuses rather than creating one that
	// can never be accepted.
	appBaseURL string

	// profiles answers "what is this caller's address", which 00033 made a
	// question this handler has to be able to answer.
	//
	// Nil is allowed and means the token's own claims are all there is. On a
	// provider that puts an address in an access token that is enough; on the
	// bundled Zitadel it is not, and acceptance then refuses rather than
	// guessing, which is the safe direction but a broken deployment. main.go
	// passes the same resolver it gives the session service.
	profiles Profiles
}

func New(appBaseURL string, profiles Profiles) *Service {
	return &Service{appBaseURL: appBaseURL, profiles: profiles}
}

// AcceptInvitation joins the organisation an invitation names.
func (s *Service) AcceptInvitation(
	ctx context.Context,
	req *connect.Request[corev1.AcceptInvitationRequest],
) (*connect.Response[corev1.AcceptInvitationResponse], error) {
	claims, ok := interceptor.ClaimsFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	// An invitation names a person, and this is where the caller is held to
	// being that person (00033).
	//
	// Never from the request body: that is caller-controlled and would hand the
	// whole check back to whoever is being checked. Either source below is
	// attested by the authorization server.
	//
	// An unverified address is refused, and the distinction is the whole
	// control. Matching on an address the provider has not confirmed would let
	// somebody register as the invited address, skip the confirmation mail,
	// and walk in: exactly the escalation this closes, one step longer.
	//
	// NotFound rather than something more specific, matching the store's one
	// answer below. A caller learning "your email is not verified" from an
	// invitation endpoint has learned that the token was real, which is the
	// oracle 00003 built this path to avoid.
	email, verified := s.callerAddress(ctx, claims)
	if email == "" || !verified {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("this invitation cannot be used"))
	}

	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	store, ok := tenant.(accepting)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot accept invitations"))
	}

	joined, err := store.AcceptInvitation(ctx, req.Msg.GetToken(), email)
	if errors.Is(err, postgres.ErrInvitationNotUsable) {
		// One answer for expired, already accepted, never existed and
		// addressed to somebody else. The caller who legitimately holds a
		// token needs no more than this, and anyone guessing tokens learns
		// nothing from it, including who a token was for.
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("this invitation cannot be used"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.AcceptInvitationResponse{
		OrgId:   joined.OrgID,
		OrgName: joined.OrgName,
		OrgSlug: joined.OrgSlug,
		Role:    joined.Role,
	}), nil
}

// callerAddress answers "which person is this", verified.
//
// # WHY THIS IS NOT JUST `claims.Email`
//
// It was, for exactly one release, and it broke every invitation on the
// deployment this product ships.
//
// `session.withProfile` says the reason in a comment that had been sitting
// there the whole time: "The bundled Zitadel puts no email in an access token
// at all, so for that deployment this is the only place the verification fact
// is ever learned." An access token minted by the compose stack carries `sub`
// and scopes and no address, however faithfully `web` asks for the `email`
// scope, because the address lives in the ID token and at userinfo rather than
// in the access token core-api verifies.
//
// So reading `claims.Email` and refusing when it was empty refused everybody,
// and not visibly. The invitation was simply not redeemed, and the caller's
// next request was their first GetCurrentUser, which found a subject with no
// membership and provisioned them a personal organisation. The invited person
// ended up owning an organisation nobody invited them to, alone, with the real
// invitation still sitting unused. That is precisely the failure §1.8
// describes and that the ordering in this package exists to prevent, arrived
// at from the other direction.
//
// # WHY THE FALLBACK IS SAFE
//
// The usual objection to userinfo is a network call in a request path. This is
// the same call `session` already makes, on the same deployment, for the same
// reason, against the authorization server this service already trusts, made
// AS THE CALLER with the caller's own token, so it can only ever describe the
// caller. A provider that does put the address in the token skips it entirely.
//
// A failure returns an empty, unverified address, so acceptance refuses. That
// is the safe direction: a provider that cannot be asked who somebody is must
// not be taken to have said they are the invitee.
func (s *Service) callerAddress(ctx context.Context, claims *oidc.Claims) (string, bool) {
	if claims.Email != "" {
		return claims.Email, claims.EmailVerified
	}
	if s.profiles == nil {
		return "", false
	}

	token, ok := interceptor.TokenFrom(ctx)
	if !ok {
		return "", false
	}

	profile, err := s.profiles.Profile(ctx, token, claims.Subject)
	if err != nil {
		// The subject is not in the message: this line reaches a log and the
		// subject identifies a person.
		slog.WarnContext(ctx, "could not resolve the caller's address to check an invitation",
			slog.String("reason", err.Error()))
		return "", false
	}
	return profile.Email, profile.EmailVerified
}
