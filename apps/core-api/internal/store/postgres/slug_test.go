package postgres

import (
	"context"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// Slug minting at provisioning time (ENT-198).
//
// Against the real database for the same reason the rest of this file is: the
// property under test belongs to a unique constraint and to what two
// transactions do to each other, not to any expression in Go. In particular
// the collision path cannot be exercised at all without a database, because
// what drives it is a unique violation coming back from a statement.

// slugFor reads the slug of a subject's personal organisation, as the
// migrator, which bypasses RLS.
func slugFor(t *testing.T, subjectClaim string) string {
	t.Helper()

	userID, err := subject.UUID(testIssuer, subjectClaim)
	if err != nil {
		t.Fatalf("deriving the user id: %v", err)
	}

	var slug string
	err = migratorPool(t).QueryRow(context.Background(),
		`select slug from organisations where personal_owner_id = $1`, userID).Scan(&slug)
	if err != nil {
		t.Fatalf("reading the slug: %v", err)
	}
	return slug
}

func TestAPersonalOrganisationGetsASlugDerivedFromItsName(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-slug-derived"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	err := provision(context.Background(), store, org.Subject{
		Issuer:      testIssuer,
		Subject:     claim,
		DisplayName: "Ada Lovelace",
	})
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	if got := slugFor(t, claim); got != "ada-lovelace" {
		t.Fatalf("slug = %q, want %q", got, "ada-lovelace")
	}
}

// The acceptance criterion: two organisations created with the same name
// produce `name` and `name-2`, and both resolve.
//
// Two different people who happen to share a display name is the ordinary way
// this happens, and it must not be the way one of them fails to get an
// organisation. The assertion is on both slugs rather than on the absence of
// an error, because the failure mode being guarded against is the second
// insert being refused by the unique constraint, which surfaces as a failed
// sign-in for whoever arrives second.
func TestTwoPeopleWithTheSameNameBothGetAWorkingSlug(t *testing.T) {
	store := testStore(t)
	const first = "test-subject-slug-collision-one"
	const second = "test-subject-slug-collision-two"

	cleanup(t, first, second)
	t.Cleanup(func() { cleanup(t, first, second) })

	for _, claim := range []string{first, second} {
		err := provision(context.Background(), store, org.Subject{
			Issuer:      testIssuer,
			Subject:     claim,
			DisplayName: "Grace Hopper",
		})
		if err != nil {
			t.Fatalf("provisioning %s: %v", claim, err)
		}
	}

	if got := slugFor(t, first); got != "grace-hopper" {
		t.Fatalf("first slug = %q, want %q", got, "grace-hopper")
	}
	if got := slugFor(t, second); got != "grace-hopper-2" {
		t.Fatalf("second slug = %q, want %q", got, "grace-hopper-2")
	}
}

// The slug reaches the caller, since the console routes on it: a membership
// list without slugs cannot build a single link.
func TestMembershipsCarryTheSlug(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-slug-in-memberships"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	ctx := context.Background()
	if err := provision(ctx, store, org.Subject{
		Issuer:      testIssuer,
		Subject:     claim,
		DisplayName: "Katherine Johnson",
	}); err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	tenant, err := store.BeginTenant(ctx, claim, "")
	if err != nil {
		t.Fatalf("beginning the tenant transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	memberships, err := tenant.Memberships(ctx)
	if err != nil {
		t.Fatalf("reading memberships: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("memberships = %d, want 1", len(memberships))
	}
	if memberships[0].OrgSlug != "katherine-johnson" {
		t.Fatalf("slug = %q, want %q", memberships[0].OrgSlug, "katherine-johnson")
	}
}
