// Package onboarding is the first conversation, and the rules that keep what
// it produces checkable (ENT-212, ENT-254, §2, §24 step 6).
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
// # ENT-254: THERE IS ONE INTERVIEW NOW, AND EVERY ANSWER IS A TAP
//
// Two question sets existed and overlapped. `/readiness` asked thirteen
// questions on the marketing site with no account and no server, every answer
// a tapped token out of a closed list, with the corpus on screen narrowing as
// the answers arrived. This package asked about eleven, typed, parsed and
// stored. Asking a customer both was the problem, and the ruling was that
// onboarding takes the readiness flow and the public page goes.
//
// So the questions below are the merged set and every one of them declares its
// options. That is not a rendering detail. A typed list of processors produces
// values only a human can read, and the two evaluators that decide which
// obligations apply (`watcher_obligation_applies` in plpgsql, and the console's
// port of it) both match on tokens. A closed set is the only kind of answer
// either of them can read, and `Parse` refuses anything outside it.
//
// # WHAT IS NO LONGER ASKED, BECAUSE THE ABSENCES LOOK LIKE OVERSIGHTS
//
// ENT-254 says a question either maps to a fact the Watcher reads or it is
// dropped and the drop is argued. Six were dropped:
//
//   - `industry`, `data_subjects` and `eu_jurisdictions`. Nothing reads them.
//     Not `watcher_obligation_applies`, not `watcher_gap_satisfied`, not the
//     narrator, not the console. They were three free-text questions whose
//     answers changed nothing, in an interview whose ruling was that it should
//     be easy. They remain in the memory vocabulary and on the memory page, so
//     an organisation that wants to record them still can, and the day
//     something reads one is the day it comes back as a question.
//   - `staff_count`. ENT-246 deleted `employees_min` from the vocabulary
//     precisely so nobody encodes Article 30(5) as a number, which left this
//     question collecting a figure the Watcher then visibly ignores. Asking
//     for it invites the next reader to wire it back up.
//   - `dsar_process` and `breach_plan`, which the readiness page asked. They
//     are good questions and they are not facts: the corpus raises no gap
//     token for either, so there is no key to store them under. On a page that
//     recorded nothing they could be reported back as the visitor's own words.
//     Here every answer is written down as it is given, and a question whose
//     answer is written nowhere is one this interview cannot ask honestly.
//
// # WHAT LIVES HERE AND WHAT DOES NOT
//
// The vocabulary of facts is `domain/memory`, deliberately: onboarding feeds
// the organisation memory rather than a profile of its own, so the keys and
// their kinds have exactly one definition. This package adds the questions, the
// order, the options, the parsing and the projection, all of which are
// decisions rather than invariants and are therefore Go rather than schema.
package onboarding

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// NoneChoice is the option that means "none of these", and it records the
// empty list rather than the word.
//
// The distinction is load-bearing. `watcher_obligation_applies` decides an
// organisation engages a processor by testing `btrim(vendor_list) <> ”` and
// decides an AI system is in use by testing `array_length(ai_systems, 1)`, so
// a stored "none" would tell the Watcher the opposite of what the person said.
const NoneChoice = "none"

// UnsureChoice is "I could not say", where that is a different answer from
// "none" rather than a way of declining.
//
// It is offered on the two list questions where a non-empty list is what the
// evaluators read: not knowing which processors touch your data is not the
// same claim as nobody touching it, and both the plpgsql and its console port
// agree that a list holding anything at all means the obligation stays open.
//
// It is NOT offered on `transfer_destinations`, and that is the one place the
// two evaluators would have disagreed:
// `watcher_gap_satisfied('transfer_safeguards')` counts any destination, while
// the console's port drops the sentinel before counting. Declining is the
// answer there, and an absent fact reads identically to both. It is not
// offered on `lawful_bases` either, because the memory vocabulary refuses any
// value outside the Article 6(1) set.
const UnsureChoice = "unsure"

// Option is one answer a list question offers.
type Option struct {
	Value string
	Label string

	// Exclusive means this answer stands alone. "Nobody outside the company
	// touches it" and "I could not say" are each a complete answer, and
	// neither combines with naming a supplier.
	Exclusive bool
}

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

	// The corpus obligation to quote when somebody asks why we want to know.
	//
	// A slug rather than a sentence, because the sentence is the corpus's to
	// write (ENT-248). The console renders that obligation's `summary`
	// unedited, and nothing in this file says what any of it requires.
	Basis string

	// The closed set of answers a list question accepts. Empty for a
	// tri-state, whose three answers are `TriStateChoices`.
	Options []Option
}

// Shape is the kind of value this question's answer must take.
//
// Read from the memory vocabulary rather than declared again here, so a
// question and the fact it fills cannot disagree about whether an answer is a
// list or a number.
func (q Question) Shape() memory.Kind { return memory.Kinds[q.Key] }

// Offers reports whether a token is one of this question's answers.
func (q Question) Offers(value string) (Option, bool) {
	for _, option := range q.Options {
		if option.Value == value {
			return option, true
		}
	}
	return Option{}, false
}

// vocabulary renders the offered tokens for a refusal message, sorted so two
// runs read identically.
func (q Question) vocabulary() string {
	values := make([]string, 0, len(q.Options))
	for _, option := range q.Options {
		values = append(values, option.Value)
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

// TriStateChoices is the only set of answers a tri-state question accepts.
//
// Exported because the console renders exactly these three as buttons, and a
// console offering a fourth would produce answers the server refuses.
var TriStateChoices = []string{"yes", "no", "unsure"}

// script is the interview, in order.
//
// The order is the readiness assessment's, which was arranged so that the
// corpus column visibly narrows early: what data is held and on what grounds
// opens most of the GDPR obligations, the processor question opens Article 28,
// and the AI questions close the AI Act half at the end for the many
// organisations that run none.
//
// Plain English throughout, and no legal term the person did not use first.
// "Record of processing activities" appears because it is the name of a
// document somebody either keeps or does not; "Article 30" does not.
//
// AND NOTHING HERE STATES WHAT THE LAW REQUIRES (ENT-248). Every prompt, every
// note and every option label below asks about the organisation or clarifies
// the scope of the question. None of them says an obligation applies, or does
// not, or is often fine to have skipped. That is not caution about wording:
// driving the narrator against the real model produced a narrative citing
// Article 30 correctly and stating the law wrongly beside it, and the citation
// validator structurally cannot catch that. The statement of law comes from the
// corpus row, which is what `Basis` names.
var script = []Question{
	{
		Key:    memory.KeyDataCategories,
		Prompt: "What kinds of personal information does your company hold?",
		Help:   "Pick everything that applies. If you are unsure whether something counts, pick it.",
		Options: []Option{
			{Value: "contact_details", Label: "Names and contact details"},
			{Value: "account", Label: "Account, device or online identifiers"},
			{Value: "payment", Label: "Payment or financial details"},
			{Value: "health", Label: "Health or medical information"},
			{Value: "biometric", Label: "Biometric or genetic information"},
			{Value: "location", Label: "Location or movement data"},
			{Value: "employment", Label: "Employment and HR records"},
			{Value: "behaviour", Label: "Behaviour, usage or tracking data"},
			{Value: "children", Label: "Information about children"},
			{Value: NoneChoice, Label: "None of this", Exclusive: true},
		},
	},
	{
		Key:    memory.KeyLawfulBases,
		Prompt: "On what grounds do you use it?",
		Help:   "Pick every one you rely on. Most organisations rely on more than one.",
		Basis:  "gdpr-art-6-lawful-basis",
		// The values are the Article 6(1) closed set as `domain/memory` spells
		// it, and `memory.ValidateValue` refuses anything else. The labels are
		// the plain-English version; the values are what is stored and what
		// `lawful_basis_includes` is matched against, so a spelling that
		// drifted here is Article 7 silently ceasing to apply.
		Options: []Option{
			{Value: "consent", Label: "The person agreed to it"},
			{Value: "contract", Label: "We need it to deliver what they asked for"},
			{Value: "legal_obligation", Label: "We are under a separate legal obligation to hold it"},
			{Value: "vital_interests", Label: "Somebody's life could depend on it"},
			{Value: "public_task", Label: "We carry out a public task"},
			{Value: "legitimate_interests", Label: "We have a business reason and weighed it against their interests"},
		},
	},
	{
		Key:    memory.KeyVendorList,
		Prompt: "Which other companies handle that information for you?",
		Help:   "The suppliers who touch personal information on your behalf, and the ones they use in turn.",
		Basis:  "gdpr-art-28-processor-contracts",
		Options: []Option{
			{Value: "hosting", Label: "Hosting or cloud infrastructure"},
			{Value: "payments", Label: "Payments"},
			{Value: "email", Label: "Email and marketing"},
			{Value: "analytics", Label: "Analytics and product tracking"},
			{Value: "support", Label: "Support desk or CRM"},
			{Value: "hr", Label: "HR and payroll"},
			{Value: "ai_vendors", Label: "AI or model providers"},
			{Value: NoneChoice, Label: "Nobody outside the company touches it", Exclusive: true},
			// Not folded into "nobody". Not knowing who handles your data is a
			// different situation from nobody handling it, and only the second
			// one narrows the obligation away.
			{Value: UnsureChoice, Label: "I could not say", Exclusive: true},
		},
	},
	{
		Key:    memory.KeyTransfersOutsideEU,
		Prompt: "Does any of that information leave the EU or the EEA?",
		Help:   "Anything hosted, backed up, or supported from outside the EU or EEA is inside this question.",
		Basis:  "gdpr-chapter-v-international-transfers",
	},
	{
		Key:    memory.KeyTransferDestination,
		Prompt: "Where does it go?",
		Help:   "Name the places you know about. If nobody has checked, skip this one rather than guessing.",
		Options: []Option{
			{Value: "united_states", Label: "United States"},
			{Value: "united_kingdom", Label: "United Kingdom"},
			{Value: "canada", Label: "Canada"},
			{Value: "india", Label: "India"},
			{Value: "australia", Label: "Australia"},
			{Value: "japan", Label: "Japan"},
			{Value: "elsewhere", Label: "Somewhere else"},
		},
	},
	{
		Key:    memory.KeyHighRiskProcessing,
		Prompt: "Does what you do with that information create a serious risk to the people it is about?",
		Help:   `Answer for how it looks from where you sit. "Not sure" is a real answer here and is treated as one.`,
		Basis:  "gdpr-art-35-dpia",
	},
	{
		Key:    memory.KeyLargeScaleMonitoring,
		Prompt: "Do you track or monitor people regularly, systematically, and at scale?",
		Help:   "Continuous behavioural tracking, location histories, or cameras covering places the public can walk into.",
		Basis:  "gdpr-art-37-dpo-appointment",
	},
	{
		Key:    memory.KeyHasROPA,
		Prompt: "Do you keep a record of processing activities?",
		Help:   "A written list of what you do with personal information and why. It is often a spreadsheet.",
		Basis:  "gdpr-art-30-ropa",
	},
	{
		Key:    memory.KeyHasDPO,
		Prompt: "Have you appointed a data protection officer?",
		Help:   "A named person formally appointed to be responsible for data protection.",
		Basis:  "gdpr-art-37-dpo-appointment",
	},
	{
		Key:    memory.KeyAISystems,
		Prompt: "Which AI is in use, inside your product or by your team?",
		Help:   "Include the ones that feel too ordinary to mention.",
		Basis:  "ai-act-art-4-ai-literacy",
		Options: []Option{
			{Value: "assistants", Label: "Bought-in assistants and copilots"},
			{Value: "in_product", Label: "AI features inside our own product"},
			{Value: "built", Label: "Models we build or fine-tune ourselves"},
			{Value: "embedded", Label: "AI inside tools we bought for something else"},
			{Value: NoneChoice, Label: "None that we know of", Exclusive: true},
			{Value: UnsureChoice, Label: "I could not say", Exclusive: true},
		},
	},
	{
		Key: memory.KeyHighRiskAISystem,
		// The question names the classification rather than describing it, and
		// the description comes from the Annex III corpus row the console
		// renders beside it. Summarising Annex III here in our own words is
		// exactly the sentence ENT-248 says belongs to the corpus.
		Prompt: "Does any of that AI fall inside the EU AI Act's high-risk list?",
		Help:   `The list is quoted below, straight from what Kindlast holds. Answer "not sure" if nobody has checked.`,
		Basis:  "ai-act-annex-iii-high-risk-systems",
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
// `Text` is verbatim what the person answered and `ValueJSON` is what it was
// taken to mean. Both, because a reader has to be able to hold them side by
// side: "we read this as two processors" is checkable and the list on its own
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
// Two rules, and both earn their place: a question with no meaning for this
// person should not be asked, because the answer is either an awkward "not
// applicable" or a skip that reads as a refusal.
//
// ONLY A DEFINITE NO REMOVES A QUESTION. "We do not know whether anything
// leaves the EU" is not the same claim as "nothing does", and an organisation
// that cannot say what AI it runs still has an answer worth having about
// whether any of it decides about people.
//
// Rules rather than constraints, per AGENTS.md: they consult another answer and
// could reasonably be different next quarter.
func Applicable(key string, answers Answers) bool {
	switch key {
	case memory.KeyTransferDestination:
		transfers, answered := answers[memory.KeyTransfersOutsideEU]
		if !answered || transfers.Skipped {
			return true
		}
		return transfers.ValueJSON != `"no"`

	case memory.KeyHighRiskAISystem:
		systems, answered := answers[memory.KeyAISystems]
		if !answered || systems.Skipped {
			return true
		}
		// `[]` is what "none that we know of" records. A list holding the
		// unsure sentinel is not empty, so the question stays.
		return systems.ValueJSON != `[]`
	}
	return true
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

// Parse turns what a person answered into the value its fact will hold.
//
// # IT REFUSES RATHER THAN INTERPRETS, WHICH IS THE WHOLE POINT
//
// A token nothing offered is not an answer, and "probably" is not yes, no or
// unsure. A parser that resolved either would be doing the guessing this design
// exists to remove, one layer lower down and with less visibility than a model
// would have. The refusal carries a sentence naming what would be accepted, so
// the person answers again rather than being told they were wrong.
//
// The one liberty taken is whitespace, separators and case, which are transport
// rather than meaning.
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
		question, asked := QuestionFor(key)
		if asked && len(question.Options) > 0 {
			picked, err := parseChoices(question, trimmed)
			if err != nil {
				return "", err
			}
			payload = picked
			break
		}
		// A list fact the interview does not collect, reached through the
		// memory surface rather than through here. Kept working rather than
		// refused, because `Parse` is the one place a fact's shape is applied.
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

// parseChoices reads the tokens a list question was answered with.
//
// # AN UNOFFERED TOKEN IS REFUSED, WHICH IS THE CLOSED VOCABULARY
//
// The console renders the options this package declares, so a token outside
// them means the console and the server disagree about what was asked. Refusing
// is the safe direction for that disagreement to fail in: the alternative is a
// profile holding a value the applicability rules will silently never match,
// and an obligation that quietly stops applying is exactly the failure ENT-246
// spent a whole issue undoing.
//
// # "NONE" IS AN ANSWER AND IT RECORDS THE EMPTY LIST
//
// Not the word. See `NoneChoice`: both evaluators decide "does this
// organisation engage a processor" and "is any AI in use" by looking at whether
// the list holds anything, so storing "none" would tell them the opposite of
// what the person said.
func parseChoices(question Question, answer string) ([]string, error) {
	tokens := splitList(strings.ToLower(answer))
	if len(tokens) == 0 {
		return nil, fmt.Errorf("onboarding: pick at least one of the answers offered, or skip the question")
	}

	picked := make([]string, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		option, offered := question.Offers(token)
		if !offered {
			return nil, fmt.Errorf(
				"onboarding: %q is not one of the answers offered for that question (%s)",
				token, question.vocabulary())
		}
		if option.Exclusive && len(tokens) > 1 {
			return nil, fmt.Errorf(
				"onboarding: %q is a complete answer on its own and cannot be combined with another",
				option.Label)
		}
		if seen[token] {
			// The same chip twice. Transport rather than meaning, unlike a
			// typed list where a repeated vendor may be two arrangements.
			continue
		}
		seen[token] = true
		picked = append(picked, token)
	}

	if len(picked) == 1 && picked[0] == NoneChoice {
		// Empty rather than nil, so this marshals as `[]` and not as `null`.
		return []string{}, nil
	}
	return picked, nil
}

// splitList breaks an answer into items.
//
// Commas, semicolons and newlines, because the console joins tapped tokens with
// commas and a person answering a free-text list fact through the memory
// surface reaches for all three.
//
// " AND " IS NOT A SEPARATOR, WHICH IS A DELIBERATE LIMITATION. "Ireland, Spain
// and Portugal" therefore yields two items, the second of which reads oddly.
// Splitting on it would read better and would also turn "Marks and Spencer"
// into two companies. A slightly clumsy item a person can see and fix beats a
// silently invented one, which is the same trade the tri-state refusal makes.
//
// A leading "and" left over from an Oxford comma is dropped, because that one
// is unambiguous: nothing is called "and Portugal".
//
// Duplicates are kept here and collapsed by `parseChoices`. If somebody lists a
// vendor twice in free text they may mean two arrangements with it, and
// collapsing them would be an edit rather than a parse.
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
