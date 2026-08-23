package sweep

import (
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// The Analyst: turning a signal into a finding a person can act on.
//
// Four judgements, all of which were `analyst_severity`, `analyst_effort`,
// `analyst_citation_label`, `analyst_citation_url` and the CASE expression
// inside `analyst_convert_signal`. Each one consults a status, a threshold or a
// kind and could reasonably be different next quarter, which is ENT-225's test
// for a decision.
//
// The citation half lives in `domain/corpus` rather than here, because a
// citation is a property of the regulation and is rendered identically for the
// obligation page. One renderer, two callers; see that file's header for the
// divergence this arrangement exists to prevent.

// Severity bands, worst last. The order is the arithmetic: the rule below adds
// and subtracts positions in this slice.
var severityBands = []string{"low", "medium", "high", "critical"}

// bandOf maps a severity name onto its position, 1-based, 0 for unrecognised.
func bandOf(severity string) int {
	for i, s := range severityBands {
		if s == strings.ToLower(severity) {
			return i + 1
		}
	}
	return 0
}

// sensitiveMarkers are the substrings that make a data category special.
//
// Substring matching rather than equality, deliberately: these are free-text
// answers a founder typed during onboarding, so "customer health records" and
// "employee financial data" both have to count. The cost is that a category
// mentioning one of these words in passing raises severity by one band, which
// is the safe direction to be wrong in for a compliance product.
//
// GDPR Article 9's special categories, plus the ones Article 10 and the
// supervisory authorities treat with the same care in practice.
var sensitiveMarkers = []string{
	"health", "medical", "biometric", "genetic", "financial", "bank", "payment",
	"children", "child", "racial", "ethnic", "religious", "sexual", "criminal",
	"political",
}

// Severity is how urgent a finding is, given the obligation, the signal and
// what the organisation processes.
//
// Four inputs, in the order they are applied:
//
//   - the obligation's own baseline, which is the corpus's opinion before it
//     meets a customer, defaulting to medium when it says nothing;
//   - proximity, which is why a deadline gets worse as it approaches rather
//     than only when it passes;
//   - data sensitivity, one band for any special category;
//   - recency, one band for a regulatory update, because a change that has just
//     taken effect is the one nobody has read yet.
//
// Then it is clamped to the four bands and floored at the signal's own
// severity. That last step is what makes an escalated DSAR stay critical: the
// escalation detector already decided, and an arithmetic that could talk it
// back down would silently undo a rule written to be loud.
//
// daysRemaining is nil when the signal carries none, which is every signal that
// is not about a date.
func Severity(baseline, signalSeverity, kind string, daysRemaining *int, dataCategories []string) string {
	level := bandOf(baseline)
	if level == 0 {
		// An obligation whose severity the corpus does not recognise is
		// treated as medium, the same default the column carries.
		level = 2
	}

	if daysRemaining != nil {
		switch {
		case *daysRemaining < 3:
			level += 2
		case *daysRemaining < 7:
			level++
		}
	}

	for _, category := range dataCategories {
		if isSensitive(category) {
			level++
			break
		}
	}

	if kind == KindRegulatoryUpdate {
		level++
	}

	level = max(1, min(len(severityBands), level))

	// Never below what the signal itself said.
	if signal := bandOf(signalSeverity); signal > level {
		level = signal
	}

	return severityBands[level-1]
}

func isSensitive(category string) bool {
	lower := strings.ToLower(category)
	for _, marker := range sensitiveMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// Signal kinds. The same four `watcher_findings_kind_check` allows, named here
// so a typo is a compile error rather than a row the constraint refuses at
// runtime.
const (
	KindDeadline         = "deadline"
	KindProfileGap       = "profile_gap"
	KindDSAR             = "dsar"
	KindRegulatoryUpdate = "regulatory_update"
)

// Effort is how much work a finding is likely to be.
//
// A coarse two-value estimate, and coarse on purpose: the question it answers
// is "can I do this before lunch or do I need to book time", and a product that
// pretended to know more than that would be inventing precision. Anything
// unrecognised is hours, which is the answer that gets a founder to look.
func Effort(kind string) string {
	switch kind {
	case KindDSAR, KindRegulatoryUpdate:
		return "hours"
	case KindDeadline, KindProfileGap:
		return "days"
	default:
		return "hours"
	}
}

// ProposedAction is the sentence put in front of the person, telling them what
// this finding is asking of them.
//
// Per signal kind rather than per obligation, because the kind is what says
// whether the work is "prepare for something coming" or "put something in
// place that is missing". What the obligation itself requires is in its summary,
// which travels beside this as the supporting context.
func ProposedAction(kind string) string {
	switch kind {
	case KindDeadline:
		return "Review this obligation and prepare to meet its upcoming deadline."
	case KindProfileGap:
		return "Put the missing control in place to satisfy this obligation."
	case KindDSAR:
		return "Prepare and log a response to this data-subject request before its deadline."
	case KindRegulatoryUpdate:
		return "Review this regulatory update and assess its impact on your obligations."
	default:
		return "Review this finding and take the appropriate action."
	}
}

// CitedObligation is the obligation a signal resolved to, as the Analyst reads
// it.
type CitedObligation struct {
	ID         string
	Slug       string
	Summary    string
	Severity   string
	ActionType string
	Citation   corpus.Citation
}

// AnalysedSignal is one open signal on its way to becoming a finding.
type AnalysedSignal struct {
	ID       string
	Kind     string
	Title    string
	DedupKey string
	Severity string
	// DaysRemaining is `metadata ->> 'days_remaining'`, nil when absent.
	DaysRemaining *int
	MetadataJSON  string
}

// Finding is what the Analyst proposes, before it is written.
//
// Field for field what `analyst_convert_signal` inserted, so the store's job is
// a parameterised upsert and nothing else.
type Finding struct {
	Detected             string
	Severity             string
	ProposedAction       string
	RegulatoryObligation string
	CitationURL          string
	SupportingContext    string
	Effort               string
	ActionType           string
}

// Convert turns one signal and the obligation it cites into a finding.
//
// The signal's title becomes `detected`, which is the sentence saying what was
// noticed; the obligation supplies the citation, the summary and what approving
// the finding should do. dataCategories comes from the profile and only affects
// severity.
//
// A signal citing no resolvable obligation never reaches here. That refusal
// belongs to the caller because it is a skip rather than a value: the plpgsql
// logged and returned NULL, and the store does the same thing with a reason
// somebody can read.
func Convert(signal AnalysedSignal, obligation CitedObligation, dataCategories []string) Finding {
	return Finding{
		Detected: signal.Title,
		Severity: Severity(
			obligation.Severity, signal.Severity, signal.Kind,
			signal.DaysRemaining, dataCategories,
		),
		ProposedAction:       ProposedAction(signal.Kind),
		RegulatoryObligation: obligation.Citation.Label(),
		CitationURL:          obligation.Citation.URL(),
		SupportingContext:    obligation.Summary,
		Effort:               Effort(signal.Kind),
		ActionType:           obligation.ActionType,
	}
}
