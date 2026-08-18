package onboarding_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/onboarding"
)

// The interview's rules, without a database and without a model (ENT-212).
//
// These are table tests over pure functions because that is what the rules are:
// which questions exist, what an answer is allowed to become, and what an
// unanswered question projects to. AGENTS.md puts decisions in Go precisely so
// they can be exercised like this rather than through a live stack.
//
// The one worth reading twice is the projection: it decides what the Watcher
// sees, so its defaults decide which findings a customer gets.

func TestEveryQuestionAsksAboutAFactTheProductUnderstands(t *testing.T) {
	// The failure this catches is quiet. A question naming a key the memory
	// vocabulary does not hold would collect answers nobody could store, and
	// the person would find out at the confirmation step, after the interview.
	for _, question := range onboarding.Script() {
		if _, known := memory.Kinds[question.Key]; !known {
			t.Errorf("question %q asks about a fact that does not exist", question.Key)
		}
		if question.Prompt == "" {
			t.Errorf("question %q has no prompt", question.Key)
		}
	}
}

func TestTheInterviewCoversEverythingTheWatcherReads(t *testing.T) {
	// `watcher_obligation_applies` and `watcher_gap_satisfied` read these out
	// of the compliance profile. A question missing for any of them means the
	// column takes its default forever and the organisation is quietly told a
	// set of obligations that was never about them.
	//
	// NOT ENT-246'S FOUR, AND THAT IS A DECISION RATHER THAN AN OVERSIGHT.
	// `high_risk_processing`, `high_risk_ai_system`, `large_scale_monitoring`
	// and `lawful_bases` are read straight out of `org_profile_facts`, and an
	// absent one means the obligation does not apply, which is the direction
	// ENT-246 chose deliberately. Adding them here would mean asking a founder
	// four questions that are legal tests rather than facts about their
	// business ("is your processing likely to result in a high risk to the
	// rights and freedoms of natural persons"), and a wrong answer either
	// asserts an expensive Data Protection Impact Assessment or hides one.
	// Phrasing those so a non-lawyer can answer them correctly is a product
	// problem that deserves its own review, not four lines appended to a
	// script. They are answerable today on the memory page.
	needed := []string{
		memory.KeyAISystems,
		memory.KeyTransfersOutsideEU,
		memory.KeyTransferDestination,
		memory.KeyStaffCount,
		memory.KeyVendorList,
		memory.KeyHasROPA,
		memory.KeyHasDPO,
	}

	asked := map[string]bool{}
	for _, question := range onboarding.Script() {
		asked[question.Key] = true
	}
	for _, key := range needed {
		if !asked[key] {
			t.Errorf("the Watcher reads %q and the interview never asks about it", key)
		}
	}
}

func TestParsingAnAnswer(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		answer  string
		want    string
		refused bool
	}{
		{
			name:   "text is kept as typed",
			key:    memory.KeyIndustry,
			answer: "  we sell scheduling software to dentists ",
			want:   `"we sell scheduling software to dentists"`,
		},
		{
			name:   "a list splits on commas",
			key:    memory.KeyEUJurisdictions,
			answer: "Ireland, Spain, Portugal",
			want:   `["Ireland","Spain","Portugal"]`,
		},
		{
			name:   "a list splits on semicolons and drops an Oxford comma's and",
			key:    memory.KeyEUJurisdictions,
			answer: "Ireland; Spain, and Portugal",
			want:   `["Ireland","Spain","Portugal"]`,
		},
		{
			// The limitation, asserted rather than left to be discovered.
			// Splitting on a bare " and " would read better here and would
			// turn "Marks and Spencer" into two processors on the question
			// two lines up the script. A clumsy item somebody can see and fix
			// beats an invented one.
			name:   "a bare and is not a separator, because company names contain it",
			key:    memory.KeyVendorList,
			answer: "Stripe, Marks and Spencer",
			want:   `["Stripe","Marks and Spencer"]`,
		},
		{
			name:   "yes is yes",
			key:    memory.KeyHasDPO,
			answer: "Yes",
			want:   `"yes"`,
		},
		{
			name:   "unsure is a real answer",
			key:    memory.KeyHasROPA,
			answer: "unsure",
			want:   `"unsure"`,
		},
		{
			// The single most important refusal here. "Probably" resolved to
			// yes or no would be the product deciding something about a
			// customer's legal position that nobody told it.
			name:    "a tri-state will not be interpreted",
			key:     memory.KeyHasROPA,
			answer:  "probably, I think Dave made one",
			refused: true,
		},
		{
			name:   "a number is a number",
			key:    memory.KeyStaffCount,
			answer: " 12 ",
			want:   `12`,
		},
		{
			// The legacy extraction prompt said "if they gave a range, take the
			// midpoint", which is a model inventing a headcount. A threshold
			// obligation then applies or does not because of an average nobody
			// stated.
			name:    "prose is not a number",
			key:     memory.KeyStaffCount,
			answer:  "about fifty, maybe sixty",
			refused: true,
		},
		{
			name:    "a blank answer is refused rather than stored as nothing",
			key:     memory.KeyIndustry,
			answer:  "   ",
			refused: true,
		},
		{
			name:    "a list of nothing but separators is refused",
			key:     memory.KeyDataCategories,
			answer:  " , ; , ",
			refused: true,
		},
		{
			name:    "a question the product does not ask",
			key:     "favourite_colour",
			answer:  "blue",
			refused: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := onboarding.Parse(tc.key, tc.answer)
			if tc.refused {
				if err == nil {
					t.Fatalf("Parse(%q, %q) = %s, want a refusal", tc.key, tc.answer, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q, %q): %v", tc.key, tc.answer, err)
			}
			if got != tc.want {
				t.Fatalf("Parse(%q, %q) = %s, want %s", tc.key, tc.answer, got, tc.want)
			}
		})
	}
}

func TestWhereDataGoesIsNotAskedOfSomebodyWhoSendsItNowhere(t *testing.T) {
	answers := onboarding.Answers{
		memory.KeyTransfersOutsideEU: {Text: "no", ValueJSON: `"no"`},
	}
	if onboarding.Applicable(memory.KeyTransferDestination, answers) {
		t.Fatal("asked where data goes after being told it goes nowhere")
	}

	answers[memory.KeyTransfersOutsideEU] = onboarding.Answer{Text: "yes", ValueJSON: `"yes"`}
	if !onboarding.Applicable(memory.KeyTransferDestination, answers) {
		t.Fatal("did not ask where data goes after being told it leaves the EU")
	}

	// Unsure keeps the question, because an organisation that can name a
	// destination has answered the earlier one by doing so.
	answers[memory.KeyTransfersOutsideEU] = onboarding.Answer{Text: "unsure", ValueJSON: `"unsure"`}
	if !onboarding.Applicable(memory.KeyTransferDestination, answers) {
		t.Fatal("stopped asking where data goes on an unsure answer")
	}
}

func TestTheInterviewWalksForwardAndStops(t *testing.T) {
	answers := onboarding.Answers{}

	first, more := onboarding.NextQuestion(answers)
	if !more {
		t.Fatal("an empty interview has no first question")
	}
	if first.Key != memory.KeyIndustry {
		t.Fatalf("the interview opens with %q, want %q", first.Key, memory.KeyIndustry)
	}

	// Answer everything applicable, saying nothing leaves the EU so the
	// destinations question drops out.
	for {
		question, more := onboarding.NextQuestion(answers)
		if !more {
			break
		}
		answer := onboarding.Answer{Text: "x", ValueJSON: `"x"`}
		if question.Key == memory.KeyTransfersOutsideEU {
			answer = onboarding.Answer{Text: "no", ValueJSON: `"no"`}
		}
		answers[question.Key] = answer
	}

	if _, more := onboarding.NextQuestion(answers); more {
		t.Fatal("the interview never ends")
	}
	if _, asked := answers[memory.KeyTransferDestination]; asked {
		t.Fatal("asked where data goes despite being told it goes nowhere")
	}

	total, done := onboarding.Progress(answers)
	if total != done {
		t.Fatalf("progress is %d of %d after answering everything", done, total)
	}
}

func TestASkipCountsAsAnsweredRatherThanBeingAskedAgain(t *testing.T) {
	answers := onboarding.Answers{memory.KeyIndustry: {Skipped: true}}
	next, more := onboarding.NextQuestion(answers)
	if !more {
		t.Fatal("skipping the first question ended the interview")
	}
	if next.Key == memory.KeyIndustry {
		t.Fatal("a skipped question was asked again")
	}
}

func TestProjectingWhatWeBelieveOntoWhatTheWatcherReads(t *testing.T) {
	projected, err := onboarding.Project(map[string]string{
		memory.KeyIndustry:            `"scheduling software"`,
		memory.KeyEUJurisdictions:     `["Ireland","Spain"]`,
		memory.KeyAISystems:           `["ChatGPT (internal)"]`,
		memory.KeyHasROPA:             `"no"`,
		memory.KeyTransfersOutsideEU:  `"yes"`,
		memory.KeyTransferDestination: `["United States (Stripe)"]`,
		memory.KeyVendorList:          `["Stripe","Hetzner"]`,
		memory.KeyStaffCount:          `9`,
	})
	if err != nil {
		t.Fatalf("projecting: %v", err)
	}

	if projected.Industry != "scheduling software" {
		t.Fatalf("industry is %q", projected.Industry)
	}
	// The list becomes the line the plpgsql tests with `btrim(...) <> ''`.
	if projected.VendorList != "Stripe, Hetzner" {
		t.Fatalf("vendor list is %q, want the joined line", projected.VendorList)
	}
	if projected.StaffCount == nil || *projected.StaffCount != 9 {
		t.Fatalf("staff count is %v", projected.StaffCount)
	}
	if projected.HasROPA != "no" {
		t.Fatalf("has_ropa is %q", projected.HasROPA)
	}
	// Never asked. `unsure` and not `no`, because `no` would claim somebody
	// said so; both raise the gap, and only one of them is true.
	if projected.HasDPO != "unsure" {
		t.Fatalf("an unanswered has_dpo projected to %q, want unsure", projected.HasDPO)
	}
}

func TestAnEmptyProfileProjectsToSomethingTheColumnsAccept(t *testing.T) {
	projected, err := onboarding.Project(map[string]string{})
	if err != nil {
		t.Fatalf("projecting nothing: %v", err)
	}

	// The three tri-state columns carry a check constraint, so a zero value
	// would be rejected by the database rather than by anything here.
	for name, value := range map[string]string{
		"has_dpo":              projected.HasDPO,
		"has_ropa":             projected.HasROPA,
		"transfers_outside_eu": projected.TransfersOutsideEU,
	} {
		if value != "unsure" {
			t.Errorf("%s projected to %q, want unsure", name, value)
		}
	}

	// Empty rather than nil: the array columns are `not null default '{}'`, and
	// a nil slice reaches the driver as null.
	if projected.EUJurisdictions == nil || projected.AISystems == nil ||
		projected.DataCategories == nil || projected.DataSubjects == nil ||
		projected.TransferDestinations == nil {
		t.Fatal("an array projected to nil, which the not-null columns refuse")
	}

	// Null, and deliberately: `watcher_obligation_applies` reads null as
	// "unknown, so the obligation applies". A number here would decide a
	// threshold nobody stated.
	if projected.StaffCount != nil {
		t.Fatalf("an unanswered staff count projected to %d", *projected.StaffCount)
	}
}

func TestTheProjectionRefusesAFactOfTheWrongShape(t *testing.T) {
	// Not reachable through the interview, which parses before it stores. It is
	// reachable through a fact written by something else, and the projection is
	// the last place to notice before a wrong value reaches the Watcher.
	if _, err := onboarding.Project(map[string]string{
		memory.KeyStaffCount: `"about fifty"`,
	}); err == nil {
		t.Fatal("projected a text value into the staff count column")
	}
}
