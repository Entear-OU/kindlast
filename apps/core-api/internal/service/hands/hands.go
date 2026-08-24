// Package hands serves HandsService: what approving a finding will do, and
// the record it prepares (ENT-261, §26.5, §3).
//
// # THE NAME OF THE AGENT IS THE SPECIFICATION
//
// It explains, it prepares, it never decides. This package is where the last
// clause stops being a sentence and becomes a property of the code, so it is
// worth writing down exactly what makes it true, because "the prompt says not
// to" would not.
//
// Approving is `FindingsService.ApproveFinding`, on `findings:act`, which only
// a human's token carries, and which reads the approver from the session GUC
// so that not even a caller who reached it could name somebody else. Nothing
// in this package calls it and nothing in this package can.
//
// Creating a compliance record is `ExecutorService.ExecuteJob`, which acts on
// an `executor_jobs` row. Those rows are inserted in exactly one place: inside
// the transaction that writes an approval (00036). Nothing here inserts one.
//
// What this package writes is `findings.metadata`: the plan a person reads
// before deciding, and the payload the Executor would use if they decide yes.
// A proposal on a finding is not an entry in a register. It changes nothing a
// regulator would read, it sits beside the decision it informs, and it is
// refused the instant an approval has been enqueued, so a run arriving late
// cannot rewrite what somebody approved.
//
// # THE HALF THAT IS NOT A GUARDRAIL, AND IS THE PRODUCT
//
// Today an approved ROPA finding produces a row that says "Not recorded" in
// every column and is marked "Needs review". That is correct and useless. The
// Hands fills what the organisation's own memory can support and says, field
// by field, what it filled and from which fact, and what it left for a person
// and why. A record that reads as complete and is not would be worse than the
// empty one, which is why `left_for_you` is carried everywhere `prepared` is
// and why a value with no fact behind it is refused rather than trimmed.
package hands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Approvals is what this service needs of the agent pool, declared where it is
// used (§21.6).
type Approvals interface {
	ApprovalContextFor(ctx context.Context, orgID, findingID string) (postgres.ApprovalContext, error)
	PrepareRecord(ctx context.Context, orgID, findingID string, plan postgres.Plan) (postgres.Plan, error)
}

// Explainer is the Intelligence service, as much of it as this caller uses.
//
// An interface rather than the generated client, for the reason
// `narrative.Drafter` is one: the tests below exercise success, refusal,
// failure and unavailability without a model, and this package then depends on
// no HTTP client.
type Explainer interface {
	ExplainApproval(
		ctx context.Context,
		req *connect.Request[platformv1.ExplainApprovalRequest],
	) (*connect.Response[platformv1.ExplainApprovalResponse], error)
}

// ModelRoute resolves which model serves one organisation, declared here for
// the same reason (§21.6). Nil for a deployment that runs no Intelligence.
//
// Names only, which is the whole of what this service does with it: where a
// completion goes and what authenticates it stay core-api's, and Intelligence
// asks CompletionService for every call (ENT-256, part five).
type ModelRoute interface {
	Resolve(ctx context.Context, orgID string) (modelroute.Route, error)
}

type Service struct {
	approvals Approvals
	explainer Explainer
	models    ModelRoute
	logger    *slog.Logger
}

// Option configures the service. A deployment with no Intelligence and no
// hosted providers is the ordinary one and should not have to pass nils to
// say so.
type Option func(*Service)

// WithModelRoute makes this service record runs against an organisation's
// chosen model by name.
func WithModelRoute(models ModelRoute) Option {
	return func(s *Service) {
		if models == nil {
			return
		}
		s.models = models
	}
}

// New builds the service. A nil explainer is Intelligence not being
// configured, which is supported: the deployment answers
// `failed_precondition` rather than failing to start.
func New(approvals Approvals, explainer Explainer, logger *slog.Logger, options ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{approvals: approvals, explainer: explainer, logger: logger}
	for _, apply := range options {
		apply(service)
	}
	return service
}

// ExplainApproval runs the Hands over one finding.
func (s *Service) ExplainApproval(
	ctx context.Context,
	req *connect.Request[platformv1.HandsExplainRequest],
) (*connect.Response[platformv1.HandsExplainResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	orgID := req.Msg.GetOrgId()
	findingID := req.Msg.GetFindingId()
	if orgID == "" || findingID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("org_id and finding_id are both required"))
	}

	if s.explainer == nil {
		// A deployment without the model profile is a supported one. Refused
		// rather than answered with an empty explanation, because an empty
		// explanation reads as "there is nothing to say about approving this",
		// which is a claim about the finding rather than about the deployment.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this deployment runs no Intelligence, so nothing can explain an approval"))
	}

	context, err := s.approvals.ApprovalContextFor(ctx, orgID, findingID)
	switch {
	case errors.Is(err, postgres.ErrBadOrganisation), errors.Is(err, postgres.ErrNoSuchFinding):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, postgres.ErrNothingToPrepare):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	endpoint, err := s.modelEndpointFor(ctx, orgID)
	if err != nil {
		return nil, err
	}

	response, err := s.explainer.ExplainApproval(ctx, connect.NewRequest(
		&platformv1.ExplainApprovalRequest{
			OrgId:         orgID,
			Context:       approvalContextPB(context),
			ModelEndpoint: endpoint,
		}))
	if err != nil {
		// Not translated into an outcome. A transport failure is not a run
		// that was refused: there may be no run at all, and reporting one as
		// REFUSED would put a guardrail's name on a network problem in the
		// column a customer reads to decide whether to trust the result.
		return nil, connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("asking Intelligence to explain this approval: %w", err))
	}

	msg := response.Msg
	return connect.NewResponse(&platformv1.HandsExplainResponse{
		Outcome:       outcomeOf(msg.GetOutcome()),
		OutcomeDetail: msg.GetOutcomeDetail(),
		Explanation:   msg.GetExplanation(),
		Prepared:      msg.GetPrepared(),
		LeftForYou:    msg.GetLeftForYou(),
		AgentRunId:    msg.GetAgentRunId(),
	}), nil
}

// PrepareRecord records what the Hands prepared: the Hands' one tool.
//
// # WHAT IS CHECKED HERE, AND WHY THE HARNESS CHECKS IT TOO
//
// Both sides check the field names and the fact each value claims to come
// from, and both are wanted, for the reason `harness/watch.py` gives about
// citations. This side is the INVARIANT: no finding's payload carries a field
// the register does not have, or a value attributed to a fact this
// organisation never recorded, whoever wrote it. The harness side is the
// GUARDRAIL: this run cited nothing it was not shown. They refuse different
// things, and a fact that exists but was never offered to the run is refused
// only by the second.
func (s *Service) PrepareRecord(
	ctx context.Context,
	req *connect.Request[platformv1.PrepareRecordRequest],
) (*connect.Response[platformv1.PrepareRecordResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	orgID := req.Msg.GetOrgId()
	findingID := req.Msg.GetFindingId()
	if orgID == "" || findingID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("org_id and finding_id are both required"))
	}

	// RE-READ RATHER THAN TRUSTED FROM THE REQUEST. The caller could name a
	// register, and a caller that names its own register is a caller that
	// chooses which fields it is allowed to write. The register comes from the
	// finding's action type, here, every time.
	context, err := s.approvals.ApprovalContextFor(ctx, orgID, findingID)
	switch {
	case errors.Is(err, postgres.ErrBadOrganisation), errors.Is(err, postgres.ErrNoSuchFinding):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, postgres.ErrNothingToPrepare):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	prepared := preparedFrom(req.Msg.GetFields())
	known := make(map[string]bool, len(context.Facts))
	for _, fact := range context.Facts {
		known[fact.Key] = true
	}
	if err := records.ValidatePrepared(context.Register, prepared, known); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	left := make([]records.LeftForYou, 0, len(req.Msg.GetLeftForYou()))
	for _, l := range req.Msg.GetLeftForYou() {
		left = append(left, records.LeftForYou{Name: l.GetName(), Why: l.GetWhy()})
	}

	plan, err := s.approvals.PrepareRecord(ctx, orgID, findingID, postgres.Plan{
		Register:    context.Register,
		Explanation: req.Msg.GetExplanation(),
		Fields:      prepared,
		LeftForYou:  left,
	})
	switch {
	case errors.Is(err, postgres.ErrAlreadyEnqueued):
		// FailedPrecondition rather than Internal: nothing broke. A person
		// approved while this run was thinking, and the guardrail held.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, postgres.ErrNoSuchFinding):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.PrepareRecordResponse{
		Filled: int32(len(plan.Fields)),
		Left:   int32(len(plan.LeftForYou)),
	}), nil
}

// modelEndpointFor names the model this run is recorded against.
//
// # AN UNRESOLVABLE ROUTE REFUSES, IT DOES NOT FALL BACK
//
// The same stance `watcher.route` takes and for the same reason: an
// organisation that chose a provider which cannot be honoured must not have
// its compliance context processed on a model it did not choose, with the run
// record naming a provider that did not serve it. `failed_precondition`,
// because nothing broke and somebody has to fix it.
//
// Nil with a nil error means the deployment's own model, which is the default.
func (s *Service) modelEndpointFor(
	ctx context.Context, orgID string,
) (*platformv1.ModelEndpoint, error) {
	if s.models == nil {
		return nil, nil
	}
	route, err := s.models.Resolve(ctx, orgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return &platformv1.ModelEndpoint{Provider: route.Provider, Model: route.Model}, nil
}

func preparedFrom(fields []*platformv1.PreparedField) []records.PreparedField {
	out := make([]records.PreparedField, 0, len(fields))
	for _, f := range fields {
		out = append(out, records.PreparedField{
			Name:     f.GetName(),
			Values:   f.GetValues(),
			FromFact: f.GetFromFact(),
		})
	}
	return out
}

func approvalContextPB(context postgres.ApprovalContext) *platformv1.ApprovalContext {
	out := &platformv1.ApprovalContext{
		Finding: &platformv1.ApprovalFinding{
			FindingId:         context.Finding.ID,
			Status:            context.Finding.Status,
			Severity:          context.Finding.Severity,
			Detected:          context.Finding.Detected,
			ProposedAction:    context.Finding.ProposedAction,
			ActionType:        context.Finding.ActionType,
			ObligationSlug:    context.Finding.ObligationSlug,
			ObligationTitle:   context.Finding.ObligationTitle,
			ObligationSummary: context.Finding.ObligationSummary,
			CitationLabel:     context.Finding.CitationLabel,
		},
		Register:      context.Register.Name,
		RegisterLabel: context.Register.Label,
	}
	for _, f := range context.Register.Fields {
		out.Fields = append(out.Fields, &platformv1.RegisterField{
			Name:        f.Name,
			Label:       f.Label,
			Required:    f.Required,
			ListValued:  f.ListValued,
			Description: f.Description,
		})
	}
	for _, f := range context.Facts {
		fact := &platformv1.ProfileFact{
			Key:       f.Key,
			ValueJson: f.ValueJSON,
			Source:    f.Source,
		}
		if !f.ValidFrom.IsZero() {
			fact.ValidFrom = timestamppb.New(f.ValidFrom)
		}
		out.Facts = append(out.Facts, fact)
	}
	for _, p := range context.AlreadyProposed {
		out.AlreadyProposed = append(out.AlreadyProposed, &platformv1.PreparedField{
			Name:     p.Name,
			Values:   p.Values,
			FromFact: p.FromFact,
		})
	}
	return out
}

func outcomeOf(outcome platformv1.ExplainOutcome) platformv1.HandsOutcome {
	// Mapped explicitly rather than cast. They are two enums with the same
	// three members today, and a cast would keep compiling on the day one of
	// them gains a fourth.
	switch outcome {
	case platformv1.ExplainOutcome_EXPLAIN_OUTCOME_SUCCEEDED:
		return platformv1.HandsOutcome_HANDS_OUTCOME_SUCCEEDED
	case platformv1.ExplainOutcome_EXPLAIN_OUTCOME_REFUSED:
		return platformv1.HandsOutcome_HANDS_OUTCOME_REFUSED
	case platformv1.ExplainOutcome_EXPLAIN_OUTCOME_FAILED:
		return platformv1.HandsOutcome_HANDS_OUTCOME_FAILED
	default:
		return platformv1.HandsOutcome_HANDS_OUTCOME_UNSPECIFIED
	}
}
