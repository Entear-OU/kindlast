package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Findings waiting for a narrative, and what a run may cite (ENT-245).
//
// # ON THE AGENT POOL, WITH ONE GUC AND NO USER
//
// Same exception `RunSweep` takes and for the same reason: narrating happens
// for organisations nobody is signed in to, so there is an org to name and no
// member to name. The agent's policies on `findings` are unconditional, which
// is what makes that work, and what keeps it honest is that the role reaches
// almost nothing else and that every row it touches names its organisation.

// PendingFinding is one finding with no narrative, and the single obligation a
// run drafting for it is permitted to cite.
type PendingFinding struct {
	ID             uuid.UUID
	Detected       string
	ProposedAction string
	Severity       string

	// THE OFFERED SET, AND IT IS DELIBERATELY ONE OBLIGATION.
	//
	// The validator checks the model's citations against what it was offered
	// rather than against the corpus, so the narrower this is, the stronger the
	// check. A finding is about exactly one obligation; a narrative for it that
	// cites a different article is wrong even when that article exists, and
	// offering the whole corpus would make that fabrication indistinguishable
	// from a good citation.
	//
	// This is the check that catches the failure the local model actually
	// exhibits. Asked which GDPR article requires a record of processing
	// activities it answers 50, then 34, then 54, all schema-valid.
	ObligationSlug    string
	ObligationTitle   string
	ObligationSummary string

	// The obligation's declared applicability conditions, as stored (ENT-248).
	//
	// Raw JSON rather than a parsed shape, because the parsing belongs in
	// `domain/corpus` next to the vocabulary that defines it: this store's job
	// is to hand back what the row says. `corpus.AppliesBecause` turns it into
	// the sentences the Analyst is given as grounds, which is what stops the
	// model working out its own reason for the finding and getting the law
	// wrong on the way.
	ObligationAppliesWhen string
}

// FindingsAwaitingNarrative returns findings with neither a narrative nor a
// recorded refusal, oldest first.
//
// Oldest first because a finding somebody has been looking at unexplained for
// a week is worth explaining before one created a minute ago. A newest-first
// job on a slow model would leave the oldest backlog permanently unnarrated.
func (a *AgentStore) FindingsAwaitingNarrative(
	ctx context.Context,
	orgID string,
	limit int32,
) ([]PendingFinding, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading findings to narrate: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		select f.id,
		       f.detected,
		       f.proposed_action,
		       f.severity::text,
		       coalesce(o.slug, ''),
		       coalesce(o.title, ''),
		       coalesce(o.summary, ''),
		       coalesce(o.applies_when::text, '')
		  from public.findings f
		  join public.obligations o on o.id = f.obligation_id
		 where f.narrative is null
		   and f.narrative_refusal is null
		   and f.status = 'pending'
		 order by f.created_at
		 limit $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading findings to narrate: %w", err)
	}
	defer rows.Close()

	pending := make([]PendingFinding, 0, limit)
	for rows.Next() {
		var f PendingFinding
		if err := rows.Scan(
			&f.ID,
			&f.Detected,
			&f.ProposedAction,
			&f.Severity,
			&f.ObligationSlug,
			&f.ObligationTitle,
			&f.ObligationSummary,
			&f.ObligationAppliesWhen,
		); err != nil {
			return nil, fmt.Errorf("postgres: scanning a finding to narrate: %w", err)
		}
		pending = append(pending, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pending, tx.Commit(ctx)
}

// RecordNarrative stores a drafted narrative and the run that produced it.
//
// # IT WRITES THREE COLUMNS AND TOUCHES NOTHING ELSE
//
// Not `detected`, not `proposed_action`, not `severity`, not `status`. The
// deterministic sweep owns those and the model adds beside them. The version
// of this that overwrote `detected` is ENT-164: prose landed in the slot the
// card renders as a heading.
//
// `agent_run_id` is written in the same statement as the narrative rather than
// after it. A narrative whose provenance is a second write is a narrative that
// can exist without one, and "how this was produced" would then have a case
// where the honest answer is "we do not know".
func (a *AgentStore) RecordNarrative(
	ctx context.Context,
	orgID string,
	findingID uuid.UUID,
	narrative, agentRunID string,
) error {
	return a.updateNarrative(ctx, orgID, `
		update public.findings
		   set narrative = $1,
		       agent_run_id = nullif($2, '')::uuid,
		       narrative_generated_at = now(),
		       updated_at = now()
		 where id = $3`, narrative, agentRunID, findingID)
}

// RecordNarrativeRefusal stores why no narrative was produced.
//
// # A REFUSAL IS RECORDED RATHER THAN RETRIED FOREVER
//
// Without this, a finding the model cannot narrate correctly is picked up by
// every subsequent pass, and a stack with one such finding burns its whole
// model budget on it in a loop. Writing the refusal takes it out of the queue.
//
// It is also a fact worth keeping. "We tried, and the model cited an article
// that does not apply to you" is exactly what a customer deciding whether to
// trust this product should be able to see, and a refusal that leaves no trace
// is indistinguishable from never having run.
//
// The run id is recorded too, so a refusal is as inspectable as a success.
func (a *AgentStore) RecordNarrativeRefusal(
	ctx context.Context,
	orgID string,
	findingID uuid.UUID,
	reason, agentRunID string,
) error {
	return a.updateNarrative(ctx, orgID, `
		update public.findings
		   set narrative_refusal = $1,
		       agent_run_id = nullif($2, '')::uuid,
		       updated_at = now()
		 where id = $3`, reason, agentRunID, findingID)
}

func (a *AgentStore) updateNarrative(
	ctx context.Context,
	orgID, query, text, agentRunID string,
	findingID uuid.UUID,
) error {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: recording the narrative: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, query, text, agentRunID, findingID)
	if err != nil {
		return fmt.Errorf("postgres: recording the narrative: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// The finding belongs to another organisation, or has gone. Reported
		// rather than ignored: a narrator that silently wrote nothing would
		// look exactly like one that worked, and the finding would be picked up
		// again on every pass.
		return fmt.Errorf("postgres: finding %s was not updated", findingID)
	}
	return tx.Commit(ctx)
}
