// Package onboarding is the first conversation, and the rules that keep what
// it produces checkable (ENT-212, §2, §24 step 6).
//
// # THE INTERVIEW IS A SCRIPT, AND THAT IS THE SAFETY PROPERTY
//
// The legacy flow (`db0bf83`, `lib/onboarding/`) interviewed freely and then
// ran a second model pass over the transcript to extract a structured profile.
// The profile decides which obligations apply, so a field that pass invented
// produces wrong findings later, at enough distance from the mistake that
// nobody traces it back. ENT-212 names that as the thing to design out.
//
// So the order is inverted here. Every question names the fact it asks about,
// and `Parse` turns the answer into that fact's declared kind at the moment it
// is given. Nothing reads the transcript afterwards to decide what was meant,
// because by then it has already been decided, in Go, against a closed
// vocabulary, with a refusal available.
//
// What that costs is the warmth of a model that can ask a follow-up in its own
// words. What it buys is that no value in a customer's profile came from a
// model's reading of a sentence. Given that the profile is what a finding cites
// regulation about, that trade is not close.
//
// # AND IT IS ALSO THE DEGRADED PATH
//
// ENT-212 requires that "an instance with no model provider configured degrades
// to a form rather than failing". A scripted interview IS that form, so the
// deployment with no model and the deployment with one run the same code and
// produce the same profile. There is no second path to keep working.
//
// # WHAT LIVES HERE AND WHAT DOES NOT
//
// The vocabulary of facts is `domain/memory`, deliberately: onboarding feeds
// the organisation memory rather than a profile of its own, so the keys and
// their kinds have exactly one definition. This package adds the questions, the
// order, the parsing and the projection, all of which are decisions rather than
// invariants and are therefore Go rather than schema.
package onboarding

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// Question is one thing the interview asks.
//
// The prompt lives here rather than in the console, so the transcript a
// customer reads back is the text they were actually asked. Prompts that lived
// only in a browser would leave `onboarding_messages` holding answers to
// questions nobody could reconstruct a year later.
type Question struct {
	Key    string
	Prompt string
	Help   string
}

// Shape is the kind of value this question's answer must take.
//
// Read from the memory vocabulary rather than declared again here, so a
// question and the fact it fills cannot disagree about whether an answer is a
// list or a number.
func (q Question) Shape() memory.Kind { return memory.Kinds[q.Key] }

// TriStateChoices is the only set of answers a tri-state question accepts.
//
// Exported because the console renders exactly these three as buttons, and a
// console offering a fourth would produce answers the server refuses.
var TriStateChoices = []string{"yes", "no", "unsure"}

// script is the interview, in order.
//
// The six topics are the ones `system-prompt.ts` settled at `db0bf83` and are
// product decisions that were reviewed once already: what the company does,
// what data and whose, which countries, which AI tools, whether there is a data
// protection officer, whether there is a record of processing activities. Three
// more are here because the Watcher reads them and an unanswered one silently
// changes which obligations a customer is shown: transfers and their
// destinations, the processors, and headcount.
//
// Plain English throughout, and no legal term the person did not use first.
// "Record of processing activities" appears because it is the name of a
// document somebody either keeps or does not; "Article 30" does not.
//
// AND NOTHING HERE STATES WHAT THE LAW REQUIRES (ENT-248). Every prompt and
// every note below asks about the organisation or clarifies the scope of the
// question. None of them says an obligation applies, or does not, or is often
// fine to have skipped. That is not caution about wording: driving the narrator
// against the real model produced a narrative citing Article 30 correctly and
// stating the law wrongly beside it, and the citation validator structurally
// cannot catch that. The statement of law comes from the corpus row, so a
// surface that has no corpus row in front of it says nothing about the law.
var script = []Question{
	{
		Key:    memory.KeyIndustry,
		Prompt: "To start: in a sentence or two, what does your company do?",
		Help:   "However you would describe it to someone at a party.",
	},
	{
		Key:    memory.KeyDataCategories,
		Prompt: "What kinds of personal information do you hold?",
		Help:   "Names, email addresses, payment details, whatever it is. Separate them with commas.",
	},
	{
		Key:    memory.KeyDataSubjects,
		Prompt: "And whose information is that?",
		Help:   "Customers, staff, applicants, people who signed up and never came back. Separate them with commas.",
	},
	{
		Key:    memory.KeyEUJurisdictions,
		Prompt: "Which EU or EEA countries are those people in?",
		Help:   "Name the countries you know about. Separate them with commas.",
	},
	{
		Key:    memory.KeyAISystems,
		Prompt: "Which AI tools are in use, either inside your product or by your team?",
		Help:   "Include the ones that feel too ordinary to mention. Separate them with commas, and say if there are none.",
	},
	{
		Key:    memory.KeyVendorList,
		Prompt: "Which other companies handle that information for you?",
		Help:   "Hosting, payments, email, analytics, support desks. Separate them with commas.",
	},
	{
		Key:    memory.KeyTransfersOutsideEU,
		Prompt: "Does any of this information leave the EU or EEA?",
		Help:   "Anything hosted outside the EU or EEA is inside this question. If you are not sure, say so: that is a real answer.",
	},
	{
		Key:    memory.KeyTransferDestination,
		Prompt: "Where does it go, and with which provider?",
		Help:   `Pair the place with the company where you can, such as "United States (Stripe)". Separate them with commas.`,
	},
	{
		Key:    memory.KeyHasDPO,
		Prompt: "Have you appointed a data protection officer?",
		Help:   "A named person formally appointed to be responsible for data protection. Many small companies have not appointed one.",
	},
	{
		Key:    memory.KeyHasROPA,
		Prompt: "Do you keep a record of processing activities?",
		Help:   "A written list of what you do with personal information and why. It is often a spreadsheet.",
	},
	{
		Key:    memory.KeyStaffCount,
		Prompt: "Last one: how many people work there?",
		Help:   "A round number is fine. Include founders and part-timers.",
	},
}

// Script returns the interview.
//
// A copy, so a caller cannot reorder the questions everybody else is asking.
func Script() []Question {
	out := make([]Question, len(script))
	copy(out, script)
	return out
}

// QuestionFor returns the question that asks about a key.
func QuestionFor(key string) (Question, bool) {
	for _, q := range script {
		if q.Key == key {
			return q, true
		}
	}
	return Question{}, false
}

// Answer is what one question got.
//
// `Text` is verbatim what the person typed and `ValueJSON` is what it was taken
// to mean. Both, because a reader has to be able to hold them side by side: "we
// read this as a list of three countries" is checkable and the list on its own
// is not.
type Answer struct {
	Text      string
	ValueJSON string
	Skipped   bool
}

// Answers is the latest answer to each question, keyed by fact.
type Answers map[string]Answer

// Applicable reports whether a question is worth asking, given what has been
// answered so far.
//
// One rule today and it earns its place: where data is transferred is a
// question with no meaning for an organisation that has just said it transfers
// nothing. Asking it anyway produces either an awkward "not applicable" typed
// into a list, or a skip that reads as a refusal to answer rather than as a
// question that did not apply.
//
// A rule rather than a constraint, per AGENTS.md: it consults another answer
// and could reasonably be different next quarter.
func Applicable(key string, answers Answers) bool {
	if key != memory.KeyTransferDestination {
		return true
	}
	// ONLY A DEFINITE NO REMOVES THE QUESTION. Not yet asked, declined, and
	// unsure all keep it, because "we do not know whether anything leaves the
	// EU" is not the same claim as "nothing does", and an organisation that can
	// name a destination has answered the earlier question by doing so.
	transfers, answered := answers[memory.KeyTransfersOutsideEU]
	if !answered || transfers.Skipped {
		return true
	}
	return transfers.ValueJSON != `"no"`
}

// NextQuestion returns what to ask next, and whether there is anything left.
//
// A skip counts as answered. Declining is a decision the person made, and an
// interview that kept re-asking a declined question would be arguing with them.
func NextQuestion(answers Answers) (Question, bool) {
	for _, q := range script {
		if !Applicable(q.Key, answers) {
			continue
		}
		if _, done := answers[q.Key]; !done {
			return q, true
		}
	}
	return Question{}, false
}

// Progress reports how many applicable questions there are and how many are
// done, for a line that tells a person how long this is.
func Progress(answers Answers) (total, done int) {
	for _, q := range script {
		if !Applicable(q.Key, answers) {
			continue
		}
		total++
		if _, answered := answers[q.Key]; answered {
			done++
		}
	}
	return total, done
}

// Parse turns what a person typed into the value its fact will hold.
//
// # IT REFUSES RATHER THAN INTERPRETS, WHICH IS THE WHOLE POINT
//
// "About fifty" is not a number and "probably" is not yes, no or unsure. A
// parser that resolved either would be doing the guessing this design exists to
// remove, one layer lower down and with less visibility than a model would
// have. The refusal carries a sentence naming what would be accepted, so the
// person answers again rather than being told they were wrong.
//
// The one liberty taken is whitespace and separators, which are typing rather
// than meaning.
func Parse(key, answer string) (string, error) {
	kind, known := memory.Kinds[key]
	if !known {
		return "", fmt.Errorf("onboarding: %q is not a question this product asks", key)
	}

	trimmed := strings.TrimSpace(answer)
	if trimmed == "" {
		return "", fmt.Errorf("onboarding: there is nothing in that answer; skip the question instead of leaving it blank")
	}

	var payload any
	switch kind {
	case memory.KindText:
		payload = trimmed

	case memory.KindTriState:
		normalised := strings.ToLower(trimmed)
		valid := false
		for _, choice := range TriStateChoices {
			if normalised == choice {
				valid = true
				break
			}
		}
		if !valid {
			// UNSURE IS OFFERED AND IS NOT A FAILURE. The message names all
			// three so a person who genuinely does not know reaches for the
			// answer that says so rather than picking whichever of yes and no
			// feels less bad.
			return "", fmt.Errorf(
				"onboarding: answer that one with yes, no or unsure; unsure is a real answer and we would rather have it than a guess")
		}
		payload = normalised

	case memory.KindNumber:
		count, err := strconv.Atoi(trimmed)
		if err != nil || count < 0 {
			return "", fmt.Errorf("onboarding: that needs to be a whole number; a rough one is fine, but it has to be a number")
		}
		payload = count

	case memory.KindList:
		items := splitList(trimmed)
		if len(items) == 0 {
			return "", fmt.Errorf("onboarding: name at least one, or skip the question")
		}
		payload = items
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("onboarding: encoding the answer to %q: %w", key, err)
	}

	// Checked against the same validator every other writer of this fact goes
	// through, rather than trusted because this function just built it. If the
	// two ever disagree, the disagreement is what is caught, and it is caught
	// here rather than by a constraint three layers down.
	if err := memory.ValidateValue(key, string(encoded)); err != nil {
		return "", err
	}
	return string(encoded), nil
}

// splitList breaks a typed line into items.
//
// Commas, semicolons and newlines, because people use all three and which one
// they reached for is typing rather than meaning.
//
// " AND " IS NOT A SEPARATOR, WHICH IS A DELIBERATE LIMITATION. "Ireland,
// Spain and Portugal" therefore yields two items, the second of which reads
// oddly. Splitting on it would read better and would also turn "Marks and
// Spencer" into two companies, and this same parser handles the question about
// which processors an organisation uses. A slightly clumsy item a person can
// see and fix on the confirmation screen beats a silently invented one, which
// is the same trade the tri-state refusal makes.
//
// A leading "and" left over from an Oxford comma is dropped, because that one
// is unambiguous: nothing is called "and Portugal".
//
// Duplicates are kept. If somebody lists a vendor twice they may mean two
// arrangements with it, and collapsing them would be an edit rather than a
// parse.
func splitList(answer string) []string {
	fields := strings.FieldsFunc(answer, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	items := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if rest, found := strings.CutPrefix(strings.ToLower(field), "and "); found {
			// Cut from the original rather than the lowered copy, so the item
			// keeps the capitalisation the person typed.
			field = strings.TrimSpace(field[len(field)-len(rest):])
		}
		if field == "" {
			continue
		}
		items = append(items, field)
	}
	return items
}
