package corpus

import (
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// Helper: validate one obligation's `appliesWhen` and return the joined
// problems, so a test asserts on the message a curator would actually read.
func appliesWhenProblems(t *testing.T, raw string) string {
	t.Helper()
	return Problems(validateAppliesWhen(`obligation "test"`, raw))
}

// A gap token nobody evaluates is the failure this whole file exists for.
//
// It is not symmetrical with the other checks and the asymmetry is the point.
// An unevaluated `requires` token raises no gap, so the obligation is ingested,
// reported as applying, and then never fires. The customer sees a clean feed.
func TestAGapTokenNobodyEvaluatesIsRefused(t *testing.T) {
	problems := appliesWhenProblems(t, `{"requires":["access_review"]}`)

	if problems == "" {
		t.Fatal("an unevaluated gap token was accepted, which is the silent failure")
	}
	for _, want := range []string{"access_review", "requires"} {
		if !strings.Contains(problems, want) {
			t.Errorf("the message does not name %q, so the curator cannot act on it: %s", want, problems)
		}
	}
}

// Unlike a threshold, a gap token has no "declared but not evaluated" tier, and
// a test asserts that rather than leaving it to a reader of the vocabulary.
func TestThereIsNoUnevaluatedTierForGapTokens(t *testing.T) {
	for token := range gapTokens {
		if !gapTokens[token] {
			t.Errorf("gap token %q is declared but not evaluated; that tier does not exist "+
				"because an unevaluated gap token is a finding that never fires", token)
		}
	}
}

func TestAnUnknownApplicabilityKeyIsRefused(t *testing.T) {
	problems := appliesWhenProblems(t, `{"jurisdiction":"us-ca"}`)
	if !strings.Contains(problems, "jurisdiction") {
		t.Errorf("an unknown top-level key was accepted or not named: %q", problems)
	}
}

func TestAnUnknownThresholdIsRefused(t *testing.T) {
	problems := appliesWhenProblems(t, `{"thresholds":{"annual_revenue_min":1000000}}`)
	if !strings.Contains(problems, "annual_revenue_min") {
		t.Errorf("an unknown threshold was accepted or not named: %q", problems)
	}
}

func TestAnUnknownRoleIsRefused(t *testing.T) {
	problems := appliesWhenProblems(t, `{"role":"auditor"}`)
	if !strings.Contains(problems, "auditor") {
		t.Errorf("an unknown role was accepted or not named: %q", problems)
	}
}

// Shape errors, which a curator hits by hand-editing JSON.
func TestAppliesWhenMustBeAnObjectOfTheRightShape(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"an array", `["role"]`, "object"},
		{"a bare string", `"controller"`, "object"},
		{"requires not an array", `{"requires":"ropa"}`, "requires"},
		{"a non-string gap token", `{"requires":[7]}`, "requires"},
		{"thresholds not an object", `{"thresholds":true}`, "thresholds"},
		{"role not a string", `{"role":42}`, "role"},
		{"not JSON at all", `{`, "JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := appliesWhenProblems(t, tc.raw)
			if !strings.Contains(problems, tc.want) {
				t.Errorf("%s was accepted or the message does not mention %q: %q",
					tc.name, tc.want, problems)
			}
		})
	}
}

// The vocabulary the curated corpus actually uses has to keep validating, or
// this check is one that can only be satisfied by deleting curator intent.
func TestTheVocabularyTheCorpusUsesValidates(t *testing.T) {
	for _, raw := range []string{
		`{"role":"controller","requires":["ropa"]}`,
		`{"role":"controller","thresholds":{"high_risk_processing":true}}`,
		`{"role":"controller","thresholds":{"large_scale_monitoring":true},"requires":["dpo"]}`,
		`{"role":"controller","lawful_basis_includes":"consent"}`,
		`{"role":"controller","thresholds":{"cross_border_transfers":true},"requires":["transfer_safeguards"]}`,
		`{"role":"controller","engages_processor":true}`,
		`{"role":"deployer","requires":["ai_register"]}`,
		`{"role":"provider","thresholds":{"high_risk_ai_system":true}}`,
		`{}`,
		``,
	} {
		if problems := appliesWhenProblems(t, raw); problems != "" {
			t.Errorf("the corpus's own vocabulary was refused: %s -> %s", raw, problems)
		}
	}
}

// There is no declared-but-unevaluated tier left (ENT-246).
//
// It used to hold three keys, and holding them was the bug: an unread threshold
// narrows nothing, so `gdpr-art-35-dpia` reached every controller rather than
// only those doing high-risk processing. Each is now answered from a profile
// fact.
//
// The assertion is emptiness rather than a pinned list, because the list is the
// thing that should not come back. A token nobody evaluates is either an
// obligation that binds too many organisations or, on the `requires` side, one
// that never fires at all.
func TestNoTokenIsDeclaredWithoutBeingEvaluated(t *testing.T) {
	if keys := UnevaluatedKeys(); len(keys) != 0 {
		t.Fatalf("these tokens are declared and not evaluated: %v. An unread applicability "+
			"token makes its obligation apply more widely than the curator wrote, which is "+
			"the fabricated obligation ENT-246 removed. Evaluate it, or take it out of the "+
			"vocabulary and out of the obligations that carry it", keys)
	}
}

// `employees_min` is gone, and a pack that uses it is refused.
//
// Kept as its own test because "we deleted an unused thing" is exactly the
// change somebody restores in good faith. The reason it is not merely unused is
// in the vocabulary: it invites Article 30(5) to be encoded as a headcount,
// which would exempt organisations Article 30(5) does not exempt.
func TestTheHeadcountThresholdIsNotInTheVocabulary(t *testing.T) {
	problems := appliesWhenProblems(t, `{"thresholds":{"employees_min":250}}`)
	if !strings.Contains(problems, "employees_min") {
		t.Errorf("employees_min was accepted: %q", problems)
	}
}

// Every threshold answered from a fact names a fact the product understands.
//
// The failure this catches is silent in the worst way. A threshold whose fact
// key is misspelled reads a key nobody ever writes, which is indistinguishable
// from an organisation that has not answered, which means the obligation stops
// applying to everybody with no error anywhere.
func TestEveryThresholdFactIsAFactTheProductUnderstands(t *testing.T) {
	for _, pair := range ThresholdFacts() {
		threshold, fact := pair[0], pair[1]

		if !thresholdKeys[threshold] {
			t.Errorf("threshold %q is answered from a fact but is not in the vocabulary", threshold)
		}
		if _, known := memory.Kinds[fact]; !known {
			t.Errorf("threshold %q reads the fact %q, which is not in memory.Kinds. Nothing "+
				"writes that key, so the obligation would silently apply to nobody",
				threshold, fact)
		}
		if memory.Kinds[fact] != memory.KindTriState {
			t.Errorf("threshold %q reads %q, which is not a tri-state. The evaluator asks "+
				"whether the fact is yes or unsure, and any other shape answers neither",
				threshold, fact)
		}
	}
}

// A lawful basis outside Article 6(1) is refused at ingest.
//
// The Watcher matches this string against the organisation's recorded bases, so
// "Consent" is not a cosmetic difference: it is an obligation that never
// applies to anybody and reports nothing.
func TestALawfulBasisOutsideArticle6IsRefused(t *testing.T) {
	problems := appliesWhenProblems(t, `{"lawful_basis_includes":"Consent"}`)
	if !strings.Contains(problems, "Consent") {
		t.Errorf("an unrecognised lawful basis was accepted: %q", problems)
	}

	if problems := appliesWhenProblems(t, `{"lawful_basis_includes":"legitimate_interests"}`); problems != "" {
		t.Errorf("a real Article 6(1) basis was refused: %q", problems)
	}
}

// The pack-level wiring: an obligation carrying a bad `appliesWhen` fails the
// whole pack, rather than the check living somewhere only a unit test reaches.
func TestAPackWithAnUnevaluatedGapTokenDoesNotValidate(t *testing.T) {
	pack := validPack()
	pack.Obligations[0].AppliesWhenJSON = `{"requires":["soc2_access_review"]}`

	problems := Problems(pack.Validate())
	if !strings.Contains(problems, "soc2_access_review") {
		t.Errorf("Pack.Validate did not refuse an unevaluated gap token: %q", problems)
	}
}
