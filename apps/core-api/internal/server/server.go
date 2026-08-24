package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	apikeysservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/apikeys"
	auditservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/audit"
	billingservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/billing"
	completionservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/completion"
	conversationservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/conversation"
	corpusservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/corpus"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/dashboard"
	deliveryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/delivery"
	executorservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/executor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/findings"
	ingestservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/ingest"
	memoryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	narrativeservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/narrative"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/notifications"
	onboardingservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/onboarding"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	sweepservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/sweep"
	watcherservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/watcher"
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

	// Delegations resolves the credential an agent presents to act for a
	// person (ENT-230).
	//
	// Nil is supported and fails closed: a request presenting a delegation is
	// refused rather than being run as the machine principal that sent it. That
	// is the only safe reading of an unwired resolver, because the caller's
	// intent was to act as somebody with less authority, not more.
	Delegations interceptor.DelegationResolver

	// APIKeys authenticates a partner's key (ENT-262).
	//
	// Nil disables the whole credential model: a request under the `ApiKey`
	// scheme is then refused at authentication rather than falling through to
	// the bearer path, so a caller presenting a good key is told their key is
	// not usable rather than told they presented no token. Every deployment
	// wires it, and the nil case exists for the internal chain, which takes no
	// authenticator on purpose.
	APIKeys interceptor.Authenticator

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

	// Outbox is the transactional outbox on the same agent pool, served as
	// DeliveryService to the Temporal worker (ENT-256, part three). Nil means
	// the service is not served, which goes with Producer: no agent pool, no
	// internal surface.
	Outbox deliveryservice.Outbox

	// ExecutorJobs lists approvals whose record has not been created, on the
	// producer pool; Executions creates it, on the application pool as the
	// approver (ENT-271). Nil for either means ExecutorService is not served,
	// which goes with having no agent pool.
	ExecutorJobs executorservice.Jobs
	Executions   executorservice.Executions

	// Watcher is what an agentic Watcher reads and writes (ENT-258), on the
	// producer pool. Registered with Producer, because they are the same pool
	// and the same supported absence.
	Watcher watcherservice.Producer
	// Channels is every channel DeliveryService can send on (ENT-263). An
	// empty or nil router is the supported state before KINDLAST_SMTP_ADDR and
	// KINDLAST_TELEGRAM_BOT_TOKEN are set: the rows queue, the list and the
	// reclaim still answer, and a delivery is refused with a message naming
	// the settings rather than the service being absent. Finding notifications
	// also need AppBaseURL below, for the links they carry.
	//
	// It is also what the capabilities endpoint reads, so a console never
	// offers a channel this deployment cannot deliver on, and it reads the
	// same value the dispatcher sends through rather than a parallel boolean
	// that could disagree with it.
	Channels *delivery.Router

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

	// Tokens redeems capability tokens for callers with no session (ENT-209).
	//
	// Optional: nil leaves the unsubscribe endpoint answering 501 rather than
	// panicking, which is the honest reply from a deployment that has not wired
	// it, and keeps this dependency from being one every test has to supply.
	Tokens CapabilityTokens

	// Approvals spends §8's one-tap approve link (ENT-249).
	//
	// Optional, and nil leaves the endpoint answering 501 rather than
	// panicking, exactly like Tokens above: a deployment that has not wired a
	// database has no approve path, and saying so is better than a stack trace.
	Approvals Approvals

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

	// Corpus writes the regulatory corpus, on the kindlast_ingest pool
	// (ENT-207).
	//
	// Nil when this deployment has not configured one, and then IngestService is
	// not registered at all. An interface rather than the concrete store, so
	// this package keeps depending on no database driver.
	Corpus ingestservice.Writer

	// AgentRuns records what an agent run did, on the kindlast_agent pool
	// (ENT-218, §26.3).
	//
	// A separate field from Corpus above because the two are different roles on
	// different pools: `kindlast_ingest` can write the corpus and reach nothing
	// else, `kindlast_agent` can insert a run record and cannot touch the
	// corpus. Nil when this deployment runs no agents, and then RecordAgentRun
	// answers Unimplemented rather than panicking.
	AgentRuns ingestservice.RunRecorder

	// Evidence records what a machine fetched from a customer's system
	// (ENT-231), on the same kindlast_agent pool.
	//
	// A separate field from AgentRuns for the reason that one is separate from
	// Corpus: a dependency is a statement about what a caller may do, and
	// "record that a run happened" and "write into an organisation's memory"
	// are different permissions even when they travel on one connection pool.
	Evidence ingestservice.EvidenceRecorder

	// Narratives reads findings that have none and records what a run produced,
	// on the kindlast_agent pool (ENT-245).
	//
	// Nil when this deployment runs no agents, and then NarrativeService is not
	// registered at all.
	Narratives narrativeservice.Findings

	// Drafter is Intelligence, when this deployment has one (ENT-245).
	//
	// NIL IS A SUPPORTED DEPLOYMENT AND NOT A MISCONFIGURATION. The model
	// service sits behind a compose profile, so a stack can run without it, and
	// then findings simply carry the deterministic text the sweep wrote. The
	// handler says so in its response rather than failing, because "we have no
	// model" and "the model is broken" want different reactions from an
	// operator.
	Drafter narrativeservice.Drafter

	// Answerer is Intelligence again, for the question a person asks about a
	// finding (ENT-270). In production it is the same client as Drafter, and it
	// is a second field rather than a widened interface because the two callers
	// need different halves of that service and §21.6 wants each dependency
	// declared as what its user actually calls. Nil is the same supported
	// deployment Drafter's nil is, and the handler answers
	// `intelligence_available: false` for it.
	Answerer conversationservice.Answerer

	// ModelRouter answers where an organisation's completions go: the
	// deployment's own model, or the provider it chose (ENT-236), with the
	// sealed key opened only here in Go (ENT-256, part five). Two services
	// ask it: NarrativeService, to refuse a batch whose provider cannot be
	// honoured before it starts, and CompletionService, to make the call.
	// Always set by main, even on a deployment with no model: the resolver
	// then refuses every completion with a reason, which is better than a
	// service that is absent.
	ModelRouter *modelroute.Resolver

	// Integrations serves the console's control over which customer systems
	// Kindlast may reach (ENT-231).
	//
	// Nil when this deployment has no gateway, and then IntegrationsService is
	// not registered at all rather than registered and failing on every call.
	// A console that shows an Integrations page whose every button returns an
	// error is worse than one that shows no page: the first looks broken, the
	// second looks unconfigured, and only one of those sends an operator to
	// the right place.
	Integrations corev1connect.IntegrationsServiceHandler

	// ModelChoice serves where an organisation's model runs (ENT-236).
	//
	// Registered on every deployment, including one that permits no hosted
	// provider, and that is deliberate. A stack with the surface missing tells
	// a member nothing; a stack with it present answers "this deployment runs
	// its own model and permits no other", which is the true and useful answer
	// and the one somebody checking where their data is processed came for.
	ModelChoice corev1connect.ModelServiceHandler

	// Logger is what the internal handlers report to. An ingest that refused a
	// pack has to say so somewhere a person will look, because its caller is a
	// schedule rather than a browser.
	Logger *slog.Logger
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

// Approvals redeems an approve link and performs the approval it authorises.
//
// Declared here rather than exported from the store, for the same reason
// CapabilityTokens is: this package depends on no database driver. One method,
// and it returns the organisation slug because the interstitial's next move is
// to send the person into `/o/{slug}/`, which §8 requires to be the
// organisation the credential named rather than wherever a session pointed.
//
// `applied` false means the finding was already approved. Not an error: a
// second click, a retry and a colleague who got there first are the same
// non-event, and the person should be told it is done.
type Approvals interface {
	ApproveFromEmail(ctx context.Context, token, findingID string) (orgSlug string, applied bool, err error)
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
		interceptor.Auth(deps.Verifier, interceptor.WithAPIKeys(deps.APIKeys)),
		interceptor.JTI(deps.DenyList),
		// Acting for a person (ENT-230). A no-op for every request that does not
		// present a delegation, which is all of them today, and the stage that
		// decides who the two below are talking about when one does.
		interceptor.ActOnBehalf(deps.Delegations),
		scopes.Interceptor(),
		interceptor.Tenancy(deps.Tenants),
	)

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(deps.Profiles), chain))
	mux.Handle(corev1connect.NewOrgServiceHandler(org.New(deps.AppBaseURL, deps.Profiles), chain))
	mux.Handle(corev1connect.NewFindingsServiceHandler(findings.New(deps.BillingEnabled), chain))
	mux.Handle(corev1connect.NewDashboardServiceHandler(dashboard.New(), chain))
	// Asking the Analyst about one finding (ENT-270). Registered
	// unconditionally, like FindingsService and unlike NarrativeService: a
	// deployment with no model is supported and the handler says so in its
	// response, and registering only when an answerer exists would make "no
	// model here" and "wrong URL" the same 404.
	mux.Handle(corev1connect.NewConversationServiceHandler(
		conversationservice.New(deps.Answerer, deps.Logger,
			conversationservice.WithRouter(conversationRouterOrNil(deps.ModelRouter))),
		chain))
	mux.Handle(corev1connect.NewRecordsServiceHandler(records.New(), chain))
	mux.Handle(corev1connect.NewNotificationServiceHandler(
		notifications.New(deps.Channels), chain))
	mux.Handle(corev1connect.NewBillingServiceHandler(
		billingservice.New(deps.BillingWebhook != nil, deps.BillingEnabled), chain))
	// The audit log (ENT-223). On the same chain as everything else, and with
	// no configuration switch: unlike billing, there is no deployment where
	// this surface is absent. A compliance record whose audit view depends on
	// how the operator configured the stack is not one an auditor can rely on.
	mux.Handle(corev1connect.NewAuditServiceHandler(auditservice.New(), chain))
	// The regulatory corpus (ENT-207). Read only and unconditional: there is no
	// deployment where a customer should be unable to look up the obligation a
	// finding cites.
	mux.Handle(corev1connect.NewCorpusServiceHandler(corpusservice.New(), chain))
	// What Kindlast knows about the organisation (ENT-228). On the tenant
	// chain, because unlike the corpus this is the customer's own data and RLS
	// is what keeps one organisation's profile out of another's console.
	mux.Handle(corev1connect.NewMemoryServiceHandler(memoryservice.New(), chain))
	// The first conversation (ENT-212). On the tenant chain and with no
	// configuration switch, because the interview is scripted and typed in Go:
	// there is no deployment where onboarding is unavailable, including one
	// with no model. That is ENT-212's "degrades to a form rather than failing"
	// arranged so there is no second path to keep working.
	mux.Handle(corev1connect.NewOnboardingServiceHandler(onboardingservice.New(), chain))
	// Partner API keys (ENT-262). On the tenant chain and with no configuration
	// switch, because a key is a tenant credential like everything else here:
	// RLS is what keeps one organisation's keys out of another's console, and
	// there is no deployment where the credential a customer's own integration
	// uses should be unavailable to them.
	mux.Handle(corev1connect.NewApiKeyServiceHandler(apikeysservice.New(), chain))
	// Connecting a customer's own systems (ENT-231). On the tenant chain,
	// because a connection belongs to one organisation and RLS is what keeps
	// one customer's endpoints and credentials out of another's console.
	//
	// Registered only when a gateway is configured. core-api itself opens no
	// connection to a customer-supplied address, so without the gateway there
	// is nothing behind this surface at all.
	if deps.Integrations != nil {
		mux.Handle(corev1connect.NewIntegrationsServiceHandler(deps.Integrations, chain))
	}
	// Where this organisation's model runs (ENT-236). On the tenant chain: the
	// choice belongs to one organisation, RLS is what keeps one customer's
	// provider key out of another's console, and the owner check that guards
	// the write reads the role the tenancy interceptor resolved.
	if deps.ModelChoice != nil {
		mux.Handle(corev1connect.NewModelServiceHandler(deps.ModelChoice, chain))
	}

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
	//
	// AND IT DOES NOT CARRY ActOnBehalf, WHICH IS NOT AN OVERSIGHT (ENT-230).
	//
	// A delegation replaces the caller's scopes with what a person holds, and a
	// person holds no `internal:*` scope, so a delegated call on this chain
	// would be refused by every method on it. That is the correct outcome and
	// the stage would only make it a more confusing one.
	//
	// The distinction underneath: on the chain above, a delegation is
	// AUTHORITY, and the request runs as the person. On this chain it is
	// EVIDENCE that a person asked, which is what `RecordAgentRun` uses it for,
	// and evidence travels in the message where it is recorded rather than in a
	// header that changes who the caller is.
	//
	// It also takes NO API KEY AUTHENTICATOR, and that absence is the second
	// thing worth stopping on (ENT-262). A partner's key is a tenant
	// credential: it names one organisation and borrows one person's
	// membership. The platform surface acts across every organisation and
	// belongs to no person, so there is no sense in which a key could be the
	// right caller for it. Omitting the option means a key presented here is
	// refused at authentication rather than later by a scope check, which is
	// the difference between "this credential is not for this surface" and "ask
	// for a wider grant".
	internal := connect.WithInterceptors(
		interceptor.Auth(deps.Verifier),
		interceptor.JTI(deps.DenyList),
		scopes.Interceptor(),
	)

	if deps.Producer != nil {
		mux.Handle(platformv1connect.NewSweepServiceHandler(
			sweepservice.New(deps.Producer), internal))
	}

	// Delivering mail (ENT-256, part three). On the same chain, because the
	// caller is the same service principal: the Temporal worker, listing what
	// is pending, delivering one row, reclaiming what no longer needs keeping.
	// Registered with the outbox rather than with the channel, so a deployment
	// with rows and no mail server answers "no channel configured" rather than
	// 404, which is the difference between a setting to add and a route to
	// debug.
	if deps.Outbox != nil {
		mux.Handle(platformv1connect.NewDeliveryServiceHandler(
			deliveryservice.New(deps.Outbox, deps.Channels, deps.AppBaseURL), internal))
	}

	// The Executor (ENT-271). Two halves on two pools: the producer lists
	// what is pending across every organisation, and the application creates
	// the record as the approver named on the job row. Registered when both
	// exist, which is every deployment with an agent pool.
	// What an agentic Watcher reads and writes (ENT-258). Registered with the
	// producer pool, like the sweep: a deployment with no agent pool serves
	// neither, which is the same supported configuration.
	if deps.Producer != nil {
		mux.Handle(platformv1connect.NewWatcherServiceHandler(
			watcherservice.New(deps.Watcher, watcherRouterOrNil(deps.ModelRouter)), internal))
	}

	if deps.ExecutorJobs != nil && deps.Executions != nil {
		mux.Handle(platformv1connect.NewExecutorServiceHandler(
			executorservice.New(deps.ExecutorJobs, deps.Executions), internal))
	}

	// Writing the corpus (ENT-207). On the same shorter chain, for the same
	// reason plus one of its own: the corpus has no `org_id` because it is the
	// same law for every customer, so there is no organisation for a tenancy
	// interceptor to resolve.
	//
	// Registered only when an ingest pool exists, so a deployment that has not
	// configured one answers 404 rather than 500. The pool names
	// `kindlast_ingest`, which holds grants on the ten regulatory tables and no
	// others; see 00018 for why that is a sixth role and not the migrator.
	//
	// Registered when EITHER dependency exists, because the service now carries
	// two RPCs on two pools: a deployment may load the corpus without running
	// agents, or run agents against a corpus somebody else loaded. The handler
	// answers Unimplemented for whichever half it was not given, which is a
	// better answer than a 404 on a path that does exist.
	if deps.Corpus != nil || deps.AgentRuns != nil || deps.Evidence != nil {
		mux.Handle(platformv1connect.NewIngestServiceHandler(
			ingestservice.New(deps.Corpus, deps.AgentRuns, deps.Evidence,
				deps.Delegations, deps.Logger),
			internal))
	}

	// Narrating findings (ENT-245). Registered whenever there is an agent pool
	// to read findings from, INCLUDING when no Intelligence is configured.
	//
	// That is deliberate rather than sloppy. A deployment without the model
	// profile is supported, and the handler answers
	// `intelligence_available: false` for it. Registering only when a drafter
	// exists would make the difference between "no model here" and "wrong URL"
	// a 404 in both cases, which is the kind of ambiguity somebody debugs for
	// an hour.
	if deps.Narratives != nil {
		mux.Handle(platformv1connect.NewNarrativeServiceHandler(
			narrativeservice.New(deps.Narratives, deps.Drafter, deps.Logger,
				narrativeservice.WithRouter(routerOrNil(deps.ModelRouter))),
			internal))
	}

	// Completions through core-api (ENT-256, part five). The Python service
	// asks here for every model call, naming the organisation; this resolves
	// the route and opens the key, and the key goes to the model endpoint and
	// nowhere else. Registered whenever a router exists, which main makes
	// always: a deployment with no model answers failed_precondition with a
	// reason rather than 404.
	if deps.ModelRouter != nil {
		mux.Handle(platformv1connect.NewCompletionServiceHandler(
			completionservice.New(deps.ModelRouter, nil), internal))
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

	// Approving a finding from a link in an email (§8, ENT-249).
	//
	// # WHY POST, WHICH IS THE WHOLE SECURITY PROPERTY OF THIS ENDPOINT
	//
	// Corporate mail gateways, link previewers and archiving proxies fetch every
	// URL in a message before a human sees it, and some of them follow
	// redirects and prefetch on hover afterwards. Under a GET, the act of
	// DELIVERING a finding notification would approve the finding, in the
	// customer's own compliance record, with an audit row naming a person who
	// never read the message. Findings would approve themselves in transit.
	//
	// So this route is registered POST-only. Go's ServeMux answers 405 for
	// every other method on a path that exists, which is the answer a scanner
	// deserves: the path is real, nothing happened. `web` renders an
	// interstitial at `/approve/{findingId}/{token}` on GET, and its button
	// posts here.
	//
	// The same argument as the unsubscribe route above, and this one costs
	// more if it is wrong: an unsubscribe silently stops mail, an approval
	// silently makes a regulatory decision.
	//
	// # WHY IT IS NOT ON THE CONNECT SURFACE
	//
	// The person clicking has no session, no scope and no organisation header.
	// The delegation is the only identity claim, which is why it is stored
	// hashed, expires within the hour, is spent once, and is bound to the one
	// finding the caller has to name alongside it.
	//
	// # ONE ANSWER FOR EVERY UNUSABLE LINK
	//
	// Expired, revoked, already redeemed, minted for a different finding,
	// minted for somebody since removed from the organisation, and never
	// existed all answer 404 with the same body. A caller presenting a
	// credential has proved nothing that entitles them to know which, and
	// distinguishable answers would make this an oracle for which links are
	// live. 404 rather than 401 because there is nothing to authenticate as.
	mux.HandleFunc("POST /api/v1/approve", func(w http.ResponseWriter, r *http.Request) {
		if deps.Approvals == nil {
			http.Error(w, "approving from email is not available", http.StatusNotImplemented)
			return
		}

		var body struct {
			Token     string `json:"token"`
			FindingID string `json:"findingId"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		// Missing halves join the unusable set rather than getting their own
		// reply. A 400 here would tell a caller that a request WITH both halves
		// is the one worth making, which is a hint this endpoint owes nobody.
		if body.Token == "" || body.FindingID == "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		slug, applied, err := deps.Approvals.ApproveFromEmail(r.Context(), body.Token, body.FindingID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"orgSlug":%q,"findingId":%q,"applied":%t}`,
			slug, body.FindingID, applied)
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

// routerOrNil keeps a nil *Resolver out of a non-nil interface: the same
// typed-nil trap main guards every optional dependency against.
func routerOrNil(r *modelroute.Resolver) narrativeservice.Router {
	if r == nil {
		return nil
	}
	return r
}

// conversationRouterOrNil is routerOrNil for the conversation surface, and it
// exists for the same reason the Watcher's does: a nil `*modelroute.Resolver`
// assigned to an interface is not a nil interface, so a service checking
// `router == nil` would call a method on a nil pointer.
func conversationRouterOrNil(r *modelroute.Resolver) conversationservice.Router {
	if r == nil {
		return nil
	}
	return r
}

// watcherRouterOrNil is routerOrNil for the Watcher's context surface, and it
// exists for the same reason: a nil `*modelroute.Resolver` assigned to an
// interface is not a nil interface, so a service checking `models == nil`
// would call a method on a nil pointer. One helper per interface rather than
// generics, because the trap is easier to see written out than parameterised.
func watcherRouterOrNil(r *modelroute.Resolver) watcherservice.ModelRoute {
	if r == nil {
		return nil
	}
	return r
}
