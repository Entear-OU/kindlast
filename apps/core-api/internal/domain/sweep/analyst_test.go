package sweep

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// The Analyst's four judgements, as table tests (ENT-259).
//
// Severity is the one that earns the table. It has four inputs that compose,
// and while it was plpgsql the only way to ask "what does a critical obligation
// about health data two days from its deadline come out as" was to seed a stack
// and run a sweep. Every combination below was unreachable in practice.
//
// Proven able to fail: removing the sensitivity step from Severity turns
// `sensitive data adds a band` red on its own; removing the floor turns
// `never below the signal's own band` red and nothing else.

func TestSeverity(t *testing.T) {
	for _, tc := range []struct {
		name           string
		baseline       string
		signalSeverity string
		kind           string
		daysRemaining  *int
		dataCategories []string
		want           string
	}{
		{
			name:     "the obligation's baseline, with nothing to modify it",
			baseline: "high",
			kind:     KindProfileGap,
			want:     "high",
		},
		{
			// The column's own default. An obligation whose severity the
			// corpus does not recognise is medium rather than a guess.
			name:     "an unrecognised baseline is medium",
			baseline: "urgent-ish",
			kind:     KindProfileGap,
			want:     "medium",
		},
		{
			name:     "an absent baseline is medium",
			baseline: "",
			kind:     KindProfileGap,
			want:     "medium",
		},
		{
			name:          "seven days out is still the baseline",
			baseline:      "medium",
			kind:          KindDeadline,
			daysRemaining: ptr(7),
			want:          "medium",
		},
		{
			name:          "inside a week adds a band",
			baseline:      "medium",
			kind:          KindDeadline,
			daysRemaining: ptr(6),
			want:          "high",
		},
		{
			name:          "inside three days adds two",
			baseline:      "medium",
			kind:          KindDeadline,
			daysRemaining: ptr(2),
			want:          "critical",
		},
		{
			// Proximity keeps counting after the date has passed, and the
			// clamp is what stops it running off the end of the scale.
			name:          "overdue is clamped at critical",
			baseline:      "high",
			kind:          KindDeadline,
			daysRemaining: ptr(-40),
			want:          "critical",
		},
		{
			name:           "sensitive data adds a band",
			baseline:       "medium",
			kind:           KindProfileGap,
			dataCategories: []string{"customer health records"},
			want:           "high",
		},
		{
			// Substring matching, so a free-text answer counts. See
			// sensitiveMarkers for why the false positive is the safe side.
			name:           "sensitivity is matched inside a phrase",
			baseline:       "low",
			kind:           KindProfileGap,
			dataCategories: []string{"names", "employee bank details"},
			want:           "medium",
		},
		{
			name:           "one band for sensitivity however many categories match",
			baseline:       "medium",
			kind:           KindProfileGap,
			dataCategories: []string{"health", "biometric", "criminal"},
			want:           "high",
		},
		{
			name:           "ordinary categories add nothing",
			baseline:       "medium",
			kind:           KindProfileGap,
			dataCategories: []string{"names", "email addresses"},
			want:           "medium",
		},
		{
			name:     "a regulatory update adds a band on its own",
			baseline: "medium",
			kind:     KindRegulatoryUpdate,
			want:     "high",
		},
		{
			name:           "the steps compose",
			baseline:       "medium",
			kind:           KindDeadline,
			daysRemaining:  ptr(1),
			dataCategories: []string{"genetic data"},
			want:           "critical",
		},
		{
			// The property the escalation detector depends on. Its signal is
			// written critical, and no arithmetic here may talk it down.
			name:           "never below the signal's own band",
			baseline:       "low",
			signalSeverity: "critical",
			kind:           KindDSAR,
			want:           "critical",
		},
		{
			name:           "a lower signal band does not pull the finding down",
			baseline:       "high",
			signalSeverity: "low",
			kind:           KindProfileGap,
			want:           "high",
		},
		{
			name:           "an unrecognised signal band is ignored rather than floored",
			baseline:       "medium",
			signalSeverity: "spicy",
			kind:           KindProfileGap,
			want:           "medium",
		},
		{
			name:     "never below low",
			baseline: "low",
			kind:     KindProfileGap,
			want:     "low",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Severity(tc.baseline, tc.signalSeverity, tc.kind,
				tc.daysRemaining, tc.dataCategories)
			if got != tc.want {
				t.Errorf("Severity: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffort(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{KindDSAR, "hours"},
		{KindRegulatoryUpdate, "hours"},
		{KindDeadline, "days"},
		{KindProfileGap, "days"},
		{"something new", "hours"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			if got := Effort(tc.kind); got != tc.want {
				t.Errorf("Effort(%q): got %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestProposedAction(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{KindDeadline, "Review this obligation and prepare to meet its upcoming deadline."},
		{KindProfileGap, "Put the missing control in place to satisfy this obligation."},
		{KindDSAR, "Prepare and log a response to this data-subject request before its deadline."},
		{KindRegulatoryUpdate, "Review this regulatory update and assess its impact on your obligations."},
		{"something new", "Review this finding and take the appropriate action."},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			if got := ProposedAction(tc.kind); got != tc.want {
				t.Errorf("ProposedAction(%q): got %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

// What Convert assembles, end to end over one signal.
func TestConvert(t *testing.T) {
	signal := AnalysedSignal{
		ID:            "signal-1",
		Kind:          KindDeadline,
		Title:         "Records of Processing takes effect in 2 days",
		DedupKey:      "deadline:obligation:gdpr-art-30-ropa",
		Severity:      "medium",
		DaysRemaining: ptr(2),
	}
	obligation := CitedObligation{
		ID:         "obligation-1",
		Slug:       "gdpr-art-30-ropa",
		Summary:    "Keep a record of your processing activities.",
		Severity:   "high",
		ActionType: "create_ropa",
		Citation: corpus.Citation{
			Kind:          corpus.KindArticle,
			Celex:         "32016R0679",
			ArticleNumber: 30,
		},
	}

	got := Convert(signal, obligation, []string{"names"})

	// The signal's title is what was noticed, and it becomes `detected`
	// verbatim. ENT-60 preserves it across re-runs, so getting it wrong here
	// is not something a later sweep repairs.
	if got.Detected != signal.Title {
		t.Errorf("detected: got %q, want %q", got.Detected, signal.Title)
	}
	// baseline high, two days out adds two, clamped.
	if got.Severity != "critical" {
		t.Errorf("severity: got %q, want critical", got.Severity)
	}
	if got.ProposedAction != ProposedAction(KindDeadline) {
		t.Errorf("proposed action: got %q", got.ProposedAction)
	}
	if got.RegulatoryObligation != "GDPR Art. 30" {
		t.Errorf("citation label: got %q, want %q", got.RegulatoryObligation, "GDPR Art. 30")
	}
	if got.CitationURL != "https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_30" {
		t.Errorf("citation url: got %q", got.CitationURL)
	}
	if got.SupportingContext != obligation.Summary {
		t.Errorf("supporting context: got %q", got.SupportingContext)
	}
	if got.Effort != "days" {
		t.Errorf("effort: got %q, want days", got.Effort)
	}
	// From the obligation, not the signal. ENT-165: what approving this should
	// do is a property of the regulatory requirement, so the same obligation
	// creates a ROPA entry whichever detector noticed it.
	if got.ActionType != "create_ropa" {
		t.Errorf("action type: got %q, want create_ropa", got.ActionType)
	}
}
