package interceptor

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
)

// DenyList reports whether a token id has been revoked ahead of its expiry.
type DenyList interface {
	IsDenied(ctx context.Context, tokenID string) (bool, error)
}

// JTI refuses tokens that have been revoked before they expired.
//
// This is the other half of the §1.4 trade. Verifying locally rather than
// introspecting per request means a revoked token stays valid until it
// expires; ten-minute access tokens bound that to ten minutes, and this
// deny-list closes the window where ten minutes is too long, which is exactly
// the case an offboarding or an account deletion is trying to close.
//
// Must run after Auth: it reads the `jti` off verified claims, never off an
// unverified token, or an attacker would choose their own token id.
func JTI(list DenyList) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			claims, ok := ClaimsFrom(ctx)
			if !ok {
				return nil, connect.NewError(connect.CodeInternal,
					errors.New("jti interceptor ran before authentication"))
			}

			if claims.TokenID == "" {
				// RFC 9068 requires `jti` on a JWT access token, and this
				// system needs it for a reason beyond conformance: a token
				// with no id can never be revoked before it expires, so
				// accepting one would silently opt that session out of the
				// deny-list entirely.
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("token carries no jti, so it could never be revoked (RFC 9068)"))
			}

			denied, err := list.IsDenied(ctx, claims.TokenID)
			if err != nil {
				// Fail closed, and this is the single most important line in
				// the file. If an unreachable deny-list let requests through,
				// losing Redis would silently un-revoke every token that had
				// been revoked, with no error and no log line on the request
				// that succeeded. That is the same trap §15.3 describes for
				// eviction, arriving by a different route: the availability of
				// the deny-list is itself the security control.
				return nil, connect.NewError(connect.CodeUnavailable,
					fmt.Errorf("cannot consult the revocation deny-list: %w", err))
			}
			if denied {
				return nil, connect.NewError(connect.CodeUnauthenticated,
					errors.New("token has been revoked"))
			}

			return next(ctx, req)
		}
	}
}
