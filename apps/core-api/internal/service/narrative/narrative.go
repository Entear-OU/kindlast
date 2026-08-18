// Package narrative serves NarrativeService: findings, explained (ENT-245).
//
// # THE HALF ENT-218 ASSUMED AND NOBODY SCHEDULED
//
// ENT-218 shipped the harness, the guardrail ring, the citation validator and
// the `agent_runs` record, and nothing in the product called any of it. This is
// the call.
//
// # A JOB, NOT PART OF THE SWEEP
//
// A model call inside the sweep's transaction would hold it open for minutes
// per finding on a local model, block the act path behind its locks, and lose
// everything on a timeout. The sweep stays fast and deterministic; this runs
// afterwards over findings that have no narrative. Latency and refusals become
// a queue's problem rather than a request's.
//
// # OPTIONAL, AND THAT IS A SUPPORTED DEPLOYMENT
//
// Intelligence sits behind a compose profile, so a stack can run without it.
// When it is absent this service answers with `intelligence_available: false`
// and narrates nothing, rather than failing. Every finding still carries the
// deterministic text the feed renders today, because this adds a column and
// overwrites none.
package narrative

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Findings is what this service needs of the agent pool, declared where it is
// used (§21.6).
type Findings interface {
	FindingsAwaitingNarrative(ctx context.Context, orgID string, limit int32) ([]postgres.PendingFinding, error)
	RecordNarrative(ctx context.Context, orgID string, findingID uuid.UUID, narrative, agentRunID string) error
	RecordNarrativeRefusal(ctx context.Context, orgID string, findingID uuid.UUID, reason, agentRunID string) error
}

// Drafter is the Intelligence service, as much of it as this caller uses.
//
// An interface rather than the generated client, so the tests below exercise
// refusal, failure and unavailability without a model, and so this package
// depends on no HTTP client.
type Drafter interface {
	DraftNarrative(
		ctx context.Context,
		req *connect.Request[platformv1.DraftNarrativeRequest],
	) (*connect.Response[platformv1.DraftNarrativeResponse], error)
}

type Service struct {
	findings Findings
	drafter  Drafter
	logger   *slog.Logger
}

// New builds the service. A nil drafter is Intelligence not being configured,
// which is supported.
func New(findings Findings, drafter Drafter, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{findings: findings, drafter: drafter, logger: logger}
}

func (s *Service) NarrateFindings(
	ctx context.Context,
	req *connect.Request[platformv1.NarrateFindingsRequest],
) (*connect.Response[platformv1.NarrateFindingsResponse], error) {
	orgID := req.Msg.GetOrgId()
	if orgID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("org_id is required"))
	}

	if s.drafter == nil {
		// Not an error. A deployment without the model profile is a supported
		// one, and saying so is more useful than a code the caller has to
		// interpret.
		return connect.NewResponse(&platformv1.NarrateFindingsResponse{
			IntelligenceAvailable: false,
		}), nil
	}

	pending, err := s.findings.FindingsAwaitingNarrative(ctx, orgID, req.Msg.GetMaxFindings())
	if err != nil {
		if errors.Is(err, postgres.ErrBadOrganisation) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &platformv1.NarrateFindingsResponse{
		IntelligenceAvailable: true,
		Attempted:             int32(len(pending)),
	}

	for _, finding := range pending {
		// SEQUENTIAL, NOT CONCURRENT. One `llama-server` serves one request at
		// a time, so firing these in parallel moves the queue rather than
		// shortening it, and turns one slow finding into a pile of timeouts.
		outcome, err := s.narrate(ctx, orgID, finding)
		if err != nil {
			// One finding failing does not stop the pass. The next one may be
			// fine, and a job that abandoned the batch would make a single
			// unlucky finding block every other explanation in the
			// organisation.
			s.logger.Error("narrating a finding failed",
				"finding", finding.ID, "org", orgID, "error", err.Error())
			response.Failed++
			continue
		}
		switch outcome {
		case platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED:
			response.Narrated++
		default:
			response.Refused++
		}
	}

	return connect.NewResponse(response), nil
}

func (s *Service) narrate(
	ctx context.Context,
	orgID string,
	finding postgres.PendingFinding,
) (platformv1.DraftOutcome, error) {
	// THE SIGNAL IS THE SWEEP'S OWN WORDS, AND IT IS DATA RATHER THAN
	// INSTRUCTION.
	//
	// It reaches the model inside a fenced user message on the far side, never
	// in a system prompt. AGENTS.md is unambiguous that anything a customer
	// typed is data, and a finding's text is partly derived from a compliance
	// profile a customer filled in.
	signal := fmt.Sprintf("%s\n\nProposed action: %s\nSeverity: %s",
		finding.Detected, finding.ProposedAction, finding.Severity)

	response, err := s.drafter.DraftNarrative(ctx, connect.NewRequest(
		&platformv1.DraftNarrativeRequest{
			OrgId:  orgID,
			Signal: signal,
			// ONE OBLIGATION, WHICH IS THE STRONGEST FORM OF THE CHECK.
			//
			// The validator refuses any citation outside what was offered, so
			// offering exactly the obligation this finding is about means a
			// narrative citing any other article is refused even when that
			// article genuinely exists. Offering the whole corpus would make a
			// fabrication indistinguishable from a good citation.
			Obligations: []*platformv1.ObligationContext{{
				Slug:    finding.ObligationSlug,
				Title:   finding.ObligationTitle,
				Summary: finding.ObligationSummary,
			}},
		}))
	if err != nil {
		return platformv1.DraftOutcome_DRAFT_OUTCOME_UNSPECIFIED, err
	}

	msg := response.Msg
	if msg.GetOutcome() == platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED {
		if err := s.findings.RecordNarrative(
			ctx, orgID, finding.ID, msg.GetNarrative(), msg.GetAgentRunId(),
		); err != nil {
			return platformv1.DraftOutcome_DRAFT_OUTCOME_UNSPECIFIED, err
		}
		return msg.GetOutcome(), nil
	}

	// Refused or failed. Recorded rather than retried on the next pass: a
	// finding the model cannot narrate correctly would otherwise be picked up
	// forever and burn the whole budget in a loop.
	reason := msg.GetOutcomeDetail()
	if reason == "" {
		reason = "the run produced no narrative and gave no reason"
	}
	if err := s.findings.RecordNarrativeRefusal(
		ctx, orgID, finding.ID, reason, msg.GetAgentRunId(),
	); err != nil {
		return platformv1.DraftOutcome_DRAFT_OUTCOME_UNSPECIFIED, err
	}
	return msg.GetOutcome(), nil
}
