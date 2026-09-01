package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The Hands' reads and its one write (ENT-261, §26.5).
//
// # ON THE AGENT POOL, WITH ONE GUC AND NO USER
//
// The same exception `WatcherContextFor` and the narrate path take, for the
// same reason: a Hands run happens for an organisation nobody is signed in to,
// so there is an organisation to name and no member to name. What keeps that
// honest is that the producer role reaches almost nothing, and that every row
// touched below names its organisation in the predicate rather than trusting a
// policy that is `using (true)` for this role.
//
// # WHAT THIS FILE CANNOT DO, WHICH IS THE POINT OF THE WHOLE ISSUE
//
// It cannot approve a finding: nothing here writes `findings.status`,
// `approved_by` or `approval_reviewed`, and the producer role's ability to
// update `findings` is not the boundary anyway, because approving also has to
// write an audit row naming a human and this pool has no user GUC to name one
// with.
//
// It cannot create a compliance record: it writes `findings.metadata` and
// nothing else. An entry in a register comes from `ExecuteJob`, which reads an
// `executor_jobs` row, which is inserted in exactly one place, inside the
// transaction that writes the approval (00036). So a prepared plan sits beside
// a decision until a person makes it, and if they never do, nothing was
// created.
//
// # AND THE SECOND HALF IS THE ROLE SPLIT, NOT THIS CODE, WHICH WAS MEASURED
//
// `TestPreparingARecordCreatesNoRecordAndNoExecutorJob` asserts both halves
// rather than leaving them to this comment, and it was proven able to fail by
// adding an `insert into executor_jobs` to `PrepareRecord`. What came back was
// better than the assertion: `permission denied for table executor_jobs`
// (42501), from Postgres, before the test could count anything.
//
// That is 00036's grant split doing exactly what it was written for.
// `kindlast_agent` holds `select` on `executor_jobs` and nothing else, because
// the role that lists the work does not get to say the work exists. This pool
// is that role. So the property is not "the Go code chooses not to enqueue";
// it is that the connection this file holds cannot, and a future change here
// that tried would fail loudly in every deployment rather than quietly in one.

// HandsSource is what `approval_plan.source` says about a plan a Hands run
// prepared. Its sibling is `records.DraftSource`, and see the write below for
// why a reader is told which of the two it is looking at.
const HandsSource = "hands"

// ApprovalContext is everything one Hands run reasons over, for one finding.
type ApprovalContext struct {
	Finding  ApprovalFinding
	Register records.Register
	// The open facts, newest values only: what this organisation is currently
	// believed to be, and the only material a run may claim to have filled
	// from.
	Facts []WatchedFact
	// What is already proposed for this record, from the finding's payload.
	// Shown so a run adds what is missing rather than restating what is there.
	AlreadyProposed []records.PreparedField
}

// ApprovalFinding is the finding, as the Hands is shown it.
type ApprovalFinding struct {
	ID                string
	Status            string
	Severity          string
	Detected          string
	ProposedAction    string
	ActionType        string
	ObligationSlug    string
	ObligationTitle   string
	ObligationSummary string
	CitationLabel     string
}

// ErrNoSuchFinding is returned for a finding that does not exist and for one in
// another organisation alike.
//
// One error for both, deliberately, and it is the same rule `Tenant.Finding`
// follows: a caller must not be able to tell an unknown finding from one in
// another tenant, because that difference is what probing for a tenancy leak
// looks like. This caller is a machine principal rather than a browser, which
// makes the rule cheaper to keep rather than less worth keeping.
var ErrNoSuchFinding = errors.New("postgres: no such finding for this organisation")

// ErrNothingToPrepare is returned for a finding whose approval creates no
// record.
//
// A `review` finding is approved and creates nothing, which is the ordinary
// case and not an error in the system. It is an error to ASK the Hands about
// one: there is no record to prepare, and a run that produced an explanation
// of a record that will not exist would be worse than no explanation.
var ErrNothingToPrepare = errors.New(
	"postgres: approving this finding creates no record, so there is nothing to prepare")

// ErrAlreadyEnqueued is returned when a plan arrives after the approval it was
// meant to inform.
//
// # WHY THIS IS THE LINE AND NOT THE FINDING'S STATUS
//
// The obvious rule is "refuse once the finding is approved". The rule here is
// "refuse once an `executor_jobs` row exists", which is the same instant: that
// row is written inside the approving transaction (00036) and nothing else
// writes one.
//
// Stating it as the job rather than the status says what the refusal is FOR.
// The payload stops being a proposal at the moment something is going to act
// on it, and a Hands run arriving a second later must not rewrite what a
// person approved. Keying on the status would be keying on a value that is
// also set by other transitions and read by other code; keying on the job
// keys on the thing whose existence is the hazard.
var ErrAlreadyEnqueued = errors.New(
	"postgres: this finding has been approved and its execution enqueued, so its payload is no longer a proposal")

// ApprovalContextFor assembles one finding's context for a Hands run.
func (a *AgentStore) ApprovalContextFor(
	ctx context.Context, orgID, findingID string,
) (ApprovalContext, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return ApprovalContext{}, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}
	finding, ok := parseID(findingID)
	if !ok {
		// Refused before it reaches SQL, so a malformed id reads as "no such
		// finding" rather than as a cast error from inside a policy.
		return ApprovalContext{}, ErrNoSuchFinding
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return ApprovalContext{}, fmt.Errorf("postgres: beginning the approval context: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return ApprovalContext{}, err
	}

	var context ApprovalContext
	var payload []byte
	err = tx.QueryRow(ctx, `
		select f.id::text,
		       f.status,
		       f.severity::text,
		       f.detected,
		       f.proposed_action,
		       f.action_type,
		       coalesce(f.obligation_slug, ''),
		       coalesce(o.title, ''),
		       coalesce(o.summary, ''),
		       coalesce(f.regulatory_obligation, ''),
		       coalesce(f.metadata -> 'payload', '{}'::jsonb)
		  from findings f
		  left join obligations o on o.id = f.obligation_id
		 where f.id = $1 and f.org_id = $2::uuid
	`, finding, org.String()).Scan(
		&context.Finding.ID,
		&context.Finding.Status,
		&context.Finding.Severity,
		&context.Finding.Detected,
		&context.Finding.ProposedAction,
		&context.Finding.ActionType,
		&context.Finding.ObligationSlug,
		&context.Finding.ObligationTitle,
		&context.Finding.ObligationSummary,
		&context.Finding.CitationLabel,
		&payload,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApprovalContext{}, ErrNoSuchFinding
	}
	if err != nil {
		return ApprovalContext{}, fmt.Errorf("postgres: reading the finding to explain: %w", err)
	}

	register, creates := records.RegisterFor(context.Finding.ActionType)
	if !creates {
		return ApprovalContext{}, ErrNothingToPrepare
	}
	context.Register = register
	context.AlreadyProposed = proposedFrom(register, payload)

	if context.Facts, err = watchedFacts(ctx, tx, org.String()); err != nil {
		return ApprovalContext{}, err
	}

	return context, nil
}

// proposedFrom reads a finding's existing payload as prepared fields.
//
// Only the keys the register knows about, and only the ones that hold
// something. A payload key the register does not have is skipped rather than
// shown, because showing it would invite a run to "keep" a field it is not
// allowed to write back, and it would then be refused for echoing what it was
// given.
//
// `from_fact` is empty for anything the Analyst proposed, which is honest: the
// Analyst's payload comes from the signal rather than from a fact, and
// claiming otherwise here would manufacture provenance for a value that has
// none.
func proposedFrom(register records.Register, payload []byte) []records.PreparedField {
	if len(payload) == 0 {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		// A payload that is not an object is not worth failing a run over: it
		// means nothing was proposed in a shape anything reads, which is the
		// same practical state as nothing having been proposed.
		return nil
	}

	var proposed []records.PreparedField
	for _, field := range register.Fields {
		value, ok := raw[field.Name]
		if !ok {
			continue
		}
		values := decodeValues(value)
		if len(values) == 0 {
			continue
		}
		proposed = append(proposed, records.PreparedField{Name: field.Name, Values: values})
	}
	return proposed
}

// decodeValues reads a payload value as one or more strings.
//
// A payload holds strings and arrays of strings, because that is what the
// Executor reads out of it. Anything else decodes to nothing rather than to
// its JSON spelling: a number rendered as "4" into a plan would be a value the
// plan claims was recorded in that form, and it was not.
func decodeValues(raw json.RawMessage) []string {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if one == "" {
			return nil
		}
		return []string{one}
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		out := make([]string, 0, len(many))
		for _, v := range many {
			if v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	return nil
}

// Plan is what one Hands run prepared for one finding.
type Plan struct {
	// The register the plan is against, so the payload is written in the shape
	// the Executor reads. Carried rather than looked up again here for the
	// reason `records.ValidatePrepared` is called in the service: a second
	// place that decides what a register has is a second place for the two to
	// disagree.
	Register    records.Register
	Explanation string
	Fields      []records.PreparedField
	LeftForYou  []records.LeftForYou
}

// PrepareRecord writes the plan onto the finding: the provenance a person
// reads, and the payload the Executor would create the record from.
//
// # TWO KEYS, BECAUSE THEY ANSWER TWO QUESTIONS
//
// `metadata.payload` is what the Executor reads (00036 and
// `store/postgres/executor.go`). It holds values and nothing about where they
// came from, because that is the shape the Executor's INSERT already expects
// and widening it would mean touching the one code path that writes a
// customer's compliance record.
//
// `metadata.approval_plan` is the provenance: which field came from which
// fact, what was left and why, which skill and version prepared it, and when.
// It is what makes the acceptance criterion true, that a record the Hands
// prepared says what it filled and from what rather than presenting a guess as
// a fact. It is also what a person reads BEFORE deciding, which is the half
// this agent exists for.
//
// # THE PAYLOAD IS MERGED, NOT REPLACED
//
// `||` at the top level, so a key the Analyst proposed and this run did not
// touch survives. Replacing wholesale would mean a run that filled one column
// silently deleted the rest of a proposal, and the customer would see a
// narrower record than the one they were shown a moment earlier.
func (a *AgentStore) PrepareRecord(
	ctx context.Context, orgID, findingID string, plan Plan,
) (Plan, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}
	finding, ok := parseID(findingID)
	if !ok {
		return Plan{}, ErrNoSuchFinding
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("postgres: beginning the prepare: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return Plan{}, err
	}

	// THE GATE, AND IT IS READ IN THE SAME TRANSACTION AS THE WRITE.
	//
	// `for update` on the finding is not available to this role's purpose
	// here, and it is not what the race needs anyway: the hazard is an
	// approval landing between this read and the update below, and an approval
	// is a different transaction on a different pool. What closes it is that
	// the update's own predicate repeats the check, so a job written in
	// between makes the update match nothing.
	var enqueued bool
	err = tx.QueryRow(ctx, `
		select exists (
			select 1 from executor_jobs where finding_id = $1 and org_id = $2::uuid
		)
	`, finding, org.String()).Scan(&enqueued)
	if err != nil {
		return Plan{}, fmt.Errorf("postgres: checking for an enqueued execution: %w", err)
	}
	if enqueued {
		return Plan{}, ErrAlreadyEnqueued
	}

	payload := map[string]any{}
	for _, f := range plan.Fields {
		field, known := plan.Register.Field(f.Name)
		if !known {
			// Unreachable: the service validates against the register before
			// this transaction opens. Skipped rather than written, because the
			// one thing worse than refusing a field the register does not have
			// is writing it into a customer's proposed record.
			continue
		}
		payload[f.Name] = payloadValue(field, f)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return Plan{}, fmt.Errorf("postgres: encoding the prepared payload: %w", err)
	}

	provenance := map[string]any{
		"prepared_at": time.Now().UTC().Format(time.RFC3339),
		// WHICH WRITER PRODUCED THIS PLAN (ENT-287).
		//
		// There are two now: a Hands run, and the sweep's deterministic draft
		// from the organisation's own recorded facts (`domain/records/draft.go`).
		// They write the same two keys and are read by the same surface, and a
		// person's trust in "a model prepared this" and "your own onboarding
		// answers prepared this" is not the same. Rendering them identically
		// would decide that on the customer's behalf, so the plan says which.
		"source":      HandsSource,
		"explanation": plan.Explanation,
		"fields":      provenanceFields(plan.Fields),
		"left_for_you": func() []map[string]any {
			out := make([]map[string]any, 0, len(plan.LeftForYou))
			for _, l := range plan.LeftForYou {
				out = append(out, map[string]any{"name": l.Name, "why": l.Why})
			}
			return out
		}(),
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return Plan{}, fmt.Errorf("postgres: encoding the plan: %w", err)
	}

	// The predicate repeats the gate, so an approval that landed between the
	// read above and this statement makes it match nothing. `metadata` is
	// coalesced because a finding produced before 00022 can carry a null.
	tag, err := tx.Exec(ctx, `
		update findings
		   set metadata = coalesce(metadata, '{}'::jsonb)
		                  || jsonb_build_object(
		                       'payload',
		                       coalesce(metadata -> 'payload', '{}'::jsonb) || $3::jsonb,
		                       'approval_plan', $4::jsonb)
		 where id = $1
		   and org_id = $2::uuid
		   and not exists (
		         select 1 from executor_jobs j
		          where j.finding_id = $1 and j.org_id = $2::uuid
		       )
	`, finding, org.String(), payloadJSON, provenanceJSON)
	if err != nil {
		return Plan{}, fmt.Errorf("postgres: writing the prepared plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either the finding vanished or an approval won the race. Reported as
		// the race, because the caller read the finding a moment ago and the
		// other reading would send somebody looking for a deleted row.
		return Plan{}, ErrAlreadyEnqueued
	}

	if err := tx.Commit(ctx); err != nil {
		return Plan{}, fmt.Errorf("postgres: committing the prepared plan: %w", err)
	}
	return plan, nil
}

// payloadValue renders one prepared field the way the Executor reads it: a
// bare string for a single-valued column, an array for a list.
//
// The shape comes from the REGISTER and not from how many values arrived, and
// that distinction is the bug this function exists to not have. `executor.go`
// reads `->> 'purpose'` and `jsonb_text_array(-> 'data_categories')`, so a
// list written as a string reads as one long value and a one-element list
// written as a string reads as null. Keying on `len(values) == 1` would write
// the second of those every time a run found exactly one data category.
func payloadValue(field records.Field, prepared records.PreparedField) any {
	if field.ListValued {
		return prepared.Values
	}
	if len(prepared.Values) == 0 {
		return ""
	}
	return prepared.Values[0]
}

func provenanceFields(fields []records.PreparedField) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		out = append(out, map[string]any{
			"name":      f.Name,
			"values":    f.Values,
			"from_fact": f.FromFact,
		})
	}
	return out
}
