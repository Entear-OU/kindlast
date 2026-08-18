// Package findings serves FindingsService: the feed, the detail view and the
// act path.
//
// The division of labour is the one ENT-202 settled and it is worth restating,
// because it is what keeps this file short. Scope and tenancy enforce: the
// interceptor has already refused a token without `findings:read` or
// `findings:act`, and RLS has already scoped every row to the caller's
// organisation. This layer translates refusals into errors a console can
// render, and owns the rules RLS cannot express. Plan gating is the only such
// rule here.
//
// Nothing in this package writes an audit row. The database does that (00006),
// and a handler writing one too would produce a second row describing the same
// decision.
package findings

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// reading and acting are what these handlers need of the request's
// transaction, declared where they are used rather than exported from the
// store (§21.6).
type reading interface {
	Findings(ctx context.Context, status, cursor string, pageSize int) (domain.Page, error)
	Finding(ctx context.Context, findingID string) (domain.Finding, []domain.SupportingChunk, error)
}

type acting interface {
	ApproveFinding(ctx context.Context, findingID string, reviewed bool) (domain.Acted, error)
	RejectFinding(ctx context.Context, findingID, reason string) (domain.Acted, error)
	SnoozeFinding(ctx context.Context, findingID string, days int32) (domain.Acted, error)
	Plan(ctx context.Context) (string, error)
}

// Service implements corev1connect.FindingsServiceHandler.
type Service struct {
	// billingEnabled is false for a self-hosted deployment, which is the
	// default. See config.Config.BillingEnabled.
	billingEnabled bool
}

func New(billingEnabled bool) *Service {
	return &Service{billingEnabled: billingEnabled}
}

// ListFindings is the feed.
func (s *Service) ListFindings(
	ctx context.Context,
	req *connect.Request[corev1.ListFindingsRequest],
) (*connect.Response[corev1.ListFindingsResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	if status := req.Msg.GetStatus(); status != "" && !validStatus(status) {
		// Named values rather than a bare refusal: the caller cannot guess a
		// closed set, and the database's check constraint would answer with a
		// message written for a DBA.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("status must be one of pending, approved, rejected, snoozed"))
	}

	page, err := store.Findings(ctx,
		req.Msg.GetStatus(), req.Msg.GetPageToken(), int(req.Msg.GetPageSize()))
	if errors.Is(err, postgres.ErrBadCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the page token is not one this server issued"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.ListFindingsResponse{
		Findings:      make([]*corev1.Finding, 0, len(page.Findings)),
		NextPageToken: page.NextCursor,
	}
	for _, f := range page.Findings {
		out.Findings = append(out.Findings, toProto(f))
	}
	return connect.NewResponse(out), nil
}

// GetFinding is one finding and the regulatory text behind it.
func (s *Service) GetFinding(
	ctx context.Context,
	req *connect.Request[corev1.GetFindingRequest],
) (*connect.Response[corev1.GetFindingResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	finding, chunks, err := store.Finding(ctx, req.Msg.GetFindingId())
	if errors.Is(err, pgx.ErrNoRows) {
		// One answer for "no such finding" and "not yours". RLS makes them the
		// same query result, and telling them apart would turn this endpoint
		// into a way to ask whether a given finding exists in someone else's
		// organisation.
		return nil, connect.NewError(connect.CodeNotFound,
			errors.New("no such finding"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.GetFindingResponse{
		Finding:    toProto(finding),
		Supporting: make([]*corev1.SupportingChunk, 0, len(chunks)),
	}
	for _, c := range chunks {
		out.Supporting = append(out.Supporting, &corev1.SupportingChunk{
			Ordinal:    c.Ordinal,
			Label:      c.Label,
			QuotedText: c.QuotedText,
			SourceUrl:  c.SourceURL,
		})
	}
	return connect.NewResponse(out), nil
}

// ApproveFinding approves, and reports what the Executor created.
func (s *Service) ApproveFinding(
	ctx context.Context,
	req *connect.Request[corev1.ApproveFindingRequest],
) (*connect.Response[corev1.ApproveFindingResponse], error) {
	store, err := tenantAs[acting](ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requirePaidPlan(ctx, store); err != nil {
		return nil, err
	}

	acted, err := store.ApproveFinding(ctx, req.Msg.GetFindingId(), req.Msg.GetReviewed())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.ApproveFindingResponse{
		Applied:            acted.Applied,
		CreatedRecordId:    acted.CreatedRecordID,
		CreatedRecordTable: acted.CreatedRecordTable,
	}), nil
}

// RejectFinding rejects, optionally recording why.
func (s *Service) RejectFinding(
	ctx context.Context,
	req *connect.Request[corev1.RejectFindingRequest],
) (*connect.Response[corev1.RejectFindingResponse], error) {
	store, err := tenantAs[acting](ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requirePaidPlan(ctx, store); err != nil {
		return nil, err
	}

	acted, err := store.RejectFinding(ctx, req.Msg.GetFindingId(), req.Msg.GetReason())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&corev1.RejectFindingResponse{Applied: acted.Applied}), nil
}

// SnoozeFinding defers a finding.
func (s *Service) SnoozeFinding(
	ctx context.Context,
	req *connect.Request[corev1.SnoozeFindingRequest],
) (*connect.Response[corev1.SnoozeFindingResponse], error) {
	store, err := tenantAs[acting](ctx)
	if err != nil {
		return nil, err
	}
	if err := s.requirePaidPlan(ctx, store); err != nil {
		return nil, err
	}

	acted, err := store.SnoozeFinding(ctx, req.Msg.GetFindingId(), req.Msg.GetDays())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.SnoozeFindingResponse{Applied: acted.Applied}
	if acted.SnoozedUntil != nil {
		out.SnoozedUntil = timestamppb.New(*acted.SnoozedUntil)
	}
	return connect.NewResponse(out), nil
}

// requirePaidPlan is the act path's plan gate.
//
// A rule RLS cannot express, so it lives here (§0.5). Two properties matter
// more than the check itself:
//
// It no-ops entirely when billing is disabled, which is the default and the
// self-hosted case (§18.1). A self-hoster has no subscription and never will,
// and refusing them the Executor would make the self-hosted build a demo.
//
// The code is 402 rather than 403. They mean different things to the person
// reading the console: 403 says "you may not", which sends an owner to their
// permissions, and 402 says "this needs a paid plan", which is a sentence with
// a button under it. Connect has no 402, so it travels as a resource_exhausted
// with the reason in the message; §0.5 records the mapping.
func (s *Service) requirePaidPlan(ctx context.Context, store acting) error {
	if !s.billingEnabled {
		return nil
	}

	plan, err := store.Plan(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	if plan != "pro" {
		return connect.NewError(connect.CodeResourceExhausted,
			errors.New("acting on a finding needs the Pro plan"))
	}
	return nil
}

// tenantAs pulls the request's transaction and narrows it to what a handler
// needs.
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

func validStatus(status string) bool {
	switch status {
	case "pending", "approved", "rejected", "snoozed":
		return true
	default:
		return false
	}
}

// toProto maps a stored finding onto the wire.
//
// Note what it does not do: it never assembles a citation. Label and URL are
// copied from what the Analyst recorded, and the structured fields travel
// alongside them for filtering. Building the label here would be a second
// assembler that can disagree with the record.
func toProto(f domain.Finding) *corev1.Finding {
	out := &corev1.Finding{
		FindingId:       f.ID,
		Status:          f.Status,
		Severity:        f.Severity,
		Detected:        f.Detected,
		ProposedAction:  f.ProposedAction,
		EffortEstimate:  f.EffortEstimate,
		ActionType:      f.ActionType,
		CreatedAt:       timestamppb.New(f.CreatedAt),
		ApprovedBy:      f.ApprovedBy,
		RejectionReason: f.RejectionReason,
		// What the Analyst added, beside what the sweep wrote rather than over
		// it. Copied straight across: there is nothing to decide here, and a
		// handler that chose when to reveal a narrative would be a second
		// producer of the feed's text.
		Narrative:        f.Narrative,
		AgentRunId:       f.AgentRunID,
		NarrativeRefusal: f.NarrativeRefusal,
		Citation: &corev1.Citation{
			ObligationSlug: f.Citation.ObligationSlug,
			Title:          f.Citation.Title,
			// The authored statement of law, carried so a client can render it
			// beside the model's narrative (ENT-248). Copied, never edited: the
			// point is that a reader sees the curator's words rather than a
			// paraphrase of them.
			ObligationSummary: f.Citation.Summary,
			Celex:             f.Citation.CELEX,
			Kind:              f.Citation.Kind,
			Article:           f.Citation.Article,
			Recital:           f.Citation.Recital,
			Annex:             f.Citation.Annex,
			Paragraph:         f.Citation.Paragraph,
			Label:             f.Citation.Label,
			Url:               f.Citation.URL,
		},
	}
	if f.SnoozedUntil != nil {
		out.SnoozedUntil = timestamppb.New(*f.SnoozedUntil)
	}
	return out
}
