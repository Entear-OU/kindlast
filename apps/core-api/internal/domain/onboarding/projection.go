package onboarding

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// Projected is a `compliance_profiles` row, derived from what we believe.
//
// # WHY THIS EXISTS AT ALL, WHICH IS THE THING TO UNDERSTAND FIRST
//
// `org_profile_facts` is the record: one row per fact, temporal, provenance
// stamped, correctable by the customer. `compliance_profiles` is one wide row
// with a column per question, no history and no provenance, and it is what
// `run_watcher()` reads. That plpgsql, and the two functions it leans on
// (`watcher_gap_satisfied` and `watcher_obligation_applies`), decide which
// obligations apply to an organisation and which gaps it has.
//
// Moving that read onto the fact store is real work with a real blast radius:
// every finding a customer currently has was produced against the old shape.
// Until it moves, there are two representations of the same thing, and the only
// safe arrangement is that one is derived from the other in the same
// transaction that changes it. This function is that derivation, and it is the
// one place the mapping is written down.
//
// # WHAT AN ABSENT FACT BECOMES, AND WHY IT IS NOT BLANK
//
// The columns are NOT NULL with check constraints, so an absent fact has to
// become something. The choices below are deliberate and are the difference
// between an honest profile and a flattering one:
//
//   - A tri-state nobody answered becomes `unsure`, never `no`. `unsure` is the
//     value that raises the gap, so an unanswered "do you keep a record of
//     processing activities" produces a finding rather than silence.
//     Defaulting to `no` would say the same thing to the Watcher but would
//     claim the person told us, and defaulting to `yes` would hide the gap.
//   - `staff_count` stays null, because the plpgsql already reads null as
//     "unknown, so the obligation applies". Substituting a number would decide
//     a threshold on the customer's behalf.
//   - A list nobody answered is empty. For AI systems that means "no AI system
//     obligations apply", which is the same answer as saying so, and is why the
//     question offers "none" as an answer rather than relying on a skip.
type Projected struct {
	Industry             string
	EUJurisdictions      []string
	DataCategories       []string
	DataSubjects         []string
	AISystems            []string
	HasDPO               string
	HasROPA              string
	TransfersOutsideEU   string
	TransferDestinations []string

	// A joined line rather than a list, because that is what the column holds
	// and what `watcher_obligation_applies` tests with `btrim(...) <> ''`.
	VendorList string

	// Null when nobody said. See the note above: null is a meaningful value to
	// the plpgsql that reads it, so it is carried rather than defaulted.
	StaffCount *int32
}

// unknown is the tri-state an unanswered question projects to.
const unknown = "unsure"

// Project turns the open facts into the row the Watcher reads.
//
// Takes the stored JSON rather than typed values, because that is what both
// callers have: the fact store hands back `value::text` and the interview hands
// back what `Parse` produced. Decoding here rather than at two call sites keeps
// the shape rules in one place.
func Project(facts map[string]string) (Projected, error) {
	out := Projected{
		HasDPO:             unknown,
		HasROPA:            unknown,
		TransfersOutsideEU: unknown,
		// Empty rather than nil, because the columns are `not null default
		// '{}'` and a nil slice would be sent as null.
		EUJurisdictions:      []string{},
		DataCategories:       []string{},
		DataSubjects:         []string{},
		AISystems:            []string{},
		TransferDestinations: []string{},
	}

	for key, valueJSON := range facts {
		if _, known := memory.Kinds[key]; !known {
			// A stored fact this build does not understand. Skipped rather
			// than refused: a projection that failed on an unknown key would
			// make deploying a build that dropped a key an outage for every
			// organisation holding one, and the Watcher can only read the
			// columns that exist anyway.
			continue
		}
		if err := memory.ValidateValue(key, valueJSON); err != nil {
			return Projected{}, err
		}

		switch key {
		case memory.KeyIndustry:
			if err := json.Unmarshal([]byte(valueJSON), &out.Industry); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyEUJurisdictions:
			if err := json.Unmarshal([]byte(valueJSON), &out.EUJurisdictions); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyDataCategories:
			if err := json.Unmarshal([]byte(valueJSON), &out.DataCategories); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyDataSubjects:
			if err := json.Unmarshal([]byte(valueJSON), &out.DataSubjects); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyAISystems:
			if err := json.Unmarshal([]byte(valueJSON), &out.AISystems); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyTransferDestination:
			if err := json.Unmarshal([]byte(valueJSON), &out.TransferDestinations); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyHasDPO:
			if err := json.Unmarshal([]byte(valueJSON), &out.HasDPO); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyHasROPA:
			if err := json.Unmarshal([]byte(valueJSON), &out.HasROPA); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyTransfersOutsideEU:
			if err := json.Unmarshal([]byte(valueJSON), &out.TransfersOutsideEU); err != nil {
				return Projected{}, projectionError(key, err)
			}
		case memory.KeyVendorList:
			var vendors []string
			if err := json.Unmarshal([]byte(valueJSON), &vendors); err != nil {
				return Projected{}, projectionError(key, err)
			}
			out.VendorList = strings.Join(vendors, ", ")
		case memory.KeyStaffCount:
			var count int32
			if err := json.Unmarshal([]byte(valueJSON), &count); err != nil {
				return Projected{}, projectionError(key, err)
			}
			out.StaffCount = &count
		}
	}

	return out, nil
}

func projectionError(key string, err error) error {
	return fmt.Errorf("onboarding: projecting %q into the compliance profile: %w", key, err)
}
