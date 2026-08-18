package redact_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/workers/internal/redact"
)

// Redaction runs before storage, and a fixture with a known pattern comes out
// redacted (ENT-231 acceptance criterion).
//
// The fixture is a plausible MCP tool result rather than a string of test
// patterns: a helpdesk ticket, which is exactly the shape of thing a customer
// connects and exactly where a credential ends up pasted by accident.

const ticketFixture = `{
  "ticket": {
    "id": 4821,
    "subject": "Cannot access my data",
    "requester": "ada.lovelace@example.com",
    "body": "Hi, I pay by card 4111 1111 1111 1111 and my account is GB29NWBK60161331926819. Our integration uses api_key: sk_live_9f8a7b6c5d4e3f2a1b0c and it stopped working.",
    "created_at": "2026-03-04T09:12:00Z",
    "tags": ["billing", "access"]
  }
}`

func TestAKnownPIIPatternIsRedactedBeforeItCanBeStored(t *testing.T) {
	result := redact.JSON(ticketFixture)

	for _, secret := range []string{
		"ada.lovelace@example.com",
		"4111 1111 1111 1111",
		"GB29NWBK60161331926819",
		"sk_live_9f8a7b6c5d4e3f2a1b0c",
	} {
		if strings.Contains(result.Text, secret) {
			t.Errorf("the redacted output still contains %q", secret)
		}
	}

	if result.Count == 0 {
		t.Fatal("nothing was counted as redacted, but the fixture carries four values")
	}
	if !strings.Contains(result.Text, redact.Marker) {
		t.Error("no marker in the output; a redacted value must be replaced rather than deleted")
	}
}

// The guard is only worth having if it can fail. The same fixture with the
// values removed must produce no redactions, so a Count that was hard-wired to
// something non-zero goes red here.
func TestTheRedactorCountsNothingWhenThereIsNothingToFind(t *testing.T) {
	clean := `{"ticket":{"id":4821,"subject":"Cannot access my data","tags":["billing"]}}`

	result := redact.JSON(clean)
	if result.Count != 0 {
		t.Fatalf("counted %d redactions in a document with none", result.Count)
	}
	if strings.Contains(result.Text, redact.Marker) {
		t.Errorf("a marker appeared in a clean document: %s", result.Text)
	}
}

// The shape survives, which is what running over the decoded value buys.
//
// Redacting the serialised text would let a pattern span a quote or a brace
// and produce output that is no longer JSON, and the observation would then be
// unparseable rather than redacted.
func TestTheDocumentIsStillJSONAfterwards(t *testing.T) {
	result := redact.JSON(ticketFixture)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(result.Text), &decoded); err != nil {
		t.Fatalf("the redacted output is not JSON: %v", err)
	}

	ticket, ok := decoded["ticket"].(map[string]any)
	if !ok {
		t.Fatal("the ticket object did not survive redaction")
	}
	// Numbers, and the fields that carry no secret, are untouched. A redactor
	// that flattened the useful content would be a redactor nobody could
	// derive a finding from.
	if id, _ := ticket["id"].(float64); id != 4821 {
		t.Errorf("the ticket id changed: %v", ticket["id"])
	}
	if subject, _ := ticket["subject"].(string); subject != "Cannot access my data" {
		t.Errorf("the subject changed: %v", ticket["subject"])
	}
}

// Field names survive. Redacting a key would rename a field, which changes
// what a document means rather than removing a value from it.
func TestKeysAreNotRedacted(t *testing.T) {
	result := redact.JSON(`{"api_key":"sk_live_9f8a7b6c5d4e3f2a1b0c"}`)

	if !strings.Contains(result.Text, `"api_key"`) {
		t.Errorf("the key was redacted along with its value: %s", result.Text)
	}
	if strings.Contains(result.Text, "sk_live_9f8a7b6c5d4e3f2a1b0c") {
		t.Errorf("the value was not redacted: %s", result.Text)
	}
}

// A tool that answers with a plain string rather than a JSON document is
// ordinary, and it is redacted as text rather than refused.
func TestNonJSONInputIsRedactedAsText(t *testing.T) {
	result := redact.Text("contact grace.hopper@example.com about Bearer abcdefghijklmnop")

	if strings.Contains(result.Text, "grace.hopper@example.com") {
		t.Error("the address survived")
	}
	if strings.Contains(result.Text, "abcdefghijklmnop") {
		t.Error("the bearer token survived")
	}
	if result.Count != 2 {
		t.Errorf("counted %d redactions, want 2", result.Count)
	}
}

// A private key block goes whole, rather than leaving its header and footer
// wrapped around a marker.
func TestAPrivateKeyBlockIsRemovedWhole(t *testing.T) {
	input := "before\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nIBAAKC\n-----END RSA PRIVATE KEY-----\nafter"

	result := redact.Text(input)
	if strings.Contains(result.Text, "MIIEow") {
		t.Error("the key material survived")
	}
	if strings.Contains(result.Text, "BEGIN RSA PRIVATE KEY") {
		t.Error("the block header survived, so the block was matched only in part")
	}
	if !strings.Contains(result.Text, "before") || !strings.Contains(result.Text, "after") {
		t.Errorf("content around the block was taken with it: %s", result.Text)
	}
}

// The prose this product exists to reason over survives, which is the half of
// the design that a keener redactor would destroy.
func TestOrdinaryComplianceProseIsLeftAlone(t *testing.T) {
	prose := "Our support team handles subject access requests within 30 days and logs them in the register."

	result := redact.Text(prose)
	if result.Text != prose {
		t.Errorf("prose was altered:\n got %q\nwant %q", result.Text, prose)
	}
	if result.Count != 0 {
		t.Errorf("counted %d redactions in ordinary prose", result.Count)
	}
}
