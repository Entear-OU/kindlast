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

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
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

// ModelChoices resolves which endpoint an organisation's runs go to (ENT-236).
//
// An interface for the same reason Findings is one, and nil is a supported
// deployment: a stack that permits no hosted provider narrates on whatever
// endpoint Intelligence was configured with, which is every stack by default.
type ModelChoices interface {
	ActiveModelChoiceForOrg(ctx context.Context, orgID string) (postgres.Choice, postgres.Sealed, error)
}

type Service struct {
	findings Findings
	drafter  Drafter
	logger   *slog.Logger

	choices   ModelChoices
	keys      *secrets.Keyring
	providers []modelchoice.Provider
	lookup    modelchoice.Lookup
}

// Option configures the service.
//
// Options rather than more parameters, because a deployment with no agent pool
// and no hosted providers is the ordinary one and it should not have to pass
// four nils to say so.
type Option func(*Service)

// WithModelChoice makes this service honour an organisation's chosen provider.
//
// A nil store or a nil keyring is treated as absent, because honouring a choice
// means reading it and opening a sealed key, and doing either without the other
// is not a degraded version of this feature but a broken one.
//
// AN EMPTY PROVIDER LIST IS NOT ABSENT, AND THAT IS THE SUBTLE ONE. The obvious
// reading is that a deployment permitting nothing has no work for this to do,
// so it should not be wired. That is wrong in the one direction that matters:
// an operator can withdraw the last provider while an organisation still has a
// row, and an unwired resolver would then narrate that organisation on the
// deployment's own model, silently, with nothing saying its choice had stopped
// being honoured. Wired with an empty list, the same case fails loudly, because
// `Permitted` refuses every name.
func WithModelChoice(
	choices ModelChoices,
	keys *secrets.Keyring,
	providers []modelchoice.Provider,
	lookup modelchoice.Lookup,
) Option {
	return func(s *Service) {
		if choices == nil || keys == nil {
			return
		}
		if lookup == nil {
			lookup = modelchoice.SystemLookup
		}
		s.choices, s.keys, s.providers, s.lookup = choices, keys, providers, lookup
	}
}

// New builds the service. A nil drafter is Intelligence not being configured,
// which is supported.
func New(findings Findings, drafter Drafter, logger *slog.Logger, options ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{findings: findings, drafter: drafter, logger: logger}
	for _, apply := range options {
		apply(service)
	}
	return service
}

// modelEndpointFor resolves where this organisation's runs go.
//
// # A REFUSAL HERE FAILS THE JOB RATHER THAN FALLING BACK
//
// Every error path below returns an error and none returns nil with a nil
// error. Falling back to the deployment's own model when an organisation's
// chosen provider cannot be honoured would mean processing that organisation's
// findings somewhere other than where its own record of processing says they
// are processed, quietly, with nothing in the product saying it happened. A job
// that stops and says why is recoverable; one that silently processes elsewhere
// is a disclosure nobody can date.
//
// Nil with a nil error means exactly one thing: this organisation has made no
// choice and uses the model this deployment runs.
func (s *Service) modelEndpointFor(ctx context.Context, orgID string) (*platformv1.ModelEndpoint, error) {
	if s.choices == nil {
		return nil, nil
	}

	choice, sealed, err := s.choices.ActiveModelChoiceForOrg(ctx, orgID)
	if errors.Is(err, postgres.ErrNoModelChoice) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// RE-CHECKED, NOT TRUSTED BECAUSE IT WAS CHECKED WHEN IT WAS WRITTEN. The
	// allow-list is deployment configuration, so a provider an operator has
	// withdrawn has to stop being reachable for organisations that already
	// chose it, and an endpoint that has since started resolving inside the
	// deployment has to stop being dialled. 00025 makes the same argument about
	// a connection endpoint.
	provider, err := modelchoice.Permitted(s.providers, choice.Provider)
	if err != nil {
		return nil, fmt.Errorf("this organisation model provider is no longer permitted here: %w", err)
	}
	if err := modelchoice.ValidateEndpoint(ctx, choice.BaseURL, provider, s.lookup); err != nil {
		return nil, fmt.Errorf("this organisation model endpoint can no longer be reached safely: %w", err)
	}

	endpoint := &platformv1.ModelEndpoint{
		Provider: choice.Provider,
		BaseUrl:  choice.BaseURL,
		Model:    choice.Model,
	}
	if len(sealed.Ciphertext) > 0 {
		key, err := s.keys.Open(sealed.Ciphertext, sealed.KeyID, choice.ID)
		if err != nil {
			// A key this deployment can no longer open. Refused rather than
			// dialled without one, because an unauthenticated request to a
			// hosted provider fails at the far end and takes the customer
			// signal with it on the way.
			return nil, fmt.Errorf("this organisation provider key cannot be opened: %w", err)
		}
		endpoint.ApiKey = key
	}
	return endpoint, nil
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

	// Resolved ONCE for the batch rather than per finding, so a provider
	// withdrawn mid-job cannot narrate half the findings one way and half the
	// other, and so the sealed key is opened once instead of once per run.
	endpoint, err := s.modelEndpointFor(ctx, orgID)
	if err != nil {
		// FailedPrecondition rather than Internal: nothing broke. This
		// organisation asked for a provider that cannot be honoured, and the
		// job refusing is the guardrail working.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
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
		outcome, err := s.narrate(ctx, orgID, finding, endpoint)
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
	endpoint *platformv1.ModelEndpoint,
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
				// WHY THE OBLIGATION APPLIES, RATHER THAN LEAVING THE MODEL TO
				// WORK IT OUT (ENT-248).
				//
				// Both narratives ENT-248 was filed for invented their own
				// grounds, because nothing had given them any: one asserted the
				// obligation binds every controller regardless of size, the
				// other reasoned from a missing record to a headcount
				// exemption. A model with no grounds reaches for what it
				// remembers about the regulation, which is the single thing a
				// 2B is worst at.
				//
				// Rendered from the obligation's own conditions rather than
				// re-evaluated here. The sweep already decided they hold, and
				// a second evaluator disagreeing with the first is what
				// produced ENT-246.
				AppliesBecause: corpus.AppliesBecause(finding.ObligationAppliesWhen),
			}},
			// Nil for an organisation on the deployment own model, which is the
			// default and the case where nothing leaves.
			ModelEndpoint: endpoint,
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
