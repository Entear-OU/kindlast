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
			if _, ok := item.(string); !ok {
				return fmt.Errorf("memory: %q holds a list of text", key)
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
