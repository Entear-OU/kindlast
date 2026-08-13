package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		select m.org_id::text, o.name, m.role
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
		if err := rows.Scan(&m.OrgID, &m.OrgName, &m.Role); err != nil {
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
func (t *Tenant) RecordIdentity(ctx context.Context, subject org.Subject) error {
	_, err := t.tx.Exec(ctx, `
		insert into user_identities (user_id, issuer, subject, email)
		values ($1, $2, $3, nullif($4, ''))
		on conflict (user_id) do update
		set email = excluded.email,
		    updated_at = now()
	`, t.userID, subject.Issuer, subject.Subject, subject.Email)
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

	tag, err := t.tx.Exec(ctx, `
		insert into organisations (id, name, personal_owner_id)
		values ($1, $2, $3)
		on conflict (personal_owner_id) where personal_owner_id is not null
		do nothing
	`, orgID, plan.OrganisationName, t.userID)
	if err != nil {
		return false, fmt.Errorf("postgres: creating the personal organisation: %w", err)
	}

	if tag.RowsAffected() == 0 {
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
func (t *Tenant) Plan(ctx context.Context) (string, error) {
	var plan string

	err := t.tx.QueryRow(ctx, `
		select plan from subscriptions where org_id = $1
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
func (t *Tenant) AcceptInvitation(ctx context.Context, token string) (orgID, orgName, role string, err error) {
	if token == "" {
		return "", "", "", ErrInvitationNotUsable
	}

	// coalesce, because the function returns null for the three not-usable
	// cases and a null does not scan into a string.
	err = t.tx.QueryRow(ctx, `
		select coalesce(accept_invitation($1)::text, '')
	`, HashInvitationToken(token)).Scan(&orgID)
	if err != nil {
		return "", "", "", fmt.Errorf("postgres: accepting the invitation: %w", err)
	}
	if orgID == "" {
		return "", "", "", ErrInvitationNotUsable
	}

	// Re-read through RLS rather than trusting the definer function's word:
	// the membership now exists, so the organisation is visible by ordinary
	// policy, and if it somehow is not then the join did not happen.
	err = t.tx.QueryRow(ctx, `
		select o.name, m.role
		from organisations o
		join memberships m on m.org_id = o.id and m.user_id = $2
		where o.id = $1
	`, orgID, t.userID).Scan(&orgName, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", fmt.Errorf("postgres: invitation accepted but no membership is visible for %s", orgID)
	}
	if err != nil {
		return "", "", "", fmt.Errorf("postgres: reading the joined organisation: %w", err)
	}

	return orgID, orgName, role, nil
}

// HashInvitationToken is exported so whatever issues an invitation stores the
// same hash this redeems. One function, so the two halves cannot drift.
func HashInvitationToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
