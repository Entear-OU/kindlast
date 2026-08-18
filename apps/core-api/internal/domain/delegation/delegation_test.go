package delegation_test

import (
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
)

// The two rules that are decisions rather than invariants. Both are also
// enforced by 00021, and that is not duplication: these produce a readable
// refusal at the point somebody made the mistake, where the constraint produces
// a `check_violation` from inside an insert. Only the constraint binds a role
// that bypasses RLS, so only the constraint is the security boundary.
func TestWhatAnAgentMayCallItself(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		agent   string
		refused bool
	}{
		{"a skill", "analyst", false},
		{"a hyphenated one", "dashboard-rail", false},
		{"a channel", "email", false},
		{"empty", "", true},
		{"one character, which reads as a typo rather than a name", "a", true},
		{"shouting", "Analyst", true},
		// The one that matters. This value is rendered into an audit row a
		// customer reads, so anything that could be mistaken for a sentence, a
		// link or an instruction has to be refused before it is stored.
		{"a sentence", "the Analyst, on behalf of Ada", true},
		{"markup", "<script>alert(1)</script>", true},
		{"a leading digit", "1analyst", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := delegation.Mint{ActingAgent: c.agent}.Validate()
			if c.refused && err == nil {
				t.Fatalf("%q was accepted as an agent name", c.agent)
			}
			if !c.refused && err != nil {
				t.Fatalf("%q was refused: %v", c.agent, err)
			}
		})
	}
}

func TestALifetimeIsRefusedRatherThanClamped(t *testing.T) {
	t.Parallel()

	// Refused, not clamped. A caller asking for two hours has a bug, and a
	// silently shortened delegation would surface as a run that stops working
	// half way through with nothing pointing at the request that caused it.
	if err := (delegation.Mint{
		ActingAgent: "analyst", TTL: 2 * time.Hour,
	}).Validate(); err == nil {
		t.Fatal("a two hour delegation was accepted")
	}

	if err := (delegation.Mint{
		ActingAgent: "analyst", TTL: -time.Minute,
	}).Validate(); err == nil {
		t.Fatal("a negative delegation was accepted")
	}

	if err := (delegation.Mint{
		ActingAgent: "analyst", TTL: delegation.MaxTTL,
	}).Validate(); err != nil {
		t.Fatalf("the ceiling itself was refused: %v", err)
	}
}

func TestSayingNothingAsksForMinutes(t *testing.T) {
	t.Parallel()

	if got := (delegation.Mint{ActingAgent: "analyst"}).Lifetime(); got != delegation.DefaultTTL {
		t.Fatalf("an unset lifetime resolved to %s, want %s", got, delegation.DefaultTTL)
	}
	if delegation.DefaultTTL >= delegation.MaxTTL {
		t.Fatal("the default is not shorter than the ceiling, so the ceiling means nothing")
	}
}

// §8's approve link, as a shape rather than as a redemption path (ENT-249).
func TestABindingToAFindingMeansSingleUse(t *testing.T) {
	t.Parallel()

	// Refused rather than corrected, for the same reason a two hour lifetime
	// is. Somebody asking for a reusable approve link has misunderstood what it
	// is, and quietly setting the flag would ship that misunderstanding into a
	// mailbox. 00027 refuses the row too, and that is the boundary; this is the
	// message at the point the mistake was made.
	if err := (delegation.Mint{
		ActingAgent: "email", FindingID: "f-1",
	}).Validate(); err == nil {
		t.Fatal("a finding-bound delegation was accepted as reusable")
	}

	if err := (delegation.Mint{
		ActingAgent: "email", FindingID: "f-1", SingleUse: true,
	}).Validate(); err != nil {
		t.Fatalf("a single-use approve link was refused: %v", err)
	}

	// And the run delegation is untouched: many tool calls under one
	// credential is what the rail needs, and conflating the two would break it
	// on its second call.
	if err := (delegation.Mint{ActingAgent: "analyst"}).Validate(); err != nil {
		t.Fatalf("a run delegation was refused: %v", err)
	}
}

func TestTheApproveLinkAgreesWithItself(t *testing.T) {
	t.Parallel()

	mint := delegation.Approval("f-1")

	if err := mint.Validate(); err != nil {
		t.Fatalf("the constructor built something the rules refuse: %v", err)
	}
	if !mint.SingleUse {
		t.Fatal("an approve link that can be redeemed twice can approve twice")
	}
	if mint.FindingID != "f-1" {
		t.Fatalf("the delegation is bound to %q rather than to the finding", mint.FindingID)
	}
	// The channel, which is what the audit row will name beside the person.
	// §26.3 asks the trail to say what was holding the pen, and for this path
	// the honest answer is the medium the decision arrived through.
	if mint.ActingAgent != delegation.EmailChannel {
		t.Fatalf("the audit row would name %q rather than the channel", mint.ActingAgent)
	}
	// The uncomfortable one, asserted so that changing it is a decision rather
	// than a drift. See the constructor for why the ceiling wins over the
	// argument that people read compliance mail late.
	if mint.Lifetime() != delegation.MaxTTL {
		t.Fatalf("an approve link lives %s, want the %s ceiling",
			mint.Lifetime(), delegation.MaxTTL)
	}
}
