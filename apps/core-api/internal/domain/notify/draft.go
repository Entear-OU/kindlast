package notify

// The Messenger's words, where they meet the message (ENT-280, §26.5).
//
// # THE DRAFT IS PROSE, AND ONLY PROSE
//
// A drafted doorbell replaces exactly two things: the subject line and the
// opening sentences. Everything a recipient can act on, the finding link, the
// one-tap approve link, the unsubscribe link, and the sentence saying why this
// arrived, is minted per recipient inside the delivery transaction and
// appended by FindingNotification after the draft. So a draft can neither
// remove an unsubscribe link nor add a link of its own, structurally, whatever
// it says.
//
// # WHY THE WORDS ARE CHECKED AGAIN HERE, HAVING BEEN CHECKED IN THE HARNESS
//
// The harness's LinkCritic, claim critic and house-style critic already
// refused everything AcceptableDraft refuses, before the words left the
// Python service. But the words then rode through a workflow history and a
// second service, and a value that travels and comes back is a value that
// could have been changed. The two checks are the guardrail and the
// invariant, the same pairing as the citation validator and core-api's slug
// check: they refuse the same things, and they disagree only when something
// is wrong, which is exactly when the one beside the send must win.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The bounds a recipient's client imposes, matching the harness's
// (message.MAX_SUBJECT and MAX_BODY in the Python service). A subject longer
// than this is truncated by every mail client into something that reads as
// broken; a body longer is a wall of text in a chat window.
const (
	MaxDraftSubject = 120
	MaxDraftBody    = 700
)

// The shapes a fabricated link takes. Anything with a scheme, and the two
// schemes that carry no slashes, and an email address. Deliberately not a list
// of allowed schemes: http, https, data, javascript and whatever an attacker
// invents are all equally not ours.
var (
	schemeLink  = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.\-]{1,31}://`)
	contactLink = regexp.MustCompile(`(?i)\b(?:mailto|tel|sms):`)
	address     = regexp.MustCompile(`(?i)[^\s@<>()\[\]]+@[a-z0-9.\-]+\.[a-z]{2,}`)
)

// AcceptableDraft says whether drafted words may be rendered into an outbound
// message, and why not.
//
// An error rather than a boolean, because the reason is stored on the row and
// read by an operator asking why a notification fell back to the template.
func AcceptableDraft(subject, body string) error {
	if strings.TrimSpace(subject) == "" {
		return errors.New("the draft has no subject")
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("the draft has no body")
	}
	if len(subject) > MaxDraftSubject {
		return fmt.Errorf("the subject is %d characters, and no more than %d survive a mail client's list view: too long", len(subject), MaxDraftSubject)
	}
	if len(body) > MaxDraftBody {
		return fmt.Errorf("the body is %d characters, and a doorbell is at most %d: too long", len(body), MaxDraftBody)
	}
	// A slice and not a map, so which half is named when both offend does not
	// depend on iteration order. The subject first: it is the line a
	// recipient reads without opening anything.
	for _, half := range []struct{ name, text string }{
		{"subject", subject},
		{"body", body},
	} {
		name, text := half.name, half.text
		// Control characters first: a newline in a subject is a header
		// injection attempt by the time SMTP sees it, and \r\n inside a body
		// is how a Bcc: line gets smuggled into something that concatenates
		// headers carelessly. The draft is prose; prose has no control
		// characters. (The template's own bodies have newlines, but those are
		// appended by this package, not accepted from a caller.)
		for _, r := range text {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("the %s contains a control character (U+%04X)", name, r)
			}
		}
		if schemeLink.MatchString(text) || contactLink.MatchString(text) {
			return fmt.Errorf("the %s contains a link, and every link a doorbell carries is minted per recipient after the draft", name)
		}
		if address.MatchString(text) {
			return fmt.Errorf("the %s contains an address, and moving somebody off their verified channel is not the draft's to offer", name)
		}
		// The house style's two characters, the same two AGENTS.md names and
		// prose.py refuses, written as escapes so this file obeys the rule it
		// enforces.
		if strings.ContainsAny(text, "\u2014\u2013") {
			return fmt.Errorf("the %s uses a dash character the house style does not allow", name)
		}
	}
	return nil
}
