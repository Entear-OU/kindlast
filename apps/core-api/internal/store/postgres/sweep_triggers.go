package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// `sweep_triggers` (00035), from both ends.
//
// The application enqueues inside the transaction that makes the announced
// fact true (a confirmed onboarding), and cannot mark anything done. The agent
// lists and marks, across every organisation, and cannot author a trigger. See
// the migration header for why the split is drawn there, and why it is a table
// rather than a call from inside ConfirmProfile.

// EnqueueSweepTrigger writes a sweep-triggers row in the caller's transaction.
//
// Same reason EnqueueMessage takes `*Tenant` rather than the pool: the row has
// to land in the same transaction as the fact that makes it true, or a relay
// racing the request could list it before the profile it names is visible to
// any other connection. See the migration header for the failure this closes.
func (t *Tenant) EnqueueSweepTrigger(ctx context.Context, reason string) error {
	if _, err := t.tx.Exec(ctx, `
		insert into sweep_triggers (org_id, reason)
		values ($1, $2)
	`, t.orgID, reason); err != nil {
		return fmt.Errorf("postgres: enqueuing a sweep trigger: %w", err)
	}
	return nil
}

// SweepTrigger is one pending row, as the relay lists it.
type SweepTrigger struct {
	ID     string
	OrgID  string
	Reason string
}

// PendingSweepTriggers lists up to `limit` pending triggers, oldest first.
//
// Ids and the organisation, nothing else: the answer is written into a
// workflow history, and the organisation id is the one thing the sweep
// activity needs.
func (a *AgentStore) PendingSweepTriggers(ctx context.Context, limit int) ([]SweepTrigger, error) {
	rows, err := a.pool.Query(ctx, `
		select id::text, org_id::text, reason
		  from sweep_triggers
		 where status = 'pending'
		 order by created_at
		 limit $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing pending sweep triggers: %w", err)
	}
	defer rows.Close()

	var out []SweepTrigger
	for rows.Next() {
		var t SweepTrigger
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Reason); err != nil {
			return nil, fmt.Errorf("postgres: reading a sweep trigger: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: listing pending sweep triggers: %w", err)
	}
	return out, nil
}

// ErrBadTriggerID is returned when the id is not a uuid, which is a caller
// bug rather than a state of the table.
var ErrBadTriggerID = errors.New("postgres: the trigger id is not a uuid")

// SettleSweepTrigger records what a triggered sweep did.
//
// A nil cause marks the row done; a non-nil one records the attempt and the
// reason and leaves the row pending, matching the transactional outbox's
// choice for a message that failed to send: the relay offers it again once
// the workflow that held it has closed, and `done` is reserved for a sweep
// that actually ran. Returns false when the row was already done, which a
// retry of the settle activity reads as success.
func (a *AgentStore) SettleSweepTrigger(ctx context.Context, id string, cause error) (bool, error) {
	if _, err := uuid.Parse(id); err != nil {
		return false, ErrBadTriggerID
	}
	if cause != nil {
		tag, err := a.pool.Exec(ctx, `
			update sweep_triggers
			   set attempts = attempts + 1, last_error = $2
			 where id = $1::uuid and status = 'pending'
		`, id, cause.Error())
		if err != nil {
			return false, fmt.Errorf("postgres: recording a failed sweep: %w", err)
		}
		return tag.RowsAffected() == 1, nil
	}
	tag, err := a.pool.Exec(ctx, `
		update sweep_triggers
		   set status = 'done', done_at = now(), attempts = attempts + 1, last_error = null
		 where id = $1::uuid and status = 'pending'
	`, id)
	if err != nil {
		return false, fmt.Errorf("postgres: marking a sweep trigger done: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// SweepTargets lists every organisation a scheduled sweep should visit, via
// `sweep_targets()` (00035): the producer role cannot enumerate tenants, and
// the definer function answers this one question and nothing adjacent.
func (a *AgentStore) SweepTargets(ctx context.Context) ([]string, error) {
	rows, err := a.pool.Query(ctx, `select sweep_targets()::text`)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing sweep targets: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres: reading a sweep target: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: listing sweep targets: %w", err)
	}
	return ids, nil
}

// Analysis is the result of running the Analyst alone over one organisation.
type Analysis struct {
	Findings int32
	RanAt    time.Time
}

// RunAnalyst runs the Analyst, and only the Analyst, for one organisation:
// the second activity of a sweep workflow, after RunSweep with detect_only.
//
// Same shape as RunSweep: one transaction, one GUC, no actor. `run_analyst()`
// works over the signals nobody has analysed yet, so a second call finds none
// and reports zero, which is what lets a workflow retry it on its own.
func (a *AgentStore) RunAnalyst(ctx context.Context, orgID string) (Analysis, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return Analysis{}, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return Analysis{}, fmt.Errorf("postgres: beginning the analysis: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return Analysis{}, err
	}

	var result Analysis
	if err := tx.QueryRow(ctx, `select public.run_analyst()`).Scan(&result.Findings); err != nil {
		return Analysis{}, fmt.Errorf("postgres: running the analyst: %w", err)
	}
	if err := tx.QueryRow(ctx, `select now()`).Scan(&result.RanAt); err != nil {
		return Analysis{}, fmt.Errorf("postgres: reading the analysis time: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Analysis{}, fmt.Errorf("postgres: committing the analysis: %w", err)
	}
	return result, nil
}
