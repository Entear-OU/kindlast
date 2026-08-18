package corpus

import (
	"strings"
	"testing"
)

// TestEveryApplicabilityTokenHasAPhrase pins the phrase table to the
// vocabulary, in both directions (ENT-248).
//
// A token with no phrase is silently dropped, so an obligation's real grounds
// reach the model as a shorter list than it has. A model handed a short list
// fills the rest in from what it remembers about the regulation, which is the
// exact failure this whole file exists to stop. A phrase with no token is the
// mirror: dead copy that reads as coverage.
//
// Asserted rather than trusted because the two lists live in different files
// and a new token is added to `applieswhen.go` by somebody thinking about the
// Watcher, not about the Analyst.
func TestEveryApplicabilityTokenHasAPhrase(t *testing.T) {
	checkBothWays(t, "role", boolKeys(roles), phraseKeys(rolePhrases))
	checkBothWays(t, "requires", boolKeys(gapTokens), phraseKeys(gapPhrases))
	checkBothWays(t, "thresholds", boolKeys(thresholdKeys), phraseKeys(thresholdPhrases))
}

func checkBothWays(t *testing.T, what string, tokens, phrases []string) {
	t.Helper()

	have := make(map[string]bool, len(phrases))
	for _, phrase := range phrases {
		have[phrase] = true
	}
	for _, token := range tokens {
		if !have[token] {
			t.Errorf("%s token %q has no phrase, so an obligation carrying it "+
				"would reach the Analyst with that ground missing and the model "+
				"would invent one", what, token)
		}
	}

	known := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		known[token] = true
	}
	for _, phrase := range phrases {
		if !known[phrase] {
			t.Errorf("%s phrase %q names a token that is not in the vocabulary, "+
				"so it is copy nothing can reach", what, phrase)
		}
	}
}

func boolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func phraseKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTheGroundsForTheRopaFindingReadAsFactsAboutTheOrganisation is the case
// ENT-248 was filed for.
//
// `gdpr-art-30-ropa` carries `{"role": "controller", "requires": ["ropa"]}`,
// and it is the obligation both misstated narratives cited. What the model gets
// told has to be about this organisation, because a sentence about what the law
// requires is one it will paraphrase back, which is the thing the claim critic
// then refuses.
func TestTheGroundsForTheRopaFindingReadAsFactsAboutTheOrganisation(t *testing.T) {
	reasons := AppliesBecause(`{"role": "controller", "requires": ["ropa"]}`)

	if len(reasons) != 2 {
		t.Fatalf("expected the role and the gap, got %d: %v", len(reasons), reasons)
	}
	if !strings.Contains(reasons[0], "controller") {
		t.Errorf("the role should come first and name it: %q", reasons[0])
	}
	if !strings.Contains(reasons[1], "record of processing activities") {
		t.Errorf("the gap should name what is missing: %q", reasons[1])
	}

	for _, reason := range reasons {
		for _, legal := range []string{"Article", "must maintain", "requires"} {
			if strings.Contains(reason, legal) {
				t.Errorf("the grounds must not state the law, or the model has "+
					"been handed the sentence it is forbidden to write: %q", reason)
			}
		}
	}
}

func TestAnObligationThatBindsEverybodyHasNoGroundsToState(t *testing.T) {
	// Empty `applies_when` has always meant "binds everyone". There is nothing
	// organisation-specific to say, and a heading with nothing under it reads
	// to a model as "no grounds" rather than "not supplied".
	for _, empty := range []string{"", "   ", "{}"} {
		if reasons := AppliesBecause(empty); len(reasons) != 0 {
			t.Errorf("AppliesBecause(%q) = %v, want nothing", empty, reasons)
		}
	}
}

func TestAnUnreadableConditionSetIsSilentRatherThanGuessed(t *testing.T) {
	// Ingest already refuses an `applies_when` outside the vocabulary, so
	// anything stored has been checked once. Failing a narration here for a row
	// that was accepted would be worse than narrating with fewer grounds, and
	// paraphrasing something we cannot parse would be worse than both.
	for _, bad := range []string{"not json", `{"role": 7}`, `["role"]`} {
		if reasons := AppliesBecause(bad); len(reasons) != 0 {
			t.Errorf("AppliesBecause(%q) = %v, want nothing", bad, reasons)
		}
	}
}

func TestOnlyAffirmedThresholdsAreStated(t *testing.T) {
	reasons := AppliesBecause(`{"thresholds": {"high_risk_processing": true, "cross_border_transfers": false}}`)

	if len(reasons) != 1 {
		t.Fatalf("expected only the affirmed threshold, got %v", reasons)
	}
	if !strings.Contains(reasons[0], "high risk") {
		t.Errorf("wrong threshold rendered: %q", reasons[0])
	}
}

func TestTheOrderIsStableSoTwoRunsOverOneFindingAreComparable(t *testing.T) {
	// These go into a prompt. A list whose order changes between runs defeats
	// prefix caching and makes two narrations of one finding incomparable, and
	// Go map iteration is deliberately randomised.
	condition := `{"role": "controller", "requires": ["ropa", "dpo"], ` +
		`"thresholds": {"large_scale_monitoring": true, "high_risk_processing": true}}`

	first := AppliesBecause(condition)
	for i := 0; i < 20; i++ {
		again := AppliesBecause(condition)
		if len(again) != len(first) {
			t.Fatalf("length changed between calls: %v then %v", first, again)
		}
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("order changed between calls at %d: %q then %q", j, first[j], again[j])
			}
		}
	}
}
