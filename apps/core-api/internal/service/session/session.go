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
	"log/slog"

	"connectrpc.com/connect"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
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

// Profiles resolves the human-readable identity an access token leaves out.
//
// Declared here rather than imported, for the §21.6 reason the `provisioning`
// interface above exists: this package says what it needs, and the adapter in
// internal/identity satisfies it without this package learning what HTTP is.
type Profiles interface {
	Profile(ctx context.Context, accessToken, subject string) (*oidc.Profile, error)
}

// Service implements corev1connect.SessionServiceHandler.
type Service struct {
	// profiles may be nil, which means "no provider to ask". Provisioning then
	// falls back to the token's own claims, which is the pre-ENT-197 behaviour
	// and still produces a usable organisation.
	profiles Profiles
}

func New(profiles Profiles) *Service { return &Service{profiles: profiles} }

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

	// Only when something is about to be named after this person, which mirrors
	// PlanFor's own rule. Doing it unconditionally would put a call to the
	// authorization server in the path of every page render, which is the
	// single point of failure local verification exists to avoid (§1.4).
	if len(memberships) == 0 {
		subject = s.withProfile(ctx, subject)
	}

	if plan := org.PlanFor(subject, memberships); plan.CreatePersonalOrganisation {
		if _, err := store.ProvisionPersonalOrganisation(ctx, plan); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}

		// Again, now that the profile is known. The first call above ran before
		// userinfo and is the concurrency mutex described there, so it cannot
		// move; this one records the email that call could not yet have.
		if err := store.RecordIdentity(ctx, subject); err != nil {
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
			Id: claims.Subject,
			// From `subject` rather than `claims`, so a profile fetched during
			// provisioning is reflected in the same response that created the
			// organisation. On every later call these are the token's claims
			// again, so a provider that omits them answers with empty strings;
			// filling those in for returning callers needs a cache, and is the
			// profile surface's problem rather than this endpoint's.
			Email:         subject.Email,
			DisplayName:   subject.DisplayName,
			EmailVerified: claims.EmailVerified,
		},
		Memberships: toProto(memberships),
		ActiveOrgId: activeOrg,
		Plan:        subscriptionPlan,
	}), nil
}

// withProfile fills in a name and email the access token did not carry.
//
// Every failure here returns the subject unchanged, deliberately and quietly.
// The caller is midway through giving a new person somewhere to be, and there
// is no failure of this function that is worth turning into a signed-in user
// with no organisation: a provider that declares no userinfo endpoint, one
// that is down, one that answers about the wrong subject. The result is an
// organisation named from the `sub` claim, which is ugly and recoverable,
// against a request that fails, which is neither.
//
// Skipped entirely when the token already carries either claim, so a
// conformant provider never causes a network call from this path.
func (s *Service) withProfile(ctx context.Context, subject org.Subject) org.Subject {
	if subject.DisplayName != "" || subject.Email != "" {
		return subject
	}
	if s.profiles == nil {
		return subject
	}

	token, ok := interceptor.TokenFrom(ctx)
	if !ok {
		return subject
	}

	profile, err := s.profiles.Profile(ctx, token, subject.Subject)
	if err != nil {
		// The subject is not in the message: this line reaches a log and the
		// subject identifies a person.
		slog.WarnContext(ctx, "provisioning without a profile; naming from the subject claim",
			slog.String("reason", err.Error()))
		return subject
	}

	subject.Email = profile.Email
	subject.DisplayName = profile.Name
	return subject
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
