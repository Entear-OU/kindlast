// Package sweep serves SweepService: the on-demand producer trigger.
//
// This is the only service in core-api that does NOT run behind the tenancy
// interceptor, and that is deliberate rather than an omission.
//
// Tenancy resolves the caller's membership: it maps the token's subject to a
// user and looks that user up in `memberships`. A service client has no
// membership and never will, so the interceptor would resolve it to "no
// organisation" and the sweep would run against the nil uuid, touch nothing,
// and report success. Silently doing nothing is the worst available outcome
// for a trigger whose whole purpose is to make something happen.
//
// So this handler reads the organisation header itself and opens its own
// transaction on the producer pool. The organisation is still never in the
// path, and it is still verified: the agent's policies scope every write to the
// organisation the GUC names (00008), so a header naming one organisation
// cannot reach another's data.
//
// What is NOT checked here, and must not be: whether the caller belongs to the
// organisation. A machine client belongs to none. The gate is the
// `internal:ingest` scope, which the seed issues to service clients through
// client credentials and never to the browser client.
package sweep

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Producer is what this handler needs of the agent pool, declared where it is
// used rather than exported from the store (§21.6).
type Producer interface {
	RunSweep(ctx context.Context, orgID string, detectOnly bool) (postgres.Sweep, error)
	RunAnalyst(ctx context.Context, orgID string) (postgres.Analysis, error)
	ExpireSnoozes(ctx context.Context) (postgres.Expiry, error)
	PendingSweepTriggers(ctx context.Context, limit int) ([]postgres.SweepTrigger, error)
	SettleSweepTrigger(ctx context.Context, id string, cause error) (bool, error)
	SweepTargets(ctx context.Context) ([]string, error)
}

// Bounds on one listing. What is not listed now is listed on the next relay
// tick, a few seconds later; a backlog that hits this is a worker that was
// down for a while.
const (
	DefaultListLimit = 200
	MaxListLimit     = 1000
)

// Service implements internalv1connect.SweepServiceHandler.
type Service struct {
	producer Producer
}

func New(producer Producer) *Service { return &Service{producer: producer} }

// RunSweep runs the Watcher and then the Analyst for one organisation.
func (s *Service) RunSweep(
	ctx context.Context,
	req *connect.Request[platformv1.RunSweepRequest],
) (*connect.Response[platformv1.RunSweepResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	orgID := req.Header().Get(interceptor.OrgHeader)
	if orgID == "" {
		// Refused rather than defaulted. There is no sensible default: the
		// alternatives are "do nothing", which looks like success, and "sweep
		// everyone", whose blast radius is every customer at once.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("a sweep names one organisation; send the "+
				interceptor.OrgHeader+" header"))
	}

	result, err := s.producer.RunSweep(ctx, orgID, req.Msg.GetDetectOnly())
	if errors.Is(err, postgres.ErrBadOrganisation) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the organisation header is not a uuid"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.RunSweepResponse{
		Signals:  result.Signals,
		Findings: result.Findings,
		RanAt:    timestamppb.New(result.RanAt),
	}), nil
}

// ExpireSnoozes brings back every finding whose deferral has run out, across
// every organisation at once.
//
// No organisation header is read and none is required, which is the one way
// this differs from RunSweep and is deliberate: see the proto and 00034 for
// why a maintenance pass over every tenant is not a sweep. The gate is the
// same `internal:ingest` scope, issued to service clients only, and the
// Temporal worker in `workers` is the caller this exists for (ENT-256).
func (s *Service) ExpireSnoozes(
	ctx context.Context,
	_ *connect.Request[platformv1.ExpireSnoozesRequest],
) (*connect.Response[platformv1.ExpireSnoozesResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	result, err := s.producer.ExpireSnoozes(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&platformv1.ExpireSnoozesResponse{
		Reemerged: result.Reemerged,
		RanAt:     timestamppb.New(result.RanAt),
	}), nil
}

// RunAnalyst runs the Analyst alone for one organisation (ENT-256, part four).
func (s *Service) RunAnalyst(
	ctx context.Context,
	req *connect.Request[platformv1.RunAnalystRequest],
) (*connect.Response[platformv1.RunAnalystResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}

	orgID := req.Header().Get(interceptor.OrgHeader)
	if orgID == "" {
		// Refused rather than defaulted, for RunSweep's reason.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("an analysis names one organisation; send the "+
				interceptor.OrgHeader+" header"))
	}

	result, err := s.producer.RunAnalyst(ctx, orgID)
	if errors.Is(err, postgres.ErrBadOrganisation) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the organisation header is not a uuid"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.RunAnalystResponse{
		Findings: result.Findings,
		RanAt:    timestamppb.New(result.RanAt),
	}), nil
}

// ListSweepTriggers lists the sweeps somebody asked for and nothing has run
// (00035).
func (s *Service) ListSweepTriggers(
	ctx context.Context,
	req *connect.Request[platformv1.ListSweepTriggersRequest],
) (*connect.Response[platformv1.ListSweepTriggersResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	limit := int(req.Msg.GetLimit())
	switch {
	case limit <= 0:
		limit = DefaultListLimit
	case limit > MaxListLimit:
		limit = MaxListLimit
	}

	triggers, err := s.producer.PendingSweepTriggers(ctx, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := &platformv1.ListSweepTriggersResponse{}
	for _, t := range triggers {
		res.Triggers = append(res.Triggers, &platformv1.SweepTrigger{
			TriggerId: t.ID, OrgId: t.OrgID, Reason: t.Reason,
		})
	}
	return connect.NewResponse(res), nil
}

// SettleSweepTrigger records what a triggered sweep did.
func (s *Service) SettleSweepTrigger(
	ctx context.Context,
	req *connect.Request[platformv1.SettleSweepTriggerRequest],
) (*connect.Response[platformv1.SettleSweepTriggerResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	if req.Msg.GetTriggerId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("settling names one trigger; send trigger_id"))
	}
	var cause error
	switch req.Msg.GetOutcome() {
	case platformv1.SettleSweepTriggerRequest_OUTCOME_DONE:
	case platformv1.SettleSweepTriggerRequest_OUTCOME_FAILED:
		msg := req.Msg.GetError()
		if msg == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("a failed attempt records why; send error"))
		}
		cause = fmt.Errorf("%s", msg)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("settling names an outcome; send done or failed"))
	}

	settled, err := s.producer.SettleSweepTrigger(ctx, req.Msg.GetTriggerId(), cause)
	if errors.Is(err, postgres.ErrBadTriggerID) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("trigger_id is not a uuid"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.SettleSweepTriggerResponse{Settled: settled}), nil
}

// ListSweepTargets lists every organisation a scheduled sweep should visit.
func (s *Service) ListSweepTargets(
	ctx context.Context,
	_ *connect.Request[platformv1.ListSweepTargetsRequest],
) (*connect.Response[platformv1.ListSweepTargetsResponse], error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	ids, err := s.producer.SweepTargets(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&platformv1.ListSweepTargetsResponse{OrgIds: ids}), nil
}
