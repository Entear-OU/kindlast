package org_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
)

// The decision, tested without a database, which is the payoff of it being a
// pure function. Everything here runs in microseconds and none of it needs the
// compose stack.

func TestASubjectWithNoMembershipGetsAPersonalOrganisation(t *testing.T) {
	plan := org.PlanFor(org.Subject{
		Issuer: "https://issuer.example", Subject: "sub-1",
		Email: "ada@example.com", DisplayName: "Ada Lovelace",
	}, nil)

	if !plan.CreatePersonalOrganisation {
		t.Fatal("a brand-new subject was given nothing")
	}
	if plan.Role != org.RoleOwner {
		t.Fatalf("role = %q, want owner: the organisation is theirs", plan.Role)
	}
	if plan.OrganisationName != "Ada Lovelace" {
		t.Fatalf("name = %q, want it derived from the display name", plan.OrganisationName)
	}
}

// The rule that stops an invited user acquiring a personal organisation
// alongside the one they were invited to. It is one line of code and the whole
// reason the §1.8 ordering matters.
func TestASubjectWhoAlreadyBelongsSomewhereGetsNothing(t *testing.T) {
	existing := []org.Membership{{OrgID: "org-1", OrgName: "Alpha", Role: org.RoleMember}}

	plan := org.PlanFor(org.Subject{Subject: "sub-1", Email: "invited@example.com"}, existing)

	if plan.CreatePersonalOrganisation {
		t.Fatal("an already-invited subject was also given a personal organisation")
	}
}

// Every branch has to produce something a human can pick out of an
// organisation switcher. A machine client has no name and no email, and a
// federated login may supply neither, so the fallbacks are the realistic cases
// rather than the exotic ones.
func TestThePersonalOrganisationNameAlwaysReadsAsSomething(t *testing.T) {
	cases := []struct {
		name    string
		subject org.Subject
		want    string
	}{
		{
			name:    "display name wins",
			subject: org.Subject{DisplayName: "Ada Lovelace", Email: "ada@example.com", Subject: "sub-1"},
			want:    "Ada Lovelace",
		},
		{
			name:    "falls back to the email local part",
			subject: org.Subject{Email: "ada.lovelace@example.com", Subject: "sub-1"},
			want:    "ada lovelace",
		},
		{
			name:    "address tags are not part of anyone's name",
			subject: org.Subject{Email: "ada+kindlast@example.com", Subject: "sub-1"},
			want:    "ada",
		},
		{
			name:    "falls back to the subject when there is nothing else",
			subject: org.Subject{Subject: "386089961457188867"},
			want:    "386089961457188867",
		},
		{
			name:    "never empty",
			subject: org.Subject{},
			want:    "Personal organisation",
		},
		{
			name:    "whitespace is not a name",
			subject: org.Subject{DisplayName: "   ", Email: "someone@example.com"},
			want:    "someone",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := org.PersonalOrganisationName(testCase.subject)
			if got != testCase.want {
				t.Fatalf("name = %q, want %q", got, testCase.want)
			}
		})
	}
}

// The name feeds the §20.1 slug, whose check constraint caps it at 63
// characters, so an unbounded name would produce an organisation nobody can
// route to once ENT-198 lands.
func TestThePersonalOrganisationNameIsBounded(t *testing.T) {
	long := ""
	for range 200 {
		long += "a"
	}

	got := org.PersonalOrganisationName(org.Subject{DisplayName: long})
	if len(got) > 63 {
		t.Fatalf("name is %d characters, want it bounded below the slug limit", len(got))
	}
}
