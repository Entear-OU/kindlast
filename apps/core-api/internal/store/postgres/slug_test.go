package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// Every organisation name in this file carries this token (ENT-217).
//
// `organisations.slug` is globally unique rather than tenant-scoped, and
// `go test ./...` runs packages concurrently against one database. A fixture
// that hardcodes a plain name is therefore competing for a single row in a
// single namespace with every other package that hardcodes the same one, and
// whichever loses is handed `name-2` by the collision retry. The retry is
// behaving correctly; the defect is asserting on a value you do not own.
//
// This is not hypothetical. This package and internal/server/interceptor both
// provisioned a "Grace Hopper", and the module suite failed five runs out of
// six before this token existed.
//
// A new test that asserts an exact slug needs the same treatment, and a
// different package needs a different token.
const slugToken = "storepg"

// fixtureName returns a display name no other package is competing for, and
// the slug org_slug() will derive from it.
//
// It deliberately does not reimplement org_slug(). That rule lives in SQL
// exactly once (ENT-198), and a second copy in Go is precisely how the two
// drift into minting different permanent URLs. The inputs are chosen instead
// so the expected output needs no derivation: `base` is plain ASCII words and
// the token is already lowercase alphanumeric, so the slug is the lowercased
// base with its spaces hyphenated, plus the token.
func fixtureName(base string) (name, slug string) {
	return base + " " + slugToken,
		strings.ToLower(strings.ReplaceAll(base, " ", "-")) + "-" + slugToken
}

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

	name, wantSlug := fixtureName("Ada Lovelace")
	err := provision(context.Background(), store, org.Subject{
		Issuer:      testIssuer,
		Subject:     claim,
		DisplayName: name,
	})
	if err != nil {
		t.Fatalf("provisioning: %v", err)
	}

	if got := slugFor(t, claim); got != wantSlug {
		t.Fatalf("slug = %q, want %q", got, wantSlug)
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

	name, wantSlug := fixtureName("Grace Hopper")
	for _, claim := range []string{first, second} {
		err := provision(context.Background(), store, org.Subject{
			Issuer:      testIssuer,
			Subject:     claim,
			DisplayName: name,
		})
		if err != nil {
			t.Fatalf("provisioning %s: %v", claim, err)
		}
	}

	if got := slugFor(t, first); got != wantSlug {
		t.Fatalf("first slug = %q, want %q", got, wantSlug)
	}
	if got, want := slugFor(t, second), wantSlug+"-2"; got != want {
		t.Fatalf("second slug = %q, want %q", got, want)
	}
}

// The slug reaches the caller, since the console routes on it: a membership
// list without slugs cannot build a single link.
func TestMembershipsCarryTheSlug(t *testing.T) {
	store := testStore(t)
	const claim = "test-subject-slug-in-memberships"

	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	name, wantSlug := fixtureName("Katherine Johnson")
	ctx := context.Background()
	if err := provision(ctx, store, org.Subject{
		Issuer:      testIssuer,
		Subject:     claim,
		DisplayName: name,
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
	if memberships[0].OrgSlug != wantSlug {
		t.Fatalf("slug = %q, want %q", memberships[0].OrgSlug, wantSlug)
	}
}
