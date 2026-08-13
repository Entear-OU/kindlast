package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// The failure modes a resource server has to tell apart. All four deny the
// request, so they are not a policy decision; they exist so a log line says
// which check bit, and so the test battery can assert it was the intended one
// rather than any refusal at all. A test that only asserts "denied" passes
// when the token is rejected for the wrong reason, which is how a broken
// audience check hides behind a working expiry check.
var (
	ErrTokenInvalid     = errors.New("oidc: token invalid")
	ErrTokenExpired     = errors.New("oidc: token expired")
	ErrAudienceMismatch = errors.New("oidc: token audience mismatch")
	ErrIssuerMismatch   = errors.New("oidc: token issuer mismatch")
)

// SigningAlgorithms is the allow-list, and it is the single most important
// line in this package.
//
// Both entries are asymmetric. That is what makes the two classic
// algorithm-confusion attacks impossible rather than merely unlikely:
//
//   - `alg: none`, a token with a valid-looking header and an empty signature.
//   - `alg: HS256` signed with the authorization server's *public* key as the
//     HMAC secret, which verifies if the library is allowed to pick the
//     algorithm from the token and the key material is symmetric-capable.
//
// The rule generalises: the verifier decides the algorithm, never the token.
var SigningAlgorithms = []string{jwt.SigningMethodRS256.Alg(), jwt.SigningMethodES256.Alg()}

// clockSkewLeeway tolerates the ordinary disagreement between two machines'
// clocks. Thirty seconds against a ten-minute access token (§1.2) is a 5%
// extension of its life, which is a better trade than intermittent rejections
// of freshly minted tokens on a stack whose containers drifted apart.
const clockSkewLeeway = 30 * time.Second

// Claims is the verified identity a handler is allowed to trust.
//
// Standard OIDC claims only. Nothing here knows what an organisation is: the
// active organisation travels in a request header and is resolved against the
// database, deliberately not carried in the token, so switching organisation
// needs no re-minting and there is exactly one source of truth for membership
// (§20.1).
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Scopes        []string
	// TokenID is the `jti`, and it is what the Redis deny-list keys on to
	// close the revocation window local verification opens (§1.4, §15.1).
	TokenID   string
	ExpiresAt time.Time
}

// HasScope reports whether the token carries a scope.
//
// Exact match, never a prefix or a wildcard. `records:read` must not satisfy a
// requirement for `records:ropa:write`, and a prefix comparison would let it.
func (c *Claims) HasScope(scope string) bool {
	for _, held := range c.Scopes {
		if held == scope {
			return true
		}
	}
	return false
}

// Verifier checks access tokens against one issuer and one audience.
type Verifier struct {
	keys     *KeySet
	issuer   string
	audience string
}

// NewVerifier binds a key set to the issuer and audience it is allowed to
// accept.
//
// The audience is not optional and there is no "accept any" mode, because
// §1.4 turns on it: `core-api` accepts only `aud: kindlast-core-api` and
// `intelligence` only `aud: kindlast-intelligence`. Without that, a token
// minted for one resource server replays against the other, which is the most
// common OAuth misconfiguration in a multi-service estate.
func NewVerifier(keys *KeySet, issuer, audience string) (*Verifier, error) {
	if keys == nil {
		return nil, errors.New("oidc: verifier needs a key set")
	}
	if issuer == "" {
		return nil, errors.New("oidc: verifier needs an issuer")
	}
	if audience == "" {
		return nil, errors.New("oidc: verifier needs an audience")
	}
	return &Verifier{keys: keys, issuer: issuer, audience: audience}, nil
}

// Verify checks a bearer token's signature, issuer, audience and expiry, and
// returns the claims it carries.
//
// Local and in-process, never introspection (§1.4).
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	var claims tokenClaims

	parser := jwt.NewParser(
		jwt.WithValidMethods(SigningAlgorithms),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(clockSkewLeeway),
	)

	_, err := parser.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		// Returning an asymmetric public key is the second line of defence
		// against the HS256 confusion above: even with the allow-list removed,
		// the HMAC verifier rejects an *rsa.PublicKey as the wrong key type.
		// Two independent mechanisms, because this one is worth belt and
		// braces.
		return v.keys.KeyFor(ctx, kid)
	})
	if err != nil {
		return nil, classify(err)
	}

	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: no subject claim", ErrTokenInvalid)
	}

	expires, err := claims.GetExpirationTime()
	if err != nil || expires == nil {
		return nil, fmt.Errorf("%w: no expiry claim", ErrTokenInvalid)
	}

	return &Claims{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: bool(claims.EmailVerified),
		Scopes:        claims.scopes(),
		TokenID:       claims.ID,
		ExpiresAt:     expires.Time,
	}, nil
}

// classify maps the jwt library's errors onto this package's, so nothing
// downstream imports the library to tell an expired token from a forged one.
func classify(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("%w: %v", ErrTokenExpired, err)
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return fmt.Errorf("%w: %v", ErrAudienceMismatch, err)
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return fmt.Errorf("%w: %v", ErrIssuerMismatch, err)
	default:
		return fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	}
}

// tokenClaims covers the standard set plus the two ways an authorization
// server may spell scopes.
type tokenClaims struct {
	jwt.RegisteredClaims

	// RFC 9068 names the claim `scope`, space delimited. Several servers emit
	// `scp` as an array instead. Both are read because §18.2 means this code
	// meets whichever IdP a self-hoster already runs.
	Scope string          `json:"scope,omitempty"`
	SCP   json.RawMessage `json:"scp,omitempty"`

	Email         string       `json:"email,omitempty"`
	EmailVerified flexibleBool `json:"email_verified,omitempty"`
}

func (c *tokenClaims) scopes() []string {
	if c.Scope != "" {
		return strings.Fields(c.Scope)
	}

	var list []string
	if err := json.Unmarshal(c.SCP, &list); err == nil {
		return list
	}
	var single string
	if err := json.Unmarshal(c.SCP, &single); err == nil {
		return strings.Fields(single)
	}
	return nil
}

// flexibleBool reads `email_verified` whether the IdP spells it as a JSON
// boolean or as the string "true".
//
// Not pedantry about the spec: the claim gates finding approval (§1.7), and
// the strict reading fails closed in the wrong direction. A server that sends
// "true" would have every one of its users refused, and the operator would see
// a claims parse error rather than anything pointing at the cause.
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = flexibleBool(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*b = flexibleBool(asString == "true")
		return nil
	}
	return fmt.Errorf("oidc: email_verified is neither a boolean nor a string")
}
