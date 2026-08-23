package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
)

// Partner API keys (ENT-262, 00043).
//
// Four operations and one property that matters more than the rest: a key can
// only be minted from inside a transaction that is ALREADY a person's. Mint
// therefore hangs off *Tenant rather than *Store, the same way MintDelegation
// does and for the same reason. A method on the store would have to be told
// whose key to mint, and "told whose" is the request field this design exists to
// remove: `api_keys_mint` pins `created_by` to the GUC user, so a handler that
// wanted to mint a key acting as somebody else would have to open a transaction
// as them first, which it cannot do.
//
// AuthenticateAPIKey is the exception and hangs off *Store, because it runs
// before there is a tenant to hang off. See its comment.

// The audit vocabulary for keys.
//
// Named for what a person did rather than for the table that moved, matching
// `invite_member` and `approve_finding`. §23 asks that a key's creation and
// revocation be in the regulatory record for the same reason PR #229 put
// membership there: a credential that was minted and never recorded is an
// access path with no answer to "who opened this, and when".
const (
	ActionMintAPIKey   = "mint_api_key"
	ActionRevokeAPIKey = "revoke_api_key"
)

// ErrNoSuchAPIKey is returned when a revoke matched no row.
//
// Under RLS that is ambiguous by construction and deliberately so: the key may
// not exist, or it may exist in an organisation the caller cannot see. The
// caller is told the same thing either way, because distinguishing them would
// make this a way to ask whether a given key id is real.
var ErrNoSuchAPIKey = errors.New("postgres: no such API key in this organisation")

// APIKey is one row as a console sees it.
//
// No digest and no credential, and not because this struct forgot them: the
// application role holds no select privilege on `secret_hash` at all (00043's
// column grant), so the query below could not read it if it asked.
type APIKey struct {
	ID     string
	Handle string
	Name   string
	Scopes []string

	CreatedBy  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	RevokedBy  string
}

// Revoked reports whether this key has been stopped.
func (k APIKey) Revoked() bool { return k.RevokedAt != nil }

// MintAPIKey writes a key and returns the credential, once.
//
// The id is generated here rather than by the database so the insert needs no
// `returning`, which in turn means the application needs no select privilege on
// the row it just wrote in order to write it. The same small honesty
// MintDelegation keeps.
//
// The audit row goes in the same transaction, so a key that exists is a key the
// log records. There is no window in which one is true and the other is not: if
// the audit insert is refused, the key goes with it.
//
// NO CREDENTIAL, AND NO DIGEST, IN THE AUDIT ROW. The audit log is readable by
// every member and exportable to CSV. What goes in `after` is the public handle,
// the name and the scopes, which is what somebody reading the log needs in order
// to find the key and decide whether it should still exist.
func (t *Tenant) MintAPIKey(ctx context.Context, mint apikey.Mint) (APIKey, string, error) {
	normalised, err := mint.Validate()
	if err != nil {
		return APIKey{}, "", err
	}

	key, err := apikey.Generate()
	if err != nil {
		return APIKey{}, "", err
	}

	id := uuid.New()

	// org_id and created_by come from the transaction rather than from
	// parameters. The policy would refuse anything else, and writing them this
	// way means there is no parameter for a caller to get wrong.
	const query = `
		insert into api_keys (id, org_id, key_id, secret_hash, name, scopes, created_by)
		values ($1, $2, $3, $4, $5, $6, $7)`

	if _, err := t.tx.Exec(ctx, query,
		id, t.orgID, key.Handle, key.SecretDigest, normalised.Name, normalised.Scopes, t.userID,
	); err != nil {
		return APIKey{}, "", fmt.Errorf("postgres: minting the API key: %w", err)
	}

	if err := t.recordAudit(ctx, auditEntry{
		ActionType:  ActionMintAPIKey,
		TargetTable: "api_keys",
		TargetID:    &id,
		After: auditJSON(map[string]string{
			"handle": key.Handle,
			"name":   normalised.Name,
			"scopes": joinScopes(normalised.Scopes),
		}),
	}); err != nil {
		return APIKey{}, "", err
	}

	return APIKey{
		ID:        id.String(),
		Handle:    key.Handle,
		Name:      normalised.Name,
		Scopes:    normalised.Scopes,
		CreatedBy: t.userID,
		CreatedAt: time.Now(),
	}, key.Credential, nil
}

// APIKeys lists this organisation's keys, newest first.
//
// Revoked ones are included. A console that hid them would be hiding exactly the
// rows somebody auditing access wants: "was this stopped, and when" is the
// question, and a list that only shows live keys cannot answer it.
func (t *Tenant) APIKeys(ctx context.Context) ([]APIKey, error) {
	// No org predicate: RLS supplies it from `app.current_org_id`, and the
	// select policy carries the membership `exists` besides.
	const query = `
		select id::text, key_id, name, scopes, created_by::text, created_at,
		       last_used_at, revoked_at, coalesce(revoked_by::text, '')
		  from api_keys
		 order by created_at desc, id desc`

	rows, err := t.tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing the API keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		if err := rows.Scan(
			&key.ID, &key.Handle, &key.Name, &key.Scopes, &key.CreatedBy,
			&key.CreatedAt, &key.LastUsedAt, &key.RevokedAt, &key.RevokedBy,
		); err != nil {
			return nil, fmt.Errorf("postgres: reading an API key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading the API keys: %w", err)
	}
	return keys, nil
}

// RevokeAPIKey stops a key, immediately and for good.
//
// Immediately is literal and it is the property §23 asks for: the next request
// on this key calls `authenticate_api_key`, which selects `revoked_at is null`,
// and gets nothing. There is no cache to expire, no deny-list to propagate and
// no token still valid for its remaining ten minutes. Revocation of a key is
// simply stronger than revocation of a token, which is the one advantage this
// credential model has and is worth not throwing away.
//
// A key that is already revoked is ErrNoSuchAPIKey rather than a silent success.
// A person clicking revoke twice should be told the second click did nothing,
// and the `revoked_at is null` predicate is also what makes the trigger's
// one-way rule unreachable rather than merely enforced.
func (t *Tenant) RevokeAPIKey(ctx context.Context, id string) (APIKey, error) {
	target, err := uuid.Parse(id)
	if err != nil {
		// A malformed id names no key, and the caller is told what a caller
		// naming a key that is not theirs is told.
		return APIKey{}, ErrNoSuchAPIKey
	}

	// The row is read `for update` before it is changed, so the audit `before`
	// describes the key as it actually was rather than as the caller believed.
	var key APIKey
	err = t.tx.QueryRow(ctx, `
		select key_id, name, scopes
		  from api_keys
		 where id = $1 and revoked_at is null
		 for update
	`, target).Scan(&key.Handle, &key.Name, &key.Scopes)

	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrNoSuchAPIKey
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("postgres: reading the API key to revoke: %w", err)
	}

	var revokedAt time.Time
	err = t.tx.QueryRow(ctx, `
		update api_keys
		   set revoked_at = now(), revoked_by = $2
		 where id = $1 and revoked_at is null
		 returning revoked_at
	`, target, t.userID).Scan(&revokedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrNoSuchAPIKey
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("postgres: revoking the API key: %w", err)
	}

	// `before` describes what stopped existing as a usable credential; there is
	// no `after` worth writing, because the row's only change is that it is now
	// off. The absence is the record, matching how a membership removal is
	// logged.
	if err := t.recordAudit(ctx, auditEntry{
		ActionType:  ActionRevokeAPIKey,
		TargetTable: "api_keys",
		TargetID:    &target,
		Before: auditJSON(map[string]string{
			"handle": key.Handle,
			"name":   key.Name,
			"scopes": joinScopes(key.Scopes),
		}),
	}); err != nil {
		return APIKey{}, err
	}

	key.ID = id
	key.RevokedAt = &revokedAt
	key.RevokedBy = t.userID
	return key, nil
}

// AuthenticateAPIKey turns a presented credential into the key it names.
//
// # NO TENANCY GUCs, AND THAT IS THE WHOLE DIFFICULTY
//
// Every policy on `api_keys` reads `app.current_org_id`. This call is what
// decides what that should be, so it cannot run behind them.
// `authenticate_api_key` is SECURITY DEFINER for exactly that reason and for no
// other, and it is deliberately mechanical: it finds a live row by its public
// handle and says what the row contains. It decides nothing.
//
// # AND IT DOES NOT CHECK MEMBERSHIP
//
// That is BeginAPIKeyTenant's job, one call later, using the same membership
// query every human request already runs under the `memberships` select policy.
// So a person removed from the organisation has their keys refused on the next
// request, by the check the rest of the system already depends on rather than by
// a second implementation that could disagree with it.
//
// # THE COMPARISON IS IN GO AND IT IS CONSTANT TIME
//
// See apikey.Presented.Matches. The digest travels back here rather than the
// candidate travelling in, which is what makes that possible.
func (s *Store) AuthenticateAPIKey(
	ctx context.Context, credential string,
) (apikey.Principal, error) {
	presented, err := apikey.Parse(credential)
	if err != nil {
		// Refused without a round trip. The shape of a key is published, so
		// this leaks nothing, and it keeps a stream of garbage from becoming a
		// stream of index lookups.
		return apikey.Principal{}, apikey.ErrMalformed
	}

	var (
		id, orgID, createdBy uuid.UUID
		scopes               []string
		digest               []byte
	)
	err = s.pool.QueryRow(ctx,
		`select id, org_id, created_by, scopes, secret_hash
		   from authenticate_api_key($1)`,
		presented.Handle,
	).Scan(&id, &orgID, &createdBy, &scopes, &digest)

	if errors.Is(err, pgx.ErrNoRows) {
		// One answer for unknown and revoked, matching what the function itself
		// refuses to distinguish.
		return apikey.Principal{}, apikey.ErrMalformed
	}
	if err != nil {
		// NOT folded into ErrMalformed. A database that will not answer is a
		// different thing from a key that is not real, and the caller above
		// turns the two into different status codes on purpose: one says rotate
		// your credential, the other says try again.
		return apikey.Principal{}, fmt.Errorf("postgres: authenticating the API key: %w", err)
	}

	if !presented.Matches(digest) {
		// The handle existed and the secret did not match, which is the only
		// interesting failure in this function and still gets the same answer.
		// Note what does NOT happen here: no touch. `last_used_at` moves only
		// after a match, so the console's "last used" column reports use rather
		// than reporting that somebody guessed a handle.
		return apikey.Principal{}, apikey.ErrMalformed
	}

	// Best effort, and deliberately so. A key that authenticated must not be
	// refused because a bookkeeping write failed, and `touch_api_key` is
	// coarsened to a minute so this is a no-op on almost every call anyway.
	if _, err := s.pool.Exec(ctx, `select touch_api_key($1)`, id); err != nil {
		// Swallowed rather than returned: see above. It is not silent, because
		// the next reader of `last_used_at` sees a stale value, which is the
		// visible symptom and the right one.
		_ = err
	}

	return apikey.Principal{
		ID:     id.String(),
		UserID: createdBy.String(),
		OrgID:  orgID.String(),
		Scopes: scopes,
	}, nil
}

// BeginAPIKeyTenant opens the transaction a partner's key runs in.
//
// The tenancy GUCs are set to THE PERSON WHO MINTED THE KEY, not to anything the
// key carries of its own, and that is the whole feature: from here down, a key's
// read sees exactly the rows its minter would see and its write is refused
// exactly where theirs would be. There is no second policy surface for keys to
// get wrong, because there is no second policy surface.
//
// The one thing that distinguishes the transaction is `app.current_api_key_id`,
// which no policy reads and which decides nothing. It exists so every audit row
// this transaction writes names the credential that acted (00043), including the
// rows written by triggers deep inside an UPDATE, which have no other way to
// learn it.
//
// Membership is verified here rather than at mint, on the same ordering
// BeginTenant relies on: the user GUC is set first, so the membership lookup
// runs inside the policy surface it is checking against.
func (s *Store) BeginAPIKeyTenant(
	ctx context.Context, key apikey.Principal,
) (*Tenant, error) {
	userID, err := uuid.Parse(key.UserID)
	if err != nil {
		return nil, fmt.Errorf("postgres: the API key names no user: %w", err)
	}
	if _, err := uuid.Parse(key.OrgID); err != nil {
		return nil, fmt.Errorf("postgres: the API key names no organisation: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: beginning an API key transaction: %w", err)
	}

	tenant, err := s.resolve(ctx, tx, userID, key.OrgID)
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}

	// After resolve, so a caller whose membership was refused never gets as far
	// as labelling a row. `is_local` true, so it ends with this transaction and
	// cannot leak onto the next request that borrows the connection.
	if _, err := tx.Exec(ctx,
		`select set_config('app.current_api_key_id', $1, true)`, key.ID,
	); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("postgres: naming the acting API key: %w", err)
	}

	return tenant, nil
}

// joinScopes renders a scope set for an audit row.
//
// Space separated, matching how OAuth writes a scope list everywhere else in
// this system, so the value in the log reads the same as the value in a token.
func joinScopes(scopes []string) string {
	joined := ""
	for i, scope := range scopes {
		if i > 0 {
			joined += " "
		}
		joined += scope
	}
	return joined
}
