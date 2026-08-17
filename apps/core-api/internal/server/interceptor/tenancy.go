package interceptor

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
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
			claims, ok := ClaimsFrom(ctx)
			if !ok {
				return nil, connect.NewError(connect.CodeInternal,
					errors.New("tenancy interceptor ran before authentication"))
			}

			tenant, err := store.BeginTenant(ctx, claims.Subject, req.Header().Get(OrgHeader))
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
