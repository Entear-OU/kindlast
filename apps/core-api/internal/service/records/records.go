// Package records serves RecordsService: the three registers that make up the
// compliance record.
//
// Same division of labour as `service/findings`. Scope and tenancy have already
// enforced: the interceptor refused a token without `records:read`, and RLS
// scoped every row to the caller's organisation. This layer translates refusals
// into errors a console can render, and derives the two values the contract says
// are computed server-side.
//
// Read-only. The write scopes and the six database functions behind them exist
// (00002), but no RPC here writes anything, so nothing in this package needs an
// audit row: the database writes those, and a handler writing one too would
// produce a second row describing the same change.
package records

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// reading is what these handlers need of the request's transaction, declared
// where it is used rather than exported from the store (§21.6).
type reading interface {
	ProcessingActivities(ctx context.Context, cursor string, pageSize int) (domain.Page[domain.ProcessingActivity], error)
	ProcessingActivity(ctx context.Context, activityID string) (domain.ProcessingActivity, error)
	ManualActivityQuota(ctx context.Context) (domain.Quota, error)
	AiSystems(ctx context.Context, cursor string, pageSize int) (domain.Page[domain.AiSystem], error)
	AiSystem(ctx context.Context, systemID string) (domain.AiSystem, error)
	Dsars(ctx context.Context, status, cursor string, pageSize int) (domain.Page[domain.Dsar], error)
	Dsar(ctx context.Context, dsarID string) (domain.Dsar, error)
}

// Service implements corev1connect.RecordsServiceHandler.
//
// Carries no billing flag, deliberately. It held one until the cap moved into
// the database, where it belongs: the function that ENFORCES the cap is the one
// that should decide it, and a copy of the flag here was how the shown cap and
// the enforced cap came to disagree. See `quota`.
type Service struct {
	// now is injectable so the urgency bands can be tested against a fixed
	// clock. Nil means time.Now.
	now func() time.Time
}

func New() *Service {
	return &Service{}
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// ListProcessingActivities is the Article 30 record.
func (s *Service) ListProcessingActivities(
	ctx context.Context,
	req *connect.Request[corev1.ListProcessingActivitiesRequest],
) (*connect.Response[corev1.ListProcessingActivitiesResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	page, err := store.ProcessingActivities(ctx, req.Msg.GetPageToken(), int(req.Msg.GetPageSize()))
	if errors.Is(err, postgres.ErrBadCursor) {
		return nil, badCursor()
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	quota, err := s.quota(ctx, store)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.ListProcessingActivitiesResponse{
		ProcessingActivities: make([]*corev1.ProcessingActivity, 0, len(page.Items)),
		NextPageToken:        page.NextCursor,
		ManualQuota:          quota,
	}
	for _, p := range page.Items {
		out.ProcessingActivities = append(out.ProcessingActivities, activityToProto(p))
	}
	return connect.NewResponse(out), nil
}

// quota reports the manual-entry cap, whatever the database says it is.
//
// # NO BILLING CHECK HERE ANY MORE, AND THAT IS THE FIX
//
// This used to short-circuit to an empty quota when billing was disabled, which
// looked right and was half a rule. The database function knew nothing about the
// flag, so it kept returning three, and a self-hosted deployment got the worst
// of both: the console showed no limit, and then the fourth activity was refused
// with a message about a plan that deployment does not sell.
//
// The flag now reaches the database as a session GUC, so there is one answer to
// the question and this layer reports it rather than second-guessing it. A cap
// shown here and a cap enforced there cannot disagree, because they are the same
// computation.
func (s *Service) quota(ctx context.Context, store reading) (*corev1.ManualQuota, error) {
	q, err := store.ManualActivityQuota(ctx)
	if err != nil {
		return nil, err
	}
	return &corev1.ManualQuota{Used: q.Used, Limit: q.Limit}, nil
}

// GetProcessingActivity is one Article 30 entry.
func (s *Service) GetProcessingActivity(
	ctx context.Context,
	req *connect.Request[corev1.GetProcessingActivityRequest],
) (*connect.Response[corev1.GetProcessingActivityResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	activity, err := store.ProcessingActivity(ctx, req.Msg.GetProcessingActivityId())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("no such processing activity")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetProcessingActivityResponse{
		ProcessingActivity: activityToProto(activity),
	}), nil
}

// ListAiSystems is the AI Act register.
func (s *Service) ListAiSystems(
	ctx context.Context,
	req *connect.Request[corev1.ListAiSystemsRequest],
) (*connect.Response[corev1.ListAiSystemsResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	page, err := store.AiSystems(ctx, req.Msg.GetPageToken(), int(req.Msg.GetPageSize()))
	if errors.Is(err, postgres.ErrBadCursor) {
		return nil, badCursor()
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.ListAiSystemsResponse{
		AiSystems:     make([]*corev1.AiSystem, 0, len(page.Items)),
		NextPageToken: page.NextCursor,
	}
	for _, a := range page.Items {
		out.AiSystems = append(out.AiSystems, systemToProto(a))
	}
	return connect.NewResponse(out), nil
}

// GetAiSystem is one register entry.
func (s *Service) GetAiSystem(
	ctx context.Context,
	req *connect.Request[corev1.GetAiSystemRequest],
) (*connect.Response[corev1.GetAiSystemResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	system, err := store.AiSystem(ctx, req.Msg.GetAiSystemId())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("no such ai system")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetAiSystemResponse{
		AiSystem: systemToProto(system),
	}), nil
}

// ListDsars is the DSAR log, soonest deadline first.
func (s *Service) ListDsars(
	ctx context.Context,
	req *connect.Request[corev1.ListDsarsRequest],
) (*connect.Response[corev1.ListDsarsResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	if status := req.Msg.GetStatus(); status != "" && !validDsarStatus(status) {
		// Named values rather than a bare refusal: the caller cannot guess a
		// closed set, and the database's check constraint would answer with a
		// message written for a DBA.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("status must be one of open, in_progress, responded, closed"))
	}

	page, err := store.Dsars(ctx,
		req.Msg.GetStatus(), req.Msg.GetPageToken(), int(req.Msg.GetPageSize()))
	if errors.Is(err, postgres.ErrBadCursor) {
		return nil, badCursor()
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// One clock reading for the whole page, so two requests in the same
	// response cannot be banded against different "now"s.
	now := s.clock()

	out := &corev1.ListDsarsResponse{
		Dsars:         make([]*corev1.Dsar, 0, len(page.Items)),
		NextPageToken: page.NextCursor,
	}
	for _, d := range page.Items {
		out.Dsars = append(out.Dsars, dsarToProto(d, now))
	}
	return connect.NewResponse(out), nil
}

// GetDsar is one data-subject request.
func (s *Service) GetDsar(
	ctx context.Context,
	req *connect.Request[corev1.GetDsarRequest],
) (*connect.Response[corev1.GetDsarResponse], error) {
	store, err := tenantAs[reading](ctx)
	if err != nil {
		return nil, err
	}

	d, err := store.Dsar(ctx, req.Msg.GetDsarId())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("no such data-subject request")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetDsarResponse{
		Dsar: dsarToProto(d, s.clock()),
	}), nil
}

func activityToProto(p domain.ProcessingActivity) *corev1.ProcessingActivity {
	return &corev1.ProcessingActivity{
		ProcessingActivityId: p.ID,
		Name:                 p.Name,
		Purpose:              p.Purpose,
		LegalBasis:           p.LegalBasis,
		DataCategories:       p.DataCategories,
		Recipients:           p.Recipients,
		RetentionPeriod:      p.RetentionPeriod,
		SourceFindingId:      p.SourceFindingID,
		Completeness:         domain.Completeness(p),
		CreatedAt:            timestamppb.New(p.CreatedAt),
		UpdatedAt:            timestamppb.New(p.UpdatedAt),
	}
}

func systemToProto(a domain.AiSystem) *corev1.AiSystem {
	out := &corev1.AiSystem{
		AiSystemId:          a.ID,
		Name:                a.Name,
		Vendor:              a.Vendor,
		Purpose:             a.Purpose,
		RiskClassification:  a.RiskClassification,
		DocumentationStatus: a.DocumentationStatus,
		SourceFindingId:     a.SourceFindingID,
		CreatedAt:           timestamppb.New(a.CreatedAt),
		UpdatedAt:           timestamppb.New(a.UpdatedAt),
	}
	// Left absent rather than sent as the epoch. Never reviewed is a state a
	// client renders differently, and a zero timestamp on the wire would be
	// read as 1970 by anyone formatting it without checking.
	if !a.LastReviewedAt.IsZero() {
		out.LastReviewedAt = timestamppb.New(a.LastReviewedAt)
	}
	return out
}

func dsarToProto(d domain.Dsar, now time.Time) *corev1.Dsar {
	out := &corev1.Dsar{
		DsarId:          d.ID,
		SubjectName:     d.SubjectName,
		RequestType:     d.RequestType,
		Status:          d.Status,
		ReceivedAt:      timestamppb.New(d.ReceivedAt),
		ResponseDueAt:   timestamppb.New(d.ResponseDueAt),
		Handler:         d.Handler,
		Urgency:         domain.Urgency(d, now),
		DaysUntilDue:    domain.DaysUntilDue(d, now),
		SourceFindingId: d.SourceFindingID,
		CreatedAt:       timestamppb.New(d.CreatedAt),
		UpdatedAt:       timestamppb.New(d.UpdatedAt),
		TrailEntryCount: d.TrailEntryCount,
	}
	if !d.RespondedAt.IsZero() {
		out.RespondedAt = timestamppb.New(d.RespondedAt)
	}
	return out
}

// notFound is the one answer for "no such record" and "not yours".
//
// RLS makes them the same query result, and telling them apart would turn every
// Get here into a way to ask whether a given record exists in someone else's
// organisation.
func notFound(message string) error {
	return connect.NewError(connect.CodeNotFound, errors.New(message))
}

func badCursor() error {
	return connect.NewError(connect.CodeInvalidArgument,
		errors.New("the page token is not one this server issued"))
}

func validDsarStatus(status string) bool {
	switch status {
	case "open", "in_progress", "responded", "closed":
		return true
	default:
		return false
	}
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
