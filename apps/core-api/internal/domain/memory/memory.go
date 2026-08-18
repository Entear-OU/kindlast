// Package memory holds what Kindlast believes about an organisation, and what
// it observed (ENT-228, §26.4, §26.5).
//
// # THE KEY VOCABULARY LIVES HERE, NOT IN THE DATABASE
//
// `org_profile_facts.key` is unconstrained text on purpose. Which facts the
// product understands is a decision that changes as it learns, and a check
// constraint would make every new question a migration. AGENTS.md puts
// decisions in Go, so the closed set is the proto enum and the mapping below,
// and the database enforces the invariants instead: one open value per key,
// closed values immutable.
//
// # WHICH TYPE A KEY TAKES IS ALSO A DECISION
//
// `Kinds` pairs each key with the shape its value must have. Without it the
// oneof on the wire is only a suggestion: a caller could send a number for
// "has a DPO" and the database would store it, because jsonb takes anything.
// The validation is what makes the profile typed rather than merely
// typed-looking.
package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind is the shape a fact's value must have.
type Kind int

const (
	KindText Kind = iota
	KindList
	KindNumber
	KindTriState
)

// Fact keys, as stored. The strings are the database values and the proto enum
// maps onto them; they are deliberately the same names the legacy
// `compliance_profiles` columns used, so ENT-212 can move onboarding across
// without a translation table nobody maintains.
const (
	KeyIndustry            = "industry"
	KeyEUJurisdictions     = "eu_jurisdictions"
	KeyDataCategories      = "data_categories"
	KeyDataSubjects        = "data_subjects"
	KeyAISystems           = "ai_systems"
	KeyHasDPO              = "has_dpo"
	KeyHasROPA             = "has_ropa"
	KeyTransfersOutsideEU  = "transfers_outside_eu"
	KeyTransferDestination = "transfer_destinations"
	KeyStaffCount          = "staff_count"

	// The four the corpus needed and nobody had asked (ENT-246).
	//
	// Each one is a condition an obligation already narrows itself by, so
	// until these existed the Watcher could not evaluate the condition and the
	// obligation applied to everybody. `docs/regulation-packs.md` has the
	// table of who wrote what and who read it.
	//
	// The two high-risk keys are deliberately two. Article 35's "likely to
	// result in a high risk to the rights and freedoms of natural persons" is
	// a test on the PROCESSING; the AI Act's is a classification of a SYSTEM
	// under Annex III. One key for both would mean answering a GDPR question
	// decided an AI Act obligation.
	KeyHighRiskProcessing   = "high_risk_processing"
	KeyHighRiskAISystem     = "high_risk_ai_system"
	KeyLargeScaleMonitoring = "large_scale_monitoring"
	KeyLawfulBases          = "lawful_bases"
)

// Kinds is the closed vocabulary: which facts exist, and what each one holds.
var Kinds = map[string]Kind{
	KeyIndustry:            KindText,
	KeyEUJurisdictions:     KindList,
	KeyDataCategories:      KindList,
	KeyDataSubjects:        KindList,
	KeyAISystems:           KindList,
	KeyHasDPO:              KindTriState,
	KeyHasROPA:             KindTriState,
	KeyTransfersOutsideEU:  KindTriState,
	KeyTransferDestination: KindList,
	KeyStaffCount:          KindNumber,

	KeyHighRiskProcessing:   KindTriState,
	KeyHighRiskAISystem:     KindTriState,
	KeyLargeScaleMonitoring: KindTriState,
	KeyLawfulBases:          KindList,
}

// LawfulBases is the Article 6(1) closed set, in the spelling stored.
//
// # WHY THIS ONE LIST IS CONSTRAINED AND `industry` IS NOT
//
// A list of jurisdictions or data categories is descriptive: a value nobody
// recognises is untidy, and nothing downstream changes its mind because of it.
// The lawful bases are not descriptive. `gdpr-art-7-consent-conditions`
// narrows itself with `lawful_basis_includes: "consent"`, and the Watcher
// decides whether Article 7 binds an organisation by asking whether that exact
// string is in this list. A fact recorded as "Consent" or "consent (marketing)"
// would answer no, silently, and the obligation would stop applying to an
// organisation that relies on consent.
//
// So the two halves are matched against one vocabulary: this one. The corpus
// side is checked in `domain/corpus`, which reads this map rather than
// repeating it.
//
// Article 6(1) is the six bases, and it has not moved since 2016. If a Member
// State derogation ever needs a seventh, it is a line here and a line in the
// pack, which is the unit this vocabulary is meant to change in.
var LawfulBases = map[string]bool{
	"consent":              true,
	"contract":             true,
	"legal_obligation":     true,
	"vital_interests":      true,
	"public_task":          true,
	"legitimate_interests": true,
}

// Sources a fact or an observation may come from. Mirrors the database's check
// constraint, which is the authority; this is here so a bad source is refused
// with a sentence rather than with a constraint name.
var Sources = map[string]bool{
	"onboarding":  true,
	"integration": true,
	"human":       true,
	"agent":       true,
	"import":      true,
}

// Fact is one value, believed over one interval.
type Fact struct {
	Key        string
	ValueJSON  string
	Source     string
	EvidenceID string
	ValidFrom  time.Time
	ValidTo    *time.Time
	RecordedBy string
	Note       string
}

// Current reports whether this is what we believe now.
func (f Fact) Current() bool { return f.ValidTo == nil }

// Observation is one thing we saw, and where we saw it.
type Observation struct {
	ID           string
	Source       string
	Kind         string
	ConnectionID string
	ObservedAt   time.Time
	FetchedAt    time.Time
	BodyJSON     string
	SupersededBy string
}

// ValidateValue refuses a value that does not match its key's kind.
//
// # WHY THIS IS NOT LEFT TO THE WIRE FORMAT
//
// The oneof stops a caller sending two arms at once. It does not stop them
// sending the wrong arm: `staff_count` as text passes protobuf validation
// perfectly, and jsonb would store the string happily. Every reader would then
// have to cope with a number that is sometimes "about fifty".
//
// So the pairing is checked once, here, before anything is written.
func ValidateValue(key, valueJSON string) error {
	kind, known := Kinds[key]
	if !known {
		return fmt.Errorf("memory: %q is not a fact this product understands", key)
	}
	if !json.Valid([]byte(valueJSON)) {
		return fmt.Errorf("memory: the value for %q is not valid JSON", key)
	}

	var decoded any
	if err := json.Unmarshal([]byte(valueJSON), &decoded); err != nil {
		return fmt.Errorf("memory: decoding the value for %q: %w", key, err)
	}

	switch kind {
	case KindText:
		if _, ok := decoded.(string); !ok {
			return fmt.Errorf("memory: %q holds text", key)
		}
	case KindTriState:
		s, ok := decoded.(string)
		if !ok || (s != "yes" && s != "no" && s != "unsure") {
			// UNSURE IS A REAL ANSWER. "We do not know whether we have a
			// record of processing activities" is a finding in itself, and
			// collapsing it to "no" would be a different claim about the same
			// organisation.
			return fmt.Errorf("memory: %q holds yes, no or unsure", key)
		}
	case KindNumber:
		if _, ok := decoded.(float64); !ok {
			return fmt.Errorf("memory: %q holds a number", key)
		}
	case KindList:
		items, ok := decoded.([]any)
		if !ok {
			return fmt.Errorf("memory: %q holds a list of text", key)
		}
		for _, item := range items {
			text, ok := item.(string)
			if !ok {
				return fmt.Errorf("memory: %q holds a list of text", key)
			}
			// The one list whose members are matched rather than displayed.
			// See LawfulBases: an unrecognised spelling here is not untidy
			// data, it is Article 7 silently ceasing to apply.
			if key == KeyLawfulBases && !LawfulBases[text] {
				return fmt.Errorf(
					"memory: %q is not an Article 6(1) lawful basis (%s)",
					text, vocabulary(LawfulBases))
			}
		}
	}
	return nil
}

// ValidateSource refuses a source the schema does not recognise.
func ValidateSource(source string) error {
	if !Sources[source] {
		return fmt.Errorf("memory: %q is not a source", source)
	}
	return nil
}

// vocabulary renders a token set for an error message, sorted so two runs read
// identically.
func vocabulary(set map[string]bool) string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
