package records

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// The DSAR trail (ENT-226): the evidence behind `Dsar.responded_at`.
//
// # WHY THERE IS NO UPDATE OR DELETE HANDLER HERE
//
// Not an unfinished surface. A trail entry is evidence about how a response to a
// statutory request was assembled, and evidence a producer can revise after the
// fact is worth less than evidence it cannot. The database refuses an UPDATE
// with a trigger that binds even the migrator, and `kindlast_app` holds no
// DELETE grant, so a handler for either could not be served whatever this
// package offered.
//
// Correcting a mistake means appending an entry that says so, which is how a
// paper file works and is the behaviour a compliance record should have.
//
// # WHAT THIS LAYER DECIDES AND WHAT IT DOES NOT
//
// It validates the action vocabulary so a caller gets a sentence rather than a
// check-constraint name, and it maps store refusals onto codes. It does not
// decide whether a response may go out with an empty trail: nothing here or
// anywhere else refuses MarkDsarResponded on a zero count, because a customer
// who assembled the response somewhere else has still met the deadline, and a
// product that invented that rule would be asserting an obligation Article 12(3)
// does not contain.

// trailing is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type trailing interface {
	DsarTrail(ctx context.Context, dsarID, cursor string, pageSize int) (domain.Page[domain.TrailEntry], error)
	AddTrailEntry(ctx context.Context, dsarID string, entry domain.TrailEntry) (domain.TrailEntry, error)
}

// ListDsarTrail is one request's trail, oldest first.
func (s *Service) ListDsarTrail(
	ctx context.Context,
	req *connect.Request[corev1.ListDsarTrailRequest],
) (*connect.Response[corev1.ListDsarTrailResponse], error) {
	store, err := tenantAs[trailing](ctx)
	if err != nil {
		return nil, err
	}

	page, err := store.DsarTrail(ctx,
		req.Msg.GetDsarId(), req.Msg.GetPageToken(), int(req.Msg.GetPageSize()))
	if errors.Is(err, postgres.ErrBadCursor) {
		return nil, badCursor()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound("no such data-subject request")
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.ListDsarTrailResponse{
		Entries:       make([]*corev1.DsarTrailEntry, 0, len(page.Items)),
		NextPageToken: page.NextCursor,
	}
	for _, e := range page.Items {
		out.Entries = append(out.Entries, trailEntryToProto(e))
	}
	return connect.NewResponse(out), nil
}

// AddDsarTrailEntry appends one step to a request's trail.
func (s *Service) AddDsarTrailEntry(
	ctx context.Context,
	req *connect.Request[corev1.AddDsarTrailEntryRequest],
) (*connect.Response[corev1.AddDsarTrailEntryResponse], error) {
	store, err := tenantAs[trailing](ctx)
	if err != nil {
		return nil, err
	}

	// Both refusals below are `invalid_argument` and both name the acceptable
	// values, because a caller cannot guess a closed set and the database's
	// check constraint would answer with a message written for a DBA. Same
	// treatment ListDsars gives its status filter.
	if strings.TrimSpace(req.Msg.GetSource()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(
			"a trail entry has to name the store that was searched"))
	}
	if !domain.ValidTrailAction(req.Msg.GetAction()) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf(
			"action must be one of %s", strings.Join(domain.TrailActions, ", ")))
	}

	entry := domain.TrailEntry{
		Source:     req.Msg.GetSource(),
		Action:     req.Msg.GetAction(),
		Detail:     req.Msg.GetDetail(),
		AgentRunID: req.Msg.GetAgentRunId(),
	}
	// Absent means now, and absent has to survive the round trip as the zero
	// time rather than as the epoch: an unset timestamp answers `AsTime()` with
	// 1970, which the store would take for a real and very old date.
	if req.Msg.GetOccurredAt() != nil {
		entry.OccurredAt = req.Msg.GetOccurredAt().AsTime()
	}

	written, err := store.AddTrailEntry(ctx, req.Msg.GetDsarId(), entry)
	if err != nil {
		return nil, trailError(err)
	}

	return connect.NewResponse(&corev1.AddDsarTrailEntryResponse{
		Entry: trailEntryToProto(written),
	}), nil
}

// trailError maps a store refusal onto the code that describes it.
//
// Separate from `writeError` rather than folded into it, because two of these
// refusals have no analogue in the register writes and folding them in would
// mean a ROPA update that could return "no such agent run".
func trailError(err error) error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, postgres.ErrFutureOccurrence):
		// `invalid_argument` rather than `failed_precondition`, matching the
		// receipt date: the caller sends a different value, they do not satisfy
		// a condition and retry the same one.
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("a search cannot have happened in the future"))

	case errors.Is(err, postgres.ErrUnknownAgentRun):
		// Not `not_found`, which would be about the request in the path. This
		// is a field in the body naming something the caller cannot see, and a
		// console has to be able to tell those apart or it sends somebody
		// looking at the wrong thing.
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("that agent run is not one this organisation has"))

	case errors.Is(err, pgx.ErrNoRows):
		return connect.NewError(connect.CodeNotFound,
			errors.New("no such data-subject request"))

	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

func trailEntryToProto(e domain.TrailEntry) *corev1.DsarTrailEntry {
	return &corev1.DsarTrailEntry{
		EntryId:    e.ID,
		DsarId:     e.DsarID,
		Source:     e.Source,
		Action:     e.Action,
		Detail:     e.Detail,
		OccurredAt: timestamppb.New(e.OccurredAt),
		RecordedAt: timestamppb.New(e.RecordedAt),
		CreatedBy:  e.CreatedBy,
		AgentRunId: e.AgentRunID,
	}
}
