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
	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// accepting is what this handler needs of the request's transaction, declared
// where it is used rather than exported from the store (§21.6).
type accepting interface {
	AcceptInvitation(ctx context.Context, token string) (domain.Joined, error)
}

// Service implements corev1connect.OrgServiceHandler.
type Service struct{}

func New() *Service { return &Service{} }

// AcceptInvitation joins the organisation an invitation names.
func (s *Service) AcceptInvitation(
	ctx context.Context,
	req *connect.Request[corev1.AcceptInvitationRequest],
) (*connect.Response[corev1.AcceptInvitationResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
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

	joined, err := store.AcceptInvitation(ctx, req.Msg.GetToken())
	if errors.Is(err, postgres.ErrInvitationNotUsable) {
		// One answer for expired, already accepted and never existed. The
		// caller who legitimately holds a token needs no more than this, and
		// anyone guessing tokens learns nothing from it.
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
