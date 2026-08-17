// Package audit serves AuditService: the decisions, filtered, paged and
// exportable (ENT-223).
//
// # READ ONLY, AND NOT BY POLITENESS
//
// There is no write RPC here and there cannot usefully be one. `audit_log`
// carries an append-only trigger, `kindlast_app` holds no update or delete
// grant on it, and the only insert path is bound by policy to the human in the
// GUC. A record a customer can edit is not an audit log, so the absence is the
// feature.
//
// # AND IT READS `audit_log` AND NOTHING ELSE
//
// Not traces, not model calls, not anything an observability tool holds
// (§7.2). An auditor is buying a record a regulator can be shown, and a record
// assembled partly from a vendor's telemetry has completeness that depends on
// that vendor's retention settings. A console rendering this should say so in
// its own words rather than leave it to be inferred.
package audit

import (
	"bytes"
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/audit"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// reading is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type reading interface {
	AuditEntries(ctx context.Context, filter domain.Filter, cursor string, pageSize int) ([]domain.Entry, string, error)
	AuditEntriesForExport(ctx context.Context, filter domain.Filter) ([]domain.Entry, bool, error)
	AuditActionTypes(ctx context.Context) ([]string, error)
}

// Service implements corev1connect.AuditServiceHandler.
//
// # NO ROLE GATE, UNLIKE BILLING
//
// Every member sees the log, viewers included. That is a deliberate difference
// from BillingService, which is owner-only: a plan and a renewal date are
// commercial facts about the company, whereas "who approved this finding" is
// the shared record of the work these people did together. Hiding a colleague's
// decision from a colleague would make the compliance record something people
// have to ask permission to check, which is the opposite of what it is for.
//
// The organisation boundary is still absolute. RLS scopes every row, and a
// caller sees their own organisation's log or nothing.
type Service struct {
	// now is injected so the export's filename is testable. Nothing else here
	// reads a clock.
	now func() time.Time
}

func New() *Service { return &Service{now: time.Now} }

func (s *Service) ListAuditEntries(
	ctx context.Context,
	req *connect.Request[corev1.ListAuditEntriesRequest],
) (*connect.Response[corev1.ListAuditEntriesResponse], error) {
	store, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	filter, err := toFilter(req.Msg.GetFilter())
	if err != nil {
		return nil, err
	}

	entries, next, err := store.AuditEntries(
		ctx, filter, req.Msg.GetPageToken(), int(req.Msg.GetPageSize()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Unfiltered, because it populates a filter control. Offering only the
	// values that survive the current filter would make the control empty
	// itself the moment somebody used it.
	actionTypes, err := store.AuditActionTypes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListAuditEntriesResponse{
		NextPageToken:        next,
		AvailableActionTypes: actionTypes,
	}
	for _, entry := range entries {
		response.Entries = append(response.Entries, toProto(entry))
	}
	return connect.NewResponse(response), nil
}

func (s *Service) ExportAuditEntries(
	ctx context.Context,
	req *connect.Request[corev1.ExportAuditEntriesRequest],
) (*connect.Response[corev1.ExportAuditEntriesResponse], error) {
	store, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	// UNSPECIFIED is accepted as CSV rather than refused. It is the only format
	// there is, so refusing an unset enum would fail every caller written
	// against the obvious reading of a one-value enum for no benefit. A second
	// format changes that, and this switch is where it changes.
	switch req.Msg.GetFormat() {
	case corev1.ExportFormat_EXPORT_FORMAT_UNSPECIFIED, corev1.ExportFormat_EXPORT_FORMAT_CSV:
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("that export format is not supported"))
	}

	filter, err := toFilter(req.Msg.GetFilter())
	if err != nil {
		return nil, err
	}

	entries, truncated, err := store.AuditEntriesForExport(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var body bytes.Buffer
	if err := domain.WriteCSV(&body, entries); err != nil {
		// Reported rather than returning what was written so far. A short file
		// that arrives without an error is the worst outcome available here:
		// the auditor believes they have the record.
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.ExportAuditEntriesResponse{
		Result: &corev1.ExportAuditEntriesResponse_Content{
			Content: &corev1.ExportContent{
				Data:        body.Bytes(),
				Filename:    domain.ExportFilename(s.now()),
				ContentType: "text/csv; charset=utf-8",
			},
		},
		RowCount: int32(len(entries)),
		// Never dropped on the floor. A truncated CSV is a valid CSV that simply
		// stops, so a console that does not surface this lets an auditor attach
		// an incomplete record to a report without knowing.
		Truncated: truncated,
	}), nil
}

func tenantFrom(ctx context.Context) (reading, error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	store, ok := tenant.(reading)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot read the audit log"))
	}
	return store, nil
}

// toFilter converts and normalises, and reports a backwards range as the
// caller's mistake rather than silently widening the set.
func toFilter(in *corev1.AuditFilter) (domain.Filter, error) {
	filter := domain.Filter{
		ActionTypes:  in.GetActionTypes(),
		ActorUserIDs: in.GetActorUserIds(),
		Query:        in.GetQuery(),
	}
	if since := in.GetSince(); since != nil {
		filter.Since = since.AsTime()
	}
	if until := in.GetUntil(); until != nil {
		filter.Until = until.AsTime()
	}

	normalised, err := filter.Normalise()
	if err != nil {
		// `invalid_argument` rather than `failed_precondition`, because the fix
		// is a different value rather than a condition to satisfy and retry.
		return domain.Filter{}, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return normalised, nil
}

func toProto(entry domain.Entry) *corev1.AuditEntry {
	kind := corev1.ActorKind_ACTOR_KIND_HUMAN
	if entry.Actor.Kind == domain.ActorService {
		kind = corev1.ActorKind_ACTOR_KIND_SERVICE
	}

	return &corev1.AuditEntry{
		Id:         entry.ID,
		OccurredAt: timestamppb.New(entry.OccurredAt),
		ActionType: entry.ActionType,
		Actor: &corev1.Actor{
			UserId:      entry.Actor.UserID,
			DisplayName: entry.Actor.DisplayName,
			Email:       entry.Actor.Email,
			ActorRole:   entry.Actor.Role,
			Kind:        kind,
		},
		FindingId:   entry.FindingID,
		TargetTable: entry.TargetTable,
		TargetId:    entry.TargetID,
		BeforeJson:  entry.BeforeJSON,
		AfterJson:   entry.AfterJSON,
		AgentRunId:  entry.AgentRunID,
	}
}
