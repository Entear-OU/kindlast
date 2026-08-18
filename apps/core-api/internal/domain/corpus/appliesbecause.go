package corpus

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// The applicability conditions, in words a person can read (ENT-248).
//
// # WHY THIS EXISTS
//
// Two live narrations on the 2B tier stated the law wrongly beside a citation
// that resolved correctly. Both had been told WHICH obligation applied and
// nothing about WHY, so both filled the gap themselves: one asserted the
// obligation binds every controller regardless of size, the other reasoned from
// the absence of a record to a headcount exemption. A model with no grounds
// invents grounds, and what it invents comes from whatever it remembers about
// the regulation, which is the single thing a 2B is worst at.
//
// So the grounds become an input. `NarrativeService` puts these sentences in
// `ObligationContext.applies_because`, and the Analyst is asked to explain the
// organisation rather than to work out why it was picked.
//
// # THIS RENDERS THE CONDITIONS, IT DOES NOT EVALUATE THEM
//
// The distinction is the whole reason this file is short, and it is not
// pedantry. `watcher_obligation_applies` decided that these conditions hold
// before the finding existed; a second evaluator in Go, disagreeing with the
// plpgsql one, is precisely the arrangement that produced ENT-246, where a
// threshold nothing read let Article 35's DPIA reach every controller. 00023
// says the same thing from the other side: the alternative available there was
// a second Go evaluator that nothing calls, and two implementations of one
// rule, one of them decorative, is how the bug happened.
//
// So this takes an obligation's declared `applies_when` and says what it
// declares. The finding's existence is what makes the sentences true: the sweep
// raised it, so the conditions held. If ENT-225 moves the evaluator into Go,
// this is the natural place for it to report per-condition outcomes, and the
// phrasing below already reads that way.
//
// # THE PHRASES ARE PINNED TO THE VOCABULARY BY A TEST
//
// Every token in `applieswhen.go` must have a phrase here, asserted in both
// directions by `TestEveryApplicabilityTokenHasAPhrase`. A token without one
// would be silently dropped, which means an obligation's real grounds reaching
// the model as a shorter list than it has, and a shorter list is a model
// filling the gap again. That is the failure this file exists to stop, arriving
// through the file itself.

// What each role means about the organisation. Present tense and second person,
// because these are read by a model that has been told to write about this
// organisation and nothing else.
var rolePhrases = map[string]string{
	"controller": "your organisation is recorded as a controller of personal data",
	"deployer":   "your organisation is recorded as deploying an AI system",
	"provider":   "your organisation is recorded as providing an AI system",
}

// The gap tokens, phrased as the gap rather than as the requirement.
//
// `requires` is what `watcher_gap_satisfied` answers, and a finding exists
// BECAUSE the answer was no. Phrasing these as "the obligation requires a
// ROPA" would hand the model a statement of law to paraphrase, which is exactly
// what it must not do; phrasing them as what is missing from this
// organisation's record keeps them facts about the organisation.
var gapPhrases = map[string]string{
	"ropa":                "your compliance record does not show a record of processing activities",
	"dpo":                 "your compliance record does not name a data protection officer",
	"ai_register":         "your compliance record does not show a register of AI systems",
	"transfer_safeguards": "your compliance record does not show safeguards for transfers outside the EEA",
}

// The thresholds, phrased as what the organisation answered.
//
// "or told us you were not sure" is in each one because that is what the
// evaluator actually treats as affirmed (00023, `watcher_fact_affirms`), and a
// phrase claiming a plain yes would be a small lie in the one direction that
// matters: an organisation reading "you told us your processing is high risk"
// when it answered "unsure" would reasonably say we made that up.
var thresholdPhrases = map[string]string{
	"cross_border_transfers": "you told us you transfer personal data outside the EEA, or told us you were not sure",
	"high_risk_processing":   "you told us your processing is likely to be high risk, or told us you were not sure",
	"high_risk_ai_system":    "you told us you handle a high-risk AI system, or told us you were not sure",
	"large_scale_monitoring": "you told us you monitor people on a large scale, or told us you were not sure",
}

// The remaining top-level keys, which are neither roles nor thresholds nor
// gaps. `lawful_basis_includes` takes its value into the sentence, so it is not
// a plain lookup and is handled in the switch below.
const engagesProcessorPhrase = "you told us you use third parties to process personal data on your behalf"

// AppliesBecause turns an obligation's `applies_when` into plain sentences.
//
// Returns nil for an empty or unparseable condition set, and nil is the right
// answer for both: an obligation binding everyone has no grounds worth stating,
// and a condition set we cannot read is one we must not paraphrase. The caller
// omits the block entirely rather than rendering an empty heading, because a
// model shown an empty list reads it as "no grounds" rather than "not supplied".
//
// Malformed input is not an error here. Ingest already refuses an `applies_when`
// outside the vocabulary (`validateAppliesWhen`), so anything stored has been
// checked once; a second refusal at render time would fail a narration for a
// row that was accepted, which is a worse outcome than a narrative with fewer
// grounds.
func AppliesBecause(appliesWhen string) []string {
	if strings.TrimSpace(appliesWhen) == "" {
		return nil
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(appliesWhen), &parsed); err != nil {
		return nil
	}

	// Ordered rather than emitted as the map iterates, because these go into a
	// prompt: a list whose order changes between runs defeats prefix caching
	// and makes two runs over one finding incomparable. Role first because it
	// is the broadest fact, the gap last because it is the reason a finding
	// exists at all.
	var reasons []string
	for _, key := range []string{
		"role",
		"thresholds",
		"engages_processor",
		"lawful_basis_includes",
		"requires",
	} {
		raw, present := parsed[key]
		if !present {
			continue
		}
		reasons = append(reasons, phrasesFor(key, raw)...)
	}
	return reasons
}

func phrasesFor(key string, raw json.RawMessage) []string {
	switch key {
	case "role":
		var role string
		if err := json.Unmarshal(raw, &role); err != nil {
			return nil
		}
		if phrase, known := rolePhrases[role]; known {
			return []string{phrase}
		}

	case "requires":
		var tokens []string
		if err := json.Unmarshal(raw, &tokens); err != nil {
			return nil
		}
		return lookupAll(tokens, gapPhrases)

	case "thresholds":
		var thresholds map[string]bool
		if err := json.Unmarshal(raw, &thresholds); err != nil {
			return nil
		}
		// Only the affirmed ones. A threshold written `false` would narrow the
		// obligation to organisations that answered no, and no obligation in
		// the corpus does that today, so rendering a phrase for it would be
		// inventing a sentence for a case nobody has authored.
		var affirmed []string
		for token, wanted := range thresholds {
			if wanted {
				affirmed = append(affirmed, token)
			}
		}
		sort.Strings(affirmed)
		return lookupAll(affirmed, thresholdPhrases)

	case "engages_processor":
		var engages bool
		if err := json.Unmarshal(raw, &engages); err == nil && engages {
			return []string{engagesProcessorPhrase}
		}

	case "lawful_basis_includes":
		var basis string
		if err := json.Unmarshal(raw, &basis); err != nil || strings.TrimSpace(basis) == "" {
			return nil
		}
		// The basis is corpus data rather than customer text, and it has been
		// through `checkLawfulBasis` at ingest, so it names one of the six in
		// Article 6(1). Interpolated for that reason and no other.
		return []string{fmt.Sprintf(
			"you told us you rely on %s as a lawful basis for this processing", basis)}
	}

	return nil
}

func lookupAll(tokens []string, phrases map[string]string) []string {
	var found []string
	for _, token := range tokens {
		if phrase, known := phrases[token]; known {
			found = append(found, phrase)
		}
	}
	return found
}
