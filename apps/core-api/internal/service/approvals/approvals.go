// Package approvals serves ApprovalService: asking the Hands what approving
// one finding will do (ENT-278, §26.5).
//
// # THE SKILL SHIPPED WITHOUT A SURFACE
//
// ENT-261 built the whole of `hands.prepare` and put every entry point to it on
// `internal:ingest`, which a browser never holds. So it ran, under a budget and
// an allow-list, and left `agent_runs` rows nobody could cause and nobody could
// see. This package is the door: the same run, asked for by the person who is
// about to decide, from the page where they decide.
//
// # WHY THIS IS THIN, AND WHERE THE WORK ACTUALLY HAPPENS
//
// It delegates to `service/hands`, in process, rather than assembling an
// approval context of its own and calling Intelligence directly. That is the
// important design decision in the file and the reason is the one AGENTS.md
// gives about two evaluators: the context a Hands run sees decides what it may
// fill and from which facts, and a second assembler would be a second thing
// that can disagree with the first about what a register has. One run shape,
// one validator, one place a field can be refused, whoever asked for the run.
//
// What this package adds is the three things that change when a browser is the
// caller:
//
//   - THE ORGANISATION IS NOT THE CALLER'S TO NAME. The platform request
//     carries an `org_id` and is believed because `internal:ingest` is issued
//     through client credentials. This request carries none: it comes from the
//     tenancy interceptor, which resolved the header against the caller's own
//     memberships, and it is put into the delegated request here.
//   - THE FINDING IS READ THROUGH THE CALLER'S OWN TRANSACTION FIRST, so RLS
//     answers "not yours" as "not there", before a model is asked anything.
//     Reading first rather than last is what stops this being a way to spend
//     another organisation's model budget by guessing ids.
//   - THE COLUMNS ARE NAMED IN WORDS THIS PRODUCT WROTE. `domain/records` holds
//     the register's label and each column's, so the sentence a customer reads
//     about what approving does is authored rather than generated.
package approvals

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// reading is what this handler needs of the request's transaction, declared
// where it is used (§21.6).
//
// It reads and never writes. The run writes a proposal onto the finding, and it
// does that on the agent pool through the path that already validates it, so
// there is nothing this handler should be able to change about a compliance
// record and nothing in this interface that could.
type reading interface {
	// OrgID is the organisation the tenancy interceptor resolved. Used to name
	// the tenant on the delegated call, never to filter a query: the query is
	// filtered by RLS, and a second predicate here would be a second place the
	// two could disagree.
	OrgID() string
	Finding(ctx context.Context, findingID string) (domain.Finding, []domain.SupportingChunk, error)
}

// Hands is the platform service, as much of it as this caller uses.
//
// An interface rather than the concrete service, so these tests exercise
// success, refusal, failure and a deployment with no model without a database,
// a model or an HTTP client. In production it is the same `*hands.Service` the
// platform surface is registered with: one run path, reached two ways.
type Hands interface {
	ExplainApproval(
		ctx context.Context,
		req *connect.Request[platformv1.HandsExplainRequest],
	) (*connect.Response[platformv1.HandsExplainResponse], error)
}

type Service struct {
	hands  Hands
	logger *slog.Logger
}

// New builds the service. A nil Hands is a deployment that runs no agents or no
// Intelligence, which is supported: the handler answers
// `intelligence_available: false` rather than 404, so "this stack has no model"
// and "wrong URL" stay different answers.
func New(hands Hands, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{hands: hands, logger: logger}
}

// ExplainApproval runs the Hands over one finding, for the person about to
// decide it.
func (s *Service) ExplainApproval(
	ctx context.Context,
	req *connect.Request[corev1.ExplainApprovalRequest],
) (*connect.Response[corev1.ExplainApprovalResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	// READ BEFORE ANYTHING ELSE IS DECIDED, so a finding the caller cannot see
	// answers the same way whatever this deployment is running. Checking for a
	// model first would make "no model here" the answer for another
	// organisation's finding, which is a different answer for somebody probing
	// than the one they get for their own.
	finding, _, err := store.Finding(ctx, req.Msg.GetFindingId())
	if errors.Is(err, pgx.ErrNoRows) {
		// One answer for "no such finding" and "not yours", exactly as
		// GetFinding gives.
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such finding"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// The register is resolved here as well as inside the run, and the
	// duplication is deliberate: this one decides whether to ask at all, and it
	// is the same pure function over the same action type, so the two cannot
	// disagree about what a register has.
	register, creates := records.RegisterFor(finding.ActionType)
	if !creates {
		// Approving a `review` finding records the decision and creates
		// nothing. Refused with a reason rather than answered with an empty
		// plan, because a run would have produced an explanation of a record
		// that will not exist, which is worse than no explanation at all.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("approving this finding records your decision and creates no record, so there is nothing to prepare"))
	}

	if s.hands == nil {
		// Not an error. Intelligence sits behind a compose profile, so a stack
		// can run without it, and a console can then say "this deployment runs
		// no model" rather than "the Hands would not explain".
		return connect.NewResponse(&corev1.ExplainApprovalResponse{
			IntelligenceAvailable: false,
			RegisterLabel:         register.Label,
		}), nil
	}

	explained, err := s.hands.ExplainApproval(ctx, connect.NewRequest(
		&platformv1.HandsExplainRequest{
			// FROM THE TENANCY INTERCEPTOR, NEVER FROM THE REQUEST. The
			// customer-facing message has no organisation field for exactly
			// this reason: a field carrying one is a field somebody can edit.
			OrgId:     store.OrgID(),
			FindingId: finding.ID,
		}))
	if err != nil {
		return nil, s.failure(finding.ID, store.OrgID(), err)
	}

	msg := explained.Msg
	return connect.NewResponse(&corev1.ExplainApprovalResponse{
		IntelligenceAvailable: true,
		Outcome:               outcomeFor(msg.GetOutcome()),
		// Carried as it came. The platform service withholds a refused
		// explanation already, so this is empty on a refusal without this
		// handler deciding anything.
		Explanation:   msg.GetExplanation(),
		OutcomeDetail: msg.GetOutcomeDetail(),
		RegisterLabel: register.Label,
		Prepared:      preparedFor(register, msg.GetPrepared()),
		LeftForYou:    leftFor(register, msg.GetLeftForYou()),
		AgentRunId:    msg.GetAgentRunId(),
	}), nil
}

// failure turns a delegated error into what the caller should see.
//
// FAILED_PRECONDITION IS PASSED THROUGH AND NOTHING ELSE IS. The run refuses
// with it for two things a person can act on: this deployment has no
// Intelligence, and this organisation chose a model that cannot be honoured.
// Both are sentences worth reading. Anything else becomes `unavailable`,
// because a code invented by the transport is not a statement about a
// compliance record, and because a message from deeper in the stack is not
// written for the person on the finding page.
//
// Nothing here becomes a refusal. A refusal arrives as a 200 with an outcome,
// so an error is the transport or the process, and drawing it as a guardrail
// firing would tell a customer their guardrails worked when nothing ran.
//
// `invalid_argument` from the delegate is deliberately in the second group. It
// means the finding vanished between this handler's read and the run's, or that
// its action type changed under both, and neither is something the person on
// the finding page can do anything with beyond trying again.
func (s *Service) failure(findingID, orgID string, err error) error {
	if connect.CodeOf(err) == connect.CodeFailedPrecondition {
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New(unwrapMessage(err)))
	}
	s.logger.Error("asking the Hands to explain an approval failed",
		"finding", findingID, "org", orgID, "error", err.Error())
	return connect.NewError(connect.CodeUnavailable,
		errors.New("the Hands could not be reached just now"))
}

// unwrapMessage is the reason a refusal carried, without the code Connect
// prefixes onto `Error()`.
func unwrapMessage(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}
	return err.Error()
}

// preparedFor names each filled column in a person's words.
//
// A column the register does not have keeps its payload name and nothing else.
// It cannot happen (the run validates every field against this same register
// before writing it), and rendering the raw name is the right thing for the
// case that cannot happen: inventing a label would be this handler making up a
// sentence about a customer's record.
func preparedFor(
	register records.Register, fields []*platformv1.PreparedField,
) []*corev1.PreparedField {
	out := make([]*corev1.PreparedField, 0, len(fields))
	for _, f := range fields {
		out = append(out, &corev1.PreparedField{
			Name:     f.GetName(),
			Label:    labelFor(register, f.GetName()),
			Values:   f.GetValues(),
			FromFact: f.GetFromFact(),
		})
	}
	return out
}

func leftFor(
	register records.Register, fields []*platformv1.LeftForYou,
) []*corev1.LeftForYou {
	out := make([]*corev1.LeftForYou, 0, len(fields))
	for _, f := range fields {
		out = append(out, &corev1.LeftForYou{
			Name:  f.GetName(),
			Label: labelFor(register, f.GetName()),
			Why:   f.GetWhy(),
		})
	}
	return out
}

func labelFor(register records.Register, name string) string {
	if field, known := register.Field(name); known {
		return field.Label
	}
	return ""
}

// outcomeFor maps the platform surface's outcome onto the customer-facing one.
//
// A switch rather than a shared enum, so a value added on one side cannot
// default silently on the other. UNSPECIFIED becomes FAILED rather than
// UNSPECIFIED: a response whose outcome nobody set did not come from a run this
// code understands, and "we have no idea" above a decision panel is worse than
// "it did not work".
func outcomeFor(outcome platformv1.HandsOutcome) corev1.ExplainOutcome {
	switch outcome {
	case platformv1.HandsOutcome_HANDS_OUTCOME_SUCCEEDED:
		return corev1.ExplainOutcome_EXPLAIN_OUTCOME_SUCCEEDED
	case platformv1.HandsOutcome_HANDS_OUTCOME_REFUSED:
		return corev1.ExplainOutcome_EXPLAIN_OUTCOME_REFUSED
	default:
		return corev1.ExplainOutcome_EXPLAIN_OUTCOME_FAILED
	}
}

// tenantAs pulls the request's transaction and narrows it to what this handler
// needs. The same helper `conversation` has, written out rather than shared:
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
