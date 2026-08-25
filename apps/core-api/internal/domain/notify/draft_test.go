package notify

import (
	"strings"
	"testing"
)

// The Messenger's words, where they meet the message (ENT-280).
//
// Two properties, and they are the two halves of ENT-260's title. The drafted
// prose replaces only the template's opening: every link, and the sentence
// saying why this arrived, are still appended here, per recipient, so a draft
// cannot remove an unsubscribe link or add a link of its own. And the words
// are checked again before they are rendered, because they rode through a
// workflow history and a second service, and a value that travels and comes
// back is a value that could have been changed. The harness's critics are the
// guardrail; AcceptableDraft is the invariant.

func drafted() Doorbell {
	return Doorbell{
		RecipientEmail: "cco@acme.example",
		OrgName:        "Acme Ltd",
		Severity:       "high",
		FindingURL:     "http://localhost:3000/o/acme/feed/f1",
		UnsubscribeURL: "http://localhost:3000/unsubscribe/tok",
		ApproveURL:     "http://localhost:3000/approve/f1/tok2",
		DraftedSubject: "A serious finding is waiting on you in Acme Ltd",
		DraftedBody: "Something in Acme Ltd's compliance record needs a " +
			"decision from you, and it is the most serious of the five now open.",
	}
}

func TestADraftedDoorbellUsesTheDraftedWords(t *testing.T) {
	msg := FindingNotification(drafted())

	if msg.Subject != "A serious finding is waiting on you in Acme Ltd" {
		t.Fatalf("the drafted subject was not used: %q", msg.Subject)
	}
	if !strings.HasPrefix(msg.BodyText, "Something in Acme Ltd's") {
		t.Fatalf("the drafted body does not open the message:\n%s", msg.BodyText)
	}
	// And the template's opening sentence is gone, not doubled. A message
	// carrying both is two authors talking over each other.
	if strings.Contains(msg.BodyText, "Kindlast has raised a") {
		t.Fatalf("the template opening survived alongside the draft:\n%s", msg.BodyText)
	}
}

func TestTheDraftReplacesOnlyTheProse(t *testing.T) {
	// The whole reason a model may write these words at all: everything a
	// recipient can act on is still minted and appended here, after the draft.
	msg := FindingNotification(drafted())

	for _, want := range []string{
		"http://localhost:3000/o/acme/feed/f1",
		"http://localhost:3000/unsubscribe/tok",
		"http://localhost:3000/approve/f1/tok2",
		"You are receiving this because",
	} {
		if !strings.Contains(msg.BodyText, want) {
			t.Fatalf("a drafted message lost %q:\n%s", want, msg.BodyText)
		}
	}
}

func TestADoorbellWithNoDraftIsTheTemplateUnchanged(t *testing.T) {
	// The ordinary case, and forever the case for a deployment with no model.
	d := drafted()
	d.DraftedSubject = ""
	d.DraftedBody = ""

	msg := FindingNotification(d)

	if !strings.Contains(msg.Subject, "high") || !strings.Contains(msg.Subject, "Acme Ltd") {
		t.Fatalf("the template subject regressed: %q", msg.Subject)
	}
	if !strings.Contains(msg.BodyText, "Kindlast has raised a high finding for Acme Ltd.") {
		t.Fatalf("the template opening regressed:\n%s", msg.BodyText)
	}
}

func TestHalfADraftRendersAsNoDraft(t *testing.T) {
	// The RPC refuses a half-draft before this function ever sees one; this is
	// the rendering behaving safely anyway, because a renderer that trusts its
	// caller's validation is one bug away from a subject with no body behind
	// it.
	for name, d := range map[string]Doorbell{
		"subject only": func() Doorbell { d := drafted(); d.DraftedBody = ""; return d }(),
		"body only":    func() Doorbell { d := drafted(); d.DraftedSubject = ""; return d }(),
	} {
		msg := FindingNotification(d)
		if !strings.Contains(msg.BodyText, "Kindlast has raised a") {
			t.Fatalf("%s: half a draft was rendered rather than ignored:\n%s", name, msg.BodyText)
		}
	}
}

func TestAcceptableDraftRefusesWhatTheCriticsRefuse(t *testing.T) {
	// The same three rules the harness enforces, restated in Go beside the
	// send, because this side must hold even if the far side changes. Each
	// case names what a compromised or drifted upstream would try.
	cases := map[string]struct {
		subject string
		body    string
		want    string
	}{
		"a link with a scheme": {
			subject: "A finding is waiting",
			body:    "Open https://phish.example/login to decide.",
			want:    "link",
		},
		"a link in the subject": {
			subject: "Act now at http://phish.example",
			body:    "Something needs a decision.",
			want:    "link",
		},
		"a mailto": {
			subject: "A finding is waiting",
			body:    "Reply to mailto:ceo@phish.example with your password.",
			want:    "link",
		},
		"an email address": {
			subject: "A finding is waiting",
			body:    "Contact helpdesk@phish.example instead.",
			want:    "address",
		},
		"an em dash": {
			subject: "A finding is waiting",
			body:    "Something needs you \u2014 decide today.",
			want:    "dash",
		},
		"an en dash": {
			subject: "A finding \u2013 act now",
			body:    "Something needs a decision.",
			want:    "dash",
		},
		"a control character": {
			subject: "A finding is waiting",
			body:    "Something\r\nBcc: everyone@phish.example",
			want:    "control",
		},
		"an oversize subject": {
			subject: strings.Repeat("x", 121),
			body:    "Something needs a decision.",
			want:    "long",
		},
		"an oversize body": {
			subject: "A finding is waiting",
			body:    strings.Repeat("y", 701),
			want:    "long",
		},
	}
	for name, c := range cases {
		err := AcceptableDraft(c.subject, c.body)
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: the reason %q does not name %q", name, err.Error(), c.want)
		}
	}
}

func TestAcceptableDraftAcceptsOrdinaryCopy(t *testing.T) {
	// A guard that fires on prose is one somebody eventually switches off, so
	// the false-positive cases are asserted rather than hoped for.
	for _, body := range []string{
		"Something in Acme Ltd's compliance record needs a decision from you.",
		"Four others are open.The most serious is this one.",
		"Decide within 2-4 days.",
		"Acme Ltd. has one finding at high severity.",
	} {
		if err := AcceptableDraft("A high severity finding is waiting in Acme Ltd", body); err != nil {
			t.Fatalf("refused ordinary copy %q: %v", body, err)
		}
	}
}

func TestAcceptableDraftRefusesTheEmptyHalves(t *testing.T) {
	if err := AcceptableDraft("", "Something."); err == nil {
		t.Fatal("an empty subject was accepted")
	}
	if err := AcceptableDraft("Something", ""); err == nil {
		t.Fatal("an empty body was accepted")
	}
}
