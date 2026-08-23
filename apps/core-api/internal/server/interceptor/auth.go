package interceptor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
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
// AuthOption adjusts which credentials this stage will accept.
//
// An option rather than a second parameter, matching NewScope's shape, and the
// default is the one that fails closed: a deployment that says nothing about API
// keys accepts none, and a request under the `ApiKey` scheme is refused rather
// than falling through to the bearer path. Opening a credential model has to be
// something somebody wrote down.
type AuthOption func(*authConfig)

type authConfig struct{ keys Authenticator }

// WithAPIKeys accepts a partner's key as well as a bearer token (ENT-262).
//
// The two paths do not share a line of verification (§1.7). See the dispatch in
// the interceptor below, and APIKeyScheme for why the scheme is named rather
// than inferred.
func WithAPIKeys(keys Authenticator) AuthOption {
	return func(c *authConfig) { c.keys = keys }
}

func Auth(verifier TokenVerifier, options ...AuthOption) connect.UnaryInterceptorFunc {
	config := authConfig{}
	for _, apply := range options {
		apply(&config)
	}
	keys := config.keys

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			header := req.Header().Get(AuthorizationHeader)

			// THE DISPATCH, AND IT IS A DISPATCH RATHER THAN A FALLBACK.
			//
			// A credential presented under the `ApiKey` scheme is authenticated
			// as a key or refused. It is never retried as a bearer token, and a
			// bearer token that fails verification is never retried as a key.
			// §1.7's rule is that the four credential schemes must not share a
			// verification path, and two paths that fall through to each other
			// are one path wearing a disguise: the fallback is precisely where a
			// value gets a second chance at a verifier that was not meant for
			// it.
			if credential, presented := apiKeyCredential(header); presented {
				return authenticateAPIKey(ctx, next, req, keys, credential)
			}

			raw, err := bearerToken(header)
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
		return "", errors.New("the Authorization header is not a Bearer credential")
	}

	credential = strings.TrimSpace(credential)
	if credential == "" {
		return "", errors.New("the Bearer credential is empty")
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

// authenticateAPIKey resolves a partner's key and attaches it to the request.
//
// A separate function from the token path rather than a branch inside it, so
// the two never share a line. What it produces is an apikey.Principal and
// deliberately NOT a set of oidc.Claims: claims mean "the JWKS verified this",
// and synthesising them for a credential no signature was ever checked on would
// make ClaimsFrom lie to every reader downstream. The stages after this ask
// APIKeyFrom instead, and each says what it does about the answer.
//
// A deployment that wired no authenticator refuses rather than ignoring the
// scheme. Ignoring it would fall through to the bearer path, which would then
// report a missing token, and a caller who presented a perfectly good key would
// be told they presented none.
func authenticateAPIKey(
	ctx context.Context,
	next connect.UnaryFunc,
	req connect.AnyRequest,
	keys Authenticator,
	credential string,
) (connect.AnyResponse, error) {
	if keys == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, ErrNoUsableAPIKey)
	}

	principal, err := keys.AuthenticateAPIKey(ctx, credential)
	if err != nil {
		if errors.Is(err, apikey.ErrMalformed) {
			// One answer for malformed, unknown and revoked. See
			// ErrNoUsableAPIKey for why they are not distinguished.
			return nil, connect.NewError(connect.CodeUnauthenticated, ErrNoUsableAPIKey)
		}
		// A database that will not answer is Unavailable rather than
		// Unauthenticated, and the distinction matters: telling a caller their
		// key is bad when the truth is that Postgres is down sends them to
		// rotate a credential that was never the problem. It also fails closed
		// either way, which is the part that must not change.
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("cannot authenticate the API key: %w", err))
	}

	// The credential does NOT travel onwards. WithToken exists so a handler can
	// present the caller's own token upstream to the authorization server, and
	// there is no upstream that would accept one of these. Carrying a live
	// secret further into the process than it has to go is a cost with nothing
	// on the other side of it.
	return next(WithAPIKey(ctx, principal), req)
}
