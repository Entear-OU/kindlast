package sweep

import (
	"fmt"
	"time"
)

// The Watcher's deterministic detectors (ENT-55, ENT-56, ENT-57), in Go.
//
// # THE ORDER IS LOAD BEARING
//
// Detect runs deadlines, then gaps, then DSAR escalation, and that is the order
// `run_watcher_for_profile` used. It matters because the deadline detector and
// the escalation detector write the SAME deduplication key for the same
// request, `dsar:{id}`: a DSAR inside the thirty-day window produces a medium
// signal, and one inside the ten-day window produces a critical one over the
// top of it. Reversing them would leave the medium restatement last and quietly
// downgrade every urgent DSAR in the product.
//
// Stated here rather than left to the caller because a slice of signals with an
// overwrite rule inside it is exactly the kind of ordering somebody sorts for
// tidiness. EmitSignals applies them in order, and the test named for this says
// what happens if it does not.

// Detect runs every detector over one profile, in the order the signals must be
// applied.
func Detect(in Inputs) []Signal {
	var signals []Signal
	signals = append(signals, DetectDeadlines(in)...)
	signals = append(signals, DetectGaps(in)...)
	signals = append(signals, DetectDSAREscalation(in)...)
	return signals
}

// DetectDeadlines raises what is coming due within the next thirty days.
//
// Two sources, one window. An obligation whose effective date is approaching is
// the regulation itself arriving; a DSAR approaching its response deadline is
// the clock GDPR Article 12(3) started. They are separate signals because they
// ask for different work, and they share a window because thirty days is how
// far ahead a founder can usefully plan.
//
// The obligations arriving here have already been found applicable; see the
// Obligation doc comment for where that is decided.
func DetectDeadlines(in Inputs) []Signal {
	var signals []Signal

	for _, o := range in.Obligations {
		if o.EffectiveDate == nil {
			continue
		}
		effective := *o.EffectiveDate
		days := daysBetween(in.Today, effective)
		// Already in force, or beyond the window. `>= current_date` and
		// `<= current_date + 30` in the plpgsql.
		if days < 0 || days > DeadlineWindowDays {
			continue
		}

		signals = append(signals, Signal{
			Kind:     KindDeadline,
			DedupKey: "deadline:obligation:" + o.Slug,
			Title: fmt.Sprintf("%s takes effect in %d %s",
				o.Title, days, plural(days, "day")),
			Detail: fmt.Sprintf(
				"This obligation's effective date (%s) is within %d days.",
				formatDate(effective), DeadlineWindowDays),
			Severity:       o.Severity,
			ObligationSlug: o.Slug,
			Metadata: map[string]any{
				"days_remaining": days,
				"effective_date": formatDate(effective),
			},
		})
	}

	for _, d := range in.DSARs {
		// `response_due_at <= now() + interval '30 days'`, which is an instant
		// comparison rather than a date one. Kept as an instant so a request
		// due late on the thirtieth day is inside the window, exactly as it
		// was.
		if d.ResponseDueAt.After(in.Now.Add(DeadlineWindowDays * 24 * time.Hour)) {
			continue
		}
		days := daysBetween(in.Today, d.DueDate)

		signals = append(signals, Signal{
			Kind:     KindDSAR,
			DedupKey: "dsar:" + d.ID,
			Title: fmt.Sprintf("DSAR response due in %d %s",
				days, plural(days, "day")),
			Detail: fmt.Sprintf(
				"A data-subject request%s has a response deadline within %d days "+
					"and no logged response.",
				fromSubject(d.SubjectName), DeadlineWindowDays),
			Severity:       "medium",
			ObligationSlug: DataSubjectRightsSlug,
			Metadata: map[string]any{
				"days_remaining":  days,
				"dsar_id":         d.ID,
				"response_due_at": d.ResponseDueAt.UTC().Format(time.RFC3339),
			},
		})
	}

	return signals
}

// DataSubjectRightsSlug is the obligation every DSAR signal cites.
//
// One slug, hard-coded, because Article 12(3)'s one-month response deadline is
// what makes a DSAR a finding at all: a signal about a late DSAR that cited
// something else would be citing the wrong law. If the corpus stops carrying
// it, the Analyst refuses to convert the signal and says so, which is the
// failure this product wants rather than a finding citing nothing.
const DataSubjectRightsSlug = "gdpr-arts-12-22-data-subject-rights"

// DetectGaps raises an obligation that applies and whose control is not in
// place.
//
// # THE RE-SURFACE RULE
//
// A gap the person has dismissed comes back with a different sentence rather
// than silently or not at all. The partial unique index only covers open rows,
// so a dismissed signal frees its key and a fresh one opens beside it: the
// person sees "Recurring gap" and is told, in as many words, that they
// dismissed this before and the profile still says it applies. That is the
// honest handling of a dismissal that was about the moment rather than about
// the obligation, and it is why the dismissed keys are an input here rather
// than something the writer works out.
func DetectGaps(in Inputs) []Signal {
	var signals []Signal

	for _, o := range in.Obligations {
		if len(o.Requires) == 0 {
			continue
		}

		var missing []string
		for _, token := range o.Requires {
			if !GapSatisfied(token, in.Profile) {
				missing = append(missing, token)
			}
		}
		if len(missing) == 0 {
			continue
		}

		key := "gap:obligation:" + o.Slug
		recurring := in.DismissedGapKeys[key]

		title := "Profile gap: " + o.Title
		detail := "Your profile indicates this obligation applies, but the " +
			"corresponding control does not appear to be in place yet."
		if recurring {
			title = "Recurring gap: " + o.Title
			detail = "You previously dismissed this finding, but the gap is still " +
				"present in your profile and maps to an obligation that applies " +
				"to you. Revisit it or dismiss it again."
		}

		signals = append(signals, Signal{
			Kind:           KindProfileGap,
			DedupKey:       key,
			Title:          title,
			Detail:         detail,
			Severity:       o.Severity,
			ObligationSlug: o.Slug,
			Metadata: map[string]any{
				"missing":   missing,
				"recurring": recurring,
			},
		})
	}

	return signals
}

// DetectDSAREscalation restates a DSAR as critical once its deadline is close
// or gone.
//
// Ten days, and a separate detector rather than a branch inside the deadline
// one, because it says something different: the first is "this is coming", the
// second is "this is about to be a breach of Article 12(3)". It writes the same
// deduplication key on purpose, so the person has one row per request that gets
// more urgent rather than two rows saying the same thing at two volumes.
//
// Severity is critical unconditionally, and Severity() floors the finding at the
// signal's own band precisely so the Analyst's arithmetic cannot talk it back
// down.
func DetectDSAREscalation(in Inputs) []Signal {
	var signals []Signal

	for _, d := range in.DSARs {
		days := daysBetween(in.Today, d.DueDate)
		if days >= EscalationWindowDays {
			continue
		}

		var title, when string
		if days < 0 {
			overdue := -days
			title = fmt.Sprintf("DSAR response is %d %s overdue",
				overdue, plural(overdue, "day"))
			when = " is past its GDPR Article 12(3) one-month deadline with no logged response."
		} else {
			title = fmt.Sprintf("URGENT: DSAR response due in %d %s",
				days, plural(days, "day"))
			when = fmt.Sprintf(
				" is within %d days of its GDPR Article 12(3) one-month deadline "+
					"with no logged response.", EscalationWindowDays)
		}

		signals = append(signals, Signal{
			Kind:           KindDSAR,
			DedupKey:       "dsar:" + d.ID,
			Title:          title,
			Detail:         "A data-subject request" + fromSubject(d.SubjectName) + when,
			Severity:       "critical",
			ObligationSlug: DataSubjectRightsSlug,
			Metadata: map[string]any{
				"days_remaining":  days,
				"dsar_id":         d.ID,
				"response_due_at": d.ResponseDueAt.UTC().Format(time.RFC3339),
				"escalated":       true,
			},
		})
	}

	return signals
}

// GapSatisfied answers whether one required control is in place.
//
// The `requires` vocabulary, which `domain/corpus` validates on ingest and this
// evaluates on a sweep. The two lists are asserted to agree by a test, because
// a token the corpus accepts and this does not recognise is an obligation whose
// gap can never be raised.
//
// An unknown token is SATISFIED rather than missing, which is the safe
// direction here and the opposite of the usual one. A token this does not
// recognise means the corpus is ahead of the code, and raising a gap from a
// rule nobody implemented would put a finding in front of a founder that no
// action can clear.
func GapSatisfied(token string, p Profile) bool {
	switch token {
	case "ropa":
		return p.HasROPA == "yes"
	case "dpo":
		return p.HasDPO == "yes"
	case "ai_register":
		// Satisfied only when the organisation operates no AI system. Using AI
		// with nothing registered is the gap; there is no register field yet,
		// so "operates any AI system" stands in until one lands.
		return len(p.AISystems) == 0
	case "transfer_safeguards":
		// Satisfied when at least one destination is documented. Paired with
		// the cross-border applicability gate, so this only reaches an
		// organisation that actually transfers outside the EU.
		return len(p.TransferDestinations) > 0
	default:
		return true
	}
}

// daysBetween is whole days from one date to another, negative when the second
// is in the past. The plpgsql's `date - date`, which yields an integer.
//
// Both arguments are dates that came out of Postgres as dates, so this reads
// the calendar triple and ignores the location each one was decoded into.
// Subtracting the instants instead would be off by one whenever the two were
// decoded in different zones, and a day is the whole unit here.
func daysBetween(from, to time.Time) int {
	fromYear, fromMonth, fromDay := from.Date()
	toYear, toMonth, toDay := to.Date()
	f := time.Date(fromYear, fromMonth, fromDay, 0, 0, 0, 0, time.UTC)
	t := time.Date(toYear, toMonth, toDay, 0, 0, 0, 0, time.UTC)
	return int(t.Sub(f).Hours() / 24)
}

// formatDate renders a date the way Postgres renders one into text, which is
// what the plpgsql's `|| o.effective_date` produced.
func formatDate(t time.Time) string {
	year, month, day := t.Date()
	return fmt.Sprintf("%04d-%02d-%02d", year, int(month), day)
}

// plural picks the singular for exactly one, which is the rule the string
// concatenation in the plpgsql spelled out inline three times.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// fromSubject renders the requester's name when the request carries one.
//
// A DSAR logged without a name is normal: a request can arrive from an address
// with no name attached, and the detail sentence reads correctly without it.
func fromSubject(name *string) string {
	if name == nil || *name == "" {
		return ""
	}
	return " from " + *name
}
