package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
)

// Acting for a person (ENT-230, 00021).
//
// Three operations and one property that matters more than all of them: a
// delegation can only be minted from inside a transaction that is ALREADY the
// person's. MintDelegation therefore hangs off *Tenant rather than *Store,
// which is not a stylistic choice. A method on the store would need to be told
// whose delegation to mint, and "told whose" is the request field this whole
// design exists to avoid. Here there is nothing to tell it: the transaction
// carries the two GUCs, the policy checks the row against them, and a handler
// that wanted to mint for somebody else would have to open a transaction as
// them first, which it cannot do.

// Delegation is a minted delegation as the minting caller sees it.
type Delegation struct {
	ID uuid.UUID
	// Token is the credential, returned exactly once, here. Nothing stores it
	// and nothing can recover it: the table holds the digest.
	Token     string
	ExpiresAt time.Time
}

// MintDelegation writes one and returns the credential.
//
// The id is generated here rather than by the database so the insert needs no
// `returning`, which in turn means the application needs no select privilege on
// a table of credentials to mint one. A small thing that keeps the grant set
// honest.
func (t *Tenant) MintDelegation(ctx context.Context, mint delegation.Mint) (Delegation, error) {
	if err := mint.Validate(); err != nil {
		return Delegation{}, err
	}

	token, err := newDelegationToken()
	if err != nil {
		return Delegation{}, err
	}

	id := uuid.New()
	expires := time.Now().Add(mint.Lifetime())

	// org_id and user_id come from the transaction rather than from parameters.
	// The policy would refuse anything else, and writing them this way means
	// there is no parameter for a caller to get wrong in the first place.
	const query = `
		insert into act_delegations
			(id, org_id, user_id, acting_agent, token_hash, single_use, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7)`

	if _, err := t.tx.Exec(ctx, query,
		id, t.orgID, t.userID, mint.ActingAgent, HashDelegationToken(token), false, expires,
	); err != nil {
		return Delegation{}, fmt.Errorf("postgres: minting the delegation: %w", err)
	}

	return Delegation{ID: id, Token: token, ExpiresAt: expires}, nil
}

// RevokeDelegation ends one early.
//
// The fast path for "expires with the run": a run that finishes cleanly hands
// its delegation back. It is best effort by nature, because a crashed run
// revokes nothing, which is why the TTL rather than this is what the design
// actually rests on (00021's ceiling).
//
// Revoking an unknown, foreign or already revoked delegation is not an error.
// The policy limits the statement to this person's own rows, so "no rows
// touched" is the same answer for a delegation that was never theirs and one
// that was already handed back, and telling the caller apart would make this a
// way to ask whether an id exists.
func (t *Tenant) RevokeDelegation(ctx context.Context, id uuid.UUID) error {
	const query = `
		update act_delegations
		   set revoked_at = now()
		 where id = $1 and revoked_at is null`

	if _, err := t.tx.Exec(ctx, query, id); err != nil {
		return fmt.Errorf("postgres: revoking the delegation: %w", err)
	}
	return nil
}

// ResolveDelegation turns a presented credential into the person it names.
//
// # NO TENANCY GUCs, AND THAT IS THE WHOLE DIFFICULTY
//
// Every policy in this schema reads `app.current_org_id` and
// `app.current_user_id`. This call is what decides what those should be, so it
// cannot run behind them. `resolve_act_delegation` is SECURITY DEFINER for
// exactly that reason and for no other, and it is deliberately mechanical: it
// finds a live row and says who it names. It does not decide anything.
//
// # AND IT DOES NOT CHECK MEMBERSHIP
//
// That is BeginDelegatedTenant's job, one call later, using the same membership
// query every human request already runs under the `memberships` select policy.
// Two things follow, both wanted. A person who was removed from the
// organisation mid-run is refused on their agent's next tool call rather than
// at the next mint, and the check that refuses them is the one the rest of the
// system already depends on rather than a second implementation that could
// disagree with it.
func (s *Store) ResolveDelegation(ctx context.Context, token string) (delegation.Grant, error) {
	if token == "" {
		return delegation.Grant{}, delegation.ErrUnusable
	}

	var grant delegation.Grant
	err := s.pool.QueryRow(ctx,
		`select user_id::text, org_id::text, acting_agent
		   from resolve_act_delegation($1)`,
		HashDelegationToken(token),
	).Scan(&grant.UserID, &grant.OrgID, &grant.ActingAgent)

	if errors.Is(err, pgx.ErrNoRows) {
		// One answer for expired, revoked, redeemed and never existed, matching
		// what the function itself refuses to distinguish.
		return delegation.Grant{}, delegation.ErrUnusable
	}
	if err != nil {
		return delegation.Grant{}, fmt.Errorf("postgres: resolving the delegation: %w", err)
	}
	return grant, nil
}

// BeginDelegatedTenant opens the transaction an agent's tool call runs in.
//
// The tenancy GUCs are set to THE PERSON, not to the machine that presented the
// credential, and that is the point of the whole feature: from here down, an
// agent's read sees exactly the rows the person would see and its write is
// refused exactly where the person's would be. There is no second policy
// surface for agents to get wrong, because there is no second policy surface.
//
// The only thing that distinguishes the transaction is `app.acting_agent`,
// which no policy reads and which decides nothing. It exists so the audit row
// this transaction writes can name what was holding the pen (00021).
//
// Membership is verified here rather than at mint, and the ordering is the same
// one BeginTenant relies on: the user GUC is set first, so the membership
// lookup runs inside the policy surface it is checking against.
func (s *Store) BeginDelegatedTenant(ctx context.Context, grant delegation.Grant) (*Tenant, error) {
	userID, err := uuid.Parse(grant.UserID)
	if err != nil {
		return nil, fmt.Errorf("postgres: the delegation names no user: %w", err)
	}
	if _, err := uuid.Parse(grant.OrgID); err != nil {
		return nil, fmt.Errorf("postgres: the delegation names no organisation: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: beginning a delegated transaction: %w", err)
	}

	tenant, err := s.resolve(ctx, tx, userID, grant.OrgID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}

	if err := setLocal(ctx, tx, "app.acting_agent", grant.ActingAgent); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return tenant, nil
}

// HashDelegationToken is the digest stored in `act_delegations.token_hash`.
//
// Exported for the same reason HashInvitationToken is: a fixture that needs a
// working delegation must hash it the way the store does, and a second
// implementation in a test file is a second thing to drift.
func HashDelegationToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// newDelegationToken makes the credential.
//
// 32 bytes from crypto/rand, base64url without padding. Long enough that
// guessing is not a strategy, and URL safe because the second consumer of this
// primitive (§8's approve link, ENT-230) puts one in a link.
func newDelegationToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("postgres: generating a delegation token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
