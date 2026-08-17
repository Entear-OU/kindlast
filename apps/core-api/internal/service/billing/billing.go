// Package billing serves BillingService: what an organisation is on, and what
// a console may offer it (ENT-210).
//
// Read only. There is no RPC here that changes a plan, deliberately: a plan
// changes because a payment provider said so, through the signed webhook, and
// an endpoint that could change one from a session would be a way to grant
// yourself a paid entitlement with a request.
package billing

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// Subscription is what the store reports about an organisation's billing.
type Subscription struct {
	// Plan in force, taking status into account: a cancelled or past_due `pro`
	// row yields `free` here.
	Plan string
	// The raw status, so a console can distinguish a downgrade the customer
	// chose from a payment that failed. Empty when there is no row.
	Status          string
	PeriodEnd       *timestamppb.Timestamp
	HasSubscription bool
}

// reading is what this handler needs of the request's transaction, declared
// where it is used (§21.6).
type reading interface {
	Subscription(ctx context.Context) (Subscription, error)
	Role() string
}

// Service implements corev1connect.BillingServiceHandler.
type Service struct {
	// configured is whether this deployment has a payment provider wired at
	// all. Read from configuration rather than inferred from the absence of a
	// subscription row: a hosted customer between provisioning and their first
	// row looks identical, and would be shown a page telling them their own
	// product cannot be bought.
	configured bool
	// gating is whether any feature is withheld for being on the free plan.
	// Independent of `configured`, because an operator may wire a provider
	// before turning gating on.
	gating bool
}

func New(configured, gating bool) *Service {
	return &Service{configured: configured, gating: gating}
}

func (s *Service) GetBilling(
	ctx context.Context,
	_ *connect.Request[corev1.GetBillingRequest],
) (*connect.Response[corev1.GetBillingResponse], error) {
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
			errors.New("the tenant transaction cannot read billing"))
	}

	// Owner-only (§20.1). A plan and a renewal date are commercial facts about
	// the company, and the role that manages them is the one that signed for
	// them.
	//
	// Refused in the handler as well as by the scope, because the scope says
	// what a token may attempt and this says who inside the organisation may.
	// A member holding a console token carries `billing:read` like everybody
	// else; the role is what separates them.
	if store.Role() != "owner" {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only an owner can see billing"))
	}

	subscription, err := store.Subscription(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetBillingResponse{
		Plan:              subscription.Plan,
		Status:            subscription.Status,
		CurrentPeriodEnd:  subscription.PeriodEnd,
		BillingConfigured: s.configured,
		GatingEnabled:     s.gating,
	}), nil
}
