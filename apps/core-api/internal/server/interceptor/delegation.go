package interceptor

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
)

// DelegationHeader carries the credential an agent presents to act for a
// person (ENT-230, §26.3).
//
// A header rather than a message field, and the reason is the same one that
// keeps the organisation in a header: it applies to the request rather than to
// what the request is about, and putting it in the body would mean every RPC an
// agent may call growing a field, with the ones that forgot silently acting as
// the machine instead.
//
// No `X-` prefix, per RFC 6648.
const DelegationHeader = "Kindlast-Delegation"

// ActOnBehalfScope is what a caller must hold to present a delegation at all.
//
// Issued to machine principals through client credentials and never to the
// browser client, exactly like the other `internal:*` scopes. Note that it is
// checked against the token's GRANTED scopes below rather than through
// Scope.holds: the human client's scopes are replaced by a constant that
// deliberately contains no `internal:*` entry, so checking it the other way
// would be checking a set this scope can never be in. Reading the claim
// directly keeps "a browser token can never present a delegation" a property of
// what the authorization server granted rather than of two files agreeing.
const ActOnBehalfScope = "internal:act-on-behalf"

// ErrNoUsableDelegation is the single answer for every delegation that will not
// resolve. See delegation.ErrUnusable for why there is only one.
var ErrNoUsableDelegation = errors.New("interceptor: no usable delegation")

// DelegationResolver turns a presented credential into the person it names.
//
// Declared here because this is where it is used (§21.6), and satisfied by the
// Postgres store without that package importing this one. Like TenantOpener, it
// must not be mocked in the tests of this package: a stubbed resolver would
// assert that the interceptor calls something, which is not the property worth
// having.
type DelegationResolver interface {
	ResolveDelegation(ctx context.Context, token string) (delegation.Grant, error)
}

// ActOnBehalf resolves a presented delegation into the person it names.
//
// # WHAT THIS STAGE IS FOR
//
// It runs after JTI and before Scope, and both halves of that placement matter.
// After JTI, because a revoked machine token must not be able to spend a
// database round trip resolving anything. Before Scope, because what the caller
// may do changes completely once a delegation resolves: the machine's own
// scopes stop applying and the person's take over (see Scope.holds).
//
// # NO HEADER, NO CHANGE
//
// A request without the header passes through untouched, which is every request
// the system serves today. This stage adds a way for a machine to become a
// person for the length of one call; it must not alter what happens when nobody
// asks for that.
//
// # AND A DELEGATION IS NOT A SECOND WAY TO AUTHENTICATE
//
// The caller still needs a valid token, still needs it to be unrevoked, and
// still needs `internal:act-on-behalf` on it. The delegation says WHO the call
// is for; the token says the caller is entitled to ask that question at all.
// Dropping either half would turn a leaked delegation into a session.
func ActOnBehalf(resolver DelegationResolver) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			presented := req.Header().Get(DelegationHeader)
			if presented == "" {
				return next(ctx, req)
			}

			// A PARTNER'S KEY MAY NEVER PRESENT A DELEGATION (ENT-262).
			//
			// Refused explicitly rather than left to fail on the scope check
			// below, because the two failures mean different things and only
			// one of them is a sentence worth reading. A key holds no
			// `internal:*` scope by construction (00043 refuses the row), so it
			// could never pass that check, but a caller told "does not carry
			// the internal:act-on-behalf scope" would reasonably go and try to
			// have it granted. The truth is that no key ever can.
			//
			// The rule underneath: a delegation is how a MACHINE PRINCIPAL
			// becomes a person for one call, and it is safe because the machine
			// holds a platform credential this system issued to it. A key is
			// already acting as a person, its minter, and letting it present a
			// delegation would let a partner's credential become somebody else.
			if _, isKey := APIKeyFrom(ctx); isKey {
				return nil, connect.NewError(connect.CodePermissionDenied,
					errors.New("an API key cannot act on behalf of a person"))
			}

			claims, ok := ClaimsFrom(ctx)
			if !ok {
				return nil, connect.NewError(connect.CodeInternal,
					errors.New("act-on-behalf interceptor ran before authentication"))
			}

			// Checked before the credential is resolved, so a caller with no
			// business presenting one cannot use this endpoint to find out
			// whether a delegation is live.
			if !claims.HasScope(ActOnBehalfScope) {
				return nil, connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("token does not carry the %q scope", ActOnBehalfScope))
			}

			if resolver == nil {
				// A deployment that wired no resolver refuses rather than
				// ignoring the header. Ignoring it would run the call as the
				// machine principal, which is the one outcome nobody asked for:
				// the caller intended to act as a person and would instead act
				// with a platform credential.
				return nil, connect.NewError(connect.CodePermissionDenied, ErrNoUsableDelegation)
			}

			grant, err := resolver.ResolveDelegation(ctx, presented)
			if err != nil {
				if errors.Is(err, delegation.ErrUnusable) {
					return nil, connect.NewError(connect.CodePermissionDenied, ErrNoUsableDelegation)
				}
				return nil, connect.NewError(connect.CodeInternal,
					fmt.Errorf("resolving the delegation: %w", err))
			}

			return next(WithGrant(ctx, grant), req)
		}
	}
}

// WithGrant attaches a resolved delegation to the request.
//
// Exported for tests that exercise a later stage in isolation, the same reason
// WithClaims is. Nothing in production calls it except ActOnBehalf.
func WithGrant(ctx context.Context, grant delegation.Grant) context.Context {
	return context.WithValue(ctx, grantKey, grant)
}

// GrantFrom returns the delegation this request is acting under, if any.
//
// The boolean means "an agent is acting for a person", and false means "the
// caller is acting as themselves", which is the ordinary case. A handler that
// treats the two the same is usually right: that is the design working, because
// the delegated request is already running as the person in every way that
// decides anything.
func GrantFrom(ctx context.Context) (delegation.Grant, bool) {
	grant, ok := ctx.Value(grantKey).(delegation.Grant)
	return grant, ok && grant.UserID != ""
}
