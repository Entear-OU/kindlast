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
