// Package claims is the legal-assertion detector, in Go (ENT-248, ENT-254).
//
// # WHY THIS EXISTS A THIRD TIME
//
// `apps/intelligence/.../harness/claims.py` refuses a model's free text when it
// asserts law. `apps/web/lib/onboarding/claims.ts` is that detector in
// TypeScript, guarding the sentences the console writes for itself. This is the
// same detector again, guarding the sentences core-api writes.
//
// Three copies is a real cost and it buys something specific: each one guards
// text authored in that language, and the alternative is not two copies, it is
// text no critic ever sees. Before ENT-254 the interview's questions lived in
// TypeScript on the marketing site and the TypeScript detector walked them.
// Moving the interview into core-api moved the questions into Go, and without
// this they would have been the only prompts in the product that nothing
// checked, on the surface where a customer meets the product's claims first.
//
// # WHAT IT IS FOR, WHICH IS NOT WHAT IT SOUNDS LIKE
//
// It does not check whether a statement of law is TRUE. It checks whether a
// sentence is a statement of law at all, because in this product those come
// from a corpus row verbatim and from nowhere else. ENT-248 settled that after
// the narrator produced a paragraph citing Article 30 correctly and stating the
// opposite of Article 30(5) beside it: a citation validator structurally cannot
// catch that, because the citation was right.
//
// So a question may ask about the organisation, and it may point at a corpus
// row for the law. It may not summarise the row in its own words.
//
// # A TEST-TIME GUARD, NOT A RUNTIME FILTER
//
// Nothing core-api writes here is generated, so there is no output to
// intercept. What this guards is static text in a Go file, and running it over
// that text at test time is the whole control.
package claims

import (
	"fmt"
	"regexp"
)

// legalSubject is the set of subjects a claim about the law is made about.
//
// Second person is deliberately absent, and the reasoning transfers exactly
// from the Python original: "you" is this organisation, which is what these
// surfaces are entitled to talk about. "Controllers must keep a record" is the
// corpus's sentence to make.
const legalSubject = `controllers?|processors?|organisations?|organizations?` +
	`|companies|company|businesses|business|firms?|deployers?|providers?` +
	`|entities|entity`

// instrument is the set of instruments. An assertion whose subject is a
// regulation states law.
const instrument = `the law|the regulation|the gdpr|gdpr|the ai act|the act|article \d+`

// Pattern is one shape of legal assertion, with the name a failure reports.
type Pattern struct {
	Name  string
	Regex *regexp.Regexp
}

// Patterns mirrors `PATTERNS` in `claims.py` and `LEGAL_ASSERTION_PATTERNS` in
// `claims.ts`, in the same order and with the same names.
//
// A list rather than one alternation so a failure names the rule that objected
// rather than the whole regex, which is the difference between a test telling
// somebody what to rewrite and telling them that something is wrong.
//
// Every pattern is case insensitive and none uses a backreference or a
// lookaround, so RE2 compiles all of them and the three implementations agree.
var Patterns = []Pattern{
	{
		Name:  "a provision reference",
		Regex: regexp.MustCompile(`(?i)\b(?:articles?|arts?\.|recitals?|annexe?s?)\s*\d+`),
	},
	{
		Name:  "a provision reference",
		Regex: regexp.MustCompile(`(?i)\bannexe?s?\s+[ivxlc]+\b`),
	},
	{
		Name: "a claim about who the law applies to",
		Regex: regexp.MustCompile(`(?i)\bapplies\s+to\s+(?:every|all|any|each|both)\b` +
			`|\bapply\s+to\s+(?:every|all|any|each|both)\b` +
			`|\b(?:every|all|any|each)\s+(?:` + legalSubject + `)\b`),
	},
	{
		Name: "a claim that the law admits no exception",
		Regex: regexp.MustCompile(`(?i)\bregardless\s+of\b|\birrespective\s+of\b` +
			`|\bno\s+matter\s+how\b|\bwithout\s+exception\b|\bin\s+all\s+cases\b`),
	},
	{
		Name: "a claim about an exemption or a threshold",
		Regex: regexp.MustCompile(`(?i)\bexempt\w*\b|\bexception\w*\b|\bderogation\w*\b` +
			`|\bcarve[- ]?out\b|\bthresholds?\b|\bfewer\s+than\s+\d+\b` +
			`|\bmore\s+than\s+\d+\s+employees\b|\bunder\s+\d+\s+employees\b` +
			`|\bat\s+least\s+\d+\s+employees\b`),
	},
	{
		Name: "a statement of what the law requires",
		Regex: regexp.MustCompile(`(?i)\b(?:` + instrument + `)\s+(?:requires?|mandates?` +
			`|obliges?|demands?|states?|says?|provides?|stipulates?|prohibits?` +
			`|forbids?|permits?|allows?)\b`),
	},
	{
		// The passive, which the first version of the Python file missed and a
		// live run produced: "the written records required by the law".
		Name: "a statement of what the law requires",
		Regex: regexp.MustCompile(`(?i)\b(?:required|mandated|obliged|obligated|prohibited` +
			`|permitted|forbidden|exempted|demanded)\s+(?:by|under)\s+(?:law\b|` +
			instrument + `)`),
	},
	{
		Name: "an obligation stated over a class of organisations",
		Regex: regexp.MustCompile(`(?i)\b(?:` + legalSubject + `)\s+(?:must|shall` +
			`|are\s+required\s+to|is\s+required\s+to|have\s+to|has\s+to` +
			`|are\s+obliged\s+to|need\s+to)\b`),
	},
}

// Assertion is one rule that objected, and the text that set it off.
type Assertion struct {
	Rule    string
	Matched string
}

func (a Assertion) String() string {
	return fmt.Sprintf("%s (%q)", a.Rule, a.Matched)
}

// LegalAssertions returns every rule that objects to a string, so one rewrite
// can answer all of them.
func LegalAssertions(text string) []Assertion {
	var found []Assertion
	for _, pattern := range Patterns {
		if match := pattern.Regex.FindString(text); match != "" {
			found = append(found, Assertion{Rule: pattern.Name, Matched: match})
		}
	}
	return found
}

// AssertsLaw reports whether a string states law and therefore belongs to the
// corpus rather than to us.
func AssertsLaw(text string) bool {
	return len(LegalAssertions(text)) > 0
}
