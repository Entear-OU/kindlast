package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
)

// Evidence ingest, on the producer pool (ENT-231, §26.4).
//
// # THE DOOR EVIDENCE COMES IN BY WHEN NOBODY IS SIGNED IN
//
// `IngestService.IngestEvidence` is what the scheduled Watcher at build-order
// step 8 uses, and any gateway-initiated fetch running for an organisation
// with no session behind it. It runs as `kindlast_agent`, whose insert
// policies on these two tables are org-scoped through the GUC and carry no
// membership check, matching every producer policy since 00008: there is no
// member to check because nobody asked.
//
// # THE GUC IS SET, AND IT IS THE WHOLE TENANCY STORY
//
// Unlike `RecordAgentRun` next door, whose policy is unconditional, these two
// policies read `app.current_org_id`. So a caller that sets no GUC writes
// NOTHING rather than everything, which is the direction this should fail in:
// `current_setting(..., true)` returns null when unset, the `with check`
// compares against null, and the insert is refused.
//
// # ONE TRANSACTION, BECAUSE HALF A FETCH IS UNREADABLE
//
// The observation and the fetch record are written together. A fetch record
// pointing at an evidence row that was never written offers the "what we
// fetched" view a link to nothing, and an observation with no fetch record is
// something that appeared in the customer's memory with no account of how.

// FetchRecord is one fetch as it arrives from a machine caller.
type FetchRecord struct {
	OrgID        uuid.UUID
	ConnectionID uuid.UUID
	Tool         string
	// ArgumentsJSON and ContentJSON have already been through the gateway's
	// redactor. This store does not re-apply it and could not: a second
	// implementation here would be free to disagree with the first, and the
	// one that matters is the one that ran before the content crossed the
	// network.
	ArgumentsJSON string
	ContentJSON   string
	Outcome       string
	Detail        string
	Redactions    int32
	ObservedAt    time.Time
	RequestedAt   time.Time
}

// IngestEvidence writes one observation and the fetch that produced it.
//
// Returns the evidence id, which is empty when the fetch produced nothing, and
// the fetch id, which is never empty because a fetch is recorded whatever
// became of it.
func (a *AgentStore) IngestEvidence(
	ctx context.Context, record FetchRecord,
) (evidenceID string, fetchID string, err error) {
	// Validated here rather than trusted, because a malformed payload stored
	// now is a page that cannot render later, and the failure would surface to
	// a customer reading their own evidence rather than to whoever sent it.
	// The same check `RecordAgentRun` makes, for the same reason.
	for name, raw := range map[string]string{
		"arguments": record.ArgumentsJSON,
		"content":   record.ContentJSON,
	} {
		if raw == "" {
			continue
		}
		if !json.Valid([]byte(raw)) {
			return "", "", fmt.Errorf("postgres: the %s is not valid JSON", name)
		}
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("postgres: ingesting evidence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", record.OrgID.String()); err != nil {
		return "", "", err
	}

	observed := record.ObservedAt
	if observed.IsZero() {
		// A caller that did not say when it was true gets the moment it was
		// fetched, and not year one. `observed_at` is NOT NULL, and a zero
		// time stored there reads as data corruption rather than as a missing
		// stamp.
		observed = time.Now().UTC()
	}
	requested := record.RequestedAt
	if requested.IsZero() {
		requested = observed
	}

	if record.Outcome == integrations.OutcomeSucceeded && record.ContentJSON != "" {
		err = tx.QueryRow(ctx, `
			insert into public.org_evidence
			       (org_id, source, connection_id, observed_at, kind, body,
			        content_hash)
			values ($1, 'integration', $2, $3, $4, $5::jsonb,
			        encode(sha256(convert_to($6, 'UTF8')), 'hex'))
			returning id::text`,
			record.OrgID, record.ConnectionID, observed,
			"integration."+record.Tool, record.ContentJSON,
			// THE SAME VALUE TWICE, UNDER TWO PLACEHOLDERS, WHICH IS NOT A
			// TYPO. Postgres infers a parameter's type from its first use, so
			// one placeholder written `$5::jsonb` here and `$5::bytea` there
			// asks it to cast jsonb to bytea, which it refuses outright. Found
			// by a test rather than by review, and worth the note because the
			// single-placeholder version reads better and does not work.
			record.ContentJSON).Scan(&evidenceID)
		if err != nil {
			return "", "", fmt.Errorf("postgres: recording the observation: %w", err)
		}
	}

	arguments := record.ArgumentsJSON
	if arguments == "" {
		arguments = "{}"
	}

	err = tx.QueryRow(ctx, `
		insert into public.integration_fetches
		       (org_id, integration_id, tool, arguments_json, requested_at,
		        finished_at, outcome, detail, evidence_id, redactions)
		values ($1, $2, $3, $4::jsonb, $5, now(), $6, nullif($7, ''),
		        nullif($8, '')::uuid, $9)
		returning id::text`,
		record.OrgID, record.ConnectionID, record.Tool, arguments, requested,
		record.Outcome, record.Detail, evidenceID, record.Redactions).Scan(&fetchID)
	if err != nil {
		return "", "", fmt.Errorf("postgres: recording the fetch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", "", fmt.Errorf("postgres: ingesting evidence: %w", err)
	}
	return evidenceID, fetchID, nil
}
