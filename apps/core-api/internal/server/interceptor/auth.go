package interceptor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// AuthorizationHeader carries the bearer token, per RFC 6750.
const AuthorizationHeader = "Authorization"

// TokenVerifier is the narrow view of the chassis verifier this interceptor
// needs.
//
// An interface rather than the concrete type, but note what it deliberately
// does NOT enable: §13.2 forbids substituting a mock here in tests, because
// the verifier is the code most worth testing and stubbing it means every
// scope and tenancy test is really testing the stub. The interface exists so
// the interceptor does not import an HTTP client, not so the tests can skip
// the cryptography. Every test in this package mints real tokens against a
// real JWKS.
type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (*oidc.Claims, error)
}

// Auth verifies the bearer token and attaches the resulting claims.
//
// Verification is local, against a cached JWKS, and never an introspection
// call to the authorization server (§1.4). That is what makes `auth` survivable
// as a dependency: when it goes down, existing sessions keep working until
// their tokens expire, because nothing in this path talks to it per request.
func Auth(verifier TokenVerifier) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			raw, err := bearerToken(req.Header().Get(AuthorizationHeader))
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}

			claims, err := verifier.Verify(ctx, raw)
			if err != nil {
				// The specific reason goes in the error for the log, and every
				// reason maps to the same code. Telling a caller whether their
				// token was expired, forged or minted for another audience is
				// a distinction only an attacker probing the boundary has a
				// use for.
				return nil, connect.NewError(connect.CodeUnauthenticated, redactVerificationError(err))
			}

			// The token travels alongside the claims, not instead of them.
			// Everything downstream that decides anything reads the claims;
			// see WithToken for the one thing this is for.
			return next(WithToken(WithClaims(ctx, claims), raw), req)
		}
	}
}

// bearerToken pulls the credential out of the header.
//
// The scheme comparison is case-insensitive because RFC 7235 says the scheme
// is, and a client sending "bearer" rather than "Bearer" is conformant even
// though it is unusual. Nothing else about the header is tolerated: no
// fallback to a query parameter, no cookie, no second scheme. Every additional
// place a credential may arrive from is another verifier someone eventually
// writes badly (§1.7 lists the four credential schemes that must never share a
// verification path).
func bearerToken(header string) (string, error) {
	if header == "" {
		return "", errors.New("no Authorization header")
	}

	scheme, credential, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", errors.New("Authorization header is not a Bearer credential")
	}

	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", errors.New("Bearer credential is empty")
	}
	return credential, nil
}

// redactVerificationError keeps the failure class, which is useful in a log,
// and drops the detail, which is useful to someone forging tokens.
func redactVerificationError(err error) error {
	switch {
	case errors.Is(err, oidc.ErrTokenExpired):
		return errors.New("token expired")
	case errors.Is(err, oidc.ErrAudienceMismatch):
		return fmt.Errorf("token audience is not this resource server")
	case errors.Is(err, oidc.ErrIssuerMismatch):
		return errors.New("token issuer is not trusted")
	case errors.Is(err, oidc.ErrKeyNotFound):
		return errors.New("token signing key is unknown")
	default:
		return errors.New("token invalid")
	}
}
