package org

import "time"

// Member is one person in an organisation, as the members surface shows them.
//
// DisplayName and Email may both be empty and a caller must cope: the
// authorization server is not obliged to return a name, and neither is present
// for a member who has not signed in since user_identities gained a row. They
// are readable at all because of a decision taken on 2026-08-15 that members
// see each other within an organisation; see 00005_co_member_identity.sql.
type Member struct {
	UserID      string
	Role        string
	DisplayName string
	Email       string
	JoinedAt    time.Time
}

// Invitation is a pending offer of membership.
//
// No token field, deliberately. The raw token exists exactly twice: in the
// email that carries it and in the hash stored against this row. A struct that
// held it would invite something to log it.
type Invitation struct {
	ID        string
	Email     string
	Role      string
	ExpiresAt time.Time
}

// ValidRole reports whether a role is one of the three this system has.
//
// Checked in the handler as well as by the database's check constraint, and
// the duplication is deliberate: the constraint produces "new row violates
// check constraint memberships_role_check", which is a correct answer to the
// wrong audience. A caller sending "admin" deserves to be told the three
// values that exist.
func ValidRole(role string) bool {
	switch role {
	case RoleOwner, RoleMember, RoleViewer:
		return true
	default:
		return false
	}
}

// WouldLeaveNoOwner reports whether applying a change to `members` would leave
// the organisation with nobody who can administer it.
//
// `newRole` empty means removal; otherwise it is the role the member would be
// given. Both cases go through one function because they are one invariant,
// and checking only removal would be theatre: an owner who cannot be removed
// while last can simply be demoted to viewer and then removed, arriving at the
// same ownerless organisation by two steps instead of one.
//
// An organisation with no owner has nobody who can invite, change roles or
// manage billing, and no way back that does not involve an operator opening a
// database session. That is the failure this prevents.
//
// Pure, and takes the membership list rather than reading it, because the
// interesting part is a decision rather than a query (§21.6). It is also why
// this is tested without a database.
func WouldLeaveNoOwner(members []Member, userID, newRole string) bool {
	if newRole == RoleOwner {
		return false
	}

	var owners, affected int
	for _, m := range members {
		if m.Role != RoleOwner {
			continue
		}
		owners++
		if m.UserID == userID {
			affected++
		}
	}

	// Nothing is lost if the member being changed was not an owner to begin
	// with, however few owners there are.
	if affected == 0 {
		return false
	}
	return owners-affected == 0
}
