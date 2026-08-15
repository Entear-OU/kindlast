package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
)

// ErrNoSuchMember is returned when a role change or removal matched no row.
//
// Under RLS that is ambiguous by construction and deliberately so: the row may
// not exist, or it may exist in an organisation the caller cannot see. The
// caller is told the same thing either way, because distinguishing them would
// turn this into a way to ask whether a given person belongs to a given
// organisation.
var ErrNoSuchMember = errors.New("postgres: no such member in this organisation")

// InvitationLifetime is how long an invitation stays redeemable.
//
// Seven days: long enough to survive a holiday and a forwarded email, short
// enough that a link found in an old inbox has usually stopped working. The
// value matters less than it being finite, since the token is a bearer
// capability and an immortal one is a standing key to a customer's compliance
// record.
const InvitationLifetime = 7 * 24 * time.Hour

// Members lists everyone in the active organisation.
//
// The join is a LEFT join and the identity columns are coalesced, because a
// membership can legitimately outlive or precede an identity row: user_identities
// is written on sign-in, so a member invited but not yet returned has none.
// An inner join would silently drop exactly the people an owner most wants to
// see, which is a bug that looks like an empty state.
//
// No role check here. Reading the member list needs `org:read`, which the
// interceptor has already enforced, and RLS scopes the rows.
func (t *Tenant) Members(ctx context.Context) ([]org.Member, error) {
	rows, err := t.tx.Query(ctx, `
		select m.user_id::text,
		       m.role,
		       coalesce(i.display_name, ''),
		       coalesce(i.email, ''),
		       m.created_at
		from memberships m
		left join user_identities i on i.user_id = m.user_id
		where m.org_id = $1
		order by m.created_at, m.user_id
	`, t.orgID)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing members: %w", err)
	}
	defer rows.Close()

	var members []org.Member
	for rows.Next() {
		var m org.Member
		if err := rows.Scan(&m.UserID, &m.Role, &m.DisplayName, &m.Email, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("postgres: scanning a member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading members: %w", err)
	}
	return members, nil
}

// CreateOrganisation creates an organisation and makes the caller its owner.
//
// The id is generated here rather than by the database, and that is not a
// style preference. `insert ... returning` needs a SELECT policy to satisfy
// before it can hand the row back, and organisations_select_member requires a
// membership that does not exist until the next statement. So the id is known
// in advance, the membership is written, and only then is the row read back.
//
// Both writes are in the caller's transaction, so an organisation with no
// owner cannot survive a failure between them.
func (t *Tenant) CreateOrganisation(ctx context.Context, name string) (org.Joined, error) {
	orgID := uuid.NewString()

	if err := t.insertNamedOrganisation(ctx, orgID, name); err != nil {
		return org.Joined{}, err
	}

	// The bootstrap branch of memberships_insert_owner_or_bootstrap: a caller
	// may make themselves owner of an organisation that has no members yet.
	if _, err := t.tx.Exec(ctx, `
		insert into memberships (org_id, user_id, role)
		values ($1, $2, 'owner')
	`, orgID, t.userID); err != nil {
		return org.Joined{}, fmt.Errorf("postgres: taking ownership of the new organisation: %w", err)
	}

	var joined org.Joined
	if err := t.tx.QueryRow(ctx, `
		select id::text, name, slug from organisations where id = $1
	`, orgID).Scan(&joined.OrgID, &joined.OrgName, &joined.OrgSlug); err != nil {
		return org.Joined{}, fmt.Errorf("postgres: reading back the new organisation: %w", err)
	}
	joined.Role = org.RoleOwner
	return joined, nil
}

// insertNamedOrganisation is insertOrganisation's sibling for organisations
// that belong to no one person.
//
// Same savepoint-per-attempt shape and the same reason for it: a unique
// violation aborts the whole transaction, so without a savepoint the first
// slug collision would take the membership insert down with it. Pre-checking
// which slugs are free is not an option either, because under RLS such a check
// answers only about the organisations this caller can already see.
func (t *Tenant) insertNamedOrganisation(ctx context.Context, orgID, name string) error {
	for ordinal := 1; ordinal <= maxSlugAttempts; ordinal++ {
		savepoint, err := t.tx.Begin(ctx)
		if err != nil {
			return fmt.Errorf("postgres: opening a savepoint for the slug attempt: %w", err)
		}

		_, err = savepoint.Exec(ctx, `
			insert into organisations (id, name, slug)
			values ($1, $2, org_slug($2, $3))
		`, orgID, name, ordinal)

		if err != nil {
			_ = savepoint.Rollback(ctx)
			if isSlugCollision(err) {
				continue
			}
			return fmt.Errorf("postgres: creating the organisation: %w", err)
		}

		if err := savepoint.Commit(ctx); err != nil {
			return fmt.Errorf("postgres: committing the slug attempt: %w", err)
		}
		return nil
	}

	return fmt.Errorf(
		"postgres: no free slug for an organisation named %q after %d attempts",
		name, maxSlugAttempts)
}

// RenameOrganisation changes the active organisation's name.
//
// The slug is untouched, and not because it is awkward to change: it is in
// bookmarks and in emailed capability links, and a compliance product that
// breaks those has broken the audit trail's front door. `returning slug` makes
// the unchanged value visible to the caller rather than leaving them to assume.
//
// Owner-only, enforced by organisations_update_owner. A non-owner matches no
// row and gets ErrNoSuchMember's sibling treatment: pgx.ErrNoRows here means
// "you may not", and the handler turns it into a permission error.
func (t *Tenant) RenameOrganisation(ctx context.Context, name string) (org.Joined, error) {
	var joined org.Joined
	err := t.tx.QueryRow(ctx, `
		update organisations set name = $1, updated_at = now()
		where id = $2
		returning id::text, name, slug
	`, name, t.orgID).Scan(&joined.OrgID, &joined.OrgName, &joined.OrgSlug)

	if errors.Is(err, pgx.ErrNoRows) {
		return org.Joined{}, ErrNoSuchMember
	}
	if err != nil {
		return org.Joined{}, fmt.Errorf("postgres: renaming the organisation: %w", err)
	}
	joined.Role = t.role
	return joined, nil
}

// SetMemberRole changes one member's role in the active organisation.
//
// The last-owner rule is not enforced here. It is a decision about a set of
// members, so it belongs to the domain, and the handler applies it against a
// list it has just read inside this same transaction. Doing it in SQL would
// mean expressing "would this leave nobody" as a subquery nobody can read.
func (t *Tenant) SetMemberRole(ctx context.Context, userID, role string) error {
	tag, err := t.tx.Exec(ctx, `
		update memberships set role = $1 where org_id = $2 and user_id = $3
	`, role, t.orgID, userID)
	if err != nil {
		return fmt.Errorf("postgres: changing a member's role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSuchMember
	}
	return nil
}

// RemoveMember removes someone from the active organisation.
func (t *Tenant) RemoveMember(ctx context.Context, userID string) error {
	tag, err := t.tx.Exec(ctx, `
		delete from memberships where org_id = $1 and user_id = $2
	`, t.orgID, userID)
	if err != nil {
		return fmt.Errorf("postgres: removing a member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoSuchMember
	}
	return nil
}

// CreateInvitation records an invitation and returns everything except the
// token.
//
// The raw token is the caller's to deliver and is never stored: only its hash
// reaches the database, through the same HashInvitationToken the redeem path
// uses, so the two halves cannot drift.
func (t *Tenant) CreateInvitation(ctx context.Context, email, role, token string) (org.Invitation, error) {
	invitation := org.Invitation{Email: email, Role: role}
	err := t.tx.QueryRow(ctx, `
		insert into invitations (org_id, email, role, token_hash, invited_by, expires_at)
		values ($1, $2, $3, $4, $5, now() + $6::interval)
		returning id::text, expires_at
	`,
		t.orgID, email, role, HashInvitationToken(token), t.userID,
		fmt.Sprintf("%d seconds", int(InvitationLifetime.Seconds())),
	).Scan(&invitation.ID, &invitation.ExpiresAt)

	if err != nil {
		return org.Invitation{}, fmt.Errorf("postgres: creating the invitation: %w", err)
	}
	return invitation, nil
}
