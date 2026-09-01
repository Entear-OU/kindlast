package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// The draft the sweep leaves on a finding, against the real database
// (ENT-287).
//
// Everything here is a property of the sweep's own transaction and the two
// roles, so a fake of either would be testing the fake. The sweep runs on the
// producer pool, which is what makes a read it is not granted a `42501` here
// rather than in a deployment, and the guards are in a SQL predicate rather
// than in Go, so only a real statement exercises them.
//
// PROVEN ABLE TO FAIL, each one on its own:
//
//	Removing the `draftPayload` call from `runAnalyst` turns the first two red.
//	Restoring `metadata = excluded.metadata` in `writeFinding` turns
//	"a second sweep keeps what was prepared" red, which is the regression that
//	change fixes.
//	Dropping the `not exists (select 1 from executor_jobs ...)` term turns
//	"nothing is drafted once the execution is enqueued" red.

// seedFacts records what the organisation has told us about itself.
func seedFacts(t *testing.T, org uuid.UUID, facts map[string]string) {
	t.Helper()
	pool := migratorPool(t)
	for key, value := range facts {
		if _, err := pool.Exec(context.Background(), `
			insert into org_profile_facts (org_id, key, value, source)
			values ($1, $2, $3::jsonb, 'onboarding')
		`, org, key, value); err != nil {
			t.Fatalf("seeding the fact %q: %v", key, err)
		}
	}
}

// ropaFacts is an organisation that has answered three of the six Article 30
// questions and none of the other three, which is the ordinary state after
// onboarding.
func ropaFacts() map[string]string {
	return map[string]string{
		"data_categories": `["names", "payroll data"]`,
		"vendor_list":     `["Acme Payroll"]`,
		"lawful_bases":    `["contract"]`,
	}
}

// sweptROPAFinding runs the sweep for an organisation and returns the
// `create_ropa` finding it raised, with its proposed record.
func sweptROPAFinding(t *testing.T, org uuid.UUID) (id string, metadata map[string]json.RawMessage) {
	t.Helper()

	if _, err := agentStore(t).RunSweep(t.Context(), org.String(), false); err != nil {
		t.Fatalf("running the sweep: %v", err)
	}
	return ropaFindingFor(t, org)
}

func ropaFindingFor(t *testing.T, org uuid.UUID) (string, map[string]json.RawMessage) {
	t.Helper()

	var id string
	var raw []byte
	if err := migratorPool(t).QueryRow(context.Background(), `
		select id::text, coalesce(metadata, '{}'::jsonb)
		  from findings
		 where org_id = $1 and action_type = 'create_ropa'
	`, org).Scan(&id, &raw); err != nil {
		t.Fatalf("reading the swept ROPA finding: %v", err)
	}

	metadata := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("decoding the finding's metadata: %v", err)
	}
	return id, metadata
}

func decode[T any](t *testing.T, raw json.RawMessage, what string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding %s: %v", what, err)
	}
	return out
}

// The finding a person opens already carries the record approving it would
// create, filled from their own answers and saying so.
func TestASweptROPAFindingCarriesTheRecordApprovingItWouldCreate(t *testing.T) {
	org, _ := seedExecutorOrg(t)
	seedFacts(t, org, ropaFacts())

	_, metadata := sweptROPAFinding(t, org)

	payload := decode[map[string]any](t, metadata["payload"], "the payload")

	// TYPED THE WAY THE EXECUTOR READS IT, which is the assertion rather than
	// a convenience. `executor.go` reads `->> 'legal_basis'` and
	// `jsonb_text_array(-> 'data_categories')`, so a list written as a string
	// reads as one long value and a one-element list written as a string reads
	// as null. Decoding into these types is what would fail if the shape came
	// from how many values arrived rather than from the register.
	shaped := decode[struct {
		LegalBasis     string   `json:"legal_basis"`
		DataCategories []string `json:"data_categories"`
		Recipients     []string `json:"recipients"`
	}](t, metadata["payload"], "the payload")

	if shaped.LegalBasis != "contract" {
		t.Errorf("legal_basis = %q, want the basis this organisation recorded", shaped.LegalBasis)
	}
	if len(shaped.DataCategories) != 2 || shaped.DataCategories[0] != "names" {
		t.Errorf("data_categories = %v, want the two this organisation recorded", shaped.DataCategories)
	}
	if len(shaped.Recipients) != 1 || shaped.Recipients[0] != "Acme Payroll" {
		t.Errorf("recipients = %v, want the vendor this organisation recorded", shaped.Recipients)
	}

	// THE THREE COLUMNS NOBODY HAS ANSWERED ARE ABSENT RATHER THAN EMPTY.
	// An empty string in the payload would create a record with a column that
	// LOOKS answered, which is the plausible-and-wrong record this whole
	// surface exists to not produce.
	for _, absent := range []string{"name", "purpose", "retention_period"} {
		if _, present := payload[absent]; present {
			t.Errorf("%q was filled, and nothing this organisation recorded says it", absent)
		}
	}

	plan := decode[struct {
		Source string `json:"source"`
		Expl   string `json:"explanation"`
		Fields []struct {
			Name     string `json:"name"`
			FromFact string `json:"from_fact"`
		} `json:"fields"`
		LeftForYou []struct{ Name, Why string } `json:"left_for_you"`
	}](t, metadata["approval_plan"], "the plan")

	if plan.Source != "profile_facts" {
		t.Errorf("source = %q; a person is entitled to know a model did not write this", plan.Source)
	}
	if plan.Expl == "" {
		t.Error("the plan carries no sentence to read above the decision")
	}
	for _, field := range plan.Fields {
		if field.FromFact == "" {
			t.Errorf("%q was filled with no fact behind it", field.Name)
		}
	}
	left := map[string]string{}
	for _, l := range plan.LeftForYou {
		left[l.Name] = l.Why
	}
	for _, name := range []string{"name", "purpose", "retention_period"} {
		if left[name] == "" {
			t.Errorf("%q is neither filled nor left with a reason", name)
		}
	}
}

// The acceptance criterion, end to end and on the real database: a person
// approves what the finding showed them, and the register gains an entry with
// their own answers in it rather than six empty columns.
func TestApprovingASweptROPAFindingCreatesAPopulatedRecord(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	seedFacts(t, org, ropaFacts())

	finding, _ := sweptROPAFinding(t, org)

	if _, err := approve(t, store, org, owner, finding, false); err != nil {
		t.Fatalf("approving: %v", err)
	}
	jobID, _, _ := jobFor(t, finding)
	execution, err := store.ExecuteJob(t.Context(), jobID)
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !execution.Settled || execution.RecordTable != "processing_activities" {
		t.Fatalf("execution = %+v", execution)
	}

	var legalBasis *string
	var categories, recipients []string
	var recordFinding string
	if err := migratorPool(t).QueryRow(context.Background(), `
		select legal_basis, data_categories, recipients, finding_id::text
		  from processing_activities where id = $1::uuid
	`, execution.RecordID).Scan(&legalBasis, &categories, &recipients, &recordFinding); err != nil {
		t.Fatalf("reading the created record: %v", err)
	}

	if legalBasis == nil || *legalBasis != "contract" {
		t.Errorf("legal_basis = %v, want the basis the finding proposed", legalBasis)
	}
	if len(categories) != 2 {
		t.Errorf("data_categories = %v, want the two the finding proposed", categories)
	}
	if len(recipients) != 1 {
		t.Errorf("recipients = %v, want the one the finding proposed", recipients)
	}
	// The record still points back at the decision that created it, which is
	// the property that makes the whole chain auditable.
	if recordFinding != finding {
		t.Errorf("finding_id = %q, want %q", recordFinding, finding)
	}

	// And the audit row still names the human, not the sweep that drafted the
	// values and not the worker that executed the job.
	var actor, approving string
	if err := migratorPool(t).QueryRow(context.Background(), `
		select user_id::text, approving_user_id::text from audit_log
		 where finding_id = $1::uuid and action_type = 'create_ropa'
	`, finding).Scan(&actor, &approving); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if actor != owner.String() || approving != owner.String() {
		t.Errorf("the audit row names %q (approving %q), want the approver %q",
			actor, approving, owner)
	}
}

// The regression this branch fixes: the sweep used to replace `metadata`
// wholesale, so the next scheduled run deleted a payload and a plan that had
// been prepared hours earlier. Nobody would have been told.
func TestASecondSweepKeepsWhatWasPreparedAndDoesNotOverwriteIt(t *testing.T) {
	org, _ := seedExecutorOrg(t)
	seedFacts(t, org, ropaFacts())

	finding, _ := sweptROPAFinding(t, org)

	// The Hands, or a person: a payload with a column the drafter cannot
	// reach, and a plan naming a model run.
	if _, err := migratorPool(t).Exec(context.Background(), `
		update findings
		   set metadata = metadata || jsonb_build_object(
		         'payload', metadata -> 'payload' || '{"purpose": "Paying people"}'::jsonb,
		         'approval_plan', '{"source": "hands"}'::jsonb)
		 where id = $1::uuid
	`, finding); err != nil {
		t.Fatalf("standing in for a prepared plan: %v", err)
	}

	if _, err := agentStore(t).RunSweep(t.Context(), org.String(), false); err != nil {
		t.Fatalf("running the second sweep: %v", err)
	}

	_, metadata := ropaFindingFor(t, org)
	payload := decode[map[string]any](t, metadata["payload"], "the payload")
	if payload["purpose"] != "Paying people" {
		t.Errorf("the second sweep lost the prepared purpose: %v", payload)
	}
	if payload["legal_basis"] != "contract" {
		t.Errorf("the second sweep lost the drafted legal basis: %v", payload)
	}
	plan := decode[map[string]any](t, metadata["approval_plan"], "the plan")
	if plan["source"] != "hands" {
		t.Errorf("the second sweep overwrote the plan a run had prepared: %v", plan)
	}

	// The signal keys still refresh, which is what the upsert is for.
	if _, present := metadata["signal_kind"]; !present {
		t.Error("the merge dropped the signal keys the upsert exists to refresh")
	}
}

// Once an approval has been enqueued the payload is not a proposal any more:
// something is about to create a record out of it. This is `ErrAlreadyEnqueued`
// (store/postgres/hands.go) held on the other write path.
func TestNothingIsDraftedOnceTheExecutionIsEnqueued(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	seedFacts(t, org, ropaFacts())

	finding, _ := sweptROPAFinding(t, org)
	if _, err := approve(t, store, org, owner, finding, false); err != nil {
		t.Fatalf("approving: %v", err)
	}

	// Emptied, so a draft that ignored the guard would be visible rather than
	// indistinguishable from the one already there.
	if _, err := migratorPool(t).Exec(context.Background(), `
		update findings set metadata = metadata - 'payload' - 'approval_plan'
		 where id = $1::uuid
	`, finding); err != nil {
		t.Fatalf("emptying the payload: %v", err)
	}

	if _, err := agentStore(t).RunSweep(t.Context(), org.String(), false); err != nil {
		t.Fatalf("running the sweep: %v", err)
	}

	_, metadata := ropaFindingFor(t, org)
	if _, present := metadata["payload"]; present {
		t.Error("a sweep rewrote the payload of a finding whose execution was enqueued")
	}
}

// A `review` finding is approved and creates nothing, which is most findings.
// Nothing about one changes here: no proposal, no plan, and approving it still
// enqueues no job.
func TestAReviewFindingGainsNoProposedRecord(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	seedFacts(t, org, ropaFacts())

	// An organisation operating an AI system with nothing registered, which is
	// the AI literacy gap: a `review` finding, and the shape most findings
	// have. The facts above are deliberately still there, so this asserts the
	// drafter is silent because the ACTION TYPE creates nothing rather than
	// because there was nothing to draft from.
	if _, err := migratorPool(t).Exec(context.Background(),
		`update compliance_profiles set ai_systems = array['a support triage model'] where org_id = $1`,
		org); err != nil {
		t.Fatalf("recording an AI system: %v", err)
	}

	if _, err := agentStore(t).RunSweep(t.Context(), org.String(), false); err != nil {
		t.Fatalf("running the sweep: %v", err)
	}

	var finding string
	var raw []byte
	if err := migratorPool(t).QueryRow(context.Background(), `
		select id::text, coalesce(metadata, '{}'::jsonb)
		  from findings
		 where org_id = $1 and action_type = 'review'
		 limit 1
	`, org).Scan(&finding, &raw); err != nil {
		t.Fatalf("reading a review finding: %v", err)
	}

	metadata := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("decoding the metadata: %v", err)
	}
	for _, key := range []string{"payload", "approval_plan"} {
		if _, present := metadata[key]; present {
			t.Errorf("a review finding carries %q, and approving it creates nothing", key)
		}
	}

	acted, err := approve(t, store, org, owner, finding, false)
	if err != nil {
		t.Fatalf("approving: %v", err)
	}
	if acted.CreatedRecordID != "" {
		t.Errorf("approving a review finding created %q", acted.CreatedRecordID)
	}
	var jobs int
	if err := migratorPool(t).QueryRow(context.Background(),
		`select count(*) from executor_jobs where finding_id = $1::uuid`, finding).
		Scan(&jobs); err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	if jobs != 0 {
		t.Errorf("approving a review finding enqueued %d jobs", jobs)
	}
}
