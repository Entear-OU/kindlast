package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
)

// ErrInvitationNotUsable covers expired, already accepted, and never existed.
//
// One error for three causes, deliberately. Telling a caller which of them
// applies turns the endpoint into an oracle for which tokens are real, and
// nobody holding a legitimate invitation needs the distinction.
var ErrInvitationNotUsable = errors.New("postgres: invitation is not usable")

// Memberships returns every organisation a subject belongs to.
//
// Readable because the `memberships` select policy lets a caller see their own
// rows, so this runs under RLS rather than around it.
func (t *Tenant) Memberships(ctx context.Context) ([]org.Membership, error) {
	rows, err := t.tx.Query(ctx, `
		select m.org_id::text, o.name, o.slug, m.role
		from memberships m
		join organisations o on o.id = m.org_id
		where m.user_id = $1
		order by o.created_at, o.id
	`, t.userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading memberships: %w", err)
	}
	defer rows.Close()

	var memberships []org.Membership
	for rows.Next() {
		var m org.Membership
		if err := rows.Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role); err != nil {
			return nil, fmt.Errorf("postgres: scanning membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

// RecordIdentity stores the raw claims behind the derived user id.
//
// The derivation is one-way, so without this row a uuid in the database
// answers to nobody: not an operator during an incident, and not a subject
// access request, which in a GDPR product has to be answerable rather than
// merely intended.
//
// Upsert rather than insert, because the email on a token can change and the
// last one seen is the useful one.
//
// The coalesce is what makes "last one seen" mean the last one actually seen.
// An absent claim means the token did not say, not that the address is gone,
// and the two are easy to conflate into a statement that quietly nulls a good
// row. That is not hypothetical here: the bundled Zitadel puts no email in an
// access token at all, so provisioning learns the address from userinfo and
// then every subsequent call arrives with nothing. Assigning excluded.email
// unconditionally would erase it on the very next page load, leaving a
// user_identities row that answers nobody, which is the one thing this table
// exists to prevent.
func (t *Tenant) RecordIdentity(ctx context.Context, subject org.Subject) error {
	// display_name joins email here as of ENT-202. Until then the name arrived
	// from userinfo on every sign-in and was discarded on every sign-in, so the
	// product knew it and forgot it, and the members list had nothing but uuids
	// to render.
	//
	// Both are coalesced on conflict rather than overwritten, so a later
	// sign-in whose token or userinfo carries no name does not erase one we
	// already had. Absence is not a correction.
	_, err := t.tx.Exec(ctx, `
		insert into user_identities (user_id, issuer, subject, email, display_name)
		values ($1, $2, $3, nullif($4, ''), nullif($5, ''))
		on conflict (user_id) do update
		set email = coalesce(excluded.email, user_identities.email),
		    display_name = coalesce(excluded.display_name, user_identities.display_name),
		    updated_at = now()
	`, t.userID, subject.Issuer, subject.Subject, subject.Email, subject.DisplayName)
	if err != nil {
		return fmt.Errorf("postgres: recording identity: %w", err)
	}
	return nil
}

// ProvisionPersonalOrganisation creates the organisation a subject arrives
// with, and is safe to run concurrently with itself.
//
// This is the race in §1.8, and it is worth being precise about how it is
// won, because the obvious implementation looks correct and is not.
//
// Two tabs open the app at once. Two requests arrive with the same unseen
// `sub`, both read no memberships, and both decide to create a personal
// organisation. `on conflict do nothing` on `memberships` does not help:
// each transaction is inserting a DIFFERENT organisation id, so neither
// conflicts, and the subject ends up owning two organisations.
//
// What settles it is the partial unique index on
// `organisations (personal_owner_id) where personal_owner_id is not null`
// added in 00003. The second insert blocks until the first commits, then
// does nothing and returns no row, and this reports that it created nothing.
// The caller then re-reads rather than trusting what it just wrote.
//
// A plain insert would abort the transaction on the unique violation, which
// is why this is an `on conflict` rather than an error to catch: an aborted
// transaction cannot go on to re-read anything.
//
// The id is generated here rather than by the database, and that is not a
// style preference. `RETURNING` applies the table's SELECT policy to the row
// it returns, and `organisations_select_member` requires a membership that, at
// this exact moment, does not exist yet: it is created two statements below.
// So `insert ... returning id` fails with "new row violates row-level security
// policy" even though the insert itself is permitted, which reads as a
// permissions bug and is really a chicken-and-egg one. Generating the id in
// advance sidesteps it without widening any policy, and the command tag tells
// us whether we won the race just as well as a returned row would.
func (t *Tenant) ProvisionPersonalOrganisation(ctx context.Context, plan org.Plan) (created bool, err error) {
	orgID := uuid.New().String()

	created, err = t.insertOrganisation(ctx, orgID, plan)
	if err != nil {
		return false, err
	}
	if !created {
		// Another transaction got there first. Not an error: the desired end
		// state exists, which is all idempotence promises. The caller re-reads
		// rather than trusting either outcome.
		return false, nil
	}

	// Belt and braces against the same race arriving by a different route.
	// The (org_id, user_id) primary key shipped in 00002 makes this a no-op
	// on a retry.
	_, err = t.tx.Exec(ctx, `
		insert into memberships (org_id, user_id, role)
		values ($1, $2, $3)
		on conflict (org_id, user_id) do nothing
	`, orgID, t.userID, plan.Role)
	if err != nil {
		return false, fmt.Errorf("postgres: creating the owner membership: %w", err)
	}

	return true, nil
}

// maxSlugAttempts bounds the collision search.
//
// Twenty-five people sharing one derived name is already implausible, and the
// number matters less than the bound existing: without one, a bug that made
// every attempt collide would spin against the database rather than fail.
const maxSlugAttempts = 25

// insertOrganisation writes the organisation row, taking the first slug the
// unique constraint will accept.
//
// The slug is derived by `org_slug` in the database rather than in Go, so the
// rule here and the rule the ENT-198 backfill used are the same rule. A second
// implementation would be one that can drift, and a slug is immutable once
// minted (§20.1), so a drifted rule produces URLs nobody can correct.
//
// Collisions are found rather than predicted, and that is deliberate. Asking
// "is this slug free" before inserting would be both a race and a lie: a race
// because another transaction can take it in between, and a lie because RLS
// hides other organisations from this caller, so the answer would describe
// only the slugs they can already see. Letting the unique constraint refuse
// the insert asks the only question that has a true answer.
//
// Each attempt runs inside a savepoint, which is what makes retrying possible
// at all: a unique violation marks the whole transaction as aborted, and an
// aborted transaction cannot go on to try anything else, let alone the
// membership insert and the re-read the caller depends on.
func (t *Tenant) insertOrganisation(ctx context.Context, orgID string, plan org.Plan) (bool, error) {
	for ordinal := 1; ordinal <= maxSlugAttempts; ordinal++ {
		savepoint, err := t.tx.Begin(ctx)
		if err != nil {
			return false, fmt.Errorf("postgres: opening a savepoint for the slug attempt: %w", err)
		}

		tag, err := savepoint.Exec(ctx, `
			insert into organisations (id, name, slug, personal_owner_id)
			values ($1, $2, org_slug($2, $4), $3)
			on conflict (personal_owner_id) where personal_owner_id is not null
			do nothing
		`, orgID, plan.OrganisationName, t.userID, ordinal)

		if err != nil {
			_ = savepoint.Rollback(ctx)
			if isSlugCollision(err) {
				continue
			}
			return false, fmt.Errorf("postgres: creating the personal organisation: %w", err)
		}

		if err := savepoint.Commit(ctx); err != nil {
			return false, fmt.Errorf("postgres: committing the slug attempt: %w", err)
		}
		return tag.RowsAffected() > 0, nil
	}

	return false, fmt.Errorf(
		"postgres: no free slug for an organisation named %q after %d attempts",
		plan.OrganisationName, maxSlugAttempts)
}

// isSlugCollision distinguishes "that slug is taken" from every other unique
// violation this statement can raise.
//
// Named constraint rather than bare error code, because the same statement can
// violate the personal-owner index, and treating that as a collision would
// retry a conflict that `on conflict do nothing` has already handled
// correctly.
func isSlugCollision(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	const uniqueViolation = "23505"
	return pgErr.Code == uniqueViolation && pgErr.ConstraintName == "organisations_slug_key"
}

// RenamePersonalOrganisation repairs a personal organisation still named after
// its owner's subject claim.
//
// Two conditions narrow this beyond what RLS already enforces, and both are
// deliberate. `personal_owner_id = t.userID` means this can only ever touch an
// organisation created for this caller by provisioning, never a real one they
// happen to own; and `name = $2` means it only replaces the exact identifier
// it was asked to replace, so a caller who has since renamed their
// organisation to something they chose keeps it, even if this runs late.
//
// The update policy (`organisations_update_owner`, 00002) requires owner, so
// the statement is enforced twice over. That is not redundancy for its own
// sake: the policy is the security boundary and these predicates are about
// touching the right row, which are different questions (§0.5).
func (t *Tenant) RenamePersonalOrganisation(ctx context.Context, currentName, newName string) (bool, error) {
	tag, err := t.tx.Exec(ctx, `
		update organisations
		set name = $3
		where personal_owner_id = $1
		  and name = $2
	`, t.userID, currentName, newName)
	if err != nil {
		return false, fmt.Errorf("postgres: renaming the personal organisation: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// UseOrganisation points the transaction's tenancy GUC at an organisation.
//
// Needed after provisioning, because the interceptor resolved the active
// organisation before the handler ran, and for a brand-new subject there was
// nothing to resolve. Without this the request would finish with the GUC still
// on the nil uuid and read nothing from any tenant table.
//
// Safe to expose despite taking an arbitrary id: every policy still carries
// the membership `exists` clause, so setting an organisation the caller does
// not belong to yields zero rows rather than access. That is exactly the bug
// class RLS is there to survive (§20.1).
func (t *Tenant) UseOrganisation(ctx context.Context, orgID string) error {
	if err := setLocal(ctx, t.tx, "app.current_org_id", orgID); err != nil {
		return err
	}
	t.orgID = orgID
	return nil
}

// Plan returns the active organisation's subscription plan.
//
// Free when there is no row: a newly provisioned organisation has no
// subscription, and billing keys on the organisation rather than the user
// (§20.1).
//
// Free also when the row is not active. Reading the plan column alone would
// keep a paid feature working indefinitely after a customer stopped paying,
// because `status` moving to `canceled` or `past_due` leaves `plan` saying
// `pro`. The status filter was added when ENT-203 made this the act path's
// gate; before that nothing consulted it in a way that could be wrong.
func (t *Tenant) Plan(ctx context.Context) (string, error) {
	var plan string

	err := t.tx.QueryRow(ctx, `
		select plan from subscriptions where org_id = $1 and status = 'active'
	`, t.orgID).Scan(&plan)

	if errors.Is(err, pgx.ErrNoRows) {
		return "free", nil
	}
	if err != nil {
		return "", fmt.Errorf("postgres: reading the plan: %w", err)
	}
	return plan, nil
}

// AcceptInvitation redeems an invitation token and returns the organisation
// joined.
//
// The work happens inside accept_invitation, a SECURITY DEFINER function, and
// that is not a shortcut. The invitee is not a member of the organisation yet,
// which is the entire point of an invitation, so no org-scoped policy can show
// them the row naming them, and a policy permissive enough to try would show
// every pending invitation to every authenticated stranger. Holding the token
// is the authorization; the acting user comes from the GUC rather than from an
// argument this code passes.
//
// The token is hashed here and only the hash reaches the database, so the
// value stored is useless to anyone reading a dump.
// The slug comes back with the rest, because the caller's next move is a
// redirect into the organisation just joined, and that URL is built from the
// slug. Returning without it would mean a second round trip on the one path
// where the person is already waiting on a redirect.
func (t *Tenant) AcceptInvitation(ctx context.Context, token string) (invitation org.Joined, err error) {
	if token == "" {
		return org.Joined{}, ErrInvitationNotUsable
	}

	var orgID string

	// coalesce, because the function returns null for the three not-usable
	// cases and a null does not scan into a string.
	err = t.tx.QueryRow(ctx, `
		select coalesce(accept_invitation($1)::text, '')
	`, HashInvitationToken(token)).Scan(&orgID)
	if err != nil {
		return org.Joined{}, fmt.Errorf("postgres: accepting the invitation: %w", err)
	}
	if orgID == "" {
		return org.Joined{}, ErrInvitationNotUsable
	}

	// Re-read through RLS rather than trusting the definer function's word:
	// the membership now exists, so the organisation is visible by ordinary
	// policy, and if it somehow is not then the join did not happen.
	joined := org.Joined{OrgID: orgID}
	err = t.tx.QueryRow(ctx, `
		select o.name, o.slug, m.role
		from organisations o
		join memberships m on m.org_id = o.id and m.user_id = $2
		where o.id = $1
	`, orgID, t.userID).Scan(&joined.OrgName, &joined.OrgSlug, &joined.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return org.Joined{}, fmt.Errorf("postgres: invitation accepted but no membership is visible for %s", orgID)
	}
	if err != nil {
		return org.Joined{}, fmt.Errorf("postgres: reading the joined organisation: %w", err)
	}

	return joined, nil
}

// HashInvitationToken is exported so whatever issues an invitation stores the
// same hash this redeems. One function, so the two halves cannot drift.
func HashInvitationToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
