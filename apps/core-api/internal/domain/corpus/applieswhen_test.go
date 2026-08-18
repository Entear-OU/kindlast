package corpus

import (
	"strings"
	"testing"
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
		`{"role":"controller","thresholds":{"high_risk":true}}`,
		`{"role":"controller","thresholds":{"large_scale_monitoring":true},"requires":["dpo"]}`,
		`{"role":"controller","lawful_basis_includes":"consent"}`,
		`{"role":"controller","thresholds":{"cross_border_transfers":true},"requires":["transfer_safeguards"]}`,
		`{"role":"controller","engages_processor":true}`,
		`{"role":"deployer","requires":["ai_register"]}`,
		`{"role":"provider","thresholds":{"high_risk":true}}`,
		`{}`,
		``,
	} {
		if problems := appliesWhenProblems(t, raw); problems != "" {
			t.Errorf("the corpus's own vocabulary was refused: %s -> %s", raw, problems)
		}
	}
}

// The known drift, pinned.
//
// Three keys the curator writes that the watcher does not read. Pinned as an
// exact set so that a fourth cannot be added without editing this list, which
// is the whole difference between a drift somebody decided to accept and a
// drift nobody noticed.
func TestTheDeclaredButUnevaluatedKeysAreExactlyTodaysDrift(t *testing.T) {
	want := map[string]bool{
		"lawful_basis_includes":  true,
		"high_risk":              true,
		"large_scale_monitoring": true,
	}

	got := map[string]bool{}
	for key, evaluated := range appliesWhenKeys {
		if !evaluated {
			got[key] = true
		}
	}
	for key, evaluated := range thresholdKeys {
		if !evaluated {
			got[key] = true
		}
	}

	for key := range want {
		if !got[key] {
			t.Errorf("%q is no longer declared-but-unevaluated; if the watcher now reads it, "+
				"mark it evaluated and drop it from this list", key)
		}
	}
	for key := range got {
		if !want[key] {
			t.Errorf("%q was added as declared-but-unevaluated. That is an obligation whose "+
				"applicability is wider than the curator wrote. Say so in docs/regulation-packs.md "+
				"and add it here deliberately", key)
		}
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
