package claims_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/claims"
)

// A guard that cannot go red reports a safety that is not there, so the
// detector is proved against sentences that are unambiguously statements of law
// before anything is trusted to it. These are the same sentences
// `apps/web/__tests__/lib/onboarding/corpus.test.ts` and `test_claims.py` use,
// which is what keeps the three implementations honest about being the same
// detector rather than three that happen to share a name.

func TestItCatchesTheShapesTheCodeCriticCatches(t *testing.T) {
	for _, sentence := range []string{
		"Article 30 requires a written record.",
		"This applies to every controller.",
		"Controllers must appoint a data protection officer.",
		"Small companies are exempt from this.",
		"It applies regardless of headcount.",
		"See Recital 47 for the reasoning.",
		"The GDPR requires notification within 72 hours.",
		"The written records required by the law.",
		"Annex III lists the high-risk uses.",
	} {
		if !claims.AssertsLaw(sentence) {
			t.Errorf("cleared a statement of law: %q", sentence)
		}
	}
}

func TestItClearsASentenceThatIsOnlyAboutTheOrganisation(t *testing.T) {
	// Second person is allowed, exactly as the Python critic allows it: "you"
	// is this organisation, which is what these surfaces are entitled to talk
	// about.
	for _, sentence := range []string{
		"You said personal information leaves the EU.",
		"You told us you keep no record of what you do.",
		"Nothing you told us narrows this one.",
		"You told us you have not appointed anybody.",
	} {
		if assertions := claims.LegalAssertions(sentence); len(assertions) > 0 {
			t.Errorf("objected to a sentence about the organisation: %q, %v", sentence, assertions)
		}
	}
}

func TestItNamesTheRuleThatObjected(t *testing.T) {
	// The reason the patterns are a list rather than one alternation. A failure
	// that says which shape fired tells somebody what to rewrite; one that says
	// "the regex matched" does not.
	assertions := claims.LegalAssertions("Article 30 requires a written record.")
	if len(assertions) == 0 {
		t.Fatal("no rule objected to a statement of law")
	}
	for _, assertion := range assertions {
		if assertion.Rule == "" || assertion.Matched == "" {
			t.Errorf("an objection names nothing: %+v", assertion)
		}
	}
}
