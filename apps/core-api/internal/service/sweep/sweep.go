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
}

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
