package records

import (
	"fmt"
	"sort"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// The registers, as a vocabulary (ENT-261, §26.5).
//
// # WHY THIS EXISTS AND WHY IT IS IN GO
//
// The Hands prepares the record an approval will create. To do that it has to
// be told which columns that record has, and to be refused when it names one
// that does not exist. Neither half can come from the database: the Executor
// writes each register with a hand-written INSERT, so "what columns does a
// processing activity have" is a fact about the product's understanding of
// Article 30 rather than a fact about a table, and the table has columns
// (`id`, `org_id`, `source_finding_id`) that are nobody's to fill.
//
// AGENTS.md puts decisions in Go, and this is one: which questions Kindlast
// asks about a processing activity is a product decision that will change.
// The database keeps the invariants, which here are the not-nulls and the
// tenancy policies the Executor's INSERT already runs under.
//
// # THE FIELD NAMES ARE THE EXECUTOR'S PAYLOAD KEYS, EXACTLY
//
// `store/postgres/executor.go` reads `metadata -> 'payload' ->> 'purpose'` and
// friends. A name here that does not match one of those would produce a plan
// describing a value the Executor never reads, which is the worst kind of
// wrong: it looks prepared and creates a record saying "Not recorded".
//
// `TestTheRegisterFieldNamesAreTheOnesTheExecutorReads` walks the two and
// fails if they part company, because a comment asserting they agree is not a
// test that they do.

// Register names, matching the table each entry lands in.
const (
	RegisterProcessingActivities = "processing_activities"
	RegisterAISystems            = "ai_systems"
	RegisterDSARs                = "dsars"
)

// Field is one column of the record an approval would create, as the Hands is
// shown it.
type Field struct {
	// The payload key, exactly as the Executor reads it.
	Name string
	// The column in a person's words.
	Label string
	// Whether an entry without this reads as incomplete.
	//
	// NOT a database constraint, and the difference is deliberate. A record
	// the Executor creates is allowed to be incomplete, because "we created
	// the row and told you what is missing" is more honest than refusing to
	// create it and leaving an approved finding pointing at nothing.
	Required bool
	// Whether the column holds a list. A single-valued column given two values
	// is refused rather than joined: joining would invent a spelling nobody
	// chose, and the record would then carry a value no human wrote.
	ListValued bool
	// What the column is for, in one line, so a model fills it with the right
	// kind of thing rather than with whatever the name suggests.
	Description string
}

// Register is one register and the columns of an entry in it.
type Register struct {
	Name string
	// The register in a person's words, for the explanation a customer reads.
	// Authored here rather than by the model, because it is a statement about
	// what this product does and the model is not the authority on that.
	Label  string
	Fields []Field
}

// registers is the whole vocabulary, keyed by the finding action type that
// creates an entry.
//
// A finding whose action type is `review` is absent on purpose rather than
// present with no fields: approving it records the decision and creates
// nothing, and asking the Hands to prepare a record that will not exist is
// asking it to write fiction.
var registers = map[string]Register{
	// Article 30(1). The six columns `ProcessingActivityFields` carries, which
	// are the ones the console asks a human for.
	"create_ropa": {
		Name:  RegisterProcessingActivities,
		Label: "your Article 30 record of processing activities",
		Fields: []Field{
			{
				Name: "name", Label: "what the activity is called", Required: true,
				Description: "A short name a colleague would recognise, such as \"Payroll\" or \"Customer support tickets\".",
			},
			{
				Name: "purpose", Label: "why you process this data", Required: true,
				Description: "What the organisation is trying to achieve by processing it, in its own terms.",
			},
			{
				Name: "legal_basis", Label: "the lawful basis you rely on", Required: true,
				Description: "One of the Article 6(1) bases: " + lawfulBases() + ".",
			},
			{
				Name: "data_categories", Label: "the kinds of personal data involved",
				Required: true, ListValued: true,
				Description: "The categories of personal data, such as names, contact details or payroll data.",
			},
			{
				Name: "recipients", Label: "who else receives the data", ListValued: true,
				Description: "Anyone outside the organisation who receives this data, including processors and vendors.",
			},
			{
				Name: "retention_period", Label: "how long you keep it", Required: true,
				Description: "How long the data is kept, or the criteria that decide, such as \"seven years after the tax year ends\".",
			},
		},
	},

	// The AI Act register. `AiSystemFields`, minus nothing.
	"create_ai_system": {
		Name:  RegisterAISystems,
		Label: "your AI Act register of systems",
		Fields: []Field{
			{
				Name: "name", Label: "what the system is called", Required: true,
				Description: "A short name for the system, as people in the organisation refer to it.",
			},
			{
				Name: "vendor", Label: "who supplies it",
				Description: "The provider of the system, or the organisation itself when it built it.",
			},
			{
				Name: "purpose", Label: "what it is used for", Required: true,
				Description: "What the system is used to do, in the organisation's own terms.",
			},
			{
				// PRESENT AND DELIBERATELY NOT PRE-FILLABLE IN PRACTICE.
				//
				// It is offered so a run can leave it for a person with a
				// reason, which is the honest outcome. A classification is a
				// legal judgement under Annex III, and
				// `findings.RequiresReview` refuses an unreviewed approval of
				// a High-Risk one, so a value guessed here would be a guess
				// standing between a customer and a gate that exists to make
				// them look.
				Name: "risk_classification", Label: "its risk classification under the AI Act",
				Required:    true,
				Description: "One of: prohibited, high, limited, minimal, unclassified. Leave this for a person unless the organisation has recorded the classification itself: it is a legal judgement under Annex III, not something to infer.",
			},
			{
				Name: "documentation_status", Label: "how far its documentation has got",
				Description: "Where the technical documentation stands, such as \"not started\" or \"drafted\".",
			},
		},
	},

	// Data-subject requests. Present for completeness and, today, unreachable:
	// 00009 asserts that no obligation carries `create_dsar`, because a
	// data-subject request arrives from a person and an obligation that
	// manufactured one would be inventing the requester. It is here so that a
	// finding which somehow carried the action type is handled by the same
	// vocabulary as the others rather than by a nil map entry, and so the
	// fields exist to be left for a person the day one does.
	"create_dsar": {
		Name:  RegisterDSARs,
		Label: "your record of data-subject requests",
		Fields: []Field{
			{
				Name: "requester", Label: "who asked", Required: true,
				Description: "The person who made the request, as they identified themselves.",
			},
			{
				Name: "request_type", Label: "what they asked for", Required: true,
				Description: "The right being exercised, such as access, erasure or rectification.",
			},
			{
				Name: "handler", Label: "who is dealing with it",
				Description: "The person or team inside the organisation responsible for answering.",
			},
			{
				// The statutory clock runs from this, and the Executor refuses
				// a payload without it rather than defaulting to now()
				// (00010, ENT-224). A guessed date asserts a deadline that is
				// optimistic by however long the request sat unlogged.
				Name: "received_at", Label: "when it arrived", Required: true,
				Description: "The date the request was received, as an RFC 3339 timestamp. Never infer this: the statutory deadline runs from it.",
			},
		},
	},
}

// RegisterFor returns the register an approved finding with this action type
// writes to.
//
// The second result is false for `review` and for anything unrecognised, which
// are the same answer on purpose: both mean there is no record to prepare.
func RegisterFor(actionType string) (Register, bool) {
	register, ok := registers[actionType]
	return register, ok
}

// FieldNames lists a register's payload keys, sorted, for error messages that
// tell a caller what it could have said.
func (r Register) FieldNames() []string {
	names := make([]string, 0, len(r.Fields))
	for _, f := range r.Fields {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

// Field looks one column up by payload key.
func (r Register) Field(name string) (Field, bool) {
	for _, f := range r.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// ErrUnknownField is the refusal for a prepared field the register does not
// have.
//
// A typed error rather than a formatted string the handler re-reads, for the
// reason AGENTS.md gives about `check_violation` message parsing: a caller
// deciding what happened by matching English is a caller that breaks when the
// English is improved.
type ErrUnknownField struct {
	Field    string
	Register string
	Known    []string
}

func (e ErrUnknownField) Error() string {
	return fmt.Sprintf(
		"%q is not a field of %s; it has %v",
		e.Field, e.Register, e.Known)
}

// ErrTooManyValues is the refusal for a single-valued column given more than
// one value. See Field.ListValued for why this is not joined.
type ErrTooManyValues struct {
	Field string
	Count int
}

func (e ErrTooManyValues) Error() string {
	return fmt.Sprintf(
		"%q holds one value and was given %d; a list would invent a spelling nobody chose",
		e.Field, e.Count)
}

// ErrNoValue is the refusal for a prepared field with nothing in it.
//
// An empty prepared field is worse than an absent one: it occupies a column in
// the plan, so a person reading it sees the field accounted for, and the
// record is created with nothing there. A field with no value belongs in
// `left_for_you`, with a reason.
type ErrNoValue struct {
	Field string
}

func (e ErrNoValue) Error() string {
	return fmt.Sprintf(
		"%q was prepared with no value; a field nothing could fill belongs in left_for_you with a reason",
		e.Field)
}

// ErrUnknownFact is the refusal for a value claiming to come from a fact this
// organisation does not hold.
//
// # THE CITATION VALIDATOR'S ARGUMENT, IN ANOTHER REGISTER
//
// A prepared value is a claim about where it came from, and the product's
// whole value is that a human can check a claim. A value attributed to a fact
// that does not exist is the same fabrication a citation to an invented
// obligation is, arriving by a different door, and it is more dangerous
// because it ends up in a compliance record rather than beside one.
//
// Checked here against the organisation's own rows, which is the invariant.
// The harness checks the same thing against what the run was OFFERED, which is
// the guardrail, and the two refuse different things: this refuses a key that
// is not a fact, and that refuses a key that is a fact and was never shown to
// this run.
type ErrUnknownFact struct {
	Field string
	Fact  string
}

func (e ErrUnknownFact) Error() string {
	return fmt.Sprintf(
		"%q was filled from %q, which is not a fact this organisation has recorded",
		e.Field, e.Fact)
}

// ErrNoFact is the refusal for a prepared value that names no fact at all.
type ErrNoFact struct {
	Field string
}

func (e ErrNoFact) Error() string {
	return fmt.Sprintf(
		"%q was filled with no from_fact; a value with no source behind it is a guess presented as a fact",
		e.Field)
}

// ValidatePrepared checks one plan against the register and the organisation's
// facts, and returns the first thing wrong with it.
//
// # THE FIRST, NOT ALL OF THEM, AND THE WHOLE PLAN IS REFUSED
//
// Not "keep the good fields". `run.draft_narrative` makes the same call about
// citations and the reasoning transfers exactly: a plan filling one column
// from a real fact and one from an invented one is not partially trustworthy.
// It is a document a customer checks, finds wrong, and then stops believing
// the rest of.
//
// `known` is the set of fact keys this organisation actually holds. Passed in
// rather than read here, because the domain layer holds no database handle and
// because the caller has already read the facts to offer them to the run.
func ValidatePrepared(
	register Register,
	prepared []PreparedField,
	known map[string]bool,
) error {
	for _, p := range prepared {
		field, ok := register.Field(p.Name)
		if !ok {
			return ErrUnknownField{
				Field: p.Name, Register: register.Name, Known: register.FieldNames(),
			}
		}
		if len(p.Values) == 0 {
			return ErrNoValue{Field: p.Name}
		}
		if !field.ListValued && len(p.Values) > 1 {
			return ErrTooManyValues{Field: p.Name, Count: len(p.Values)}
		}
		for _, v := range p.Values {
			if v == "" {
				return ErrNoValue{Field: p.Name}
			}
		}
		if p.FromFact == "" {
			return ErrNoFact{Field: p.Name}
		}
		if !known[p.FromFact] {
			return ErrUnknownFact{Field: p.Name, Fact: p.FromFact}
		}
	}
	return nil
}

// PreparedField is one column the Hands filled, and the fact it filled it
// from.
type PreparedField struct {
	Name     string
	Values   []string
	FromFact string
}

// LeftForYou is one column the Hands did not fill, and why.
type LeftForYou struct {
	Name string
	Why  string
}

// lawfulBases renders the Article 6(1) vocabulary for the legal-basis
// description.
//
// Read from `domain/memory` rather than written out again, for the reason that
// package gives about the corpus side: two lists of the six bases is two
// places for one to be spelled differently, and the spelling is what decides
// whether an obligation applies.
func lawfulBases() string {
	bases := make([]string, 0, len(memory.LawfulBases))
	for basis := range memory.LawfulBases {
		bases = append(bases, basis)
	}
	sort.Strings(bases)
	out := ""
	for i, b := range bases {
		switch {
		case i == 0:
			out = b
		case i == len(bases)-1:
			out += " or " + b
		default:
			out += ", " + b
		}
	}
	return out
}
