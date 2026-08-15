package org_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
)

func TestValidRoleAcceptsExactlyTheThreeRoles(t *testing.T) {
	for _, role := range []string{org.RoleOwner, org.RoleMember, org.RoleViewer} {
		if !org.ValidRole(role) {
			t.Errorf("ValidRole(%q) = false, want true", role)
		}
	}
	// "admin" is the one a caller reaches for by habit, and "" is what a
	// client sends when it forgot the field.
	for _, role := range []string{"admin", "Owner", "OWNER", "superuser", ""} {
		if org.ValidRole(role) {
			t.Errorf("ValidRole(%q) = true; only three roles exist", role)
		}
	}
}

func TestWouldLeaveNoOwner(t *testing.T) {
	const ada, miko, bob = "ada", "miko", "bob"

	twoOwners := []org.Member{
		{UserID: ada, Role: org.RoleOwner},
		{UserID: miko, Role: org.RoleOwner},
		{UserID: bob, Role: org.RoleViewer},
	}
	oneOwner := []org.Member{
		{UserID: ada, Role: org.RoleOwner},
		{UserID: miko, Role: org.RoleMember},
		{UserID: bob, Role: org.RoleViewer},
	}

	cases := []struct {
		name    string
		members []org.Member
		userID  string
		newRole string
		want    bool
	}{
		{"removing the only owner", oneOwner, ada, "", true},
		{"removing one of two owners", twoOwners, ada, "", false},
		{"removing a member", oneOwner, miko, "", false},
		{"removing a viewer", oneOwner, bob, "", false},

		// The reason removal and demotion share a function. Guarding only
		// removal would let the same end state be reached in two steps.
		{"demoting the only owner to viewer", oneOwner, ada, org.RoleViewer, true},
		{"demoting the only owner to member", oneOwner, ada, org.RoleMember, true},
		{"demoting one of two owners", twoOwners, ada, org.RoleViewer, false},

		// Promotion can never remove an owner, including re-affirming one.
		{"promoting a member to owner", oneOwner, miko, org.RoleOwner, false},
		{"setting the only owner to owner again", oneOwner, ada, org.RoleOwner, false},

		// A member who is not in the list at all changes nothing. This is the
		// concurrent case: someone else removed them between the read and the
		// write, and the answer must not be "refuse because the count looks
		// wrong".
		{"acting on a stranger", oneOwner, "nobody", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := org.WouldLeaveNoOwner(c.members, c.userID, c.newRole); got != c.want {
				t.Fatalf("WouldLeaveNoOwner(_, %q, %q) = %v, want %v",
					c.userID, c.newRole, got, c.want)
			}
		})
	}
}

// An organisation whose membership list is empty cannot lose an owner it does
// not have. Worth its own test because the loop's arithmetic (owners minus
// affected) is the kind that reads as correct and returns true on zero.
func TestWouldLeaveNoOwnerOnAnEmptyOrganisation(t *testing.T) {
	if org.WouldLeaveNoOwner(nil, "ada", "") {
		t.Fatal("an empty organisation has no owner to lose")
	}
}
