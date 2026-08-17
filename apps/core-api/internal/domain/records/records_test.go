package records_test

import (
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The two derived values in the records contract, tested here rather than
// through a query, for the reason the posture rule is: these ARE the acceptance
// criteria, and a rule that can only be exercised through the database is a rule
// nobody tests exhaustively.

func at(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

// A record the Executor created and nobody has opened.
func stub() records.ProcessingActivity {
	created := at(2026, time.August, 1)
	return records.ProcessingActivity{
		Name:            "No record of processing activities exists",
		SourceFindingID: "8f1c0e0e-0000-4000-8000-000000000001",
		CreatedAt:       created,
		UpdatedAt:       created,
	}
}

// Every Article 30 field filled in.
func filled() records.ProcessingActivity {
	return records.ProcessingActivity{
		Name:            "Payroll",
		Purpose:         "Paying staff",
		LegalBasis:      "Article 6(1)(b), contract",
		DataCategories:  []string{"name", "bank details"},
		Recipients:      []string{"our accountant"},
		RetentionPeriod: "7 years after employment ends",
		CreatedAt:       at(2026, time.August, 1),
		UpdatedAt:       at(2026, time.August, 2),
	}
}

func TestAnExecutorStubNobodyHasOpenedNeedsReview(t *testing.T) {
	if got := records.Completeness(stub()); got != records.ReviewNeeded {
		t.Fatalf("want %q, got %q", records.ReviewNeeded, got)
	}
}

func TestAStubStopsNeedingReviewOnceAHumanEditsIt(t *testing.T) {
	// Edited but still not finished: the prompt should change from "somebody
	// should look at this" to "you started this and did not finish".
	s := stub()
	s.UpdatedAt = s.CreatedAt.Add(time.Hour)
	s.Purpose = "Paying staff"

	if got := records.Completeness(s); got != records.Incomplete {
		t.Fatalf("want %q, got %q", records.Incomplete, got)
	}
}

func TestAFullyCompletedActivityIsComplete(t *testing.T) {
	if got := records.Completeness(filled()); got != records.Complete {
		t.Fatalf("want %q, got %q", records.Complete, got)
	}
}

// The divergence from the legacy console, asserted so it is a decision rather
// than an accident. Legacy additionally required `updated_at > created_at` for
// `complete`, which made a manual entry created with every field filled read as
// incomplete until someone touched it again. The stub case that condition
// existed for is already caught by ReviewNeeded, so it is dropped.
func TestAManualEntryCompleteOnCreationIsCompleteImmediately(t *testing.T) {
	f := filled()
	f.UpdatedAt = f.CreatedAt

	if got := records.Completeness(f); got != records.Complete {
		t.Fatalf("want %q, got %q", records.Complete, got)
	}
}

func TestEveryArticle30FieldIsRequiredForComplete(t *testing.T) {
	// Each subtest blanks exactly one field, so a rule that forgot to check a
	// field fails on that field alone rather than being masked by the others.
	for _, tc := range []struct {
		field string
		blank func(*records.ProcessingActivity)
	}{
		{"purpose", func(p *records.ProcessingActivity) { p.Purpose = "" }},
		{"legal basis", func(p *records.ProcessingActivity) { p.LegalBasis = "" }},
		{"retention", func(p *records.ProcessingActivity) { p.RetentionPeriod = "" }},
		{"data categories", func(p *records.ProcessingActivity) { p.DataCategories = nil }},
		{"recipients", func(p *records.ProcessingActivity) { p.Recipients = nil }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			p := filled()
			tc.blank(&p)
			if got := records.Completeness(p); got != records.Incomplete {
				t.Fatalf("blanking %s: want %q, got %q", tc.field, records.Incomplete, got)
			}
		})
	}
}

func TestWhitespaceIsNotAValueForArticle30(t *testing.T) {
	// A record of fact whose legal basis is a space is not a record of fact.
	p := filled()
	p.LegalBasis = "   "

	if got := records.Completeness(p); got != records.Incomplete {
		t.Fatalf("want %q, got %q", records.Incomplete, got)
	}
}

// A manually-created record has no source finding, so it can never be
// ReviewNeeded however empty it is: nobody promised it would be filled in.
func TestAnEmptyManualRecordIsIncompleteRatherThanNeedingReview(t *testing.T) {
	p := records.ProcessingActivity{
		Name:      "Something someone started",
		CreatedAt: at(2026, time.August, 1),
		UpdatedAt: at(2026, time.August, 1),
	}

	if got := records.Completeness(p); got != records.Incomplete {
		t.Fatalf("want %q, got %q", records.Incomplete, got)
	}
}

func dsar(due time.Time) records.Dsar {
	return records.Dsar{Status: "open", ResponseDueAt: due}
}

func TestUrgencyBands(t *testing.T) {
	now := at(2026, time.August, 16)

	for _, tc := range []struct {
		name string
		due  time.Time
		want string
	}{
		{"a month out is on track", at(2026, time.September, 16), records.OnTrack},
		// The Article 12(3) escalation window: ten days or fewer.
		{"eleven days out is still on track", at(2026, time.August, 27), records.OnTrack},
		{"exactly ten days out is due soon", at(2026, time.August, 26), records.DueSoon},
		{"tomorrow is due soon", at(2026, time.August, 17), records.DueSoon},
		{"today is due soon, not overdue", at(2026, time.August, 16), records.DueSoon},
		{"yesterday is overdue", at(2026, time.August, 15), records.Overdue},
		{"a month late is overdue", at(2026, time.July, 16), records.Overdue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := records.Urgency(dsar(tc.due), now); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// Answered outranks the clock in both directions. A request answered late is
// still answered, and the list should stop shouting about it.
func TestAnAnsweredRequestIsNeverOverdue(t *testing.T) {
	now := at(2026, time.August, 16)
	overdue := at(2026, time.July, 1)

	for _, tc := range []struct {
		name string
		d    records.Dsar
	}{
		{"responded_at set", records.Dsar{Status: "open", ResponseDueAt: overdue, RespondedAt: at(2026, time.July, 5)}},
		{"status responded", records.Dsar{Status: "responded", ResponseDueAt: overdue}},
		{"status closed", records.Dsar{Status: "closed", ResponseDueAt: overdue}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := records.Urgency(tc.d, now); got != records.Answered {
				t.Fatalf("want %q, got %q", records.Answered, got)
			}
		})
	}
}

func TestDaysUntilDueCountsCalendarDays(t *testing.T) {
	now := at(2026, time.August, 16)

	for _, tc := range []struct {
		name string
		due  time.Time
		want int32
	}{
		{"same day is zero however many hours are left", time.Date(2026, time.August, 16, 23, 59, 0, 0, time.UTC), 0},
		{"tomorrow is one", at(2026, time.August, 17), 1},
		{"yesterday is minus one", at(2026, time.August, 15), -1},
		{"thirty days out", at(2026, time.September, 15), 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := records.DaysUntilDue(records.Dsar{ResponseDueAt: tc.due}, now); got != tc.want {
				t.Fatalf("want %d, got %d", tc.want, got)
			}
		})
	}
}

// Counting by calendar date rather than by elapsed hours is the whole point: a
// deadline three hours away and one twenty-three hours away are both "today",
// and a handler reading "0 days" acts on both the same way. Elapsed-hours
// arithmetic would call the first 0 and the second 0 as well, but would call a
// deadline 25 hours away 1 while a human looking at a calendar says 2.
func TestDaysUntilDueDoesNotDependOnTheTimeOfDay(t *testing.T) {
	due := at(2026, time.September, 15)

	early := records.DaysUntilDue(records.Dsar{ResponseDueAt: due},
		time.Date(2026, time.August, 16, 0, 1, 0, 0, time.UTC))
	late := records.DaysUntilDue(records.Dsar{ResponseDueAt: due},
		time.Date(2026, time.August, 16, 23, 59, 0, 0, time.UTC))

	if early != late {
		t.Fatalf("the same calendar day gave %d and %d", early, late)
	}
}
