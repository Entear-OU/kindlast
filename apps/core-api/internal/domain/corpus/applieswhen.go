package corpus

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The applicability vocabulary: what an obligation may say about who it binds,
// and what the Watcher actually reads (ENT-233).
//
// # WHY THIS FILE EXISTS
//
// `applies_when` was opaque from end to end. The curator wrote JSON, the loader
// kept it as `json.RawMessage` deliberately, the proto carried it as a string,
// the column is `jsonb`, and the only thing with an opinion about its contents
// was a pair of plpgsql functions in 00001. Nothing in between could tell a
// token the Watcher evaluates from one it has never heard of.
//
// That is not a hypothetical. At two regulations the vocabulary had ALREADY
// drifted, in both directions, and nothing anywhere went red:
//
//   - `thresholds.high_risk` (four obligations), `thresholds.large_scale_monitoring`
//     and `lawful_basis_includes` are written by the curator and read by nobody.
//   - `thresholds.employees_min` is evaluated by `watcher_obligation_applies`
//     and written by no obligation.
//
// The two directions do not cost the same, which is why this file treats them
// differently rather than lumping them into one "unknown key" rule.
//
// # AN UNEVALUATED GAP TOKEN IS THE ONE THAT MATTERS
//
// `watcher_gap_satisfied` answers `true` for a token it does not recognise, and
// logs. Satisfied means no gap, no gap means no finding. So a pack whose
// obligations require `access_review` ingests cleanly, reports as applying, and
// produces nothing, for ever. The customer reads an empty feed as compliance.
//
// A regulation whose obligations silently never fire is worse than one that is
// missing, because a missing regulation is visible.
//
// So `requires` has no "declared but not evaluated" tier. Every gap token in
// this vocabulary is one the evaluator implements, and
// `corpus_vocabulary_test.go` proves that against the running function rather
// than trusting this file.
//
// # AN UNEVALUATED THRESHOLD FAILS THE OTHER WAY, AND IS SURVIVABLE
//
// `watcher_obligation_applies` ignores a condition it does not recognise, so an
// unread threshold NARROWS nothing and the obligation applies to more
// organisations than the curator wrote. That over-reports. It is wrong, it is
// visible to the customer, and they can dismiss it. Article 35's DPIA applying
// to every controller rather than to high-risk processing is exactly this, and
// it ships today.
//
// So thresholds and applicability keys carry a second tier: declared, so a pack
// may use them and the intent survives in the data, but marked here as not
// evaluated so the list is something a person edits on purpose. The test pins
// the set. A fourth entry cannot arrive unnoticed.
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

// Top-level keys. The value is whether the evaluator reads it.
var appliesWhenKeys = map[string]bool{
	"role":              true,
	"requires":          true,
	"thresholds":        true,
	"engages_processor": true,

	// Declared, not evaluated. `gdpr-art-7-consent-conditions` narrows itself
	// to controllers relying on consent, and the profile has no lawful-basis
	// field to narrow against, so today it binds every controller.
	"lawful_basis_includes": false,
}

// Threshold keys, same convention.
var thresholdKeys = map[string]bool{
	"employees_min":          true,
	"cross_border_transfers": true,

	// Declared, not evaluated. Both describe a property of the PROCESSING
	// rather than of the organisation, and `compliance_profiles` records only
	// the latter. ENT-228's `org_profile_facts` is where the question they need
	// can be asked without a migration.
	"high_risk":              false,
	"large_scale_monitoring": false,
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

// UnevaluatedKeys is the declared-but-not-evaluated set: applicability keys and
// thresholds a curator may write and the Watcher does not read, so an
// obligation using one binds more organisations than it says.
//
// Exported for the same reason, and worth reading before adding to it.
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
