// Package session serves SessionService.
//
// The handler is deliberately unimplemented in ENT-195, and that is worth
// stating plainly rather than leaving as a surprise. This issue ships the
// interceptor chain; `/api/v1/me` and just-in-time provisioning are ENT-196
// and consume it.
//
// It is registered anyway, because a chain with nothing behind it cannot be
// shown to work. With this handler in place, a request that reaches
// `unimplemented` has proved that authentication, revocation, scope and
// tenancy all passed, and a request refused earlier names which of them
// refused it. That is the difference between an interceptor chain that is
// tested and one that is merely written.
package session

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
)

// Service implements corev1connect.SessionServiceHandler.
type Service struct{}

func New() *Service { return &Service{} }

// GetCurrentUser returns the caller, their memberships and the active
// organisation. Not yet: ENT-196 implements it, including the provisioning
// transaction and the race that makes it interesting.
func (s *Service) GetCurrentUser(
	ctx context.Context,
	_ *connect.Request[corev1.GetCurrentUserRequest],
) (*connect.Response[corev1.GetCurrentUserResponse], error) {
	// The assertions below are not busywork. They fail loudly if the chain is
	// ever rewired so a handler runs without a verified identity or without a
	// transaction carrying the tenancy GUCs, which are the two things every
	// handler after this one will assume without checking.
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	if _, ok := interceptor.TenantFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}

	return nil, connect.NewError(connect.CodeUnimplemented,
		errors.New("GetCurrentUser arrives with ENT-196; the interceptor chain in front of it is ENT-195"))
}
