package corpus

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// The applicability vocabulary: what an obligation may say about who it binds,
// and what the Watcher actually reads (ENT-233, ENT-246).
//
// # WHY THIS FILE EXISTS
//
// `applies_when` was opaque from end to end. The curator wrote JSON, the loader
// kept it as `json.RawMessage` deliberately, the proto carried it as a string,
// the column is `jsonb`, and the only thing with an opinion about its contents
// was a pair of plpgsql functions in 00001. Nothing in between could tell a
// token the Watcher evaluates from one it has never heard of.
//
// That was not a hypothetical. At two regulations the vocabulary had drifted,
// in both directions, and nothing anywhere went red. ENT-233 wrote the
// vocabulary down and refused unknown tokens at ingest. ENT-246 closed the
// drift itself, and the rule this file states is now the simpler one: every
// token here is evaluated.
//
// # AN UNEVALUATED TOKEN FAILS IN ONE OF TWO DIRECTIONS, AND BOTH ARE WRONG
//
// `watcher_gap_satisfied` answers `true` for a token it does not recognise, and
// logs. Satisfied means no gap, no gap means no finding. So a pack whose
// obligations require `access_review` ingests cleanly, reports as applying, and
// produces nothing, for ever. The customer reads an empty feed as compliance. A
// regulation whose obligations silently never fire is worse than one that is
// missing, because a missing regulation is visible.
//
// `watcher_obligation_applies` ignores a condition it does not recognise, so an
// unread threshold NARROWS nothing and the obligation reaches organisations the
// curator never meant to bind. That is what ENT-246 fixed: Article 35's DPIA
// reached every controller rather than only high-risk processing, and a DPIA is
// expensive. A fabricated obligation is worse than none at all, because the
// product's whole value is that a human can check the claim against the law.
//
// So neither key set has a "declared but not evaluated" tier any more. A token
// is in this file only if the evaluator reads it, and
// `corpus_vocabulary_test.go` proves that against the running functions rather
// than trusting these declarations.
//
// # WHAT THE THRESHOLDS READ, AND WHAT SILENCE MEANS
//
// Three of them ask about the PROCESSING rather than about the organisation,
// and the legacy `compliance_profiles` has a column for none of them. They are
// answered from ENT-228's `org_profile_facts`, under the keys in
// `thresholdFacts` below, which is why asking these questions was not a
// migration.
//
// An absent fact means the obligation does NOT apply, and that direction was
// chosen rather than inherited:
//
//   - Asserting a DPIA from silence is the fabricated obligation above. Nobody
//     asked, so there are no grounds, and "you owe a DPIA" is the most
//     expensive sentence this product can say wrongly.
//   - The mirror risk, hiding the obligation from an organisation that does do
//     high-risk processing, is real and is one answer away from being fixed:
//     the fact is editable on the memory page today, and onboarding writes it
//     at ENT-212.
//
// `unsure` counts as applying, and that is not a rounding of "no". ENT-228 kept
// `unsure` as its own answer because "we asked and they did not know" is a
// different claim from "they said no". An organisation that does not know
// whether its processing is high-risk has not done the Article 35(1) screening,
// which is exactly the situation the obligation exists for.
//
// # THIS IS A DECISION, SO THE VOCABULARY LIVES IN GO
//
// By AGENTS.md's test: if a second process connected tomorrow and did not know
// this rule, the data would not be wrong, it would have made a different
// product decision about who an obligation binds. That is a decision, and it
// belongs here rather than in a CHECK constraint. The evaluator that consults
// it is still plpgsql, which is ENT-225's to move; this file is the declaration
// that move should inherit.

// The `requires` vocabulary: gap tokens, each naming a question
// `watcher_gap_satisfied` can answer about a profile.
//
// The bool is always true. It is a map rather than a set of strings so the type
// matches the two below, and so `TestThereIsNoUnevaluatedTierForGapTokens` has
// something to assert against: this is the vocabulary that may not have a
// false in it.
var gapTokens = map[string]bool{
	"ropa":                true,
	"dpo":                 true,
	"ai_register":         true,
	"transfer_safeguards": true,
}

// Top-level keys. The value is whether the evaluator reads it, and it is true
// for every entry: the header says why the second tier was closed rather than
// tidied.
var appliesWhenKeys = map[string]bool{
	"role":              true,
	"requires":          true,
	"thresholds":        true,
	"engages_processor": true,

	// `gdpr-art-7-consent-conditions` narrows itself to controllers relying on
	// consent, and is answered from the `lawful_bases` fact. The basis named
	// must be one of the six in Article 6(1), because the two sides are matched
	// as strings and a spelling nothing matches is Article 7 quietly not
	// applying to anybody.
	"lawful_basis_includes": true,
}

// Threshold keys, same convention: every one is read.
//
// `employees_min` is gone (ENT-246). It was evaluated and no obligation used
// it, which sounds like harmless dead code and is not. The only obligation it
// looks like it should serve is Article 30's ROPA, whose 250-employee exemption
// is narrow enough that the curated summary tells the reader most SMEs cannot
// rely on it. A headcount threshold sitting in the vocabulary invites somebody
// to encode Article 30(5) as `employees_min: 250` and exempt organisations the
// Article does not exempt. Deleting it makes that mistake unavailable.
var thresholdKeys = map[string]bool{
	"cross_border_transfers": true,

	// The three the Watcher could not read until ENT-246 gave it facts to read
	// them from. `thresholdFacts` says which fact each one asks about.
	"high_risk_processing":   true,
	"high_risk_ai_system":    true,
	"large_scale_monitoring": true,
}

// Which profile fact each threshold is answered from.
//
// Written down here, and not only inside the plpgsql evaluator, because this is
// the seam that drifts next. A threshold whose fact key is misspelled reads an
// answer nobody ever writes, which is indistinguishable from an organisation
// that has not answered, which means the obligation silently stops applying to
// everybody. The unit test checks each value is a fact the product understands;
// the db-backed test checks the evaluator reads that same key.
var thresholdFacts = map[string]string{
	"high_risk_processing":   memory.KeyHighRiskProcessing,
	"high_risk_ai_system":    memory.KeyHighRiskAISystem,
	"large_scale_monitoring": memory.KeyLargeScaleMonitoring,
}

// Roles an obligation may bind. `controller` is GDPR's, `deployer` and
// `provider` are the AI Act's, and that they sit in one flat list is itself a
// pack seam: a third regulation brings its own actor names.
var roles = map[string]bool{
	"controller": true,
	"deployer":   true,
	"provider":   true,
}

// validateAppliesWhen checks an obligation's applicability against the
// vocabulary. Empty is valid and means "binds everyone", which is what an
// absent `appliesWhen` has always meant.
//
// Every message names the offending token and the vocabulary it is missing
// from, because whoever reads it is authoring a pack and the useful question is
// whether they should be adding data or adding code.
func validateAppliesWhen(where, raw string) []error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return []error{fmt.Errorf(
			"%s: appliesWhen is not a JSON object: %v", where, err)}
	}

	var problems []error

	for key, value := range parsed {
		if _, known := appliesWhenKeys[key]; !known {
			problems = append(problems, fmt.Errorf(
				"%s: appliesWhen names %q, which is not in the applicability vocabulary (%s). "+
					"Either use an existing key or add it to appliesWhenKeys and teach the "+
					"Watcher to read it", where, key, vocabulary(appliesWhenKeys)))
			continue
		}

		switch key {
		case "role":
			problems = append(problems, checkRole(where, value)...)
		case "requires":
			problems = append(problems, checkRequires(where, value)...)
		case "thresholds":
			problems = append(problems, checkThresholds(where, value)...)
		case "lawful_basis_includes":
			problems = append(problems, checkLawfulBasis(where, value)...)
		}
	}

	return problems
}

func checkRole(where string, raw json.RawMessage) []error {
	var role string
	if err := json.Unmarshal(raw, &role); err != nil {
		return []error{fmt.Errorf("%s: appliesWhen role is not a string", where)}
	}
	if !roles[role] {
		return []error{fmt.Errorf(
			"%s: appliesWhen role %q is not one of %s. An unrecognised role binds nobody "+
				"differently, so the obligation would apply to every organisation",
			where, role, vocabulary(roles))}
	}
	return nil
}

func checkRequires(where string, raw json.RawMessage) []error {
	var tokens []string
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return []error{fmt.Errorf(
			"%s: appliesWhen requires is not an array of gap tokens", where)}
	}

	var problems []error
	for _, token := range tokens {
		if !gapTokens[token] {
			problems = append(problems, fmt.Errorf(
				"%s: appliesWhen requires %q, which is not a gap token the Watcher evaluates (%s). "+
					"An unevaluated gap token counts as satisfied, so this obligation would "+
					"never raise a finding. Teach watcher_gap_satisfied the token before "+
					"adding it to gapTokens", where, token, vocabulary(gapTokens)))
		}
	}
	return problems
}

// checkLawfulBasis refuses a basis outside Article 6(1).
//
// The comparison the Watcher makes is string equality against a member of the
// organisation's `lawful_bases` fact, so "Consent" or "consent_marketing"
// would not be a slightly-off label, it would be an obligation that never
// applies to anyone and says nothing about it.
func checkLawfulBasis(where string, raw json.RawMessage) []error {
	var basis string
	if err := json.Unmarshal(raw, &basis); err != nil {
		return []error{fmt.Errorf(
			"%s: appliesWhen lawful_basis_includes is not a string", where)}
	}
	if !memory.LawfulBases[basis] {
		return []error{fmt.Errorf(
			"%s: appliesWhen lawful_basis_includes %q is not an Article 6(1) basis (%s). "+
				"The Watcher matches this against the organisation's recorded bases, so a "+
				"spelling nothing matches is an obligation that never applies",
			where, basis, vocabulary(memory.LawfulBases))}
	}
	return nil
}

func checkThresholds(where string, raw json.RawMessage) []error {
	var thresholds map[string]json.RawMessage
	if err := json.Unmarshal(raw, &thresholds); err != nil {
		return []error{fmt.Errorf("%s: appliesWhen thresholds is not an object", where)}
	}

	var problems []error
	for key := range thresholds {
		if _, known := thresholdKeys[key]; !known {
			problems = append(problems, fmt.Errorf(
				"%s: appliesWhen has threshold %q, which is not in the threshold vocabulary (%s). "+
					"An unrecognised threshold narrows nothing, so the obligation would apply "+
					"more widely than written", where, key, vocabulary(thresholdKeys)))
		}
	}
	return problems
}

// GapTokens is the `requires` vocabulary, sorted.
//
// Exported for one caller: the test that asserts every token here is one
// `watcher_gap_satisfied` actually evaluates. This declaration and that
// function are two halves of the same rule living in two languages, which is
// exactly the arrangement that drifts, so the guard reads the list rather than
// repeating it.
func GapTokens() []string {
	return vocabularyList(gapTokens)
}

// ThresholdKeys is the threshold vocabulary, sorted.
//
// Exported so the db-backed guard can assert that something probes every one of
// them, which is how ENT-233 stopped a threshold arriving with nobody asking
// what the evaluator does with it.
func ThresholdKeys() []string {
	return vocabularyList(thresholdKeys)
}

// ThresholdFacts pairs each threshold with the profile fact it is answered
// from, sorted by threshold.
//
// Exported for the tests that keep the two ends of that pairing honest: one
// checks every fact named is a fact the product understands, and the db-backed
// one checks the evaluator narrows on that exact key.
func ThresholdFacts() [][2]string {
	pairs := make([][2]string, 0, len(thresholdFacts))
	for threshold, fact := range thresholdFacts {
		pairs = append(pairs, [2]string{threshold, fact})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	return pairs
}

// UnevaluatedKeys is the declared-but-not-evaluated set: applicability keys and
// thresholds a curator may write and the Watcher does not read, so an
// obligation using one binds more organisations than it says.
//
// IT IS EMPTY, AND THE FUNCTION IS KEPT SO THAT STAYS CHECKABLE (ENT-246). The
// tier existed because three conditions had no profile field to be evaluated
// against; they have facts now. A test asserts this returns nothing, so
// reintroducing an unread token is a red test rather than a comment nobody
// reads.
func UnevaluatedKeys() []string {
	var keys []string
	for key, evaluated := range appliesWhenKeys {
		if !evaluated {
			keys = append(keys, key)
		}
	}
	for key, evaluated := range thresholdKeys {
		if !evaluated {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// vocabulary renders a token set for an error message, sorted so two runs read
// identically.
func vocabulary(set map[string]bool) string {
	return strings.Join(vocabularyList(set), ", ")
}

func vocabularyList(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
