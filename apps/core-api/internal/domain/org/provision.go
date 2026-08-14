// Package org holds this service's rules about organisations and membership.
//
// No I/O, and dependencies point inward (§21.6): this package imports no
// store, no database driver and no Connect types. If a rule here needed a
// query to answer a question, the question would be being asked in the wrong
// layer.
//
// The reason provisioning is a pure function rather than a method on a store
// is that the interesting part is a decision, not a write. "Given this subject
// and what already exists, what should exist" is exactly the shape that is
// hard to test through a database and trivial to test directly.
package org

import (
	"strings"
	"unicode"
)

// Role values. Three, and no more: owner manages billing and members, member
// approves findings and edits records, viewer reads. Approval authority is a
// regulatory-relevant fact, so it earns a role boundary; nothing else does yet
// (§20.1).
const (
	RoleOwner  = "owner"
	RoleMember = "member"
	RoleViewer = "viewer"
)

// Subject is the verified identity, straight from the token's claims.
type Subject struct {
	Issuer      string
	Subject     string
	Email       string
	DisplayName string
}

// Membership is one row of what already exists for a subject.
type Membership struct {
	OrgID   string
	OrgName string
	// OrgSlug is the URL segment the console routes on (§20.1, ENT-198).
	// Derived from the name when the organisation is created and never
	// recomputed, so it does not follow a rename.
	OrgSlug string
	Role    string
}

// Joined is the organisation an invitation just admitted someone to.
//
// Carries the slug as well as the id because the caller's next move is a
// redirect into it, and the URL is built from the slug (ENT-198).
type Joined struct {
	OrgID   string
	OrgName string
	OrgSlug string
	Role    string
}

// Plan is what provisioning decided should exist. It is a description, not an
// action: the store carries it out, and the handler re-reads afterwards rather
// than trusting it.
type Plan struct {
	// CreatePersonalOrganisation is false whenever the subject already belongs
	// somewhere, which is what makes the whole path idempotent on the sub
	// claim.
	CreatePersonalOrganisation bool

	// OrganisationName for the organisation to create. Empty when nothing is
	// to be created.
	OrganisationName string

	// Role the subject takes in it. Always owner: it is theirs.
	Role string
}

// PlanFor decides what a subject arriving now should end up with.
//
// The rule is one line and the consequence of getting it wrong is not: a
// subject who already belongs to any organisation gets nothing new. That is
// what stops an invited user acquiring a personal organisation alongside the
// one they were invited to, and it is why the ordering in §1.8 matters, with
// invitation accept running before the first GetCurrentUser.
func PlanFor(subject Subject, existing []Membership) Plan {
	if len(existing) > 0 {
		return Plan{}
	}
	return Plan{
		CreatePersonalOrganisation: true,
		OrganisationName:           PersonalOrganisationName(subject),
		Role:                       RoleOwner,
	}
}

// PersonalOrganisationName derives a name for the organisation created on
// first arrival.
//
// Derived rather than asked for, because the alternative is a form standing
// between a new user and the product, to name something most of them will
// never rename. §20.1 derives the URL slug from this name by the same
// principle, which is ENT-198's work.
//
// The fallbacks matter more than the happy path. A machine client has no name
// and no email; a federated login may supply neither. Every branch has to
// produce something a human can read in an organisation switcher, because the
// alternative is a blank entry nobody can identify.
func PersonalOrganisationName(subject Subject) string {
	if name := clean(subject.DisplayName); name != "" {
		return name
	}
	if local := clean(emailLocalPart(subject.Email)); local != "" {
		return local
	}
	// The subject claim is opaque and often a long integer, so it makes a poor
	// label, but it is unique and always present. Better a name that looks
	// like an id than an empty one.
	if sub := clean(subject.Subject); sub != "" {
		return sub
	}
	return "Personal organisation"
}

func emailLocalPart(email string) string {
	local, _, found := strings.Cut(email, "@")
	if !found {
		return ""
	}
	// Address tags (`ada+kindlast@example.com`) are not part of anyone's name.
	local, _, _ = strings.Cut(local, "+")
	// Separators are conventions of the address, not of the person.
	local = strings.Map(func(r rune) rune {
		if r == '.' || r == '_' || r == '-' {
			return ' '
		}
		return r
	}, local)
	return local
}

// clean collapses whitespace and bounds the length.
//
// The bound is not decoration: the name feeds the §20.1 slug, whose check
// constraint caps it at 63 characters, and an organisation nobody can route to
// is worse than one with a shortened name.
func clean(value string) string {
	fields := strings.FieldsFunc(value, unicode.IsSpace)
	name := strings.Join(fields, " ")

	const maxLength = 60
	if len(name) > maxLength {
		name = strings.TrimSpace(name[:maxLength])
	}
	return name
}
