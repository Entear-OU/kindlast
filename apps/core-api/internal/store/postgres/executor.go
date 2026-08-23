package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
)

// The Executor (ENT-271, ENT-225 phase 2, migration 00036): what used to be
// three `after update of status` triggers on `findings`.
//
// Two roles, two halves, and the split is the interesting part.
//
// LISTING is the agent's, because the relay has no tenant: it asks "what is
// pending" across every organisation, which no application transaction can
// answer. `kindlast_agent` holds `select` on `executor_jobs` and nothing else
// there.
//
// EXECUTING is the application's, as the approver, because the execution
// writes a customer's compliance record. The agent role holds nothing on
// `processing_activities` or `ai_systems` and only `select` on `dsars`, and
// granting it writes would make the role that can invent a finding also able
// to write the record that finding creates. So the execution opens an ordinary
// tenant transaction whose GUCs name the organisation and the approver read
// out of the job row, which is exactly the authority the trigger had, held for
// exactly as long: one transaction.

// ExecutorJob is one pending execution, as the relay lists it.
type ExecutorJob struct {
	ID         string
	OrgID      string
	FindingID  string
	ActionType string
}

// PendingExecutorJobs lists up to `limit` pending jobs, oldest first.
//
// Ids and what they are for, and nothing else: the answer goes into a workflow
// history, so the finding's proposed payload (a customer's draft record) stays
// in the database until the execution reads it.
func (a *AgentStore) PendingExecutorJobs(ctx context.Context, limit int) ([]ExecutorJob, error) {
	rows, err := a.pool.Query(ctx, `
		select id::text, org_id::text, finding_id::text, action_type
		  from executor_jobs
		 where status = 'pending'
		 order by created_at
		 limit $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing pending executor jobs: %w", err)
	}
	defer rows.Close()

	var out []ExecutorJob
	for rows.Next() {
		var job ExecutorJob
		if err := rows.Scan(&job.ID, &job.OrgID, &job.FindingID, &job.ActionType); err != nil {
			return nil, fmt.Errorf("postgres: reading an executor job: %w", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: listing pending executor jobs: %w", err)
	}
	return out, nil
}

// Execution is what one ExecuteJob call did.
type Execution struct {
	// Settled is false when there was nothing pending by that id: it ran
	// earlier, or never existed. The caller reads that as done, because the
	// job of the call is that the job is settled and it is.
	Settled bool
	// The record this execution created, or the one an earlier attempt did.
	RecordID    string
	RecordTable string
}

// ErrBadJobID is returned when the id is not a uuid at all, which is a caller
// bug rather than a state of the table.
var ErrBadJobID = errors.New("postgres: the executor job id is not a uuid")

// ExecuteJob creates the record one approved finding asked for, and settles
// the job, in one transaction as the approver.
//
// # WHY THIS OPENS ITS OWN TENANT TRANSACTION RATHER THAN TAKING ONE
//
// Every other write in this store runs inside a transaction the tenancy
// interceptor opened for a request, whose GUCs come from a person's token.
// This one has no person: the caller is a Temporal worker holding a service
// credential, and the authority it acts with is the approver's, recorded on
// the job row when they approved. So the organisation and the user id are read
// from the row, inside the transaction that will use them, and never taken
// from the request. A caller can name a job id; it cannot name whose authority
// executes it.
//
// # IDEMPOTENT IN THE TWO WAYS THAT MATTER
//
// The job row is claimed `for update` and re-read as pending, so two workflows
// racing (which the engine's one-run-per-id makes unlikely and does not make
// impossible) produce one execution. And the record itself is guarded by the
// same `exists (... where finding_id = ...)` the trigger had, so an execution
// that created a record and then failed to commit its settle does not create a
// second on the retry: it finds the record, settles, and reports it.
func (s *Store) ExecuteJob(ctx context.Context, jobID string) (Execution, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return Execution{}, ErrBadJobID
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Execution{}, fmt.Errorf("postgres: beginning an execution: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	orgID, approver, findingID, actionType, err := s.executorContext(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, nil
	}
	if err != nil {
		return Execution{}, err
	}

	if err := setLocal(ctx, tx, "app.current_user_id", approver); err != nil {
		return Execution{}, err
	}
	if err := setLocal(ctx, tx, "app.current_org_id", orgID); err != nil {
		return Execution{}, err
	}

	tenant := &Tenant{tx: tx, orgID: orgID, userID: approver}

	// Claimed under the policy now that the GUCs are set: a job whose
	// organisation this is not, or whose approver has since left, is invisible
	// here and reads as settled, which is the right answer for both.
	var status string
	err = tx.QueryRow(ctx, `
		select status from executor_jobs where id = $1 for update
	`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, nil
	}
	if err != nil {
		return Execution{}, fmt.Errorf("postgres: claiming an executor job: %w", err)
	}
	if status != "pending" {
		return Execution{}, nil
	}

	execution, err := tenant.createRecord(ctx, findingID, actionType)
	if err != nil {
		return Execution{}, err
	}

	if _, err := tx.Exec(ctx, `
		update executor_jobs
		   set status = 'done', done_at = now(), attempts = attempts + 1, last_error = null
		 where id = $1
	`, id); err != nil {
		return Execution{}, fmt.Errorf("postgres: settling an executor job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Execution{}, fmt.Errorf("postgres: committing an execution: %w", err)
	}
	execution.Settled = true
	return execution, nil
}

// executorContext reads the job's organisation, approver, finding and action.
//
// # THIS ONE READ HAPPENS BEFORE THE TENANCY, AND IT HAS TO
//
// The policy on `executor_jobs` tests both GUCs, and the GUCs are what this
// read produces: there is no order in which a tenant transaction reads the row
// that tells it which tenant to be. So it goes through
// `executor_job_context()`, the tenth SECURITY DEFINER function, which answers
// this one question about one row addressed by its primary key and nothing
// adjacent. 00036 carries the argument, including the two alternatives that
// were rejected (a policy for the untenanted case, and the worker naming the
// approver). Every read after this one, including the claim of this same row,
// happens under the ordinary two-GUC policy.
func (s *Store) executorContext(
	ctx context.Context, tx pgx.Tx, id uuid.UUID,
) (orgID, approver, findingID, actionType string, err error) {
	err = tx.QueryRow(ctx, `
		select org_id::text, approved_by::text, finding_id::text, action_type
		  from executor_job_context($1)
	`, id).Scan(&orgID, &approver, &findingID, &actionType)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", "", err
		}
		return "", "", "", "", fmt.Errorf("postgres: reading an executor job: %w", err)
	}
	return orgID, approver, findingID, actionType, nil
}

// RecordFailedExecution records an attempt that did not work, leaving the job
// pending so it is offered again.
//
// Its own transaction, on the same pool, because the execution's transaction
// has rolled back by the time anybody knows it failed. It sets the GUCs the
// same way and writes nothing but the attempt.
func (s *Store) RecordFailedExecution(ctx context.Context, jobID string, cause error) error {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return ErrBadJobID
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: beginning a failure record: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	orgID, approver, _, _, err := s.executorContext(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := setLocal(ctx, tx, "app.current_user_id", approver); err != nil {
		return err
	}
	if err := setLocal(ctx, tx, "app.current_org_id", orgID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		update executor_jobs
		   set attempts = attempts + 1, last_error = $2
		 where id = $1 and status = 'pending'
	`, id, cause.Error()); err != nil {
		return fmt.Errorf("postgres: recording a failed execution: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: committing a failed execution: %w", err)
	}
	return nil
}

// createRecord is the trigger's body, in Go: insert the record the finding
// proposed, write the audit row naming the approver, and report what was
// created.
//
// The payload is the finding's own `metadata -> 'payload'`, which is what the
// trigger read and what the Analyst wrote; nothing in core-api updates it.
func (t *Tenant) createRecord(ctx context.Context, findingID, actionType string) (Execution, error) {
	switch actionType {
	case findings.ActionCreateROPA:
		return t.createROPA(ctx, findingID)
	case findings.ActionCreateDSAR:
		return t.createDSAR(ctx, findingID)
	case findings.ActionCreateAISystem:
		return t.createAISystem(ctx, findingID)
	default:
		// A job for an action type that creates nothing cannot be enqueued by
		// the approve path, so reaching here means the row was written by
		// something else. Settling it without a record is the honest outcome:
		// there is nothing to create.
		return Execution{}, nil
	}
}

func (t *Tenant) createROPA(ctx context.Context, findingID string) (Execution, error) {
	var existing string
	err := t.tx.QueryRow(ctx,
		`select id::text from processing_activities where finding_id = $1`, findingID).Scan(&existing)
	if err == nil {
		return Execution{RecordID: existing, RecordTable: "processing_activities"}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, fmt.Errorf("postgres: looking for an existing processing activity: %w", err)
	}

	var id string
	if err := t.tx.QueryRow(ctx, `
		insert into processing_activities (
			profile_id, org_id, created_by, finding_id,
			name, purpose, legal_basis, data_categories, recipients, retention_period
		)
		select f.profile_id, f.org_id, $2::uuid, f.id,
		       coalesce(nullif(btrim(f.metadata -> 'payload' ->> 'name'), ''), f.detected),
		       f.metadata -> 'payload' ->> 'purpose',
		       f.metadata -> 'payload' ->> 'legal_basis',
		       public.jsonb_text_array(f.metadata -> 'payload' -> 'data_categories'),
		       public.jsonb_text_array(f.metadata -> 'payload' -> 'recipients'),
		       f.metadata -> 'payload' ->> 'retention_period'
		  from findings f
		 where f.id = $1
		returning id::text
	`, findingID, t.userID).Scan(&id); err != nil {
		return Execution{}, fmt.Errorf("postgres: creating a processing activity: %w", err)
	}
	if err := t.auditCreated(ctx, findingID, "create_ropa", "processing_activities", id); err != nil {
		return Execution{}, err
	}
	return Execution{RecordID: id, RecordTable: "processing_activities"}, nil
}

func (t *Tenant) createDSAR(ctx context.Context, findingID string) (Execution, error) {
	var existing string
	err := t.tx.QueryRow(ctx,
		`select id::text from dsars where finding_id = $1`, findingID).Scan(&existing)
	if err == nil {
		return Execution{RecordID: existing, RecordTable: "dsars"}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, fmt.Errorf("postgres: looking for an existing DSAR: %w", err)
	}

	// THE CLOCK RUNS FROM RECEIPT, NOT FROM APPROVAL (00010, ENT-224).
	//
	// The payload's `received_at`, which the approval already refused to
	// approve without: a request that arrived a week ago and is approved
	// today is due a week earlier than one that arrived today, and starting
	// the clock here would tell a customer they are comfortably on time when
	// they are nearly late. The validation is at approval rather than here
	// for the reason findings.CheckReceipt gives; this is where the validated
	// value is used.
	var id string
	if err := t.tx.QueryRow(ctx, `
		insert into dsars (
			org_id, created_by, finding_id, subject_name, request_type, handler,
			status, received_at, response_due_at
		)
		select f.org_id, $2::uuid, f.id,
		       f.metadata -> 'payload' ->> 'requester',
		       f.metadata -> 'payload' ->> 'request_type',
		       f.metadata -> 'payload' ->> 'handler',
		       'open',
		       (f.metadata -> 'payload' ->> 'received_at')::timestamptz,
		       (f.metadata -> 'payload' ->> 'received_at')::timestamptz + interval '30 days'
		  from findings f
		 where f.id = $1
		returning id::text
	`, findingID, t.userID).Scan(&id); err != nil {
		return Execution{}, fmt.Errorf("postgres: creating a DSAR: %w", err)
	}
	if err := t.auditCreated(ctx, findingID, "create_dsar", "dsars", id); err != nil {
		return Execution{}, err
	}
	return Execution{RecordID: id, RecordTable: "dsars"}, nil
}

func (t *Tenant) createAISystem(ctx context.Context, findingID string) (Execution, error) {
	var existing string
	err := t.tx.QueryRow(ctx,
		`select id::text from ai_systems where finding_id = $1`, findingID).Scan(&existing)
	if err == nil {
		return Execution{RecordID: existing, RecordTable: "ai_systems"}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Execution{}, fmt.Errorf("postgres: looking for an existing AI system: %w", err)
	}

	// No High-Risk gate here, and its absence is the design rather than an
	// omission: it is checked at approval, before anything is written
	// (findings.RequiresReview). A gate at this point would leave a finding
	// approved, an audit row naming the approver, and no record, with nobody
	// told.
	//
	// `last_reviewed_at` is now for the same reason the trigger set it: the
	// approval IS the human review of the classification.
	var id string
	if err := t.tx.QueryRow(ctx, `
		insert into ai_systems (
			profile_id, org_id, created_by, finding_id,
			name, vendor, purpose, risk_classification, documentation_status, last_reviewed_at
		)
		select f.profile_id, f.org_id, $2::uuid, f.id,
		       coalesce(nullif(btrim(f.metadata -> 'payload' ->> 'name'), ''), f.detected),
		       f.metadata -> 'payload' ->> 'vendor',
		       f.metadata -> 'payload' ->> 'purpose',
		       coalesce(nullif(f.metadata -> 'payload' ->> 'risk_classification', ''), 'unclassified'),
		       coalesce(nullif(f.metadata -> 'payload' ->> 'documentation_status', ''), 'missing'),
		       now()
		  from findings f
		 where f.id = $1
		returning id::text
	`, findingID, t.userID).Scan(&id); err != nil {
		return Execution{}, fmt.Errorf("postgres: creating an AI system: %w", err)
	}
	if err := t.auditCreated(ctx, findingID, "create_ai_system", "ai_systems", id); err != nil {
		return Execution{}, err
	}
	return Execution{RecordID: id, RecordTable: "ai_systems"}, nil
}

// auditCreated writes the same audit row the trigger wrote: the action, the
// table and id of what was created, its full state after, and the approver as
// both actor and approving user.
func (t *Tenant) auditCreated(ctx context.Context, findingID, action, table, recordID string) error {
	var after []byte
	if err := t.tx.QueryRow(ctx,
		fmt.Sprintf(`select to_jsonb(r.*) from %s r where r.id = $1`, table), recordID).
		Scan(&after); err != nil {
		return fmt.Errorf("postgres: reading the created record: %w", err)
	}

	if _, err := t.tx.Exec(ctx, `
		select record_audit_log($1, $2, $3::uuid, $4, $5, $6::uuid, null, $7, $2)
	`, t.orgID, t.userID, findingID, action, table, recordID, after); err != nil {
		return fmt.Errorf("postgres: recording the executed action: %w", err)
	}
	return nil
}
