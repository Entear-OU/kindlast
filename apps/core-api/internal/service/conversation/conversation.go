// Package conversation serves ConversationService: asking the Analyst about
// one finding (ENT-270, §26.5).
//
// # THE RAIL PROMISED THIS AND THREE ICONS DID NOTHING
//
// ENT-222 ended the agent rail with "talking to them is coming" over Chat, Call
// and Walkthrough, none of which did anything. This is the first of the three,
// and it is one rather than all three because a conversation with an agent
// whose only honest answer is "I have not run" teaches nobody anything, and
// because call and walkthrough are a different problem that saying so is better
// than implying is a week away.
//
// # THE DIVISION OF LABOUR, WHICH IS WHAT KEEPS THIS FILE SHORT
//
// The scope interceptor has already refused a token without `agents:ask`. The
// tenancy interceptor has already opened a transaction with both GUCs set, so
// reading the finding through it is what makes a question about somebody else's
// finding a `not_found` rather than a check written here.
//
// What is left is one decision, and it is the important one: WHICH OBLIGATIONS
// THIS RUN IS OFFERED. The citation validator on the far side checks the
// model's citations against the offered set rather than against the corpus, so
// this handler offering exactly the finding's own obligation is the whole of
// the guardrail. Offering two would let an answer cite the wrong one; offering
// the corpus would make a fabrication indistinguishable from a good citation.
//
// # NOTHING HERE INSPECTS WHAT THE PERSON TYPED
//
// No sanitising, no scanning for "ignore previous instructions", no stripping.
// AGENTS.md is unambiguous that the model may ask and only code refuses, and a
// filter here would be neither: it would catch the one phrasing somebody wrote
// down, and it would leave the next reader believing untrusted input is handled
// in this file rather than in the ring around the model, which is where it
// actually is. The question travels verbatim, is fenced into a user message on
// the far side, and the answer is refused if it cites anything this run was not
// given or states the law.
//
// # AND NOTHING HERE IS STORED
//
// No transcript and no thread. The `agent_runs` row Intelligence writes is the
// only record, which is deliberate: a stored conversation is a second place a
// customer's words live and a second thing to answer for under retention, and
// neither is needed to explain one finding.
package conversation

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// reading is what this handler needs of the request's transaction, declared
// where it is used (§21.6).
//
// It reads and never writes, which is worth stating because the whole feature
// is a model answering a question: nothing about a conversation should be able
// to change a compliance record, and the narrowest way to guarantee that is for
// this handler to hold no method that could.
type reading interface {
	// OrgID is the organisation the tenancy interceptor resolved. Used to name
	// the tenant on the outbound call, never to filter a query: the query is
	// filtered by RLS, and a second predicate here would be a second place the
	// two could disagree.
	OrgID() string
	Finding(ctx context.Context, findingID string) (domain.Finding, []domain.SupportingChunk, error)
}

// Answerer is the Intelligence service, as much of it as this caller uses.
//
// An interface rather than the generated client, so the tests exercise refusal,
// failure and unavailability without a model, and so this package depends on no
// HTTP client.
type Answerer interface {
	AnswerFindingQuestion(
		ctx context.Context,
		req *connect.Request[platformv1.AnswerFindingQuestionRequest],
	) (*connect.Response[platformv1.AnswerFindingQuestionResponse], error)
}

// Router answers where an organisation's completions go (ENT-236). Asked here
// so that a question whose provider cannot be honoured is refused with a reason
// before anything is sent, rather than answered by the deployment's own model
// without saying so. The route's credential is never read: since ENT-256 part
// five every completion is made by CompletionService, and what travels is the
// provider and model names for the run record.
type Router interface {
	Resolve(ctx context.Context, orgID string) (modelroute.Route, error)
}

type Service struct {
	answerer Answerer
	router   Router
	logger   *slog.Logger
}

// Option configures the service.
type Option func(*Service)

// WithRouter makes this service name, for the run record, the model an
// organisation's runs go to. Nil is the same as absent: runs are recorded
// against the deployment's own model.
func WithRouter(router Router) Option {
	return func(s *Service) {
		if router != nil {
			s.router = router
		}
	}
}

// New builds the service. A nil answerer is Intelligence not being configured,
// which is a supported deployment rather than a misconfiguration.
func New(answerer Answerer, logger *slog.Logger, options ...Option) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	service := &Service{answerer: answerer, logger: logger}
	for _, apply := range options {
		apply(service)
	}
	return service
}

// AskAboutFinding puts one question to the Analyst about one finding.
func (s *Service) AskAboutFinding(
	ctx context.Context,
	req *connect.Request[corev1.AskAboutFindingRequest],
) (*connect.Response[corev1.AskAboutFindingResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	// BLANK IS REFUSED HERE AND LENGTH IS NOT, and the split is deliberate.
	//
	// There is nothing to ask, so there is no run worth spending and no record
	// worth writing: this is a caller that built the request wrongly, and
	// invalid_argument is the answer for that. A question that is merely too
	// long is a different thing. It is a judgement about what a run can afford,
	// it belongs beside the budget rather than here, and the person who typed it
	// is owed a recorded refusal they can read rather than a transport error.
	// The harness makes that call and records it.
	question := strings.TrimSpace(req.Msg.GetQuestion())
	if question == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("there is no question to ask"))
	}

	// READ BEFORE ANYTHING ELSE IS DECIDED, so a finding the caller cannot see
	// answers the same way whatever this deployment is running. Checking for a
	// model first would make "no model here" the answer for another
	// organisation's finding, which is a different answer for a caller probing
	// than the one they get for their own.
	finding, _, err := store.Finding(ctx, req.Msg.GetFindingId())
	if errors.Is(err, pgx.ErrNoRows) {
		// One answer for "no such finding" and "not yours", exactly as
		// GetFinding gives. Telling them apart would turn this into a way to
		// ask whether a given finding exists in someone else's organisation,
		// and it would do it while spending somebody else's model budget.
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such finding"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if finding.Citation.ObligationSlug == "" {
		// Nothing to cite means nothing citable can come back. A run offered an
		// empty set can only answer from what the model remembers, which is the
		// single thing a small model is worst at, so this refuses with a reason
		// rather than asking anyway.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this finding cites no obligation, so there is nothing the Analyst could answer from"))
	}

	if s.answerer == nil {
		// Not an error. Intelligence sits behind a compose profile, so a stack
		// can run without it, and saying so is more useful than a code the
		// caller has to interpret. A console can then say "this deployment has
		// no model" rather than "the Analyst would not answer", which are
		// different sentences with different things to do about them.
		return connect.NewResponse(&corev1.AskAboutFindingResponse{
			IntelligenceAvailable: false,
		}), nil
	}

	endpoint, err := s.modelEndpointFor(ctx, store.OrgID())
	if err != nil {
		// FailedPrecondition rather than Internal: nothing broke. This
		// organisation asked for a provider that cannot be honoured, and
		// refusing is the guardrail working. Falling back to the deployment's
		// own model would process this organisation's words somewhere other
		// than where its own record of processing says they are processed.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	answered, err := s.answerer.AnswerFindingQuestion(ctx, connect.NewRequest(
		&platformv1.AnswerFindingQuestionRequest{
			OrgId:    store.OrgID(),
			Question: question,
			Finding: &platformv1.FindingContext{
				Detected:       finding.Detected,
				ProposedAction: finding.ProposedAction,
				Severity:       finding.Severity,
				Narrative:      finding.Narrative,
			},
			// ONE OBLIGATION, WHICH IS THE STRONGEST FORM OF THE CHECK.
			//
			// The same decision NarrativeService makes, and the reason is
			// identical: the validator refuses any citation outside what was
			// offered, so offering exactly the obligation this finding is about
			// means an answer citing any other article is refused even when
			// that article genuinely exists.
			//
			// The summary is the authored statement of the law, read live from
			// the obligation row. It is offered because the model is forbidden
			// to state the law, so it has to be given the sentence rather than
			// asked to remember it.
			Obligations: []*platformv1.ObligationContext{{
				Slug:    finding.Citation.ObligationSlug,
				Title:   finding.Citation.Title,
				Summary: finding.Citation.Summary,
				// NO `applies_because` HERE, and it is an absence worth naming.
				// The grounds the sweep evaluated live in the obligation's
				// `applies_when`, which the tenant read does not carry: it is
				// shaped for the feed rather than for a run. A question is
				// answered without them today, which is a thinner context than
				// a narration gets, and closing that means widening the tenant
				// read rather than re-deriving them here. A second evaluator
				// disagreeing with the first is what produced ENT-246.
			}},
			// Nil for an organisation on the deployment's own model, which is
			// the default and the case where nothing leaves.
			ModelEndpoint: endpoint,
		}))
	if err != nil {
		// UNAVAILABLE, NOT A REFUSAL. Intelligence returns a refusal as a 200
		// with an outcome, so an error here is the transport or the process:
		// rendering it as "the Analyst would not answer" would tell a customer
		// their guardrails fired when nothing did.
		s.logger.Error("asking the Analyst about a finding failed",
			"finding", req.Msg.GetFindingId(), "org", store.OrgID(), "error", err.Error())
		return nil, connect.NewError(connect.CodeUnavailable,
			errors.New("the Analyst could not be reached just now"))
	}

	msg := answered.Msg
	return connect.NewResponse(&corev1.AskAboutFindingResponse{
		IntelligenceAvailable: true,
		Outcome:               outcomeFor(msg.GetOutcome()),
		// Carried as it came. Intelligence withholds a refused answer already,
		// so this is empty on a refusal without this handler deciding anything.
		Answer:        msg.GetAnswer(),
		OutcomeDetail: msg.GetOutcomeDetail(),
		Run:           runFor(msg),
	}), nil
}

// modelEndpointFor names, for the run record, the model this organisation's
// runs go to, and refuses when that model cannot be honoured.
//
// Names only. `base_url` and `api_key` on ModelEndpoint are deprecated and
// never set: the Python service refuses a request carrying either.
func (s *Service) modelEndpointFor(ctx context.Context, orgID string) (*platformv1.ModelEndpoint, error) {
	if s.router == nil {
		return nil, nil
	}
	route, err := s.router.Resolve(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &platformv1.ModelEndpoint{Provider: route.Provider, Model: route.Model}, nil
}

// runFor is the `agent_runs` row behind an answer, as a customer reads it.
//
// Assembled from what the run reported rather than read back, because
// `agent_runs` has a write path and no read path. That is a limitation being
// worked around: when there is a read path this should become the id and a
// second call, since a summary written by the thing it describes is weaker
// evidence than the record.
func runFor(msg *platformv1.AnswerFindingQuestionResponse) *corev1.AgentRunSummary {
	provenance := msg.GetProvenance()
	return &corev1.AgentRunSummary{
		AgentRunId:        msg.GetAgentRunId(),
		Skill:             provenance.GetSkill(),
		SkillVersion:      provenance.GetSkillVersion(),
		Model:             provenance.GetModel(),
		ModelVersion:      provenance.GetModelVersion(),
		Provider:          provenance.GetProvider(),
		ResolvedCitations: msg.GetResolvedCitations(),
	}
}

// outcomeFor maps the platform surface's outcome onto the customer-facing one.
//
// A switch rather than a shared enum, so a value added on one side cannot
// default silently on the other. UNSPECIFIED becomes FAILED rather than
// UNSPECIFIED: a response whose outcome nobody set is a response that did not
// come from a run this code understands, and reporting that as "we have no idea"
// on a page a person is reading is worse than reporting that it did not work.
func outcomeFor(outcome platformv1.AnswerOutcome) corev1.AnswerOutcome {
	switch outcome {
	case platformv1.AnswerOutcome_ANSWER_OUTCOME_SUCCEEDED:
		return corev1.AnswerOutcome_ANSWER_OUTCOME_SUCCEEDED
	case platformv1.AnswerOutcome_ANSWER_OUTCOME_REFUSED:
		return corev1.AnswerOutcome_ANSWER_OUTCOME_REFUSED
	default:
		return corev1.AnswerOutcome_ANSWER_OUTCOME_FAILED
	}
}

// tenantAs pulls the request's transaction and narrows it to what this handler
// needs. The same helper `findings` has, written out rather than exported from
// there: a shared one would have to live somewhere both packages import, and
// the interface it narrows to is the part that must stay per handler.
func tenantAs[T any](ctx context.Context) (T, error) {
	var zero T

	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return zero, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return zero, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}

	store, ok := tenant.(T)
	if !ok {
		return zero, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot serve this call"))
	}
	return store, nil
}
