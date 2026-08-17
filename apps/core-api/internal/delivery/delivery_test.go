package delivery

import (
	"strings"
	"testing"
)

func TestNewSMTPRefusesAnUnusableAddress(t *testing.T) {
	// Caught at construction rather than at the first send, because the first
	// send happens hours after the deployment and surfaces as an invitation
	// that never arrived rather than as a configuration error.
	for _, addr := range []string{"", "mailpit", "mailpit:", ":1025"} {
		if _, err := NewSMTP(addr, "noreply@example.invalid"); err == nil {
			t.Fatalf("address %q was accepted", addr)
		}
	}
	if _, err := NewSMTP("mailpit:1025", ""); err == nil {
		t.Fatal("an empty sender was accepted")
	}
	if _, err := NewSMTP("mailpit:1025", "noreply@example.invalid"); err != nil {
		t.Fatalf("a valid configuration was rejected: %v", err)
	}
}

// Header injection is the security property in this file.
//
// The subject is built from an organisation name, which a customer chooses. SMTP
// is line-delimited, so a newline in a header value ends that header and begins
// whatever comes next, including a `Bcc:` the sender never wrote. That makes
// this attacker-controlled input reaching a protocol that trusts line breaks.
func TestRenderStripsHeaderInjectionFromTheSubject(t *testing.T) {
	s, err := NewSMTP("mailpit:1025", "noreply@example.invalid")
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}

	msg := Message{
		To:       "invitee@example.invalid",
		Subject:  "Invitation\r\nBcc: victim@example.invalid",
		BodyText: "body",
	}

	rendered := s.render(msg)
	headers, body, found := strings.Cut(rendered, "\r\n\r\n")
	if !found {
		t.Fatalf("no header/body separator in:\n%s", rendered)
	}

	// Checked per line, not with Contains. The literal text "Bcc: ..." sitting
	// inside a Subject value is harmless; what matters is whether a *line*
	// begins with it, because that is what makes it a header. An assertion on
	// the whole blob fails on the safe case and would have been "fixed" by
	// weakening it.
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("a Bcc header was injected through the subject:\n%s", headers)
		}
	}
	if lines := strings.Split(headers, "\r\n"); len(lines) != 6 {
		t.Fatalf("expected 6 header lines, got %d:\n%s", len(lines), headers)
	}
	// The subject survives as one line, with the break neutralised rather than
	// the message rejected: refusing here would strand an already-minted
	// invitation for a name the product accepted at creation.
	if !strings.Contains(headers, "Subject: Invitation  Bcc: victim@example.invalid") {
		t.Fatalf("the subject was not folded onto one line:\n%s", headers)
	}
	if body != "body" {
		t.Fatalf("body is %q", body)
	}
}

func TestRenderCarriesTheHeadersThatKeepMailOutOfSpam(t *testing.T) {
	// Date and MIME headers are not decoration. Mail without them scores as
	// spam at most receivers, which for an invitation means it lands in a
	// folder nobody opens while the send reports success either way.
	s, err := NewSMTP("mailpit:1025", "noreply@example.invalid")
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}

	rendered := s.render(Message{
		To: "invitee@example.invalid", Subject: "Hello", BodyText: "line one\nline two",
	})

	for _, header := range []string{
		"From: noreply@example.invalid",
		"To: invitee@example.invalid",
		"Subject: Hello",
		"Date: ",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
	} {
		if !strings.Contains(rendered, header) {
			t.Fatalf("missing %q in:\n%s", header, rendered)
		}
	}

	// RFC 5322 is CRLF-delimited. A bare LF in the body is accepted by some
	// servers and mangles the message at others.
	if strings.Contains(strings.ReplaceAll(rendered, "\r\n", ""), "\n") {
		t.Fatalf("the message contains a bare LF:\n%q", rendered)
	}
}

func TestSendRefusesAMessageWithNoRecipient(t *testing.T) {
	s, err := NewSMTP("mailpit:1025", "noreply@example.invalid")
	if err != nil {
		t.Fatalf("NewSMTP: %v", err)
	}
	// Returned as an error rather than silently skipped, so the row stays
	// pending with the reason recorded. Marking it sent would be a lie, and
	// `sent` on that table is evidence.
	if err := s.Send(t.Context(), Message{Subject: "s", BodyText: "b"}); err == nil {
		t.Fatal("a message with no recipient was accepted")
	}
}

func TestNameIsStable(t *testing.T) {
	// Appears in logs and, later, in whatever records which channel delivered a
	// message. Renaming it silently rewrites history.
	s, _ := NewSMTP("mailpit:1025", "noreply@example.invalid")
	if s.Name() != "smtp" {
		t.Fatalf("name is %q", s.Name())
	}
}
