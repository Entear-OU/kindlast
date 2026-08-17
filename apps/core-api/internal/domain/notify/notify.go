// Package notify holds the shape of a transactional message and the rules for
// rendering one.
//
// Pure functions over already-loaded data, with no database, no SMTP and no
// proto, for the same reason `domain/findings` is written that way: the wording
// of an invitation is a product decision, and a decision that can only be
// exercised by sending mail is one nobody tests.
//
// A transactional message is not a notification. A notification is a doorbell
// whose recipient is resolved at delivery time from memberships and
// preferences, and which may reasonably be skipped. This is a message that
// carries a secret which exists only at mint, addressed to somebody who may not
// have an account, and losing it is unrecoverable rather than inconvenient
// (ENT-219).
package notify

import (
	"fmt"
	"net/url"
	"strings"
)

// KindInvitation is the only kind that exists today. It matches the check
// constraint on `transactional_outbox.kind`, and adding a kind here without
// widening that constraint produces a row the database refuses.
const KindInvitation = "invitation"

// Message is one queued transactional message.
type Message struct {
	Kind           string
	RecipientEmail string
	Subject        string
	BodyText       string
	// Optional. A text-only message is deliverable and readable everywhere; an
	// HTML-only one is neither in a client that will not render it.
	BodyHTML string
}

// InvitationLink builds the URL an invitee follows to accept.
//
// The shape is fixed by `apps/web/app/invite/[token]/route.ts`: the token
// occupies one path segment, which is why it is minted as unpadded base64url
// and why nothing here escapes or re-encodes it. Running it through
// url.PathEscape would corrupt a token containing `-` or `_`, and the corruption
// would only show for the fraction of tokens that happen to contain one.
func InvitationLink(baseURL, token string) string {
	return strings.TrimRight(baseURL, "/") + "/invite/" + token
}

// ValidBaseURL reports whether a base URL is usable for building a link.
//
// Checked rather than assumed because the failure is silent and permanent: a
// malformed base produces a link that 404s, the invitation is already minted,
// and the raw token is gone, so nobody can reissue the same one.
func ValidBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// Invitation renders the message sent to somebody invited to an organisation.
//
// The copy is deliberately plain. An invitation to a compliance workspace is
// something the recipient may not be expecting, from a product they may not
// have heard of, and it asks them to click a link carrying a credential. So it
// names who is inviting them and to what, states the expiry, and says what to
// do if it was not expected. A cheerful marketing tone here reads as phishing,
// which is the one impression this particular email cannot afford.
func Invitation(recipientEmail, orgName, link string, expiresInDays int) Message {
	org := strings.TrimSpace(orgName)
	if org == "" {
		// Every organisation has a name, so this is unreachable through the
		// service. It exists because the alternative on an empty string is a
		// subject line reading "Join  on Kindlast", and a message that looks
		// broken is one nobody trusts enough to click.
		org = "an organisation"
	}

	subject := fmt.Sprintf("You have been invited to join %s on Kindlast", org)

	text := strings.Join([]string{
		fmt.Sprintf("You have been invited to join %s on Kindlast, a compliance", org),
		"workspace for GDPR and the EU AI Act.",
		"",
		"Open this link to accept:",
		link,
		"",
		fmt.Sprintf("The link expires in %d days and can be used once.", expiresInDays),
		"",
		"If you were not expecting this invitation you can ignore this message.",
		"Nothing happens until the link is opened.",
	}, "\n")

	return Message{
		Kind:           KindInvitation,
		RecipientEmail: recipientEmail,
		Subject:        subject,
		BodyText:       text,
	}
}
