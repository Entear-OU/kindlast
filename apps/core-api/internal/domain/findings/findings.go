package findings

import (
	"errors"
	"time"
)

// Finding is one row of the feed, as the store reads it.
//
// Every field is stored. Nothing here is computed at read time, which is the
// point: the severity is the one the Analyst assessed and the citation is the
// one it recorded, not values re-derived against today's rules.
type Finding struct {
	ID              string
	Status          string
	Severity        string
	Detected        string
	ProposedAction  string
	EffortEstimate  string
	ActionType      string
	Citation        Citation
	CreatedAt       time.Time
	SnoozedUntil    *time.Time
	ApprovedBy      string
	RejectionReason string

	// What the Analyst added, if anything (ENT-162, ENT-164).
	//
	// Empty is the ordinary state for all three. Narration is a job that runs
	// after the sweep, Intelligence is optional, and the fields above are what
	// a finding renders with or without it. Narrative and NarrativeRefusal are
	// mutually exclusive by construction: a run either produced prose whose
	// citations all resolved, or it produced a reason it did not.
	Narrative        string
	NarrativeRefusal string
	AgentRunID       string
}

// Citation is the regulatory basis for a finding, exactly as stored.
//
// Label and URL were assembled by analyst_citation_label and
// analyst_citation_url when the finding was created. They are carried, never
// rebuilt: a second assembler is a second thing that can disagree with the
// record, and the product's whole claim is that a human can check the citation
// against the law.
type Citation struct {
	ObligationSlug string
	Title          string
	CELEX          string
	Kind           string
	Article        int32
	Recital        int32
	Annex          string
	Paragraph      string
	Label          string
	URL            string

	// The authored statement of the law, read live from the obligation row
	// (ENT-248).
	//
	// Unlike Label and URL beside it, which are what the Analyst assembled at
	// the time and must never change under a finding. This is deliberately the
	// CURRENT summary: a curator correcting a statement of law should reach
	// every finding that cites it, because the alternative is a customer
	// reading a sentence we already know to be wrong.
	//
	// It exists on the wire because the model is forbidden to state the law
	// (two live runs on the 2B tier stated it backwards beside a citation that
	// resolved), so the statement has to reach the same page from somewhere a
	// person wrote it.
	Summary string
}

// SupportingChunk is one quoted passage behind a finding, from
// finding_supporting_chunks.
type SupportingChunk struct {
	Ordinal    int32
	Label      string
	QuotedText string
	SourceURL  string
}

// Page is one page of the feed plus the cursor for the next.
//
// NextCursor is empty when this is the last page. A caller must key "there is
// more" off that rather than off a full page, because a last page that happens
// to be full is ordinary.
type Page struct {
	Findings   []Finding
	NextCursor string
}

// Acted is the outcome of an act-path call.
//
// Applied is false when the finding was already in the target state, is
// unknown, or belongs to another organisation. Those are one answer on purpose:
// distinguishing them is how a caller probes for a tenancy leak.
type Acted struct {
	Applied bool
	// The record the Executor created, when it created one. Empty when the
	// finding's action_type is `review`, which is every finding until the
	// corpus is classified (ENT-165).
	CreatedRecordID    string
	CreatedRecordTable string
	// Set by a snooze.
	SnoozedUntil *time.Time
}

// Pipeline is whether the agents have actually run.
//
// The honest answer to "is this thing working", which the console has never
// been able to give. ProfileExists separates "the agents have not run" from
// "there is nothing for them to run against": one is waiting for a schedule,
// the other is waiting for onboarding, and they need different words.
type Pipeline struct {
	WatcherLastRunAt *time.Time
	ProfileExists    bool
}

// Dashboard is everything the stat row and the agent rail render.
type Dashboard struct {
	Posture  Posture
	Headline string
	Counts   SeverityCounts
	Pipeline Pipeline
}

// The Executor (ENT-271, ENT-225 phase 2).
//
// Approving a finding whose action type is one of these creates a compliance
// record: a processing activity, a DSAR, or an AI system. Until ENT-271 three
// database triggers did it inside the approving transaction; now the approval
// writes a job and a workflow executes it, which is what §3 always specified.

// The action types that create a record when a finding is approved. A finding
// whose action type is anything else (today: `review`) is approved and
// creates nothing, which is the ordinary case.
const (
	ActionCreateROPA     = "create_ropa"
	ActionCreateDSAR     = "create_dsar"
	ActionCreateAISystem = "create_ai_system"
)

// Executes reports whether approving a finding with this action type creates
// a record, and therefore whether the approval enqueues an executor job.
func Executes(actionType string) bool {
	switch actionType {
	case ActionCreateROPA, ActionCreateDSAR, ActionCreateAISystem:
		return true
	default:
		return false
	}
}

// RiskHigh is the classification an AI Act record carries when the system is
// High-Risk, and the one an approval may not create unreviewed.
const RiskHigh = "high"

// ErrReviewRequired is the refusal for approving a High-Risk AI system
// classification without a reviewed approval (§3, surfaced as
// `failed_precondition`).
//
// # WHY THIS IS CHECKED BEFORE THE APPROVAL AND NOT DURING THE EXECUTION
//
// It used to be a `raise check_violation` inside the executor trigger, which
// aborted the approving transaction: nothing was written and the caller was
// told, if obscurely. With execution moved behind the event boundary
// (ENT-271) that shape is not available and its asynchronous equivalent is
// much worse: the finding would be approved, the audit row would say a person
// approved it, the job would fail somewhere else, and the person would be
// looking at an approved finding with no record and no reason. So the gate
// runs at approval, before anything is written, and a refused approval leaves
// the finding exactly as it was.
var ErrReviewRequired = errors.New(
	"a High-Risk AI system classification requires a reviewed approval")

// RequiresReview reports whether this approval must be refused: a finding
// that would create an AI system classified High-Risk, approved by somebody
// who did not tick the review.
//
// The classification comes from the finding's own proposed payload, which is
// what the record would be created with, so this asks about the record that
// would exist rather than about the finding's severity.
func RequiresReview(actionType, riskClassification string, reviewed bool) bool {
	return actionType == ActionCreateAISystem &&
		riskClassification == RiskHigh &&
		!reviewed
}

// The DSAR receipt rule (ENT-224, moved to Go by ENT-271).
//
// A DSAR's statutory deadline runs from receipt of the request (Article
// 12(3)), so the executor takes `received_at` from the finding's payload and
// refuses to guess it. 00010 wrote the argument out and it is unchanged: a
// request whose receipt date is unknown has an unknowable deadline, and
// defaulting to now() does not represent that, it asserts a specific deadline
// that is optimistic by however long the request sat unlogged. A compliance
// product that under-reports urgency is worse than one that says nothing.
//
// # WHY THE REFUSAL MOVED WITH THE EXECUTION
//
// 00010 accepted a real cost for that strictness: "Refusing aborts the
// approval... the human sees an error rather than a created record. That is
// the correct outcome." The refusal was a `raise exception` inside the
// trigger, so it did abort the approval.
//
// With execution behind the event boundary that is no longer true, and
// refusing at execution time would be strictly worse than defaulting: the
// finding would be approved, the audit row would name the approver, and the
// DSAR would never appear, with the reason in a job row nobody is reading. So
// the check moves to the approval, where a refusal still leaves nothing
// behind and the person still sees the error.
var (
	// ErrReceiptRequired is a DSAR approval whose payload carries no receipt
	// date.
	ErrReceiptRequired = errors.New(
		"this request has no received_at in its payload, and the Article 12(3) " +
			"deadline runs from receipt: it cannot be guessed")
	// ErrReceiptMalformed is one whose receipt date is not a timestamp.
	ErrReceiptMalformed = errors.New(
		"this request carries a received_at that is not a timestamp, and the " +
			"Article 12(3) deadline runs from receipt")
	// ErrReceiptInFuture is one that claims to have arrived later than now.
	ErrReceiptInFuture = errors.New(
		"this request carries a received_at in the future")
)

// Receipt is what the payload says about when a request arrived, as the
// database read it: `Present` is whether the key was there at all, `Valid`
// whether it parsed as a timestamp, and `At` the value when it did.
//
// Parsed by Postgres rather than in Go, deliberately: the trigger this
// replaces used `::timestamptz`, and a second parser with slightly different
// ideas about what a timestamp is would refuse dates the old path accepted, or
// accept dates it refused, for no reason anybody chose.
type Receipt struct {
	Present bool
	Valid   bool
	At      time.Time
}

// CheckReceipt validates the receipt date a DSAR approval would use, and is a
// no-op for every other action type.
func CheckReceipt(actionType string, receipt Receipt, now time.Time) error {
	if actionType != ActionCreateDSAR {
		return nil
	}
	if !receipt.Present {
		return ErrReceiptRequired
	}
	if !receipt.Valid {
		return ErrReceiptMalformed
	}
	if receipt.At.After(now) {
		return ErrReceiptInFuture
	}
	return nil
}
