package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The Hands, against the real database (ENT-261).
//
// The acceptance criterion this issue exists for is a negative one, and a
// negative asserted against a fake proves nothing about the schema. Whether
// preparing a record can cause an approval is a question about what rows exist
// afterwards, so it is asked here.
//
// PROVEN ABLE TO FAIL, and one of the two failures was more informative than
// the assertion it was checking.
//
// Adding an `insert into executor_jobs` to `AgentStore.PrepareRecord` turns
// `TestPreparingARecordCreatesNoRecordAndNoExecutorJob` red on its own. It goes
// red with `permission denied for table executor_jobs` (42501) rather than with
// a count, because 00036 grants `kindlast_agent` `select` on that table and
// nothing else. The property is held by the role split rather than by this
// code's restraint, which is the stronger place for it to live.
//
// Dropping the pre-read gate and the `not exists` predicate from the UPDATE
// turns `TestPreparingAfterAnApprovalIsRefused` red on its own, with a plan
// written over an approval that had already been made. Both were run.

func ropaPlan() Plan {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	return Plan{
		Register:    register,
		Explanation: "Approving this adds one entry to your record, for payroll.",
		Fields: []records.PreparedField{
			{Name: "purpose", Values: []string{"Paying people"}, FromFact: "industry"},
			{
				Name:     "data_categories",
				Values:   []string{"names"},
				FromFact: "data_categories",
			},
		},
		LeftForYou: []records.LeftForYou{{
			Name: "retention_period",
			Why:  "You have not told us how long you keep payroll records.",
		}},
	}
}

// seedFact writes one open profile fact, which is what a prepared value may
// name as its source.
func seedFact(t *testing.T, org uuid.UUID, key, valueJSON string) {
	t.Helper()
	if _, err := migratorPool(t).Exec(context.Background(), `
		insert into org_profile_facts (org_id, key, value, source)
		values ($1, $2, $3::jsonb, 'onboarding')
	`, org, key, valueJSON); err != nil {
		t.Fatalf("seeding a profile fact: %v", err)
	}
}

func metadataOf(t *testing.T, findingID string) map[string]json.RawMessage {
	t.Helper()
	var raw []byte
	if err := migratorPool(t).QueryRow(context.Background(),
		`select coalesce(metadata, '{}'::jsonb) from findings where id = $1::uuid`,
		findingID).Scan(&raw); err != nil {
		t.Fatalf("reading the finding's metadata: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding the finding's metadata: %v", err)
	}
	return out
}

// TestPreparingARecordCreatesNoRecordAndNoExecutorJob is the acceptance
// criterion, asked of the database rather than of a fake.
//
// "The Hands cannot create a record without an approval row" is a claim about
// rows, so this counts them. After a prepare there is a plan on the finding and
// nothing else: the finding is still pending, no `executor_jobs` row exists,
// and the register is empty. The record appears only after a human approves,
// which the next test walks all the way through.
func TestPreparingARecordCreatesNoRecordAndNoExecutorJob(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	seedFact(t, org, "industry", `"payroll services"`)
	seedFact(t, org, "data_categories", `["names"]`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA, `{}`)

	if _, err := agent.PrepareRecord(t.Context(), org.String(), finding, ropaPlan()); err != nil {
		t.Fatalf("preparing the record: %v", err)
	}

	pool := migratorPool(t)

	var status string
	if err := pool.QueryRow(context.Background(),
		`select status from findings where id = $1::uuid`, finding).Scan(&status); err != nil {
		t.Fatalf("reading the finding: %v", err)
	}
	if status != "pending" {
		t.Errorf("the finding is %q after a prepare; the Hands never decides", status)
	}

	var jobs int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from executor_jobs where finding_id = $1::uuid`,
		finding).Scan(&jobs); err != nil {
		t.Fatalf("counting executor jobs: %v", err)
	}
	if jobs != 0 {
		t.Errorf("preparing enqueued %d execution(s); an execution exists only "+
			"because a human approved (00036)", jobs)
	}

	var created int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from processing_activities where finding_id = $1::uuid`,
		finding).Scan(&created); err != nil {
		t.Fatalf("counting processing activities: %v", err)
	}
	if created != 0 {
		t.Errorf("preparing created %d register entr(ies); the record is the "+
			"Executor's and it acts on an approval", created)
	}

	// And an audit row would be the other way this could go wrong: a decision
	// attributed to somebody who did not make one.
	var audited int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from audit_log where finding_id = $1::uuid`,
		finding).Scan(&audited); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audited != 0 {
		t.Errorf("preparing wrote %d audit row(s); nothing was decided", audited)
	}
}

// TestAPreparedPayloadIsWhatTheExecutorCreatesTheRecordFrom is the other half:
// the plan is not decorative, and a person who approves gets the record the
// plan described.
//
// This is the failure ENT-261 was filed about, closed. Before it, approving a
// ROPA finding produced a row saying "Not recorded" in every column.
func TestAPreparedPayloadIsWhatTheExecutorCreatesTheRecordFrom(t *testing.T) {
	agent := agentStore(t)
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	seedFact(t, org, "industry", `"payroll services"`)
	seedFact(t, org, "data_categories", `["names"]`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA, `{}`)

	if _, err := agent.PrepareRecord(t.Context(), org.String(), finding, ropaPlan()); err != nil {
		t.Fatalf("preparing the record: %v", err)
	}

	if _, err := approve(t, store, org, owner, finding, true); err != nil {
		t.Fatalf("approving: %v", err)
	}
	jobID, _, _ := jobFor(t, finding)
	if _, err := store.ExecuteJob(t.Context(), jobID); err != nil {
		t.Fatalf("executing: %v", err)
	}

	var purpose string
	var categories []string
	if err := migratorPool(t).QueryRow(context.Background(), `
		select coalesce(purpose, ''), coalesce(data_categories, '{}')
		  from processing_activities where finding_id = $1::uuid
	`, finding).Scan(&purpose, &categories); err != nil {
		t.Fatalf("reading the created record: %v", err)
	}

	if purpose != "Paying people" {
		t.Errorf("purpose is %q; want the prepared value", purpose)
	}
	if len(categories) != 1 || categories[0] != "names" {
		t.Errorf("data_categories is %v; want the prepared list", categories)
	}
}

// TestThePlanSaysWhichColumnsCameFromWhichFactAndWhichWereLeft is the
// acceptance criterion about honesty.
//
// A record the Hands prepared has to say what it filled and from what, and what
// it left for a person, rather than presenting a guess as a fact. That lives in
// `metadata.approval_plan`, beside the payload rather than inside it, because
// the payload is the Executor's input and widening it would mean touching the
// one code path that writes a customer's compliance record.
func TestThePlanSaysWhichColumnsCameFromWhichFactAndWhichWereLeft(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	seedFact(t, org, "industry", `"payroll services"`)
	seedFact(t, org, "data_categories", `["names"]`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA, `{}`)

	if _, err := agent.PrepareRecord(t.Context(), org.String(), finding, ropaPlan()); err != nil {
		t.Fatalf("preparing the record: %v", err)
	}

	metadata := metadataOf(t, finding)
	raw, ok := metadata["approval_plan"]
	if !ok {
		t.Fatal("the finding carries no approval_plan; a prepared record that " +
			"cannot say where its values came from is a guess presented as a fact")
	}

	var plan struct {
		Source      string `json:"source"`
		Explanation string `json:"explanation"`
		Fields      []struct {
			Name     string   `json:"name"`
			Values   []string `json:"values"`
			FromFact string   `json:"from_fact"`
		} `json:"fields"`
		LeftForYou []struct {
			Name string `json:"name"`
			Why  string `json:"why"`
		} `json:"left_for_you"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decoding the approval plan: %v", err)
	}

	if plan.Explanation == "" {
		t.Error("the plan carries no explanation")
	}
	// WHICH WRITER PREPARED IT (ENT-287). The sweep drafts the same two keys
	// deterministically from the organisation's own facts, so a reader that
	// could not tell the two apart would be presenting a model's proposal and
	// a customer's own answers as the same kind of claim.
	if plan.Source != HandsSource {
		t.Errorf("the plan says it was prepared by %q, want %q", plan.Source, HandsSource)
	}
	if len(plan.Fields) != 2 {
		t.Fatalf("the plan records %d filled columns; want 2", len(plan.Fields))
	}
	for _, field := range plan.Fields {
		if field.FromFact == "" {
			t.Errorf("%s was filled with no source recorded", field.Name)
		}
	}
	if len(plan.LeftForYou) != 1 || plan.LeftForYou[0].Name != "retention_period" {
		t.Errorf("the plan leaves %+v; want retention_period with a reason",
			plan.LeftForYou)
	}
	if plan.LeftForYou[0].Why == "" {
		t.Error("a column was left with no reason, which reads as an omission")
	}
}

// TestPreparingAfterAnApprovalIsRefused is the guard that keeps a plan a
// proposal.
//
// The moment an `executor_jobs` row exists the payload is the Executor's input,
// and a Hands run arriving a second later must not rewrite what a person
// approved. Keyed on the job rather than on the finding's status, because the
// job is the thing whose existence is the hazard.
func TestPreparingAfterAnApprovalIsRefused(t *testing.T) {
	agent := agentStore(t)
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	seedFact(t, org, "industry", `"payroll services"`)
	seedFact(t, org, "data_categories", `["names"]`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA,
		`{"payload": {"purpose": "Paying people"}}`)

	if _, err := approve(t, store, org, owner, finding, true); err != nil {
		t.Fatalf("approving: %v", err)
	}

	if _, err := agent.PrepareRecord(
		t.Context(), org.String(), finding, ropaPlan(),
	); !errors.Is(err, ErrAlreadyEnqueued) {
		t.Fatalf("got %v; want ErrAlreadyEnqueued", err)
	}

	// And nothing moved. The payload a person approved is the payload the
	// Executor will read.
	metadata := metadataOf(t, finding)
	if _, wrote := metadata["approval_plan"]; wrote {
		t.Error("a refused prepare wrote its plan anyway")
	}
}

// TestPreparingMergesRatherThanReplacingTheProposedPayload keeps a column the
// Analyst proposed and this run did not touch.
//
// Replacing wholesale would mean a run that filled one column silently deleted
// the rest of a proposal, and the customer would see a narrower record than the
// one they were shown a moment earlier.
func TestPreparingMergesRatherThanReplacingTheProposedPayload(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	seedFact(t, org, "industry", `"payroll services"`)
	seedFact(t, org, "data_categories", `["names"]`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA,
		`{"payload": {"name": "Payroll", "legal_basis": "contract"}}`)

	if _, err := agent.PrepareRecord(t.Context(), org.String(), finding, ropaPlan()); err != nil {
		t.Fatalf("preparing the record: %v", err)
	}

	var payload struct {
		Name           string   `json:"name"`
		LegalBasis     string   `json:"legal_basis"`
		Purpose        string   `json:"purpose"`
		DataCategories []string `json:"data_categories"`
	}
	if err := json.Unmarshal(metadataOf(t, finding)["payload"], &payload); err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}

	if payload.Name != "Payroll" || payload.LegalBasis != "contract" {
		t.Errorf("preparing dropped what was already proposed: %+v", payload)
	}
	if payload.Purpose != "Paying people" || len(payload.DataCategories) != 1 {
		t.Errorf("preparing did not add what it filled: %+v", payload)
	}
}

// TestAListColumnIsWrittenAsAListEvenWithOneValue is the bug `payloadValue`
// exists not to have.
//
// The Executor reads `jsonb_text_array(-> 'data_categories')`, so a
// one-element list written as a bare string reads as null and the column ends
// up empty. Keying the shape on the register rather than on how many values
// arrived is what stops that, and this is what proves it.
func TestAListColumnIsWrittenAsAListEvenWithOneValue(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	seedFact(t, org, "data_categories", `["names"]`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA, `{}`)

	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	if _, err := agent.PrepareRecord(t.Context(), org.String(), finding, Plan{
		Register: register,
		Fields: []records.PreparedField{{
			Name: "data_categories", Values: []string{"names"},
			FromFact: "data_categories",
		}},
	}); err != nil {
		t.Fatalf("preparing the record: %v", err)
	}

	var isArray bool
	if err := migratorPool(t).QueryRow(context.Background(), `
		select jsonb_typeof(metadata -> 'payload' -> 'data_categories') = 'array'
		  from findings where id = $1::uuid
	`, finding).Scan(&isArray); err != nil {
		t.Fatalf("reading the payload's shape: %v", err)
	}
	if !isArray {
		t.Error("a list column with one value was written as a scalar; the " +
			"Executor reads it with jsonb_text_array and would find nothing")
	}
}

// --------------------------------------------------------------------------
// The context a run is given

func TestTheApprovalContextOffersTheRegisterAndTheOrganisationsOpenFacts(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	seedFact(t, org, "industry", `"payroll services"`)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA, `{}`)

	context, err := agent.ApprovalContextFor(t.Context(), org.String(), finding)
	if err != nil {
		t.Fatalf("assembling the approval context: %v", err)
	}

	if context.Register.Name != records.RegisterProcessingActivities {
		t.Errorf("the context names %q as the register", context.Register.Name)
	}
	if len(context.Register.Fields) == 0 {
		t.Error("the context offers no columns, so a run can fill nothing")
	}
	if context.Finding.ID != finding || context.Finding.Status != "pending" {
		t.Errorf("the context describes %+v", context.Finding)
	}

	var found bool
	for _, fact := range context.Facts {
		if fact.Key == "industry" {
			found = true
		}
	}
	if !found {
		t.Error("the context offers no industry fact, so a run that filled a " +
			"column from it would be refused for citing something real")
	}
}

func TestTheContextShowsWhatIsAlreadyProposed(t *testing.T) {
	// Shown so a run adds what is missing rather than restating what is there.
	// `from_fact` is empty for anything the Analyst proposed, which is honest:
	// that payload comes from the signal rather than from a fact.
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA,
		`{"payload": {"name": "Payroll", "data_categories": ["names", "bank details"]}}`)

	context, err := agent.ApprovalContextFor(t.Context(), org.String(), finding)
	if err != nil {
		t.Fatalf("assembling the approval context: %v", err)
	}

	proposed := map[string][]string{}
	for _, field := range context.AlreadyProposed {
		proposed[field.Name] = field.Values
		if field.FromFact != "" {
			t.Errorf("%s claims to come from %q; the Analyst's payload comes "+
				"from the signal and manufacturing provenance for it would be "+
				"the fabrication this whole surface refuses",
				field.Name, field.FromFact)
		}
	}
	if len(proposed["name"]) != 1 || proposed["name"][0] != "Payroll" {
		t.Errorf("already proposed: %v", proposed)
	}
	if len(proposed["data_categories"]) != 2 {
		t.Errorf("already proposed: %v", proposed)
	}
}

func TestAFindingWhoseApprovalCreatesNothingHasNothingToPrepare(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, org, "review", `{}`)

	if _, err := agent.ApprovalContextFor(
		t.Context(), org.String(), finding,
	); !errors.Is(err, ErrNothingToPrepare) {
		t.Fatalf("got %v; want ErrNothingToPrepare", err)
	}
}

// TestAFindingInAnotherOrganisationIsTheSameAnswerAsOneThatDoesNotExist is the
// tenancy half.
//
// One answer for both, deliberately: the difference is exactly what probing for
// a tenancy leak looks like. This caller is a machine principal rather than a
// browser, which makes the rule cheaper to keep rather than less worth keeping.
func TestAFindingInAnotherOrganisationIsTheSameAnswerAsOneThatDoesNotExist(t *testing.T) {
	agent := agentStore(t)
	mine, _ := seedExecutorOrg(t)
	theirs, _ := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, theirs, findings.ActionCreateROPA, `{}`)

	_, theirsErr := agent.ApprovalContextFor(t.Context(), mine.String(), finding)
	_, missingErr := agent.ApprovalContextFor(
		t.Context(), mine.String(), uuid.New().String())

	if !errors.Is(theirsErr, ErrNoSuchFinding) {
		t.Fatalf("another organisation's finding: got %v; want ErrNoSuchFinding", theirsErr)
	}
	if !errors.Is(missingErr, ErrNoSuchFinding) {
		t.Fatalf("a finding that does not exist: got %v; want ErrNoSuchFinding", missingErr)
	}
	if theirsErr.Error() != missingErr.Error() {
		t.Errorf("the two answers differ:\n  theirs:  %v\n  missing: %v",
			theirsErr, missingErr)
	}
}

// TestPreparingIntoAnotherOrganisationsFindingWritesNothing is the same
// property on the write path.
func TestPreparingIntoAnotherOrganisationsFindingWritesNothing(t *testing.T) {
	agent := agentStore(t)
	mine, _ := seedExecutorOrg(t)
	theirs, _ := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, theirs, findings.ActionCreateROPA, `{}`)

	// The predicate names the caller's organisation, so this matches no row.
	// Reported as the race for the reason the store's comment gives: the caller
	// read the finding a moment ago, and "it vanished" would send somebody
	// looking for a deleted row.
	if _, err := agent.PrepareRecord(
		t.Context(), mine.String(), finding, ropaPlan(),
	); err == nil {
		t.Fatal("preparing into another organisation's finding succeeded")
	}

	if _, wrote := metadataOf(t, finding)["approval_plan"]; wrote {
		t.Error("a plan was written into another organisation's finding")
	}
}

// TestAMalformedFindingIdReadsAsNoSuchFinding keeps a cast error out of a
// policy, and out of an error message a caller could learn from.
func TestAMalformedFindingIdReadsAsNoSuchFinding(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedExecutorOrg(t)

	if _, err := agent.ApprovalContextFor(
		t.Context(), org.String(), "not-a-uuid",
	); !errors.Is(err, ErrNoSuchFinding) {
		t.Fatalf("got %v; want ErrNoSuchFinding", err)
	}
}
