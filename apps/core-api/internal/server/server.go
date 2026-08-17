package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/dashboard"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/notifications"
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
	//
	// RecordsService does NOT take it. The manual-entry cap is decided by
	// `ropa_manual_activity_limit()`, which reads the same flag from a session
	// GUC the store sets: one answer to the question, so the cap a console shows
	// and the cap a write meets cannot disagree.
	BillingEnabled bool

	// AppBaseURL is where a browser reaches the console. OrgService renders the
	// invitation link from it at mint (ENT-219), and refuses to invite when it
	// is empty, because an invitation whose link cannot be built is one nobody
	// can ever accept or repair.
	AppBaseURL string

	// SMTPConfigured decides what the notification capabilities endpoint says
	// about the email channel (§18.3, ENT-209).
	//
	// Configuration rather than a probe. A mail server that happens to be down
	// is not the same as a deployment that has never been told where to submit
	// mail, and only the second is worth telling a person about on a settings
	// page: the first resolves itself and the queue survives it.
	SMTPConfigured bool

	// Tokens redeems capability tokens for callers with no session (ENT-209).
	//
	// Optional: nil leaves the unsubscribe endpoint answering 501 rather than
	// panicking, which is the honest reply from a deployment that has not wired
	// it, and keeps this dependency from being one every test has to supply.
	Tokens CapabilityTokens

	// BillingWebhook serves the payment provider's callback (ENT-210).
	//
	// Nil when this deployment has not configured billing, and then the route
	// is not registered at all. That is billing-optional expressed at the
	// routing layer rather than as a runtime refusal: a self-hoster who sells
	// nothing has no endpoint for anybody to probe.
	//
	// A handler rather than a store, so this package depends on neither a
	// database driver nor a payment provider.
	BillingWebhook http.HandlerFunc
}

// CapabilityTokens redeems a link that acts without a session.
//
// An interface here so this package does not depend on a database driver, the
// same reason TenantOpener is one. Implemented by the Postgres store on the
// application pool: redemption goes through a SECURITY DEFINER function,
// because a caller with no session sets no tenancy GUCs and every policy in the
// schema would otherwise refuse.
type CapabilityTokens interface {
	// Takes the raw token, not its hash: the store hashes with the same
	// function the mint side uses, so the two cannot drift.
	RedeemCapabilityToken(ctx context.Context, token, kind string) (string, error)
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
	mux.Handle(corev1connect.NewOrgServiceHandler(org.New(deps.AppBaseURL), chain))
	mux.Handle(corev1connect.NewFindingsServiceHandler(findings.New(deps.BillingEnabled), chain))
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboard.New(), chain))
	mux.Handle(corev1connect.NewRecordsServiceHandler(records.New(), chain))
	mux.Handle(corev1connect.NewNotificationServiceHandler(
		notifications.New(deps.SMTPConfigured), chain))

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

	// The payment provider's webhook (§0.2's second justified exception,
	// ENT-210).
	//
	// Served only when a deployment has configured billing. Absent, the route
	// does not exist rather than existing and refusing, which is
	// billing-optional (§18.1) expressed at the routing layer: a self-hoster who
	// sells nothing has no endpoint for anybody to probe, and a provider
	// misconfigured to point here gets a 404 rather than a 500 it will retry
	// for days.
	if deps.BillingWebhook != nil {
		mux.HandleFunc("POST /api/v1/billing/webhook", deps.BillingWebhook)
	}

	// Unsubscribing, for somebody with no session at all (§8, ENT-209).
	//
	// # WHY THIS IS NOT AN RPC
	//
	// Every method on the Connect chain runs behind authentication, a scope
	// check and tenancy. The person clicking this link has none of those: they
	// are reading an email, they may never have signed in, and requiring them to
	// authenticate in order to stop mail they did not ask for is how a product
	// earns a spam complaint instead of an unsubscribe.
	//
	// So the token is the only identity claim, which is why it is stored hashed,
	// expires, and is single use, and why the function behind this answers
	// identically for expired, already redeemed, wrong kind and never existed.
	// A caller who has proved nothing must not be able to learn which tokens are
	// real by comparing responses.
	//
	// # WHY POST AND NOT GET
	//
	// A GET that changes something is wrong in principle and dangerous in
	// practice here, because corporate mail gateways and link scanners follow
	// every URL in a message before a human sees it. Under a GET, the act of
	// receiving the email would unsubscribe the recipient, silently, and the
	// symptom would be a customer who stops getting compliance notifications for
	// reasons nobody can reconstruct. Web renders a page on GET and posts here.
	mux.HandleFunc("POST /api/v1/unsubscribe", func(w http.ResponseWriter, r *http.Request) {
		if deps.Tokens == nil {
			http.Error(w, "unsubscribe is not available", http.StatusNotImplemented)
			return
		}

		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.Token == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// The raw token goes to the store, which hashes it with the same
		// function the mint side uses. Hashing here would put the two halves in
		// different packages and invite them to drift, which is the arrangement
		// `invitations` already avoids.
		orgID, err := deps.Tokens.RedeemCapabilityToken(r.Context(), body.Token, "unsubscribe")
		if err != nil {
			// One answer for every unusable token, and 404 rather than 401,
			// because 401 would invite a client to authenticate and there is
			// nothing to authenticate as.
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"orgId":%q}`, orgID)
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
