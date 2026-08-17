package notify

import (
	"strings"
	"testing"
)

// The link is the whole message. If it is wrong, the invitation is
// unrecoverable: the raw token existed for one handler and reissuing produces a
// different one, leaving the original row looking valid forever.

func TestInvitationLinkLeavesTheTokenByteIdentical(t *testing.T) {
	// A base64url token containing both characters that distinguish base64url
	// from base64. Percent-encoding, or a helper that "sanitises" the path,
	// corrupts exactly these, and only for the fraction of tokens that happen to
	// contain one, which is the kind of bug that reaches a customer before it
	// reaches a test.
	token := "abc-DEF_ghi123"

	got := InvitationLink("http://localhost:3000", token)

	if want := "http://localhost:3000/invite/abc-DEF_ghi123"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, token) {
		t.Fatalf("the token was altered: %q", got)
	}
}

func TestInvitationLinkDoesNotDoubleTheSlash(t *testing.T) {
	// Operators write base URLs with and without a trailing slash, and
	// `//invite/...` is a different path to a router that does not normalise.
	for _, base := range []string{
		"http://localhost:3000",
		"http://localhost:3000/",
		"http://localhost:3000///",
	} {
		if got := InvitationLink(base, "tok"); got != "http://localhost:3000/invite/tok" {
			t.Fatalf("base %q produced %q", base, got)
		}
	}
}

func TestValidBaseURL(t *testing.T) {
	for _, valid := range []string{
		"http://localhost:3000",
		"https://app.kindlast.com",
		"https://app.kindlast.com/console",
	} {
		if !ValidBaseURL(valid) {
			t.Fatalf("%q was rejected", valid)
		}
	}

	for _, invalid := range []string{
		"",
		"localhost:3000",   // no scheme: produces a relative link
		"app.kindlast.com", // ditto
		"ftp://host",       // not a scheme a browser follows from an email
		"http://",          // no host
	} {
		if ValidBaseURL(invalid) {
			t.Fatalf("%q was accepted, and would mint an invitation nobody can accept", invalid)
		}
	}
}

func TestInvitationNamesTheOrganisationAndCarriesTheLink(t *testing.T) {
	link := "http://localhost:3000/invite/tok"
	msg := Invitation("invitee@example.invalid", "Acme GmbH", link, 7)

	if msg.Kind != KindInvitation {
		t.Fatalf("kind is %q, want %q", msg.Kind, KindInvitation)
	}
	if msg.RecipientEmail != "invitee@example.invalid" {
		t.Fatalf("recipient is %q", msg.RecipientEmail)
	}
	if !strings.Contains(msg.Subject, "Acme GmbH") {
		t.Fatalf("the subject does not name the organisation: %q", msg.Subject)
	}
	if !strings.Contains(msg.BodyText, link) {
		t.Fatal("the body does not contain the link, so the invitation cannot be accepted")
	}
	if !strings.Contains(msg.BodyText, "7 days") {
		t.Fatalf("the body does not state the expiry: %q", msg.BodyText)
	}
	// The recipient may not be expecting this and it asks them to click a link
	// carrying a credential. Saying what to do if it was unexpected is what
	// separates it from a phishing attempt in the reader's mind.
	if !strings.Contains(msg.BodyText, "not expecting") {
		t.Fatal("the body does not tell an unexpecting recipient what to do")
	}
}

func TestInvitationWithNoOrganisationNameStillReads(t *testing.T) {
	// Unreachable through the service, since every organisation has a name. It
	// exists so the failure is a slightly vaguer sentence rather than "Join  on
	// Kindlast", which looks broken and therefore untrustworthy.
	msg := Invitation("x@example.invalid", "   ", "http://x/invite/t", 7)

	if strings.Contains(msg.Subject, "  ") {
		t.Fatalf("the subject has a hole where the name should be: %q", msg.Subject)
	}
	if !strings.Contains(msg.Subject, "an organisation") {
		t.Fatalf("no fallback in the subject: %q", msg.Subject)
	}
}
