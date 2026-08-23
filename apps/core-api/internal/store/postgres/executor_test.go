package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
)

// The Executor, against the real database (ENT-271, ENT-225 phase 2).
//
// Everything interesting here is a property of the schema and the two roles,
// so a fake of either would be testing the fake: the approval enqueues in the
// transaction that approves, the listing crosses organisations on the producer
// pool, the execution creates the record as the approver under the tenant's
// own policies, and neither the record nor the audit row differs from what the
// trigger wrote.
//
// PROVEN ABLE TO FAIL. Removing the `enqueueExecutorJob` call from
// ApproveFinding turns "an approval enqueues one job" red on its own; removing
// the `exists` guard in `createROPA` turns "executing twice creates one
// record" red on its own; removing the High-Risk term from
// findings.RequiresReview turns the gate test in the service package red.

// seedExecutorOrg makes an organisation with an owner and a profile, and
// removes it after. The cascade from `organisations` takes the findings, the
// jobs and the records with it.
func seedExecutorOrg(t *testing.T) (org, owner uuid.UUID) {
	t.Helper()
	pool := migratorPool(t)
	org, owner = uuid.New(), uuid.New()

	if _, err := pool.Exec(context.Background(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "executor-"+org.String()[:8], "Executor test"); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org, owner); err != nil {
		t.Fatalf("seeding an owner: %v", err)
	}
	session, profile := uuid.New(), uuid.New()
	if _, err := pool.Exec(context.Background(),
		`insert into onboarding_sessions (id, org_id, created_by) values ($1, $2, $3)`,
		session, org, owner); err != nil {
		t.Fatalf("seeding a session: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into compliance_profiles
		  (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
		values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')
	`, profile, org, owner, session); err != nil {
		t.Fatalf("seeding a profile: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		pool.Exec(context.WithoutCancel(context.Background()),
			`delete from organisations where id = $1`, org)
	})
	return org, owner
}

// seedApprovableFinding writes a pending finding with an action type and a
// proposed payload, the shape the Analyst produces.
func seedApprovableFinding(t *testing.T, org uuid.UUID, actionType, payload string) string {
	t.Helper()
	pool := migratorPool(t)
	id, signal := uuid.New(), uuid.New()

	if _, err := pool.Exec(context.Background(), `
		insert into watcher_findings (id, org_id, profile_id, kind, title, dedup_key)
		select $1, $2, p.id, 'profile_gap', 'executor fixture', $3
		  from compliance_profiles p where p.org_id = $2 limit 1
	`, signal, org, "executor-"+signal.String()); err != nil {
		t.Fatalf("seeding a signal: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into findings (id, org_id, profile_id, watcher_finding_id, obligation_id,
		                      detected, proposed_action, status, action_type, metadata)
		select $1, $2, p.id, $3, o.id, 'executor fixture', 'create the record', 'pending', $4, $5::jsonb
		  from compliance_profiles p, obligations o
		 where p.org_id = $2 limit 1
	`, id, org, signal, actionType, payload); err != nil {
		t.Fatalf("seeding a finding: %v", err)
	}
	return id.String()
}

func approve(t *testing.T, store *Store, org, owner uuid.UUID, findingID string, reviewed bool) (findings.Acted, error) {
	t.Helper()
	tenant, err := store.BeginTenant(t.Context(), owner.String(), org.String())
	if err != nil {
		t.Fatalf("beginning the tenant transaction: %v", err)
	}
	acted, err := tenant.ApproveFinding(t.Context(), findingID, reviewed)
	if err != nil {
		tenant.Rollback(t.Context())
		return findings.Acted{}, err
	}
	if err := tenant.Commit(t.Context()); err != nil {
		t.Fatalf("committing: %v", err)
	}
	return acted, nil
}

func jobFor(t *testing.T, findingID string) (id, status string, attempts int) {
	t.Helper()
	if err := migratorPool(t).QueryRow(context.Background(), `
		select id::text, status, attempts from executor_jobs where finding_id = $1::uuid
	`, findingID).Scan(&id, &status, &attempts); err != nil {
		t.Fatalf("reading the executor job: %v", err)
	}
	return id, status, attempts
}

func TestApprovingEnqueuesOneJobAndCreatesNothingYet(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA,
		`{"payload": {"name": "Payroll", "purpose": "Paying people", "legal_basis": "contract"}}`)

	acted, err := approve(t, store, org, owner, finding, false)
	if err != nil {
		t.Fatalf("approving: %v", err)
	}
	if !acted.Applied {
		t.Fatal("the approval did not apply")
	}
	// THE CONTRACT CHANGE, ASSERTED RATHER THAN DISCOVERED. The trigger used
	// to have created the record inside this transaction and the response
	// carried its id; execution is behind the event boundary now (§3), so a
	// fresh approval reports no record and the record arrives a moment later.
	if acted.CreatedRecordID != "" {
		t.Fatalf("a fresh approval reported a record (%s): execution is asynchronous now", acted.CreatedRecordID)
	}

	_, status, attempts := jobFor(t, finding)
	if status != "pending" || attempts != 0 {
		t.Fatalf("job is %s after %d attempts, want pending after 0", status, attempts)
	}

	var records int
	if err := migratorPool(t).QueryRow(context.Background(),
		`select count(*) from processing_activities where finding_id = $1::uuid`, finding).Scan(&records); err != nil {
		t.Fatalf("counting records: %v", err)
	}
	if records != 0 {
		t.Fatal("approving created the record synchronously; the trigger is still there")
	}
}

func TestApprovingAFindingThatCreatesNothingEnqueuesNothing(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	// `review` is every finding until the corpus is classified (ENT-165).
	finding := seedApprovableFinding(t, org, "review", `{}`)

	if _, err := approve(t, store, org, owner, finding, false); err != nil {
		t.Fatalf("approving: %v", err)
	}

	var jobs int
	if err := migratorPool(t).QueryRow(context.Background(),
		`select count(*) from executor_jobs where finding_id = $1::uuid`, finding).Scan(&jobs); err != nil {
		t.Fatalf("counting jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatal("a finding that creates no record enqueued a job nothing can run")
	}
}

func TestTheExecutionCreatesTheRecordAsTheApproverAndAuditsIt(t *testing.T) {
	store := testStore(t)
	agent := agentStore(t)
	org, owner := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, org, findings.ActionCreateROPA,
		`{"payload": {"name": "Payroll", "purpose": "Paying people", "legal_basis": "contract", "data_categories": ["names"], "recipients": ["a bank"], "retention_period": "7 years"}}`)

	if _, err := approve(t, store, org, owner, finding, false); err != nil {
		t.Fatalf("approving: %v", err)
	}

	// Listed by the producer, across organisations, with no tenant set.
	jobs, err := agent.PendingExecutorJobs(t.Context(), 1000)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var mine *ExecutorJob
	for i := range jobs {
		if jobs[i].FindingID == finding {
			mine = &jobs[i]
		}
	}
	if mine == nil {
		t.Fatal("the approval's job was not listed for the relay")
	}
	if mine.OrgID != org.String() || mine.ActionType != findings.ActionCreateROPA {
		t.Fatalf("listed %+v", mine)
	}

	execution, err := store.ExecuteJob(t.Context(), mine.ID)
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !execution.Settled || execution.RecordTable != "processing_activities" || execution.RecordID == "" {
		t.Fatalf("execution = %+v", execution)
	}

	// The record the trigger would have written, column for column.
	pool := migratorPool(t)
	var name, purpose, basis, createdBy string
	var categories, recipients []string
	if err := pool.QueryRow(context.Background(), `
		select name, purpose, legal_basis, created_by::text, data_categories, recipients
		  from processing_activities where id = $1::uuid
	`, execution.RecordID).Scan(&name, &purpose, &basis, &createdBy, &categories, &recipients); err != nil {
		t.Fatalf("reading the record: %v", err)
	}
	if name != "Payroll" || purpose != "Paying people" || basis != "contract" {
		t.Fatalf("record = %s / %s / %s", name, purpose, basis)
	}
	if len(categories) != 1 || categories[0] != "names" || len(recipients) != 1 {
		t.Fatalf("arrays = %v / %v", categories, recipients)
	}
	// AS THE APPROVER, not as the system: the record exists by their decision.
	if createdBy != owner.String() {
		t.Fatalf("created_by = %s, want the approver %s", createdBy, owner)
	}

	// And the audit row the trigger wrote, with the same action and target.
	var actor, approving, actorRole, targetTable string
	var after []byte
	if err := pool.QueryRow(context.Background(), `
		select user_id::text, approving_user_id::text, actor_role, target_table, after
		  from audit_log
		 where finding_id = $1::uuid and action_type = 'create_ropa'
	`, finding).Scan(&actor, &approving, &actorRole, &targetTable, &after); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if actor != owner.String() || approving != owner.String() {
		t.Fatalf("audit row names %s (approving %s), want the approver %s", actor, approving, owner)
	}
	if targetTable != "processing_activities" || len(after) == 0 {
		t.Fatalf("audit row targets %s with %d bytes after", targetTable, len(after))
	}
	// The role snapshot record_audit_log takes is the approver's role at the
	// time, which is why the executor calls the same function rather than
	// inserting the row itself (00002; the correction ENT-225 phase 1 made).
	if actorRole != "owner" {
		t.Fatalf("actor_role = %q, want the approver's role at the time", actorRole)
	}
}

func TestExecutingTwiceCreatesOneRecord(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, org, findings.ActionCreateDSAR,
		`{"payload": {"requester": "A person", "request_type": "access", "handler": "The DPO", "received_at": "2026-08-01T09:00:00Z"}}`)

	if _, err := approve(t, store, org, owner, finding, false); err != nil {
		t.Fatalf("approving: %v", err)
	}
	jobID, _, _ := jobFor(t, finding)

	first, err := store.ExecuteJob(t.Context(), jobID)
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !first.Settled || first.RecordTable != "dsars" {
		t.Fatalf("first = %+v", first)
	}

	// The retry a workflow makes when its activity timed out after the
	// transaction committed. It must not create a second DSAR.
	second, err := store.ExecuteJob(t.Context(), jobID)
	if err != nil {
		t.Fatalf("re-executing: %v", err)
	}
	if second.Settled {
		t.Fatal("a settled job was settled again")
	}

	var records int
	if err := migratorPool(t).QueryRow(context.Background(),
		`select count(*) from dsars where finding_id = $1::uuid`, finding).Scan(&records); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if records != 1 {
		t.Fatalf("%d DSARs for one approval, want 1", records)
	}

	// THE CLOCK RUNS FROM RECEIPT (00010, ENT-224), not from approval: this
	// request arrived on 1 August and is due on 31 August, whenever it was
	// approved.
	var received, due string
	if err := migratorPool(t).QueryRow(context.Background(), `
		select received_at::date::text, response_due_at::date::text
		  from dsars where finding_id = $1::uuid
	`, finding).Scan(&received, &due); err != nil {
		t.Fatalf("reading the clock: %v", err)
	}
	if received != "2026-08-01" || due != "2026-08-31" {
		t.Fatalf("received %s, due %s; want the payload's date and thirty days after it", received, due)
	}
}

// The gate that used to be a `raise check_violation` inside the trigger. It
// refuses before anything is written, so the finding is still pending and no
// job exists: a person who is told to review can review and approve again.
func TestAnUnreviewedHighRiskApprovalIsRefusedAndWritesNothing(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)
	finding := seedApprovableFinding(t, org, findings.ActionCreateAISystem,
		`{"payload": {"name": "The CV ranker", "risk_classification": "high"}}`)

	_, err := approve(t, store, org, owner, finding, false)
	if !errors.Is(err, findings.ErrReviewRequired) {
		t.Fatalf("err = %v, want ErrReviewRequired", err)
	}

	pool := migratorPool(t)
	var status string
	if err := pool.QueryRow(context.Background(),
		`select status from findings where id = $1::uuid`, finding).Scan(&status); err != nil {
		t.Fatalf("reading the finding: %v", err)
	}
	if status != "pending" {
		t.Fatalf("the finding is %s after a refused approval, want pending", status)
	}
	var jobs, systems int
	if err := pool.QueryRow(context.Background(), `
		select (select count(*) from executor_jobs where finding_id = $1::uuid),
		       (select count(*) from ai_systems where finding_id = $1::uuid)
	`, finding).Scan(&jobs, &systems); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if jobs != 0 || systems != 0 {
		t.Fatalf("a refused approval left %d jobs and %d systems", jobs, systems)
	}

	// Reviewed, the same approval goes through and the system is created with
	// its classification.
	if _, err := approve(t, store, org, owner, finding, true); err != nil {
		t.Fatalf("approving with review: %v", err)
	}
	jobID, _, _ := jobFor(t, finding)
	if _, err := store.ExecuteJob(t.Context(), jobID); err != nil {
		t.Fatalf("executing: %v", err)
	}
	var classification, docs string
	if err := pool.QueryRow(context.Background(), `
		select risk_classification, documentation_status from ai_systems where finding_id = $1::uuid
	`, finding).Scan(&classification, &docs); err != nil {
		t.Fatalf("reading the system: %v", err)
	}
	if classification != "high" || docs != "missing" {
		t.Fatalf("system = %s / %s, want high / missing", classification, docs)
	}
}

func TestABadJobIdIsRefusedBeforeTheDatabaseIsAsked(t *testing.T) {
	store := testStore(t)
	if _, err := store.ExecuteJob(t.Context(), "not-a-uuid"); !errors.Is(err, ErrBadJobID) {
		t.Fatalf("err = %v, want ErrBadJobID", err)
	}
}

// ENT-224's rule, and the reason it had to move to the approval with the
// execution: a refusal here leaves the finding pending and nothing written,
// which is what "the human sees an error rather than a created record" means
// once execution is asynchronous.
func TestADsarApprovalWithoutABelievableReceiptIsRefusedAndWritesNothing(t *testing.T) {
	store := testStore(t)
	org, owner := seedExecutorOrg(t)

	for name, payload := range map[string]string{
		"no receipt at all":     `{"payload": {"requester": "A person", "request_type": "access"}}`,
		"not a timestamp":       `{"payload": {"requester": "A person", "received_at": "last tuesday"}}`,
		"arrived in the future": `{"payload": {"requester": "A person", "received_at": "2099-01-01T00:00:00Z"}}`,
	} {
		finding := seedApprovableFinding(t, org, findings.ActionCreateDSAR, payload)
		_, err := approve(t, store, org, owner, finding, false)
		if err == nil {
			t.Fatalf("%s: the approval went through", name)
		}
		switch {
		case errors.Is(err, findings.ErrReceiptRequired),
			errors.Is(err, findings.ErrReceiptMalformed),
			errors.Is(err, findings.ErrReceiptInFuture):
		default:
			t.Fatalf("%s: err = %v, want a receipt refusal", name, err)
		}

		var status string
		var jobs, dsars int
		if err := migratorPool(t).QueryRow(context.Background(), `
			select (select status from findings where id = $1::uuid),
			       (select count(*) from executor_jobs where finding_id = $1::uuid),
			       (select count(*) from dsars where finding_id = $1::uuid)
		`, finding).Scan(&status, &jobs, &dsars); err != nil {
			t.Fatalf("%s: reading back: %v", name, err)
		}
		if status != "pending" || jobs != 0 || dsars != 0 {
			t.Fatalf("%s: finding is %s with %d jobs and %d DSARs", name, status, jobs, dsars)
		}
	}
}
