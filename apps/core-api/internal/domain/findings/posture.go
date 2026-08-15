// Package findings holds the domain rules for the feed and the dashboard.
//
// Pure functions over already-loaded data, with no database and no proto, for
// the same reason the legacy `lib/dashboard/posture.ts` was written that way:
// the posture rule is the acceptance criterion, and a rule that can only be
// exercised through a query is a rule nobody exhaustively tests.
package findings

// Posture is the dashboard's single band.
type Posture string

const (
	// PostureNotAssessed means the Watcher has never run for this organisation.
	//
	// ENT-161, and the reason this type has four values rather than the legacy
	// three. A posture derived by counting findings returns green before
	// anything has looked, so the old console told founders "You're on track"
	// about a compliance record the product had never examined. For a product
	// whose value is that a human can check a claim, that is the worst
	// available failure: not a wrong answer, a confident one.
	//
	// It is first in the constant block and handled first in every switch
	// because it is not a severity band at all. It is the absence of an
	// assessment, and it outranks the others rather than sitting between them.
	PostureNotAssessed Posture = "not_assessed"

	PostureGreen Posture = "green"
	PostureAmber Posture = "amber"
	PostureRed   Posture = "red"
)

// NearTermDays is the window an approaching deadline has to fall inside to
// affect posture. Carried over from the legacy rule unchanged.
const NearTermDays = 30

// Deadline is an approaching or overdue regulatory deadline.
//
// DaysRemaining is negative when the deadline has passed.
//
// In the legacy stack these came from their own loader. There is no deadlines
// table in this schema and there never was: `watcher_detect_deadlines` emits
// them as signals, so by the time they reach here they are findings whose
// `metadata ->> 'signal_kind'` is `deadline` and whose signal metadata carries
// `days_remaining`. The rule is unchanged; only where the rows come from is.
type Deadline struct {
	Severity      string
	DaysRemaining int
}

// PostureInputs is everything the band is computed from.
type PostureInputs struct {
	// Severities of every open (pending) finding.
	OpenSeverities []string
	// Approaching or overdue deadlines.
	Deadlines []Deadline
	// Whether the Watcher has ever run for this organisation. False makes the
	// band NotAssessed whatever the other fields say, because they are then the
	// absence of evidence rather than evidence of absence.
	Assessed bool
}

// ComputePosture collapses open findings and deadlines into one band.
//
// The rule is the legacy one, reviewed at db0bf83 before being rewritten as
// ENT-200 requires, with ENT-161's fourth state added in front of it:
//
//   - NotAssessed — the Watcher has never run.
//   - Red         — a critical finding is open, or a critical deadline is overdue.
//   - Amber       — a high finding is open, or a critical/high deadline falls
//     inside the 30-day window.
//   - Green       — nothing critical or high is pressing.
//
// Precedence is NotAssessed, then Red, then Amber, then Green: the worst
// applicable band wins.
//
// A near-term critical deadline that has not yet lapsed is deliberately Amber
// rather than Red. It breaks Green because it is pressing, and it is not Red
// until it actually passes, which is the distinction the original AC drew and
// the one a founder acts on differently.
func ComputePosture(in PostureInputs) Posture {
	if !in.Assessed {
		return PostureNotAssessed
	}

	if hasSeverity(in.OpenSeverities, "critical") || overdue(in.Deadlines, "critical") {
		return PostureRed
	}

	if hasSeverity(in.OpenSeverities, "high") ||
		nearTerm(in.Deadlines, "high") ||
		nearTerm(in.Deadlines, "critical") {
		return PostureAmber
	}

	return PostureGreen
}

// Headline is the plain-language sentence shown with the band.
//
// Server-side so the wording is one thing rather than one per client, and
// carried over verbatim from the legacy console for the three bands that
// existed there. Changing them is a product decision, not a porting one.
func Headline(p Posture) string {
	switch p {
	case PostureNotAssessed:
		// Deliberately not reassuring. The honest content is that nothing has
		// been checked yet, and a sentence that sounds like good news here is
		// the exact bug ENT-161 describes.
		return "Not assessed yet. The Watcher has not run for this organisation."
	case PostureRed:
		return "Action required. You have something critical open."
	case PostureAmber:
		return "Needs attention. A few things are coming due."
	case PostureGreen:
		return "You're on track. Nothing urgent right now."
	default:
		// Unreachable through ComputePosture. Returning an empty string rather
		// than a cheerful default, because a band this function does not
		// recognise must not be rendered as if it were fine.
		return ""
	}
}

// SeverityCounts is the open-finding tally the stat row renders.
type SeverityCounts struct {
	Critical int
	High     int
	Medium   int
	Low      int
}

// Total is every open finding across all four severities.
func (c SeverityCounts) Total() int {
	return c.Critical + c.High + c.Medium + c.Low
}

// CountSeverities tallies open findings by severity.
//
// A severity outside the four known values is counted in none of them and is
// still absent from Total, which is deliberate: the column is check-constrained,
// so an unknown value means the constraint changed without this function, and
// silently folding it into "low" would under-report a customer's exposure.
func CountSeverities(severities []string) SeverityCounts {
	var c SeverityCounts
	for _, s := range severities {
		switch s {
		case "critical":
			c.Critical++
		case "high":
			c.High++
		case "medium":
			c.Medium++
		case "low":
			c.Low++
		}
	}
	return c
}

func hasSeverity(severities []string, want string) bool {
	for _, s := range severities {
		if s == want {
			return true
		}
	}
	return false
}

func overdue(deadlines []Deadline, severity string) bool {
	for _, d := range deadlines {
		if d.Severity == severity && d.DaysRemaining < 0 {
			return true
		}
	}
	return false
}

func nearTerm(deadlines []Deadline, severity string) bool {
	for _, d := range deadlines {
		if d.Severity == severity && d.DaysRemaining >= 0 && d.DaysRemaining <= NearTermDays {
			return true
		}
	}
	return false
}
