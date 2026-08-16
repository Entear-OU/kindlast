// Package dashboard serves DashboardService.
//
// One RPC, and the whole point of it is the fourth posture band. Everything
// else here is counting.
package dashboard

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

type reading interface {
	Dashboard(ctx context.Context) (domain.Dashboard, error)
}

// Service implements corev1connect.DashboardServiceHandler.
type Service struct{}

func New() *Service { return &Service{} }

// GetDashboard returns posture, open counts and pipeline status.
func (s *Service) GetDashboard(
	ctx context.Context,
	_ *connect.Request[corev1.GetDashboardRequest],
) (*connect.Response[corev1.GetDashboardResponse], error) {
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
			errors.New("the tenant transaction cannot read the dashboard"))
	}

	d, err := store.Dashboard(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	out := &corev1.GetDashboardResponse{
		Posture:         string(d.Posture),
		PostureHeadline: d.Headline,
		OpenTotal:       int32(d.Counts.Total()),
		OpenBySeverity: &corev1.SeverityCounts{
			Critical: int32(d.Counts.Critical),
			High:     int32(d.Counts.High),
			Medium:   int32(d.Counts.Medium),
			Low:      int32(d.Counts.Low),
		},
		Pipeline: &corev1.PipelineStatus{
			ProfileExists: d.Pipeline.ProfileExists,
		},
	}
	// Left absent rather than zeroed when the Watcher has never run. A zero
	// timestamp renders as 1970, which reads as "ran a long time ago" and is
	// the opposite of what it means.
	if d.Pipeline.WatcherLastRunAt != nil {
		out.Pipeline.WatcherLastRunAt = timestamppb.New(*d.Pipeline.WatcherLastRunAt)
	}

	return connect.NewResponse(out), nil
}
