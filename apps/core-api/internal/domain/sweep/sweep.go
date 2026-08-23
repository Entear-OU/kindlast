// Package sweep holds the Watcher's detectors and the Analyst's conversion.
//
// # WHY THESE ARE GO AND NOT PLPGSQL (ENT-225 phase 3, ENT-259)
//
// Every rule in this package decides. Which obligation is worth raising against
// which profile, how urgent a deadline has become, how much work a finding is
// likely to be, what sentence to put in front of a founder: none of that must
// hold no matter who writes, and all of it could reasonably be different next
// quarter. ENT-209's classification work already moved one of these judgements
// once. `db/README.md` states the test, and these are on the Go side of it.
//
// The invariants stay where they were. `watcher_findings` keeps its check
// constraints, its partial unique index on `(profile_id, dedup_key) where
// status = 'open'`, and its row level security; `findings` keeps its unique
// index on `watcher_finding_id` and its foreign key to `obligations`. Nothing
// here can produce a row those refuse, and if it tries the database says so.
//
// # WHAT THIS PACKAGE CANNOT REACH, AND WHY THAT IS THE POINT
//
// No database handle, no proto, no clock of its own. Everything a detector
// needs arrives as an argument, including today's date, which is what lets the
// deadline rules be table-tested at a date rather than only on the day the
// suite happens to run. The bug class ENT-259 names is a detector reading a
// table nobody granted the producer role, invisible because plpgsql only ever
// runs through a live stack. Splitting the reads (the store) from the rules
// (here) is what turns the first half into a query a grant test can see and
// the second half into a table test.
package sweep

import "time"

// Profile is what an organisation said about itself, as the detectors read it.
//
// A subset of `compliance_profiles`, deliberately: the columns the rules below
// consult and no others. A field arriving here that nothing reads is a column
// the producer role is granted for no reason.
type Profile struct {
	ID    string
	OrgID string

	// The three yes/no/unsure answers. Unsure is not yes, everywhere.
	HasDPO             string
	HasROPA            string
	TransfersOutsideEU string

	AISystems            []string
	TransferDestinations []string
	DataCategories       []string
	VendorList           string
}

// Obligation is one corpus row that has already been found to apply.
//
// Applicability is not decided here. `watcher_obligation_applies` reads
// `org_profile_facts` to answer it and is shared with the agentic Watcher's
// context assembly (ENT-258) and with the narrative service, so it stays one
// evaluator in one place rather than becoming two that can disagree. The store
// filters with it and hands this package the obligations that survived.
type Obligation struct {
	Slug     string
	Title    string
	Severity string

	// EffectiveDate is nil for an obligation already in force.
	EffectiveDate *time.Time

	// Requires is `applies_when -> 'requires'`, the controls this obligation
	// needs in place. Empty when the obligation declares none.
	Requires []string
}

// DSAR is one data-subject request that is still owed a response.
type DSAR struct {
	ID string
	// SubjectName is nil when the request was logged without one.
	SubjectName *string
	// ResponseDueAt is the instant the response is owed by, which is what the
	// thirty-day window is compared against and what the signal's metadata
	// carries.
	ResponseDueAt time.Time
	// DueDate is `response_due_at::date`, read from Postgres rather than
	// derived here.
	//
	// The cast is evaluated in the database's time zone, and a Go process in
	// another one deriving it from the instant would move the day count for
	// every request near a midnight boundary. Casting is not a decision, so
	// leaving it in the query costs nothing this move was trying to buy.
	DueDate time.Time
}

// Signal is one thing the Watcher says is worth looking at.
//
// The same shape `emit_watcher_finding` took as arguments, because the writer
// is unchanged: one row per `(profile_id, dedup_key)` while it is open, updated
// in place when the condition is still true. See EmitSignals in the store.
type Signal struct {
	Kind           string
	DedupKey       string
	Title          string
	Detail         string
	Severity       string
	ObligationSlug string
	Metadata       map[string]any
}

// Inputs is everything one profile's detectors read.
//
// Today is passed rather than taken from the clock so a deadline rule can be
// tested at a date. It is the same `current_date` the plpgsql used, which is
// the database's date in its own time zone; the store reads it from Postgres
// for that reason rather than calling time.Now, so a Go process in a different
// zone does not silently move every window by a day.
type Inputs struct {
	Profile Profile
	// Obligations that apply to this profile.
	Obligations []Obligation
	// DSARs still open or in progress, with no logged response.
	DSARs []DSAR
	// DismissedGapKeys are the dedup keys of gap signals this profile has
	// dismissed. The re-surface rule reads it; see DetectGaps.
	DismissedGapKeys map[string]bool

	// Today is the database's `current_date`, and Now its `now()`.
	//
	// Both, because the plpgsql used both and the difference is visible: the
	// day counts are date arithmetic, and the DSAR window is an instant
	// comparison, so a request due late on the thirtieth day is inside the
	// window while its day count still reads 30.
	Today time.Time
	Now   time.Time
}

// DeadlineWindowDays is how far ahead the deadline detector looks.
const DeadlineWindowDays = 30

// EscalationWindowDays is how close a DSAR deadline has to be before the
// escalation detector restates it as critical.
const EscalationWindowDays = 10
