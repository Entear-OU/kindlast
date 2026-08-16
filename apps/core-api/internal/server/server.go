package server

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/dashboard"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	sweepservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/sweep"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// Dependencies is everything the mux needs, supplied by main.
//
// Interfaces rather than concrete types, so this package knows that tokens are
// verified and that transactions carry tenancy, without knowing that one
// involves an HTTP client and the other a connection pool (§21.6: main is the
// only place that knows a Postgres URL and a Redis address exist together).
type Dependencies struct {
	Verifier interceptor.TokenVerifier
	DenyList interceptor.DenyList
	Tenants  interceptor.TenantOpener

	// Profiles resolves a display name and email when the access token carries
	// neither. Nil is allowed: provisioning then names an organisation from the
	// subject claim, which is worse and not broken.
	Profiles session.Profiles

	// Ready reports whether the service's dependencies are reachable. Nil
	// means always ready.
	Ready func(context.Context) error

	// Producer runs the sweeps, on the kindlast_agent pool. Nil is supported
	// and means SweepService is not served at all: a deployment that wants no
	// on-demand trigger leaves KINDLAST_AGENT_DATABASE_URL unset, which is
	// better than serving an endpoint that fails on every call.
	Producer sweepservice.Producer

	// HumanClientID is the OAuth client whose tokens carry the human scope set
	// (ENT-221). Empty leaves the scope interceptor reading granted scopes for
	// every caller, which is the pre-ENT-221 behaviour.
	HumanClientID string

	// BillingEnabled turns plan gating on for the act path. False, the zero
	// value, is the self-hosted default and leaves the Executor ungated
	// (§18.1). See config.Config.BillingEnabled for why this is configuration
	// rather than something inferred from the subscriptions table.
	BillingEnabled bool
}

// New builds the HTTP handler core-api serves.
//
// The interceptor order is fixed here and nowhere else, so there is one place
// to read it and one place it can be got wrong. See the package comment on
// `interceptor` for why it is this order.
func New(deps Dependencies) (http.Handler, error) {
	scopes, err := interceptor.NewScope(Services(),
		interceptor.WithHumanClient(deps.HumanClientID))
	if err != nil {
		// The binary does not start. An RPC with no declared scope would
		// otherwise be reachable with any valid token, and a process that
		// refuses to come up is the loudest possible version of failing closed
		// (§1.3).
		return nil, fmt.Errorf("server: %w", err)
	}

	chain := connect.WithInterceptors(
		interceptor.Auth(deps.Verifier),
		interceptor.JTI(deps.DenyList),
		scopes.Interceptor(),
		interceptor.Tenancy(deps.Tenants),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(deps.Profiles), chain))
	mux.Handle(corev1connect.NewOrgServiceHandler(org.New(), chain))
	mux.Handle(corev1connect.NewFindingsServiceHandler(findings.New(deps.BillingEnabled), chain))
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboard.New(), chain))
	mux.Handle(corev1connect.NewRecordsServiceHandler(records.New(deps.BillingEnabled), chain))

	// The internal surface runs on a SHORTER chain: authentication, revocation
	// and scope, but no tenancy. That is deliberate and it is the one place in
	// this file worth stopping on.
	//
	// Tenancy resolves the caller's membership, and a service client has none.
	// The interceptor would resolve it to "no organisation", the sweep would run
	// against the nil uuid, touch nothing, and report success. A trigger that
	// silently does nothing is worse than one that refuses.
	//
	// What replaces it is not nothing: the `internal:ingest` scope is issued
	// only to service clients, and the agent role's policies scope every write
	// to the organisation the header names (00008).
	if deps.Producer != nil {
		internal := connect.WithInterceptors(
			interceptor.Auth(deps.Verifier),
			interceptor.JTI(deps.DenyList),
			scopes.Interceptor(),
		)
		mux.Handle(platformv1connect.NewSweepServiceHandler(
			sweepservice.New(deps.Producer), internal))
	}

	// Unauthenticated by design, and bound to the internal listener only.
	// Requiring a credential here is a common reflex that breaks orchestrator
	// probes for no security gain, because they expose nothing a
	// network-adjacent attacker could use (§1.7).
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if deps.Ready != nil {
			if err := deps.Ready(r.Context()); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, "not ready: %v\n", err)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})

	return mux, nil
}
