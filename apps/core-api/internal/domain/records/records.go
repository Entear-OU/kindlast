// Package records holds the domain rules for the compliance record: the
// Article 30 register, the AI Act system register, and the DSAR log.
//
// Pure functions over already-loaded data, no database and no proto, for the
// same reason `domain/findings` is written that way: the two derived values
// here are acceptance criteria rather than presentation, and a rule that can
// only be exercised through a query is a rule nobody tests exhaustively.
//
// Both were derived in TypeScript in the legacy console, which was fine while
// there was one client. `Urgency` in particular encodes a regulatory threshold,
// and a second implementation of a statutory deadline is a second thing that
// can drift from the law.
package records

import (
	"strings"
	"time"
)

// Completeness bands for an Article 30 entry.
const (
	// Complete means every Article 30 field this schema carries is present.
	Complete = "complete"
	// Incomplete means a human has it in hand and has not finished.
	Incomplete = "incomplete"
	// ReviewNeeded means the Executor created it and nobody has opened it.
	ReviewNeeded = "review_needed"
)

// Urgency bands for a data-subject request.
const (
	Overdue  = "overdue"
	DueSoon  = "due_soon"
	OnTrack  = "on_track"
	Answered = "answered"
)

// EscalationDays is the Article 12(3) escalation window: at or inside this many
// days from the deadline, a request is `due_soon`.
//
// Ten is the legacy console's threshold, preserved deliberately rather than
// re-chosen. It is late enough to be urgent and early enough that a month-long
// response can still be assembled, and moving it is a product decision rather
// than a tuning one.
const EscalationDays = 10

// ProcessingActivity is one entry in the Article 30 record.
type ProcessingActivity struct {
	ID              string
	Name            string
	Purpose         string
	LegalBasis      string
	DataCategories  []string
	Recipients      []string
	RetentionPeriod string
	// Set when the Executor created this from an approved finding.
	SourceFindingID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AiSystem is one entry in the AI Act register.
type AiSystem struct {
	ID                  string
	Name                string
	Vendor              string
	Purpose             string
	RiskClassification  string
	DocumentationStatus string
	// Zero means never reviewed, which is the state worth showing rather than
	// hiding behind a created date.
	LastReviewedAt  time.Time
	SourceFindingID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Dsar is one data-subject request and the clock on it.
type Dsar struct {
	ID            string
	SubjectName   string
	RequestType   string
	Status        string
	ReceivedAt    time.Time
	ResponseDueAt time.Time
	// Zero until a response has gone out.
	RespondedAt     time.Time
	Handler         string
	SourceFindingID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Page is one page of any register, with the cursor for the next.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// Quota is a plan limit on manually-created records and usage against it.
type Quota struct {
	Used int32
	// Zero means unlimited.
	Limit int32
}

// Completeness bands one Article 30 entry.
//
// Precedence is ReviewNeeded, then Complete, then Incomplete, and the first is
// the one that carries information the other two cannot. An entry the Executor
// created exists because an approval said it must, and until someone opens it
// nobody has looked at it: that is a different state from a half-finished entry
// and wants a different prompt.
//
// # DIVERGENCE FROM THE LEGACY RULE, DELIBERATE
//
// The legacy console additionally required `updated_at > created_at` before
// calling anything complete, so that an Executor stub could never read as
// complete without a human having looked. The consequence was that a manual
// entry created with every field filled in read as incomplete until somebody
// touched it a second time, which is a lie about a record that is in fact
// complete.
//
// That condition is dropped because it is redundant here: a stub is caught by
// ReviewNeeded before completeness is considered, and a stub has empty fields
// anyway, so it cannot reach Complete by either route.
func Completeness(p ProcessingActivity) string {
	if p.SourceFindingID != "" && !p.UpdatedAt.After(p.CreatedAt) {
		return ReviewNeeded
	}

	if blank(p.Purpose) || blank(p.LegalBasis) || blank(p.RetentionPeriod) ||
		len(p.DataCategories) == 0 || len(p.Recipients) == 0 {
		return Incomplete
	}

	return Complete
}

// blank reports whether a field carries nothing a reader could act on.
//
// Whitespace counts as blank. Article 30 is a record of fact, and an entry
// whose legal basis is a single space is not a record of fact; treating it as
// one would let a register report itself complete on nothing.
func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// Urgency bands a data-subject request against its statutory deadline.
//
// Answered outranks the clock in both directions. A request answered late is
// still answered, and a list that keeps calling it overdue is asking a handler
// to act on something already done.
//
// `RespondedAt` and the two terminal statuses are both checked because they can
// disagree: `mark_dsar_responded` sets both, but a status moved to `closed` by
// some other path (a request withdrawn, a duplicate) has no response date and is
// equally finished.
func Urgency(d Dsar, now time.Time) string {
	if !d.RespondedAt.IsZero() || d.Status == "responded" || d.Status == "closed" {
		return Answered
	}

	switch days := DaysUntilDue(d, now); {
	case days < 0:
		return Overdue
	case days <= EscalationDays:
		return DueSoon
	default:
		return OnTrack
	}
}

// DaysUntilDue is whole calendar days from now to the deadline, negative once
// it has passed.
//
// Counted by calendar date in UTC rather than by elapsed hours, because that is
// how the deadline is read. A request due at 09:00 tomorrow and one due at 23:00
// tomorrow are both "due tomorrow" to the person handling them, and dividing an
// elapsed duration by 24 would call the first 0 and the second 1 depending on
// what time the page was loaded.
//
// The consequence worth stating: a deadline later today is 0 rather than
// negative, so `Urgency` calls it `due_soon` and not `overdue`. A request is not
// late until the day is out.
func DaysUntilDue(d Dsar, now time.Time) int32 {
	due := startOfDay(d.ResponseDueAt)
	today := startOfDay(now)
	return int32(due.Sub(today).Hours() / 24)
}

func startOfDay(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
