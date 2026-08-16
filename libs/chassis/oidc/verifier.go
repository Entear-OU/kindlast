package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	// Issuer is the authorization server that minted this token, already
	// verified to be the configured one. Carried because it is half of the
	// identity: the subject is only unique within an issuer, so anything
	// deriving a stable user id needs both (see libs/chassis/subject).
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	// Name is the OIDC `name` claim, used only as a human-readable label.
	// Never for identity, and never for authorization: it is self-asserted at
	// many providers and changes freely.
	Name   string
	Scopes []string

	// ClientID is the OAuth client the token was minted for, from `client_id`.
	//
	// It names the CLIENT, never the user, and that distinction is the whole of
	// its usefulness: it answers "what kind of caller is this" where Subject
	// answers "who".
	//
	// `client_id` and not `azp`, which is measured rather than chosen. On this
	// stack Zitadel emits `client_id` on both authorization-code and
	// client-credentials tokens and emits no `azp` at all, so a reader written
	// against azp would match nothing and look like it worked (ENT-221).
	//
	// Empty is normal: a provider may omit it. Anything deciding authority from
	// this must treat empty as "unknown client" and grant nothing, never as a
	// match against an unset configuration value.
	ClientID string

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
	keys        *KeySet
	issuer      string
	audience    string
	scopeClaims []string
}

// VerifierOption adjusts how a Verifier reads a token.
type VerifierOption func(*Verifier)

// WithScopeClaims names additional claims to read the caller's scopes from,
// on top of the standard `scope` and `scp`.
//
// This exists because of a fact measured against the bundled Zitadel rather
// than assumed: its access tokens carry neither `scope` nor `scp`. An
// authorization server is free to express granted authority in a claim of its
// own choosing, and several do. Zitadel asserts project roles under
// `urn:zitadel:iam:org:project:{projectID}:roles`; Keycloak uses
// `realm_access.roles`; Entra uses `roles`.
//
// Configurable rather than hard-coded, for the §18.2 reason: a self-hoster
// pointing at their own IdP must not need a code change, and this package must
// not grow a table of vendor quirks. The default stays RFC 9068, which is what
// a conformant server does.
//
// Values are read whether the claim is a space-delimited string, an array of
// strings, or an object whose keys are the grants, which is the shape Zitadel
// and Keycloak both produce.
func WithScopeClaims(names ...string) VerifierOption {
	return func(v *Verifier) { v.scopeClaims = append(v.scopeClaims, names...) }
}

// NewVerifier binds a key set to the issuer and audience it is allowed to
// accept.
//
// The audience is not optional and there is no "accept any" mode, because
// §1.4 turns on it: `core-api` accepts only `aud: kindlast-core-api` and
// `intelligence` only `aud: kindlast-intelligence`. Without that, a token
// minted for one resource server replays against the other, which is the most
// common OAuth misconfiguration in a multi-service estate.
func NewVerifier(keys *KeySet, issuer, audience string, opts ...VerifierOption) (*Verifier, error) {
	if keys == nil {
		return nil, errors.New("oidc: verifier needs a key set")
	}
	if issuer == "" {
		return nil, errors.New("oidc: verifier needs an issuer")
	}
	if audience == "" {
		return nil, errors.New("oidc: verifier needs an audience")
	}

	verifier := &Verifier{keys: keys, issuer: issuer, audience: audience}
	for _, opt := range opts {
		opt(verifier)
	}
	return verifier, nil
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
		Issuer:        claims.Issuer,
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: bool(claims.EmailVerified),
		Name:          claims.Name,
		ClientID:      claims.ClientID,
		Scopes:        withOpenID(claims.scopes(v.scopeClaims)),
		TokenID:       claims.ID,
		ExpiresAt:     expires.Time,
	}, nil
}

// ScopeOpenID is the one scope in the vocabulary that is not a permission.
const ScopeOpenID = "openid"

// withOpenID asserts `openid` on a token that has just verified.
//
// Every other scope answers "may this client touch this kind of resource".
// `openid` answers "did this caller arrive through an OIDC login", and a token
// that has passed signature, issuer, audience and expiry is exactly the proof
// of that. It is a conclusion drawn from verification, not a grant carried in
// the token, so this is where it belongs rather than in a special case inside
// the scope interceptor, which stays uniform: is the string present.
//
// Why it has to be asserted at all, measured rather than assumed: no
// authorization server issues a grant for `openid`, because it is a request
// flag rather than a permission. A real authorization-code token from the
// seeded Zitadel carries seven project roles and no `openid` (some servers,
// Keycloak among them, happen to echo requested scopes back, which is an
// implementation detail rather than a promise). Requiring it as a claim made
// GetCurrentUser unreachable by every valid token, and that is the endpoint
// where a new user's organisation is created, so a caller could never reach
// the call that would grant them anything.
//
// The rule this creates matters more than the code: **never declare `openid`
// on an endpoint that grants authority.** It means signed in, not permitted.
// The two RPCs that declare it today are both bootstrap calls, and
// AcceptInvitation's real authorization is possession of the invitation token.
func withOpenID(scopes []string) []string {
	for _, scope := range scopes {
		if scope == ScopeOpenID {
			return scopes
		}
	}
	return append(scopes, ScopeOpenID)
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

	// The OAuth client the token was minted for, per RFC 9068. Read for
	// client-class scope resolution; see Claims.ClientID for why it is
	// `client_id` and not `azp`.
	ClientID string `json:"client_id,omitempty"`

	Email         string       `json:"email,omitempty"`
	EmailVerified flexibleBool `json:"email_verified,omitempty"`
	Name          string       `json:"name,omitempty"`

	// raw keeps the whole payload so WithScopeClaims can reach a claim this
	// struct does not name.
	raw map[string]json.RawMessage
}

// UnmarshalJSON fills the named fields and keeps the raw payload alongside
// them.
//
// The local type strips the methods off tokenClaims, which is what stops this
// recursing into itself.
func (c *tokenClaims) UnmarshalJSON(data []byte) error {
	type plain tokenClaims

	var fields plain
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*c = tokenClaims(fields)

	return json.Unmarshal(data, &c.raw)
}

// scopes collects the caller's granted authority from the standard claims
// first, then from any deployment-configured ones.
//
// The union rather than the first non-empty answer: a deployment that asserts
// roles in a vendor claim may still emit `scope` for the OIDC basics, and
// dropping either half would deny requests that should pass.
func (c *tokenClaims) scopes(extraClaims []string) []string {
	found := map[string]bool{}
	var ordered []string

	add := func(values []string) {
		for _, value := range values {
			if value == "" || found[value] {
				continue
			}
			found[value] = true
			ordered = append(ordered, value)
		}
	}

	add(strings.Fields(c.Scope))
	add(valuesOf(c.SCP))
	for _, name := range extraClaims {
		add(valuesOf(c.raw[name]))
	}

	return ordered
}

// valuesOf reads a claim that may be a space-delimited string, an array of
// strings, or an object whose keys are the grants.
//
// The object case is not hypothetical: it is the shape Zitadel emits for
// project roles, where each key maps to the organisations the role was granted
// in.
func valuesOf(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return strings.Fields(single)
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		// Sorted so the resulting scope list is stable across runs, which
		// matters only for readable errors and predictable tests.
		sort.Strings(keys)
		return keys
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
