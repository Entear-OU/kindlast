// Package session serves SessionService.
//
// Thin, per §21.6: validate, call the domain rule, call the store, map errors
// to Connect codes. The decision this endpoint makes lives in
// domain/org.PlanFor, and the write lives in store/postgres.
package session

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// provisioning is what this handler needs of the request's transaction.
//
// Declared here, and satisfied by the Postgres tenant, so the interceptor
// package never learns what an organisation is. The alternative, widening
// interceptor.Tenant with these methods, would drag domain types into
// middleware and break the rule that dependencies point inward (§21.6).
type provisioning interface {
	Memberships(context.Context) ([]org.Membership, error)
	RecordIdentity(context.Context, org.Subject) error
	ProvisionPersonalOrganisation(context.Context, org.Plan) (bool, error)
	UseOrganisation(context.Context, string) error
	Plan(context.Context) (string, error)
}

// Service implements corev1connect.SessionServiceHandler.
type Service struct{}

func New() *Service { return &Service{} }

// GetCurrentUser returns the caller, their memberships, the active
// organisation and its plan, provisioning on first arrival.
//
// Just-in-time provisioning here rather than at a dedicated endpoint, and the
// reasoning is worth keeping (§1.8). A webhook from the IdP would need
// configuring per deployment, which breaks self-hosting, and it races the
// redirect. An explicit provision call after callback is a second round trip
// that can fail, leaving a signed-in user with no organisation. This call is
// already the first thing every authenticated page makes, it is idempotent on
// the subject, and it works against any OIDC provider rather than only the
// bundled one.
func (s *Service) GetCurrentUser(
	ctx context.Context,
	_ *connect.Request[corev1.GetCurrentUserRequest],
) (*connect.Response[corev1.GetCurrentUserResponse], error) {
	claims, ok := interceptor.ClaimsFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}

	store, ok := tenant.(provisioning)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction does not support provisioning"))
	}

	subject := org.Subject{
		Issuer:      claims.Issuer,
		Subject:     claims.Subject,
		Email:       claims.Email,
		DisplayName: claims.Name,
	}

	// The reverse mapping for the one-way subject derivation, recorded on
	// every call rather than only on the first, so an email change on the
	// token is reflected and a row that somehow went missing is restored.
	//
	// Its position here is load-bearing, and that was measured rather than
	// intended. Every concurrent first request for the same subject upserts
	// the same user_identities primary key, so all but one block here until
	// the first commits, and when they unblock their next read sees the
	// committed membership and decides to create nothing. That is why eight
	// simultaneous tabs produce one organisation even with the partial unique
	// index removed; take this call out and the same test produces eight.
	//
	// So this is the mutex and the index is the backstop, not the other way
	// round. Moving this below the membership read reopens the race, and the
	// index is then the only thing standing between a user and two personal
	// organisations.
	if err := store.RecordIdentity(ctx, subject); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	memberships, err := store.Memberships(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if plan := org.PlanFor(subject, memberships); plan.CreatePersonalOrganisation {
		if _, err := store.ProvisionPersonalOrganisation(ctx, plan); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}

		// Re-read rather than trusting what was just written. Under the
		// two-tab race the other transaction may be the one whose organisation
		// survived, and the answer this returns has to be what the database
		// holds, not what this request attempted (§1.8).
		memberships, err = store.Memberships(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if len(memberships) == 0 {
			return nil, connect.NewError(connect.CodeInternal,
				fmt.Errorf("provisioning left %s with no membership", claims.Subject))
		}
	}

	activeOrg := tenant.OrgID()
	if !belongsTo(memberships, activeOrg) && len(memberships) > 0 {
		// True immediately after provisioning: the interceptor resolved the
		// active organisation before this handler ran, when there was nothing
		// to resolve. Point the transaction at the real one so the plan below
		// reads from it.
		activeOrg = memberships[0].OrgID
		if err := store.UseOrganisation(ctx, activeOrg); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	subscriptionPlan, err := store.Plan(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.GetCurrentUserResponse{
		User: &corev1.User{
			Id:            claims.Subject,
			Email:         claims.Email,
			DisplayName:   claims.Name,
			EmailVerified: claims.EmailVerified,
		},
		Memberships: toProto(memberships),
		ActiveOrgId: activeOrg,
		Plan:        subscriptionPlan,
	}), nil
}

func belongsTo(memberships []org.Membership, orgID string) bool {
	for _, m := range memberships {
		if m.OrgID == orgID {
			return true
		}
	}
	return false
}

func toProto(memberships []org.Membership) []*corev1.Membership {
	out := make([]*corev1.Membership, 0, len(memberships))
	for _, m := range memberships {
		out = append(out, &corev1.Membership{
			OrgId:   m.OrgID,
			OrgName: m.OrgName,
			Role:    m.Role,
		})
	}
	return out
}
