// Package postgres is the only place in core-api that talks to the domain
// database.
//
// The `SET LOCAL` that makes every RLS policy bite lives here rather than in
// the interceptor that decides what to set, per §21.6. The interceptor knows
// which organisation; this package knows how a transaction carries it.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// ErrNotAMember is returned when the caller asked to act in an organisation
// they do not belong to.
var ErrNotAMember = errors.New("postgres: caller is not a member of the requested organisation")

// noOrganisation is set as app.current_org_id when the caller has no
// membership yet, which is the state a brand-new subject is in until ENT-196
// provisions one.
//
// A real uuid rather than an empty string, and the reason is specific to how
// the policies are written. Every tenant policy shipped in ENT-192 reads
// `current_setting('app.current_org_id')::uuid`, the single-argument form,
// which raises `unrecognized configuration parameter` when the GUC was never
// set and `invalid input syntax for type uuid` when it is the empty string.
// Either way the query errors instead of returning nothing, and an error is a
// far worse answer than an empty result (§0.5). The nil uuid belongs to no
// organisation, so every tenant table matches zero rows, which is exactly the
// intended meaning of "no active organisation".
const noOrganisation = "00000000-0000-0000-0000-000000000000"

// Store owns the connection pool.
type Store struct {
	pool   *pgxpool.Pool
	issuer string
}

// New opens the pool. The DSN must name `kindlast_app`: a role that owns
// nothing, is NOSUPERUSER and is NOBYPASSRLS, because RLS is silently absent
// for anything else (§14.1).
func New(ctx context.Context, dsn, issuer string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening the pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: pinging: %w", err)
	}
	return &Store{pool: pool, issuer: issuer}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Tenant is one request's transaction with both tenancy GUCs applied.
type Tenant struct {
	tx     pgx.Tx
	orgID  string
	role   string
	userID string
}

func (t *Tenant) OrgID() string  { return t.orgID }
func (t *Tenant) Role() string   { return t.role }
func (t *Tenant) UserID() string { return t.userID }

// Tx is the handle a handler must use for every query it makes.
//
// Using anything else means running outside the session settings this
// transaction carries, which reads nothing at best and reads across tenants at
// worst.
func (t *Tenant) Tx() pgx.Tx { return t.tx }

func (t *Tenant) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *Tenant) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// BeginTenant opens a transaction, sets the two tenancy GUCs, and verifies
// membership.
//
// The ordering is the part worth reading carefully. The user GUC is set first,
// because the `memberships` select policy is what makes the membership lookup
// below return anything at all: a caller can see their own membership rows and
// their co-members', and nothing else. So this does not "check membership and
// then trust itself" — the check is already running inside the policy surface
// it is checking against.
func (s *Store) BeginTenant(ctx context.Context, subjectClaim, requestedOrgID string) (*Tenant, error) {
	userID, err := subject.UUID(s.issuer, subjectClaim)
	if err != nil {
		return nil, fmt.Errorf("postgres: mapping the subject: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: beginning a transaction: %w", err)
	}

	tenant, err := s.resolve(ctx, tx, userID, requestedOrgID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tenant, nil
}

func (s *Store) resolve(ctx context.Context, tx pgx.Tx, userID uuid.UUID, requestedOrgID string) (*Tenant, error) {
	// Both GUCs are set before any query runs, and both use the transaction
	// local form so they cannot leak onto the next request that borrows this
	// pooled connection.
	if err := setLocal(ctx, tx, "app.current_user_id", userID.String()); err != nil {
		return nil, err
	}
	if err := setLocal(ctx, tx, "app.current_org_id", noOrganisation); err != nil {
		return nil, err
	}

	orgID, role, err := s.membership(ctx, tx, userID, requestedOrgID)
	if err != nil {
		return nil, err
	}

	if err := setLocal(ctx, tx, "app.current_org_id", orgID); err != nil {
		return nil, err
	}

	return &Tenant{tx: tx, orgID: orgID, role: role, userID: userID.String()}, nil
}

func (s *Store) membership(ctx context.Context, tx pgx.Tx, userID uuid.UUID, requestedOrgID string) (orgID, role string, err error) {
	if requestedOrgID == "" {
		// No header: resolve the caller's default organisation. Ordered so the
		// answer is stable across requests rather than whichever row the
		// planner returned first, because an unstable default would mean the
		// same caller silently acting in different organisations.
		err = tx.QueryRow(ctx, `
			select org_id::text, role
			from memberships
			where user_id = $1
			order by created_at, org_id
			limit 1
		`, userID).Scan(&orgID, &role)

		if errors.Is(err, pgx.ErrNoRows) {
			// Not an error. A verified subject with no membership is exactly
			// what arrives on first sign-in, and ENT-196 provisions from here.
			return noOrganisation, "", nil
		}
		if err != nil {
			return "", "", fmt.Errorf("postgres: resolving the default organisation: %w", err)
		}
		return orgID, role, nil
	}

	requested, parseErr := uuid.Parse(requestedOrgID)
	if parseErr != nil {
		// Refused before it reaches SQL. Passing it through would produce a
		// cast error from deep inside a policy, which reads as a server fault
		// rather than as a malformed header.
		return "", "", fmt.Errorf("%w: %q is not a uuid", ErrNotAMember, requestedOrgID)
	}

	err = tx.QueryRow(ctx, `
		select org_id::text, role
		from memberships
		where user_id = $1 and org_id = $2
	`, userID, requested).Scan(&orgID, &role)

	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotAMember
	}
	if err != nil {
		return "", "", fmt.Errorf("postgres: verifying membership: %w", err)
	}
	return orgID, role, nil
}

// setLocal applies a GUC for the life of the transaction.
//
// `set_config(name, value, true)` rather than `SET LOCAL name = value`,
// because the latter takes no parameters and would mean interpolating a value
// into SQL text.
func setLocal(ctx context.Context, tx pgx.Tx, name, value string) error {
	if _, err := tx.Exec(ctx, "select set_config($1, $2, true)", name, value); err != nil {
		return fmt.Errorf("postgres: setting %s: %w", name, err)
	}
	return nil
}
