package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AgentRun is one finished run, as it arrives from Intelligence (ENT-218).
//
// Every JSON field is carried as a string rather than a decoded structure,
// deliberately. What a customer reads back should be what the run produced,
// and round-tripping through a map reorders keys and rewrites numbers as
// floats. The same reasoning `audit_log`'s before and after payloads follow.
type AgentRun struct {
	OrgID            uuid.UUID
	Skill            string
	SkillVersion     string
	Model            string
	ModelVersion     string
	OnBehalfOfUserID *uuid.UUID
	RequestJSON      string
	ToolCallsJSON    string
	CitationsJSON    string
	Outcome          string
	OutcomeDetail    string
	// What an output critic refused, as JSON, and empty when none did
	// (ENT-248). See 00027 for why the rejected text is here rather than in
	// OutcomeDetail, which a customer reads.
	RefusalJSON       string
	InputTokens       int32
	CachedInputTokens int32
	OutputTokens      int32
	CostMicros        int64
	QueuedAt          time.Time
	StartedAt         time.Time
	FinishedAt        time.Time
}

// RecordAgentRun writes one run and returns its id.
//
// # NO GUCs, AND THAT IS THE SAME EXCEPTION RunSweep TAKES
//
// The agent runs for organisations nobody is signed in to, so there is no
// member to name and no `app.current_user_id` to set. Its policy on
// `agent_runs` is unconditional for exactly that reason (00019), and what
// keeps the exception honest is that the role can reach almost nothing else
// and that every row it writes names the organisation it was for.
//
// # THE ORG COMES FROM THE MESSAGE, WHICH IS WORTH BEING NERVOUS ABOUT
//
// Everywhere else in this service the organisation comes from a header and is
// checked against membership. Here the caller supplies it, because a run
// happens for whichever tenant the work belonged to and Intelligence has no
// session to derive it from.
//
// That is safe only because of who the caller is: `internal:intelligence` is
// issued to one service principal through client credentials and never to a
// browser client, so "the caller could name any organisation" describes a
// component we ship rather than an input a person controls. If that scope ever
// reaches something a customer can drive, this line becomes a tenancy hole and
// the org must come from somewhere the caller cannot choose.
func (a *AgentStore) RecordAgentRun(ctx context.Context, run AgentRun) (uuid.UUID, error) {
	// Validated here rather than trusted, because a malformed payload stored
	// now is a page that cannot render later, and the failure would surface to
	// a customer reading "how this was produced" rather than to whoever sent
	// it.
	for name, raw := range map[string]string{
		"request":    run.RequestJSON,
		"tool_calls": run.ToolCallsJSON,
		"citations":  run.CitationsJSON,
		"refusal":    run.RefusalJSON,
	} {
		if raw == "" {
			continue
		}
		if !json.Valid([]byte(raw)) {
			return uuid.Nil, fmt.Errorf("postgres: %s is not valid JSON", name)
		}
	}

	const query = `
		insert into agent_runs (
			org_id, skill, skill_version, model, model_version,
			on_behalf_of_user_id, request, tool_calls, citations,
			outcome, outcome_detail, refusal,
			input_tokens, cached_input_tokens, output_tokens, cost_micros,
			queued_at, started_at, finished_at
		) values (
			$1, $2, $3, $4, $5,
			$6, coalesce($7, '{}')::jsonb, coalesce($8, '[]')::jsonb,
			coalesce($9, '{"resolved": [], "rejected": []}')::jsonb,
			$10, nullif($11, ''), coalesce($12, '{}')::jsonb,
			$13, $14, $15, $16,
			$17, $18, $19
		)
		returning id`

	var id uuid.UUID
	err := a.pool.QueryRow(ctx, query,
		run.OrgID, run.Skill, run.SkillVersion, run.Model, run.ModelVersion,
		run.OnBehalfOfUserID,
		nullifEmpty(run.RequestJSON), nullifEmpty(run.ToolCallsJSON), nullifEmpty(run.CitationsJSON),
		run.Outcome, run.OutcomeDetail, nullifEmpty(run.RefusalJSON),
		run.InputTokens, run.CachedInputTokens, run.OutputTokens, run.CostMicros,
		run.QueuedAt, run.StartedAt, run.FinishedAt,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("postgres: recording the agent run: %w", err)
	}
	return id, nil
}

// nullifEmpty lets the column defaults in 00019 apply.
//
// Passing "" would store an empty string where a jsonb default is wanted, and
// `coalesce` in the statement above only fires on NULL.
func nullifEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
