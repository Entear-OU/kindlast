package findings

import (
	"testing"
	"time"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
)

// The last hop of ENT-162, and the one that was missing.
//
// The narrative layer exists (ENT-218), a job writes what it produces
// (ENT-245), and until this mapping carried the column the feed rendered a
// finding as though no run had ever happened. A store field nothing maps is
// indistinguishable on the wire from a store field nothing writes, which is
// exactly how this went unnoticed for two issues.
func TestTheNarrativeReachesTheWire(t *testing.T) {
	t.Parallel()

	out := toProto(domain.Finding{
		ID:               "f-1",
		Detected:         "No record of processing activities",
		ProposedAction:   "Put the missing control in place to satisfy this obligation.",
		Narrative:        "You process customer orders and support tickets, so Article 30 wants a written record of both.",
		AgentRunID:       "run-1",
		NarrativeRefusal: "",
		CreatedAt:        time.Now(),
	})

	if got := out.GetNarrative(); got != "You process customer orders and support tickets, so Article 30 wants a written record of both." {
		t.Fatalf("narrative did not travel: %q", got)
	}
	if got := out.GetAgentRunId(); got != "run-1" {
		t.Fatalf("the run that produced it did not travel: %q", got)
	}

	// The other half of ENT-164, asserted rather than assumed. The sweep's own
	// words are what the card renders as its heading, and a mapping that let
	// the narrative reach `detected` would put a paragraph there.
	if got := out.GetDetected(); got != "No record of processing activities" {
		t.Fatalf("the narrative displaced what the sweep detected: %q", got)
	}
	if got := out.GetProposedAction(); got != "Put the missing control in place to satisfy this obligation." {
		t.Fatalf("the narrative displaced the proposed action: %q", got)
	}
}

// A refusal is a fact the customer may read, so it travels too. The finding
// keeps every word the sweep wrote, which is what makes a refused run cost
// nothing.
func TestARefusalTravelsAndDisplacesNothing(t *testing.T) {
	t.Parallel()

	out := toProto(domain.Finding{
		ID:               "f-2",
		Detected:         "No AI register",
		ProposedAction:   "Put the missing control in place to satisfy this obligation.",
		NarrativeRefusal: "the draft cited GDPR Art. 50, which this finding is not about",
		AgentRunID:       "run-2",
		CreatedAt:        time.Now(),
	})

	if got := out.GetNarrativeRefusal(); got == "" {
		t.Fatal("the refusal did not travel, so a refused run is indistinguishable from no run")
	}
	if got := out.GetNarrative(); got != "" {
		t.Fatalf("a refused run produced a narrative on the wire: %q", got)
	}
	if got := out.GetDetected(); got != "No AI register" {
		t.Fatalf("a refusal changed what the card renders: %q", got)
	}
}

// The common case, and the one a rendering regression would show up in first:
// nothing has narrated this finding and the wire says exactly that.
func TestAnUnnarratedFindingCarriesNothingExtra(t *testing.T) {
	t.Parallel()

	out := toProto(domain.Finding{ID: "f-3", Detected: "No DPO named", CreatedAt: time.Now()})

	if out.GetNarrative() != "" || out.GetNarrativeRefusal() != "" || out.GetAgentRunId() != "" {
		t.Fatal("an unnarrated finding invented a narrative, a refusal or a run")
	}
}
