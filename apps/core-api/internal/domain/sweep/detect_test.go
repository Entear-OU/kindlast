package sweep

import (
	"testing"
	"time"
)

// The detectors, as table tests over fixture profiles (ENT-259).
//
// This is the shape the issue was filed for. While these rules were plpgsql
// they could only be exercised through a live stack, so the case nobody thought
// to seed was the case nobody tested, and the bug PR #223 fixed (a detector
// reading a table the producer role was never granted) was invisible until a
// customer saw an empty feed. Here every branch is a row in a table and the
// date is an argument, so the thirty-day boundary is tested at the boundary
// rather than on whichever day CI happens to run.
//
// Proven able to fail: changing `days > DeadlineWindowDays` to `>=` in
// DetectDeadlines turns `at the far edge of the window` red and leaves the rest
// green, and dropping the escalation detector from Detect turns the ordering
// test red on its own.

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr[T any](v T) *T { return &v }

// today is the day every fixture below is evaluated on. A constant, so a rule
// about "within thirty days" means the same thing in January and in December.
var today = date("2026-03-01")

func baseInputs() Inputs {
	return Inputs{
		Profile: Profile{
			ID:      "profile-1",
			OrgID:   "org-1",
			HasDPO:  "no",
			HasROPA: "no",
		},
		Today: today,
		Now:   at("2026-03-01T09:00:00Z"),
	}
}

func TestDetectDeadlinesOverObligations(t *testing.T) {
	for _, tc := range []struct {
		name          string
		effective     *time.Time
		wantSignal    bool
		wantTitle     string
		wantDaysLeft  int
		wantSeverity  string
		wantDedupKey  string
		wantEffective string
	}{
		{
			name:       "no effective date is not a deadline",
			effective:  nil,
			wantSignal: false,
		},
		{
			name:       "already in force is not a deadline",
			effective:  ptr(date("2026-02-28")),
			wantSignal: false,
		},
		{
			name:          "in force today is a deadline with zero days left",
			effective:     ptr(date("2026-03-01")),
			wantSignal:    true,
			wantTitle:     "Records of Processing takes effect in 0 days",
			wantDaysLeft:  0,
			wantSeverity:  "high",
			wantDedupKey:  "deadline:obligation:art-30",
			wantEffective: "2026-03-01",
		},
		{
			// The one place the singular appears. It was three separate
			// inline CASE expressions in the plpgsql and it was wrong in none
			// of them, which is worth keeping true.
			name:          "one day away says day, not days",
			effective:     ptr(date("2026-03-02")),
			wantSignal:    true,
			wantTitle:     "Records of Processing takes effect in 1 day",
			wantDaysLeft:  1,
			wantSeverity:  "high",
			wantDedupKey:  "deadline:obligation:art-30",
			wantEffective: "2026-03-02",
		},
		{
			name:          "at the far edge of the window",
			effective:     ptr(date("2026-03-31")),
			wantSignal:    true,
			wantTitle:     "Records of Processing takes effect in 30 days",
			wantDaysLeft:  30,
			wantSeverity:  "high",
			wantDedupKey:  "deadline:obligation:art-30",
			wantEffective: "2026-03-31",
		},
		{
			name:       "one day past the window",
			effective:  ptr(date("2026-04-01")),
			wantSignal: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInputs()
			in.Obligations = []Obligation{{
				Slug:          "art-30",
				Title:         "Records of Processing",
				Severity:      "high",
				EffectiveDate: tc.effective,
			}}

			signals := DetectDeadlines(in)
			if !tc.wantSignal {
				if len(signals) != 0 {
					t.Fatalf("wanted no signal, got %d: %+v", len(signals), signals)
				}
				return
			}
			if len(signals) != 1 {
				t.Fatalf("wanted one signal, got %d: %+v", len(signals), signals)
			}

			got := signals[0]
			if got.Kind != KindDeadline {
				t.Errorf("kind: got %q, want %q", got.Kind, KindDeadline)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", got.Title, tc.wantTitle)
			}
			if got.DedupKey != tc.wantDedupKey {
				t.Errorf("dedup key: got %q, want %q", got.DedupKey, tc.wantDedupKey)
			}
			if got.Severity != tc.wantSeverity {
				t.Errorf("severity: got %q, want %q", got.Severity, tc.wantSeverity)
			}
			if got.ObligationSlug != "art-30" {
				t.Errorf("obligation: got %q, want %q", got.ObligationSlug, "art-30")
			}
			if got.Metadata["days_remaining"] != tc.wantDaysLeft {
				t.Errorf("days_remaining: got %v, want %d",
					got.Metadata["days_remaining"], tc.wantDaysLeft)
			}
			if got.Metadata["effective_date"] != tc.wantEffective {
				t.Errorf("effective_date: got %v, want %q",
					got.Metadata["effective_date"], tc.wantEffective)
			}
			wantDetail := "This obligation's effective date (" + tc.wantEffective +
				") is within 30 days."
			if got.Detail != wantDetail {
				t.Errorf("detail: got %q, want %q", got.Detail, wantDetail)
			}
		})
	}
}

func TestDetectDeadlinesOverDSARs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		due        string
		subject    *string
		wantSignal bool
		wantTitle  string
		wantDetail string
	}{
		{
			name:       "inside the window, with a name",
			due:        "2026-03-20T12:00:00Z",
			subject:    ptr("Ada Lovelace"),
			wantSignal: true,
			wantTitle:  "DSAR response due in 19 days",
			wantDetail: "A data-subject request from Ada Lovelace has a response " +
				"deadline within 30 days and no logged response.",
		},
		{
			// A request can arrive from an address with no name attached, and
			// the sentence has to read correctly without one.
			name:       "inside the window, with no name",
			due:        "2026-03-20T12:00:00Z",
			subject:    nil,
			wantSignal: true,
			wantTitle:  "DSAR response due in 19 days",
			wantDetail: "A data-subject request has a response deadline within " +
				"30 days and no logged response.",
		},
		{
			name:       "beyond the window",
			due:        "2026-04-05T12:00:00Z",
			wantSignal: false,
		},
		{
			// The window is `now() + 30 days`, an instant, while the day count
			// is date arithmetic. So the thirtieth day is split by the time of
			// day the sweep runs at: due before it is inside the window and
			// reads 30 days, and due after it is outside. That asymmetry was in
			// the plpgsql and is preserved rather than tidied, because tidying
			// it would move the boundary for real customers.
			name:       "early on the thirtieth day is inside, and reads 30",
			due:        "2026-03-31T08:00:00Z",
			wantSignal: true,
			wantTitle:  "DSAR response due in 30 days",
			wantDetail: "A data-subject request has a response deadline within " +
				"30 days and no logged response.",
		},
		{
			name:       "later the same day is outside",
			due:        "2026-03-31T23:00:00Z",
			wantSignal: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInputs()
			due := at(tc.due)
			in.DSARs = []DSAR{{
				ID:            "dsar-1",
				SubjectName:   tc.subject,
				ResponseDueAt: due,
				DueDate:       date(tc.due[:10]),
			}}

			signals := DetectDeadlines(in)
			if !tc.wantSignal {
				if len(signals) != 0 {
					t.Fatalf("wanted no signal, got %+v", signals)
				}
				return
			}
			if len(signals) != 1 {
				t.Fatalf("wanted one signal, got %d: %+v", len(signals), signals)
			}

			got := signals[0]
			if got.Kind != KindDSAR {
				t.Errorf("kind: got %q, want %q", got.Kind, KindDSAR)
			}
			if got.DedupKey != "dsar:dsar-1" {
				t.Errorf("dedup key: got %q", got.DedupKey)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Detail != tc.wantDetail {
				t.Errorf("detail: got %q, want %q", got.Detail, tc.wantDetail)
			}
			if got.Severity != "medium" {
				t.Errorf("severity: got %q, want medium", got.Severity)
			}
			if got.ObligationSlug != DataSubjectRightsSlug {
				t.Errorf("obligation: got %q, want %q",
					got.ObligationSlug, DataSubjectRightsSlug)
			}
			if _, ok := got.Metadata["escalated"]; ok {
				t.Error("the deadline detector must not mark a signal escalated")
			}
		})
	}
}

func TestDetectDSAREscalation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		due        string
		wantSignal bool
		wantTitle  string
		wantDetail string
	}{
		{
			name:       "ten days away is outside the escalation window",
			due:        "2026-03-11T12:00:00Z",
			wantSignal: false,
		},
		{
			name:       "nine days away escalates",
			due:        "2026-03-10T12:00:00Z",
			wantSignal: true,
			wantTitle:  "URGENT: DSAR response due in 9 days",
			wantDetail: "A data-subject request is within 10 days of its GDPR " +
				"Article 12(3) one-month deadline with no logged response.",
		},
		{
			name:       "due tomorrow says day, not days",
			due:        "2026-03-02T12:00:00Z",
			wantSignal: true,
			wantTitle:  "URGENT: DSAR response due in 1 day",
			wantDetail: "A data-subject request is within 10 days of its GDPR " +
				"Article 12(3) one-month deadline with no logged response.",
		},
		{
			name:       "one day overdue says day, not days",
			due:        "2026-02-28T12:00:00Z",
			wantSignal: true,
			wantTitle:  "DSAR response is 1 day overdue",
			wantDetail: "A data-subject request is past its GDPR Article 12(3) " +
				"one-month deadline with no logged response.",
		},
		{
			name:       "well overdue counts the days it has been",
			due:        "2026-02-10T12:00:00Z",
			wantSignal: true,
			wantTitle:  "DSAR response is 19 days overdue",
			wantDetail: "A data-subject request is past its GDPR Article 12(3) " +
				"one-month deadline with no logged response.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInputs()
			in.DSARs = []DSAR{{
				ID:            "dsar-1",
				ResponseDueAt: at(tc.due),
				DueDate:       date(tc.due[:10]),
			}}

			signals := DetectDSAREscalation(in)
			if !tc.wantSignal {
				if len(signals) != 0 {
					t.Fatalf("wanted no signal, got %+v", signals)
				}
				return
			}
			if len(signals) != 1 {
				t.Fatalf("wanted one signal, got %d: %+v", len(signals), signals)
			}

			got := signals[0]
			if got.Title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Detail != tc.wantDetail {
				t.Errorf("detail: got %q, want %q", got.Detail, tc.wantDetail)
			}
			// Unconditionally critical. Severity() floors the finding at the
			// signal's band, which is what stops the Analyst talking it down.
			if got.Severity != "critical" {
				t.Errorf("severity: got %q, want critical", got.Severity)
			}
			if got.Metadata["escalated"] != true {
				t.Errorf("escalated: got %v, want true", got.Metadata["escalated"])
			}
		})
	}
}

// The property the detector order exists for.
//
// Both DSAR detectors write `dsar:{id}`, and EmitSignals applies signals in
// order onto one row per key. An urgent request is picked up by both, so
// whichever runs last is the one the person sees.
func TestEscalationHasTheLastWordOnADSAR(t *testing.T) {
	in := baseInputs()
	in.DSARs = []DSAR{{
		ID:            "dsar-1",
		ResponseDueAt: at("2026-03-05T12:00:00Z"),
		DueDate:       date("2026-03-05"),
	}}

	signals := Detect(in)
	if len(signals) != 2 {
		t.Fatalf("wanted both detectors to fire, got %d: %+v", len(signals), signals)
	}
	if signals[0].DedupKey != signals[1].DedupKey {
		t.Fatalf("the two DSAR signals must share a key, got %q and %q",
			signals[0].DedupKey, signals[1].DedupKey)
	}
	if signals[0].Severity != "medium" {
		t.Errorf("the deadline signal should come first, got %q", signals[0].Severity)
	}
	if signals[1].Severity != "critical" {
		t.Errorf("the escalation must come last, got %q; reversing the "+
			"detectors silently downgrades every urgent DSAR", signals[1].Severity)
	}
}

func TestDetectGaps(t *testing.T) {
	for _, tc := range []struct {
		name        string
		profile     Profile
		requires    []string
		dismissed   map[string]bool
		wantSignal  bool
		wantTitle   string
		wantMissing []string
		wantRecur   bool
	}{
		{
			name:       "an obligation requiring nothing raises nothing",
			profile:    Profile{HasROPA: "no"},
			requires:   nil,
			wantSignal: false,
		},
		{
			name:       "every control in place raises nothing",
			profile:    Profile{HasROPA: "yes", HasDPO: "yes"},
			requires:   []string{"ropa", "dpo"},
			wantSignal: false,
		},
		{
			// Unsure is not yes, everywhere. A founder who does not know
			// whether they have a ROPA does not have one they can produce.
			name:        "unsure is not yes",
			profile:     Profile{HasROPA: "unsure"},
			requires:    []string{"ropa"},
			wantSignal:  true,
			wantTitle:   "Profile gap: Records of Processing",
			wantMissing: []string{"ropa"},
		},
		{
			name:        "several missing controls are all listed",
			profile:     Profile{HasROPA: "no", HasDPO: "no"},
			requires:    []string{"ropa", "dpo"},
			wantSignal:  true,
			wantTitle:   "Profile gap: Records of Processing",
			wantMissing: []string{"ropa", "dpo"},
		},
		{
			name:        "only the missing ones are listed",
			profile:     Profile{HasROPA: "yes", HasDPO: "no"},
			requires:    []string{"ropa", "dpo"},
			wantSignal:  true,
			wantMissing: []string{"dpo"},
			wantTitle:   "Profile gap: Records of Processing",
		},
		{
			// An unknown token is satisfied, so the corpus running ahead of
			// the code never puts an unclearable finding in front of anybody.
			name:       "an unrecognised token raises nothing",
			profile:    Profile{},
			requires:   []string{"a_control_nobody_implemented"},
			wantSignal: false,
		},
		{
			name:        "a dismissed gap comes back saying so",
			profile:     Profile{HasROPA: "no"},
			requires:    []string{"ropa"},
			dismissed:   map[string]bool{"gap:obligation:art-30": true},
			wantSignal:  true,
			wantTitle:   "Recurring gap: Records of Processing",
			wantMissing: []string{"ropa"},
			wantRecur:   true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInputs()
			in.Profile = tc.profile
			in.DismissedGapKeys = tc.dismissed
			in.Obligations = []Obligation{{
				Slug:     "art-30",
				Title:    "Records of Processing",
				Severity: "high",
				Requires: tc.requires,
			}}

			signals := DetectGaps(in)
			if !tc.wantSignal {
				if len(signals) != 0 {
					t.Fatalf("wanted no signal, got %+v", signals)
				}
				return
			}
			if len(signals) != 1 {
				t.Fatalf("wanted one signal, got %d: %+v", len(signals), signals)
			}

			got := signals[0]
			if got.Kind != KindProfileGap {
				t.Errorf("kind: got %q, want %q", got.Kind, KindProfileGap)
			}
			if got.DedupKey != "gap:obligation:art-30" {
				t.Errorf("dedup key: got %q", got.DedupKey)
			}
			if got.Title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Metadata["recurring"] != tc.wantRecur {
				t.Errorf("recurring: got %v, want %v",
					got.Metadata["recurring"], tc.wantRecur)
			}
			missing, ok := got.Metadata["missing"].([]string)
			if !ok {
				t.Fatalf("missing: got %T, want []string", got.Metadata["missing"])
			}
			if len(missing) != len(tc.wantMissing) {
				t.Fatalf("missing: got %v, want %v", missing, tc.wantMissing)
			}
			for i := range missing {
				if missing[i] != tc.wantMissing[i] {
					t.Errorf("missing[%d]: got %q, want %q",
						i, missing[i], tc.wantMissing[i])
				}
			}
		})
	}
}

func TestGapSatisfied(t *testing.T) {
	for _, tc := range []struct {
		name    string
		token   string
		profile Profile
		want    bool
	}{
		{"ropa yes", "ropa", Profile{HasROPA: "yes"}, true},
		{"ropa no", "ropa", Profile{HasROPA: "no"}, false},
		{"ropa unsure", "ropa", Profile{HasROPA: "unsure"}, false},
		{"dpo yes", "dpo", Profile{HasDPO: "yes"}, true},
		{"dpo no", "dpo", Profile{HasDPO: "no"}, false},
		{
			"ai register satisfied when there is no AI",
			"ai_register", Profile{}, true,
		},
		{
			"ai register unsatisfied the moment there is",
			"ai_register", Profile{AISystems: []string{"a recommender"}}, false,
		},
		{
			"transfer safeguards need a documented destination",
			"transfer_safeguards", Profile{}, false,
		},
		{
			"transfer safeguards satisfied by one",
			"transfer_safeguards",
			Profile{TransferDestinations: []string{"us"}}, true,
		},
		{
			// Satisfied, not missing. See the function's doc comment for why
			// this direction is the safe one.
			"an unknown token is satisfied",
			"something_new", Profile{}, true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GapSatisfied(tc.token, tc.profile); got != tc.want {
				t.Errorf("GapSatisfied(%q): got %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}
