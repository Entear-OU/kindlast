package interceptor

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
)

// OrgHeader carries the organisation the caller is acting in.
//
// A header rather than a token claim, and that is a deliberate choice with
// consequences (§20.1). A consultant serving several client companies belongs
// to several organisations, so the active one has to be switchable; carrying
// it in the token would mean re-minting on every switch and would give
// membership two sources of truth. Your database owns memberships. The IdP
// owns identity, and nothing else.
//
// No `X-` prefix, per RFC 6648.
const OrgHeader = "Kindlast-Org-Id"

// ErrNotAMember is returned when the caller asked to act in an organisation
// they do not belong to.
var ErrNotAMember = errors.New("interceptor: caller is not a member of the requested organisation")

// Tenant is one request's database transaction, with both tenancy GUCs set on
// it.
//
// The transaction is the unit, not the connection, because the GUCs are set
// with `set_config(..., true)`, the `SET LOCAL` form. They therefore last
// exactly as long as this transaction and cannot leak onto the next request
// that borrows the same pooled connection, which is the failure mode a
// session-level `SET` would produce under a pool: request B inherits request
// A's organisation and reads its rows.
type Tenant interface {
	// OrgID is the organisation resolved and verified for this request.
	OrgID() string
	// Role is the caller's role in it: owner, member or viewer. Empty when the
	// caller has no membership yet, which is the state a brand-new subject is
	// in until ENT-196 provisions one.
	Role() string
	// UserID is this system's own identifier for the caller: the version 5 uuid
	// derived from (issuer, subject), which is what `memberships`, `created_by`
	// and `approved_by` store.
	//
	// Distinct from the IdP's subject claim, and deliberately so: the derivation
	// is one-way, so a client holding the subject cannot compute this, and
	// without it a client cannot recognise itself in a member list (ENT-220).
	UserID() string
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// TenantOpener starts a transaction with tenancy applied.
//
// Implemented by the Postgres store. It is an interface here only so this
// package does not depend on a database driver; §13.3 forbids satisfying it
// with a mock in tests, and the tests in this package do not, because a mocked
// membership check would assert nothing about the policies that actually
// enforce isolation.
type TenantOpener interface {
	// BeginTenant opens a transaction, sets app.current_user_id and
	// app.current_org_id, and verifies the caller's membership of the
	// organisation. An empty requestedOrgID means "resolve my default one",
	// which is what the console's first call does before it knows any
	// organisation exists.
	BeginTenant(ctx context.Context, subject, requestedOrgID string) (Tenant, error)

	// BeginDelegatedTenant opens the same kind of transaction for a person a
	// machine principal is acting for (ENT-230), with both GUCs set to THAT
	// PERSON rather than to the caller.
	//
	// A method on this interface rather than a separate optional one, so a
	// deployment cannot wire a tenant opener that quietly does not support
	// delegation and discover it as a runtime refusal. There is one way to
	// start a transaction with tenancy on it, and it has two entry points
	// because there are two ways to learn whose transaction it is.
	//
	// The membership check is the same one BeginTenant runs, deliberately: a
	// person removed from the organisation part way through a run has to be
	// refused on the agent's next call, and the check that refuses them should
	// be the one the rest of the system already depends on.
	BeginDelegatedTenant(ctx context.Context, grant delegation.Grant) (Tenant, error)

	// BeginAPIKeyTenant opens the transaction a partner's key runs in
	// (ENT-262), with both GUCs set to the person who minted the key and a
	// third, `app.current_api_key_id`, set so every audit row this transaction
	// writes names the credential that acted.
	//
	// A method on this interface rather than an optional one, for the reason
	// BeginDelegatedTenant is: a deployment cannot wire a tenant opener that
	// quietly does not support keys and find out as a runtime refusal. There is
	// one way to start a transaction with tenancy on it, and it now has three
	// entry points because there are three ways to learn whose transaction it
	// is.
	//
	// The membership check is the same one the other two run. That is what
	// makes revoking a person's access revoke their keys with it, without a
	// sweep and without anybody remembering.
	BeginAPIKeyTenant(ctx context.Context, key apikey.Principal) (Tenant, error)
}

// Tenancy resolves the active organisation, verifies membership, and sets the
// two GUCs every RLS policy shipped in ENT-192 reads.
//
// Worth being clear about what this interceptor is and is not. It is not the
// thing that enforces tenant isolation: Postgres does that, through policies
// that are forced on every table. This is the thing that tells Postgres who is
// asking. The membership check here is a courtesy that turns a wrong
// organisation into a clean 403 instead of a silently empty list; the `exists`
// clause inside every policy is what keeps isolation holding even if this code
// sets an organisation the caller does not belong to, which is exactly the bug
// class RLS exists to survive (§20.1).
func Tenancy(store TenantOpener) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			subject := ""
			if claims, ok := ClaimsFrom(ctx); ok {
				subject = claims.Subject
			} else if _, isKey := APIKeyFrom(ctx); !isKey {
				return nil, connect.NewError(connect.CodeInternal,
					errors.New("tenancy interceptor ran before authentication"))
			}

			tenant, err := open(ctx, store, subject, req.Header().Get(OrgHeader))
			if err != nil {
				if errors.Is(err, ErrNotAMember) {
					// The same answer whether the organisation does not exist
					// or the caller simply is not in it. Distinguishing them
					// would turn this endpoint into an oracle for which
					// organisation ids are real.
					return nil, connect.NewError(connect.CodePermissionDenied, ErrNotAMember)
				}
				return nil, connect.NewError(connect.CodeInternal,
					fmt.Errorf("opening a tenant transaction: %w", err))
			}

			response, err := next(WithTenant(ctx, tenant), req)
			if err != nil {
				// Rollback error is deliberately swallowed: the handler's
				// error is what the caller needs, and replacing it with a
				// cleanup failure would hide the actual cause.
				_ = tenant.Rollback(ctx)
				return nil, err
			}

			if err := tenant.Commit(ctx); err != nil {
				return nil, connect.NewError(connect.CodeInternal,
					fmt.Errorf("committing the tenant transaction: %w", err))
			}
			return response, nil
		}
	}
}

// open starts the transaction, as the caller or as the person they are acting
// for.
//
// # A DELEGATION IS SINGLE-ORG, AND THE HEADER DOES NOT GET A VOTE
//
// The organisation comes from the delegation, never from `Kindlast-Org-Id`.
// That is the difference between a delegation and a session: a person switches
// organisations by changing the header, and an agent must not, because the
// header is set by whatever is driving the agent rather than by the person the
// delegation names.
//
// A header naming a DIFFERENT organisation is refused rather than ignored.
// Ignoring it would serve a caller rows from an organisation they did not ask
// about, which is the shape of a tenancy bug even when the answer is correct;
// and a caller that sends a mismatched header has misunderstood something worth
// telling them about. A header naming the same one is fine, because a client
// that sets it uniformly is not making a claim, and refusing that would make
// the ordinary console client unusable as an agent driver.
// # A KEY IS SINGLE-ORG FOR THE SAME REASON, AND THE HEADER DOES NOT GET A VOTE
//
// A partner's key names one organisation, chosen when it was minted, and the
// header cannot move it. That rule is not a copy of the delegation rule out of
// tidiness: it closes the same hole from a worse position. A delegation lives
// for minutes; a key lives until somebody revokes it, and it sits in a partner's
// configuration where the person who set the header is not the person who minted
// it. If `Kindlast-Org-Id` could redirect a key, then a consultancy's single
// integration credential would reach every client company that consultancy
// serves, by changing one header.
//
// A mismatched header is refused rather than ignored, exactly as for a
// delegation: serving a caller rows from an organisation they did not name is
// the shape of a tenancy bug even when the rows are the right ones. A matching
// header is fine, so a client that sets it uniformly stays usable.
func open(ctx context.Context, store TenantOpener, subject, requestedOrgID string) (Tenant, error) {
	if key, isKey := APIKeyFrom(ctx); isKey {
		if requestedOrgID != "" && requestedOrgID != key.OrgID {
			return nil, ErrNotAMember
		}
		// Through the delegated opener, because a key asks the same question a
		// delegation does: open a transaction as THIS PERSON in THIS
		// ORGANISATION, having verified they are still a member. Reusing it
		// rather than adding a third entry point is what keeps there being one
		// membership check in the system rather than three that could disagree.
		//
		// ActingAgent is empty, and that is a statement rather than a gap: no
		// agent is holding the pen. What is acting is the key, and 00043 names
		// it on the audit row through `app.current_api_key_id`.
		return store.BeginAPIKeyTenant(ctx, key)
	}

	grant, delegated := GrantFrom(ctx)
	if !delegated {
		return store.BeginTenant(ctx, subject, requestedOrgID)
	}

	if requestedOrgID != "" && requestedOrgID != grant.OrgID {
		return nil, ErrNotAMember
	}
	return store.BeginDelegatedTenant(ctx, grant)
}
