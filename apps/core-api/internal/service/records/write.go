package records

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// The write half of RecordsService.
//
// # FOUR REFUSALS, FOUR CODES, AND THE DIFFERENCE IS THE WHOLE POINT
//
// A caller who cannot complete a write needs to know which kind of cannot it is,
// because each one has a different thing under it in a console:
//
//	resource_exhausted   the plan's cap is reached      -> an upgrade
//	failed_precondition  a reviewed approval is needed  -> a confirm step
//	failed_precondition  onboarding is not done         -> finish onboarding
//	not_found            no such record, or not yours   -> nothing
//
// `permission_denied` is none of these and is never returned here: the scope
// interceptor has already answered that question before a handler runs. A write
// refused for a plan or a confirmation is refused to somebody who is allowed to
// do it, and saying "denied" would send them to an owner who cannot help.
//
// The two `failed_precondition` cases share a code and differ in message, which
// is deliberate: both mean "do something else first, then retry", and a client
// that wants to tell them apart reads the message rather than branching on a
// code that would then encode product copy.
type writing interface {
	CreateProcessingActivity(ctx context.Context, f domain.ProcessingActivityFields) (domain.ProcessingActivity, error)
	UpdateProcessingActivity(ctx context.Context, activityID string, f domain.ProcessingActivityFields) (domain.ProcessingActivity, error)
	CreateAiSystem(ctx context.Context, f domain.AiSystemFields, reviewed bool) (domain.AiSystem, error)
	UpdateAiSystem(ctx context.Context, systemID string, f domain.AiSystemFields, reviewed bool) (domain.AiSystem, error)
	LogDsar(ctx context.Context, subjectName, requestType, handler string) (domain.Dsar, error)
	MarkDsarResponded(ctx context.Context, dsarID string, reviewed bool) (domain.Dsar, bool, error)
}

// writeError maps a store refusal onto the code that describes it.
//
// Returns nil for nil, and CodeInternal for anything it does not recognise,
// which is correct: an unrecognised failure is a server fault and must not be
// dressed up as a business rule the caller can act on.
func writeError(err error, what string) error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, postgres.ErrQuotaExhausted):
		// The act path uses the same code for its plan gate. HTTP 402 has no
		// Connect equivalent, and this is the closest honest one: the caller is
		// entitled to do this and has run out of allowance.
		return connect.NewError(connect.CodeResourceExhausted,
			errors.New("your plan's limit on manually added records is reached"))

	case errors.Is(err, postgres.ErrReviewRequired):
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this change is recorded as a reviewed approval, so it must be confirmed: send reviewed"))

	case errors.Is(err, postgres.ErrNoProfile):
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this organisation has no compliance profile yet, so there is nothing to attach a record to: finish onboarding first"))

	case errors.Is(err, pgx.ErrNoRows):
		return connect.NewError(connect.CodeNotFound, errors.New(what))

	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

func activityFields(f *corev1.ProcessingActivityFields) domain.ProcessingActivityFields {
	if f == nil {
		return domain.ProcessingActivityFields{}
	}
	return domain.ProcessingActivityFields{
		Name:            f.GetName(),
		Purpose:         f.GetPurpose(),
		LegalBasis:      f.GetLegalBasis(),
		DataCategories:  f.GetDataCategories(),
		Recipients:      f.GetRecipients(),
		RetentionPeriod: f.GetRetentionPeriod(),
	}
}

func systemFields(f *corev1.AiSystemFields) domain.AiSystemFields {
	if f == nil {
		return domain.AiSystemFields{}
	}
	return domain.AiSystemFields{
		Name:                f.GetName(),
		Vendor:              f.GetVendor(),
		Purpose:             f.GetPurpose(),
		RiskClassification:  f.GetRiskClassification(),
		DocumentationStatus: f.GetDocumentationStatus(),
	}
}

// CreateProcessingActivity adds an Article 30 entry.
func (s *Service) CreateProcessingActivity(
	ctx context.Context,
	req *connect.Request[corev1.CreateProcessingActivityRequest],
) (*connect.Response[corev1.CreateProcessingActivityResponse], error) {
	store, err := tenantAs[writing](ctx)
	if err != nil {
		return nil, err
	}

	activity, err := store.CreateProcessingActivity(ctx, activityFields(req.Msg.GetFields()))
	if err != nil {
		return nil, writeError(err, "no such processing activity")
	}

	return connect.NewResponse(&corev1.CreateProcessingActivityResponse{
		ProcessingActivity: activityToProto(activity),
	}), nil
}

// UpdateProcessingActivity replaces an entry's fields.
func (s *Service) UpdateProcessingActivity(
	ctx context.Context,
	req *connect.Request[corev1.UpdateProcessingActivityRequest],
) (*connect.Response[corev1.UpdateProcessingActivityResponse], error) {
	store, err := tenantAs[writing](ctx)
	if err != nil {
		return nil, err
	}

	activity, err := store.UpdateProcessingActivity(ctx,
		req.Msg.GetProcessingActivityId(), activityFields(req.Msg.GetFields()))
	if err != nil {
		return nil, writeError(err, "no such processing activity")
	}

	return connect.NewResponse(&corev1.UpdateProcessingActivityResponse{
		ProcessingActivity: activityToProto(activity),
	}), nil
}

// CreateAiSystem registers a system.
func (s *Service) CreateAiSystem(
	ctx context.Context,
	req *connect.Request[corev1.CreateAiSystemRequest],
) (*connect.Response[corev1.CreateAiSystemResponse], error) {
	store, err := tenantAs[writing](ctx)
	if err != nil {
		return nil, err
	}

	system, err := store.CreateAiSystem(ctx,
		systemFields(req.Msg.GetFields()), req.Msg.GetReviewed())
	if err != nil {
		return nil, writeError(err, "no such ai system")
	}

	return connect.NewResponse(&corev1.CreateAiSystemResponse{
		AiSystem: systemToProto(system),
	}), nil
}

// UpdateAiSystem replaces a system's fields.
func (s *Service) UpdateAiSystem(
	ctx context.Context,
	req *connect.Request[corev1.UpdateAiSystemRequest],
) (*connect.Response[corev1.UpdateAiSystemResponse], error) {
	store, err := tenantAs[writing](ctx)
	if err != nil {
		return nil, err
	}

	system, err := store.UpdateAiSystem(ctx,
		req.Msg.GetAiSystemId(), systemFields(req.Msg.GetFields()), req.Msg.GetReviewed())
	if err != nil {
		return nil, writeError(err, "no such ai system")
	}

	return connect.NewResponse(&corev1.UpdateAiSystemResponse{
		AiSystem: systemToProto(system),
	}), nil
}

// LogDsar records a request that arrived.
func (s *Service) LogDsar(
	ctx context.Context,
	req *connect.Request[corev1.LogDsarRequest],
) (*connect.Response[corev1.LogDsarResponse], error) {
	store, err := tenantAs[writing](ctx)
	if err != nil {
		return nil, err
	}

	dsar, err := store.LogDsar(ctx,
		req.Msg.GetSubjectName(), req.Msg.GetRequestType(), req.Msg.GetHandler())
	if err != nil {
		return nil, writeError(err, "no such data-subject request")
	}

	return connect.NewResponse(&corev1.LogDsarResponse{
		Dsar: dsarToProto(dsar, s.clock()),
	}), nil
}

// MarkDsarResponded stops the statutory clock.
func (s *Service) MarkDsarResponded(
	ctx context.Context,
	req *connect.Request[corev1.MarkDsarRespondedRequest],
) (*connect.Response[corev1.MarkDsarRespondedResponse], error) {
	store, err := tenantAs[writing](ctx)
	if err != nil {
		return nil, err
	}

	dsar, applied, err := store.MarkDsarResponded(ctx,
		req.Msg.GetDsarId(), req.Msg.GetReviewed())
	if err != nil {
		return nil, writeError(err, "no such data-subject request")
	}

	return connect.NewResponse(&corev1.MarkDsarRespondedResponse{
		Applied: applied,
		Dsar:    dsarToProto(dsar, s.clock()),
	}), nil
}
