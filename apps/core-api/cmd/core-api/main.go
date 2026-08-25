// Command core-api is the resource server. It owns the domain schema and is
// the only writer to it.
//
// Wiring only, no logic (§21.6). This is the one file that knows a Postgres
// URL, a Redis address and an issuer all exist together; everything else
// receives what it needs.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/billing"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/config"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/delegation"
	modelchoicedomain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/gateway"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/identity"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/conversation"
	deliveryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/delivery"
	executorservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/executor"
	fetchservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/fetch"
	handsservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/hands"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/ingest"
	integrationsservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/integrations"
	modelchoiceservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/narrative"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/sweep"
	watcherservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/watcher"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/denylist"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// The runtime image is distroless: no shell, no curl, no wget. A compose
	// healthcheck therefore has to be the binary itself, which is the standard
	// way round this and cheaper than reintroducing a package manager into the
	// image just to probe a port.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			logger.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(logger); err != nil {
		logger.Error("core-api stopped", "error", err)
		os.Exit(1)
	}
}

// healthcheck probes the readiness endpoint on the loopback interface.
func healthcheck() error {
	addr := os.Getenv("KINDLAST_CORE_API_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	// Built through NewRequestWithContext rather than client.Get, because a
	// request with no context cannot be cancelled. The context is Background
	// here and the client's own timeout still bounds the probe, so this is the
	// same three seconds it always was, now expressed where a caller can reach
	// it.
	client := &http.Client{Timeout: 3 * time.Second}
	request, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "http://"+addr+"/readyz", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz returned %s", response.Status)
	}
	return nil
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Discovery, rather than a hard-coded JWKS path, is what lets a
	// self-hoster point at their own IdP (§18.2).
	//
	// Retried because `auth` and `core-api` start together and losing that
	// race is ordinary rather than exceptional.
	transport := &oidc.Transport{Host: cfg.OIDCHostHeader}

	discoveryURL := cfg.OIDCDiscoveryURL
	if discoveryURL == "" {
		discoveryURL = strings.TrimSuffix(cfg.OIDCIssuer, "/") + oidc.DiscoveryPath
	}

	provider, err := discoverWithRetry(ctx, logger, transport, discoveryURL, cfg.OIDCIssuer)
	if err != nil {
		return err
	}
	logger.Info("discovered the authorization server",
		"issuer", provider.Issuer, "jwks_uri", provider.JWKSURI,
		"userinfo_endpoint", provider.UserInfoURI, "fetched_from", discoveryURL)

	if provider.UserInfoURI == "" {
		// Worth a line at boot, because the symptom otherwise appears much
		// later and looks unrelated: organisations named after subject claims,
		// for one deployment and not another.
		logger.Warn("the authorization server declares no userinfo endpoint; " +
			"organisations created on first sign-in will be named from the subject claim " +
			"when the access token carries no name or email")
	}

	// The credential for the one outbound call core-api makes as a client
	// rather than answers as a server (ENT-245). Built here, next to the
	// discovery it depends on, and failing at startup rather than at the first
	// narration: a deployment that meant to narrate and cannot authenticate
	// should say so while somebody is still watching the logs.
	intelligenceCredentials, err := intelligenceTokens(cfg, provider, transport)
	if err != nil {
		return err
	}
	if cfg.IntelligenceURL != "" && intelligenceCredentials == nil {
		// Not fatal, because a stack can legitimately be part-configured, but
		// never silent: "Intelligence is configured" and "Intelligence can be
		// called" differ by exactly this credential, and the difference is
		// otherwise invisible until a narration pass reports every finding
		// failed.
		logger.Warn("an Intelligence URL is set but no internal client credential is, " +
			"so findings will not be narrated; set KINDLAST_INTERNAL_CLIENT_FILE " +
			"or KINDLAST_INTERNAL_CLIENT_ID and KINDLAST_INTERNAL_CLIENT_SECRET")
	}

	keys := oidc.NewKeySet(provider.JWKSURI, transport)
	if err := keys.Warm(ctx); err != nil {
		// Not fatal, deliberately. A freshly seeded Zitadel has generated no
		// signing key yet and serves an empty set, which is correct rather
		// than broken; the first token to arrive drives the refetch that finds
		// the key (§1.4).
		logger.Warn("could not warm the JWKS cache at boot; "+
			"the first token will drive a refetch", "error", err)
	}

	verifier, err := oidc.NewVerifier(keys, provider.Issuer, cfg.OIDCAudience,
		oidc.WithScopeClaims(cfg.OIDCScopeClaims...))
	if err != nil {
		return err
	}

	// The billing flag reaches the store as a field on every transaction, and
	// decides whether the manual-record cap applies at all.
	//
	// It used to be carried as a third session GUC, because
	// `ropa_manual_activity_limit()` needed the fact and a database function
	// cannot read the environment. ENT-225 moved that decision into Go and
	// 00016 dropped both. What the flag is for is unchanged: without it a
	// self-hosted deployment, which bills nobody, capped manual Article 30
	// entries at three and refused the fourth with a message about a plan it
	// does not sell.
	store, err := postgres.New(ctx, cfg.DatabaseURL, provider.Issuer,
		postgres.WithBilling(cfg.BillingEnabled))
	if err != nil {
		return err
	}
	defer store.Close()

	// The producer pool, on the kindlast_agent role. Optional: without it
	// SweepService is not served, which is a supported configuration rather
	// than a degraded one. Opening it here rather than lazily means a bad DSN
	// or an absent role is a startup failure, not a surprise on the first
	// sweep.
	var producer sweep.Producer
	var outbox *postgres.AgentStore
	if cfg.AgentDatabaseURL != "" {
		agent, agentErr := postgres.NewAgent(ctx, cfg.AgentDatabaseURL)
		if agentErr != nil {
			return agentErr
		}
		defer agent.Close()
		producer = agent
		outbox = agent
	}

	// The corpus writer's pool, on its own role (ENT-207).
	//
	// Optional like the others, and its absence is the ordinary case: the
	// ingest path exists to write the law, and most deployments are not writing
	// the law. Without it IngestService is not registered, so an unconfigured
	// deployment answers 404 rather than failing on the first call.
	//
	// Opened at boot so a DSN naming the wrong role, or a role the migration
	// has not granted, is a startup failure rather than a surprise the first
	// time a schedule fires.
	var corpusWriter *postgres.CorpusStore
	if cfg.IngestDatabaseURL != "" {
		writer, ingestErr := postgres.NewCorpus(ctx, cfg.IngestDatabaseURL)
		if ingestErr != nil {
			return ingestErr
		}
		defer writer.Close()
		corpusWriter = writer
		logger.Info("corpus ingest enabled")
	}

	// The billing webhook's pool, on its own role (ENT-210).
	//
	// Optional, exactly like the producer pool above, and for the sharper
	// reason: a deployment that bills nobody has no provider, no secret and no
	// reason to expose an unauthenticated endpoint. Both halves are required,
	// because a webhook served without a signing secret would accept whatever
	// arrived, and one served without a database could only fail.
	var billingWebhook http.HandlerFunc
	if cfg.BillingDatabaseURL != "" && cfg.BillingWebhookSecret != "" {
		payments, payErr := postgres.NewBilling(ctx, cfg.BillingDatabaseURL)
		if payErr != nil {
			return payErr
		}
		defer payments.Close()
		billingWebhook = billing.Handler(payments, cfg.BillingWebhookSecret, logger)
		logger.Info("billing webhook enabled")
	} else if cfg.BillingEnabled {
		// Worth a line, because this combination is somebody halfway through
		// configuring billing: gating is on, so customers can be refused for
		// being on the free plan, while nothing can move them off it.
		logger.Warn("billing gating is enabled but the webhook is not configured; " +
			"plan changes cannot be applied. Set KINDLAST_BILLING_DATABASE_URL and " +
			"KINDLAST_BILLING_WEBHOOK_SECRET, or turn KINDLAST_BILLING_ENABLED off")
	}

	// The key that seals third-party credentials (ENT-231, §25).
	//
	// Built before anything is served, so a malformed key is a startup failure
	// rather than a surprise the first time somebody connects a system. An
	// ABSENT key is not a failure: connections to endpoints that need no
	// credential still work, and one that needs a credential is refused with a
	// message naming the setting.
	integrationKeys, err := secrets.NewKeyring(cfg.IntegrationKey, cfg.IntegrationKeysOld...)
	if err != nil {
		return err
	}

	// The hosted model providers this deployment permits (ENT-236, §26.6).
	//
	// Parsed here so a malformed entry stops the service with a message about
	// the setting, rather than producing a deployment that quietly permits
	// nothing while its configuration says otherwise. An EMPTY list is the
	// default and it permits nothing on purpose: the bundled model needs no API
	// key, and the property that a stack holding a compliance record can run
	// with no outbound internet has to survive somebody inside the product
	// deciding otherwise.
	modelProviders, err := modelchoicedomain.ParseProviders(strings.Join(cfg.ModelProviders, ","))
	if err != nil {
		return fmt.Errorf("KINDLAST_BYOK_PROVIDERS: %w", err)
	}
	if len(modelProviders) > 0 && !integrationKeys.Configured() {
		// Worth saying at boot rather than at the first attempt. Without a
		// sealing key a provider that needs one is refused, so the operator has
		// permitted something nobody can turn on.
		logger.Warn("hosted model providers are permitted but no integration key is set, "+
			"so a provider that needs an API key cannot be configured",
			"providers", len(modelProviders))
	}

	// The policy gateway, when this deployment has one (ENT-231).
	//
	// NIL IS A SUPPORTED DEPLOYMENT and then IntegrationsService is not served
	// at all. core-api opens no connection to a customer-supplied address, so
	// without a gateway there is nothing behind that surface: registering it
	// anyway would give a console an Integrations page whose every button
	// fails, which reads as a broken product rather than an unconfigured one.
	gatewayClient := gateway.New(cfg.GatewayURL, cfg.GatewaySecret, 0)
	switch {
	case gatewayClient != nil && !integrationKeys.Configured():
		// Worth a line, because this combination works for endpoints that need
		// no credential and refuses every one that does, which is a confusing
		// half-state to meet for the first time in a browser.
		logger.Warn("the integrations gateway is configured but KINDLAST_INTEGRATION_KEY is not; " +
			"connections to endpoints that need a credential will be refused rather than stored unencrypted")
	case gatewayClient == nil && cfg.GatewayURL != "":
		logger.Warn("KINDLAST_GATEWAY_URL is set but KINDLAST_GATEWAY_TOKEN is not, " +
			"so the integrations surface is not served; the gateway accepts no call without the shared secret")
	case gatewayClient != nil:
		logger.Info("integrations enabled", "gateway_url", cfg.GatewayURL)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() { _ = redisClient.Close() }()

	revocations := denylist.NewRedis(redisClient, "")

	// The eviction policy is a security control, not a tuning knob: an LRU
	// instance silently drops deny-list entries under memory pressure, which
	// un-revokes tokens with no error anywhere (§15.3). Checked at boot and
	// logged loudly rather than enforced, because refusing to start over a
	// Redis setting would be a poor trade for an operator mid-incident.
	if policy, err := revocations.MaxMemoryPolicy(ctx); err != nil {
		logger.Warn("could not read redis maxmemory-policy", "error", err)
	} else if policy != "noeviction" {
		logger.Error("redis maxmemory-policy is not noeviction; "+
			"revoked tokens can be silently evicted back into validity",
			"policy", policy)
	}

	// Every channel DeliveryService can send on, behind the one seam it has
	// held since ENT-219 (ENT-263). Empty without an SMTP address and without
	// a bot token, which is a supported state rather than a broken one; see
	// mailChannel.
	channels := delivery.NewRouter()
	mail := mailChannel(logger, cfg)
	channels.Register(delivery.ChannelEmail, mail)
	channels.Register(delivery.ChannelTelegram, telegramChannel(logger, cfg))

	handler, err := server.New(server.Dependencies{
		Verifier: verifier,
		DenyList: revocations,
		Tenants:  tenantOpener{store},
		// The same pool, on the application role. Resolution goes through a
		// SECURITY DEFINER function because the caller presenting a delegation
		// has no session yet, which is the whole reason there is one (00021).
		Delegations: store,
		// Partner API keys (ENT-262). The store, not a wrapper: authenticating
		// a key runs before there is a tenant to hang off, so it is a method on
		// *Store and satisfies interceptor.Authenticator directly.
		APIKeys:  store,
		Profiles: identity.NewUserInfo(provider.UserInfoURI, transport),
		Ready: func(ctx context.Context) error {
			return store.Ping(ctx)
		},
		HumanClientID: cfg.HumanClientID,
		Producer:      producer,
		// The outbox's delivery half (ENT-256, part three), on the same agent
		// pool as the producer and behind the same typed-nil guard.
		Outbox: outboxDependency(outbox),
		// The Executor (ENT-271): the producer lists, the application
		// executes as the approver. Same typed-nil guard as the rest.
		ExecutorJobs: executorJobsDependency(outbox),
		// The agentic Watcher's surface (ENT-258), on the producer pool.
		Watcher: watcherDependency(outbox),
		// The Hands' surface (ENT-261), on the same producer pool and behind
		// the same typed-nil guard. The explainer is the same Intelligence
		// client the narrator uses: one client, two RPCs, so a deployment
		// cannot end up able to narrate and unable to explain.
		HandsApprovals: handsDependency(outbox),
		Explainer:      explainerDependency(cfg.IntelligenceURL, intelligenceCredentials),
		Executions:     store,
		Channels:       channels,
		BillingEnabled: cfg.BillingEnabled,
		AppBaseURL:     cfg.AppBaseURL,
		Tokens:         store,
		// The approve-from-email endpoint (ENT-249). The same application pool:
		// redemption resolves the delegation and then acts as the person it
		// names, in one transaction, through the ordinary policy surface.
		Approvals:      store,
		BillingWebhook: billingWebhook,
		// Nil when no ingest DSN is set, and then IngestService is not
		// registered at all. A typed nil would not be: assigning a nil
		// *CorpusStore to the interface makes it non-nil, so the guard has to
		// stay on the concrete pointer.
		Corpus: corpusDependency(corpusWriter),
		// Same typed-nil trap as Corpus above, and the same shape of guard.
		// The agent pool exists only when a sweep is configured, and a
		// deployment that runs no agents should answer Unimplemented here
		// rather than accept a run record it cannot store.
		AgentRuns: agentRunsDependency(outbox),
		// What a machine fetched from a customer's system (ENT-231), on the
		// same agent pool and behind the same typed-nil guard.
		Evidence: evidenceDependency(outbox),
		// Findings to narrate, on the same agent pool, and the drafter that
		// explains them. The second is nil for a deployment with no model,
		// which is supported rather than broken (ENT-245).
		Narratives: narrativesDependency(outbox),
		Drafter:    drafterDependency(cfg.IntelligenceURL, intelligenceCredentials),
		// The same service, asked a question by a person rather than by a job
		// (ENT-270). Nil under the same conditions, and then the handler says
		// this deployment has no model rather than failing.
		Answerer: answererDependency(cfg.IntelligenceURL, intelligenceCredentials),
		// An organisation's own provider, honoured by the narration job
		// (ENT-236). Absent unless all of the agent pool, a sealing key and a
		// permitted provider list are present, because honouring a choice
		// without the checks that make it safe is worse than not honouring it.
		// Where an organisation's completions go (ENT-236, ENT-256 part
		// five): the deployment's own model, or the provider it chose, with
		// the key opened here and nowhere else. Always built, so a deployment
		// with no model refuses a completion with a reason rather than 404.
		ModelRouter: modelroute.New(cfg.ModelURL, cfg.ModelName).
			WithModelChoice(modelChoicesDependency(outbox), integrationKeys, modelProviders, nil),
		// Same typed-nil trap as Corpus above, and the same shape of guard: a
		// nil *gateway.Client assigned straight into the interface field would
		// produce an interface that is not nil, the registration guard would
		// pass, and every call would panic.
		Integrations: integrationsDependency(gatewayClient, integrationKeys),
		// The scheduled fetch that deposits the evidence the Watcher reads
		// (ENT-279). The same gateway and the same keyring the console's
		// Integrations page uses, driven by a Temporal schedule instead of a
		// person.
		Fetch: fetchDependency(gatewayClient, outbox, store, integrationKeys),
		// Where an organisation's model runs (ENT-236). Served on every
		// deployment, including one permitting no provider, so a member always
		// gets a true answer to "where is our compliance data processed".
		ModelChoice: modelchoiceservice.New(modelProviders, integrationKeys, nil),
		Logger:      logger,
	})
	if err != nil {
		return err
	}

	if outbox != nil && !channels.Empty() && cfg.AppBaseURL == "" {
		logger.Warn("KINDLAST_APP_BASE_URL is not set, " +
			"so finding notifications will queue and not be delivered: every one carries a link into the console")
	}

	httpServer := newHTTPServer(cfg.ListenAddr, handler)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("core-api listening", "addr", cfg.ListenAddr, "audience", cfg.OIDCAudience)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// mailChannel builds the SMTP channel, or nothing.
//
// # WHY A MISSING MAIL SERVER IS A WARNING AND NOT A STARTUP FAILURE
//
// Messages queue as `pending` and nothing is delivered, and that is a supported
// state rather than a broken one. The outbox exists precisely so that the write
// and the delivery are separable: rows are safe on disk, they are not lost, and
// configuring the missing piece later drains the backlog. Refusing to start
// would take a deployment that is serving every request correctly and stop it
// over a channel it may not use.
//
// What is not acceptable is silence, because the symptom of a missing channel
// is an invitation that never arrives, which nobody reports for days and which
// reads like a spam filter problem. Hence a warning naming the setting, at boot,
// every time, and a `failed_precondition` naming it again on every delivery the
// Temporal worker attempts, so the workflow history says so too.
//
// Finding notifications (ENT-209) leave through the same channel and the same
// service, and need one more thing: KINDLAST_APP_BASE_URL, for the link into
// the console that every notification carries. Same treatment: a warning at
// boot, a `failed_precondition` on each attempt, the rows wait.
//
// InviteMember still refuses without KINDLAST_APP_BASE_URL, and that asymmetry
// is deliberate: an undelivered message is recoverable, an unbuildable link is
// not.
func mailChannel(logger *slog.Logger, cfg *config.Config) delivery.Channel {
	if cfg.SMTPAddr == "" {
		logger.Warn("no mail channel: KINDLAST_SMTP_ADDR is not set, " +
			"so transactional messages such as invitation emails will queue and not be delivered")
		return nil
	}
	channel, err := delivery.NewSMTP(cfg.SMTPAddr, cfg.EmailFrom)
	if err != nil {
		logger.Error("no mail channel: the SMTP channel could not be built; "+
			"transactional messages will queue and not be delivered", "error", err)
		return nil
	}
	return channel
}

// telegramChannel builds the Telegram channel, or nothing (ENT-263).
//
// # AN ABSENT TOKEN IS NOT A DEGRADED STATE, IT IS THE CHANNEL NOT EXISTING
//
// mailChannel's absence is degraded: rows queue and drain later, because the
// product wrote them expecting to send. This is different. Nothing is ever
// addressed to Telegram on a deployment that has no bot, because linking a chat
// is refused before it writes anything and nobody can therefore choose a channel
// they cannot link. So an absent token leaves no backlog and nothing waiting; it
// leaves a product with one channel.
//
// That is also what `bun run test:airgap` asserts from the outside: with no
// token there is no adapter on the router, so no code path in this process can
// construct a call to api.telegram.org. Not "does not call it in practice",
// which is a claim about behaviour, but "has no object that could".
//
// It logs at info rather than warn for the same reason. A self-hoster who never
// wanted Telegram is not misconfigured, and a warning on every boot for a
// deliberate choice is how operators learn to stop reading warnings.
//
// The token is never logged, here or anywhere. What is logged is that there is
// one.
func telegramChannel(logger *slog.Logger, cfg *config.Config) delivery.Channel {
	if cfg.TelegramBotToken == "" {
		logger.Info("no Telegram channel: KINDLAST_TELEGRAM_BOT_TOKEN is not set, " +
			"so Telegram is not offered as a notification channel")
		return nil
	}
	channel, err := delivery.NewTelegram(cfg.TelegramBotToken, "", nil)
	if err != nil {
		logger.Error("no Telegram channel: it could not be built; "+
			"Telegram is not offered as a notification channel", "error", err)
		return nil
	}
	return channel
}

// newHTTPServer builds the listener, serving HTTP/1.1 and unencrypted HTTP/2 on
// the same port.
//
// Plaintext HTTP/2 is what lets gRPC clients reach this service on the internal
// network. TLS is terminated at `edge` rather than here, which is a deployment
// choice about where the edge sits, not an assumption that this service only
// ever serves one client from inside one network.
//
// # WHY BOTH PROTOCOLS, AND WHY THIS IS A FUNCTION
//
// This replaced `h2c.NewHandler` from `golang.org/x/net`, deprecated in favour
// of `http.Server.Protocols` (ENT-216). That is a change to how the process
// negotiates HTTP/2, not a rename, and the two failure modes it can introduce
// are both invisible to a compiler:
//
//   - Drop HTTP/1.1 and gRPC keeps working while everything else stops. Connect's
//     own protocol, gRPC-Web and the health probe all speak HTTP/1.1, so a
//     server with only the HTTP/2 bit set passes every gRPC test and takes the
//     console offline.
//   - Drop unencrypted HTTP/2 and the negotiation silently falls back to
//     HTTP/1.1, where gRPC does not work at all.
//
// Neither shows up in a build, so the configuration is lifted out of `run` to
// be reachable from a test that makes real requests over both. `run` needs a
// database, an issuer and a Redis to reach the old inline version, which is
// what kept this untested while it was three lines in the middle of wiring.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         &protocols,
	}
}

// discoverWithRetry waits for the authorization server rather than crash
// looping against it.
func discoverWithRetry(
	ctx context.Context,
	logger *slog.Logger,
	transport *oidc.Transport,
	discoveryURL, expectedIssuer string,
) (*oidc.Provider, error) {
	const attempts = 30

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		provider, err := oidc.DiscoverAt(ctx, transport, discoveryURL, expectedIssuer)
		if err == nil {
			return provider, nil
		}
		lastErr = err

		logger.Info("waiting for the authorization server",
			"discovery_url", discoveryURL, "attempt", attempt, "error", err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, lastErr
}

// tenantOpener adapts the store's concrete return type to the interface the
// interceptor consumes.
//
// The interface is declared where it is used and satisfied by the type that
// implements it, so `store` does not import `server` and dependencies keep
// pointing inward (§21.6).
type tenantOpener struct{ store *postgres.Store }

func (o tenantOpener) BeginTenant(ctx context.Context, subject, orgID string) (interceptor.Tenant, error) {
	tenant, err := o.store.BeginTenant(ctx, subject, orgID)
	if err != nil {
		if errors.Is(err, postgres.ErrNotAMember) {
			return nil, interceptor.ErrNotAMember
		}
		return nil, err
	}
	return tenant, nil
}

// BeginDelegatedTenant is the same adaptation for an agent acting for a person
// (ENT-230). The membership refusal maps to the same error, because from the
// caller's side there is no difference worth telling them: a delegation for
// somebody who has since been removed and a delegation for an organisation they
// were never in are both "you may not act here".
func (o tenantOpener) BeginDelegatedTenant(
	ctx context.Context, grant delegation.Grant,
) (interceptor.Tenant, error) {
	tenant, err := o.store.BeginDelegatedTenant(ctx, grant)
	if err != nil {
		if errors.Is(err, postgres.ErrNotAMember) {
			return nil, interceptor.ErrNotAMember
		}
		return nil, err
	}
	return tenant, nil
}

// BeginAPIKeyTenant is the same adaptation for a partner's key (ENT-262), and
// the membership refusal maps to the same error for the same reason. A key whose
// minter has been removed from the organisation and a key for an organisation
// that no longer exists are both "you may not act here", and the caller has
// proved nothing that entitles them to know which.
func (o tenantOpener) BeginAPIKeyTenant(
	ctx context.Context, key apikey.Principal,
) (interceptor.Tenant, error) {
	tenant, err := o.store.BeginAPIKeyTenant(ctx, key)
	if err != nil {
		if errors.Is(err, postgres.ErrNotAMember) {
			return nil, interceptor.ErrNotAMember
		}
		return nil, err
	}
	return tenant, nil
}

// corpusDependency keeps a nil pool out of a non-nil interface.
//
// Assigning a nil *CorpusStore straight into an interface field produces an
// interface that is not nil, so the registration guard in server.New would pass
// and every ingest call would panic on a nil pool. This is the classic Go trap
// and it is worth a named function rather than a clever inline conditional.
func corpusDependency(store *postgres.CorpusStore) ingest.Writer {
	if store == nil {
		return nil
	}
	return store
}

// The same typed-nil guard, for the delivery half of the transactional outbox
// (ENT-256, part three).
func outboxDependency(store *postgres.AgentStore) deliveryservice.Outbox {
	if store == nil {
		return nil
	}
	return store
}

// The same typed-nil guard, for the Executor's listing half (ENT-271).
func executorJobsDependency(store *postgres.AgentStore) executorservice.Jobs {
	if store == nil {
		return nil
	}
	return store
}

// The same typed-nil guard, for the Watcher's surface (ENT-258).
func watcherDependency(store *postgres.AgentStore) watcherservice.Producer {
	if store == nil {
		return nil
	}
	return store
}

// The same typed-nil guard, for the Hands' surface (ENT-261).
func handsDependency(store *postgres.AgentStore) handsservice.Approvals {
	if store == nil {
		return nil
	}
	return store
}

// The Intelligence client again, typed as what the Hands needs of it
// (ENT-261).
//
// Built from the same URL and the same credential as `drafterDependency`, so a
// deployment cannot narrate and fail to explain. A separate function rather
// than one returning both, because §21.6 puts an interface where it is used
// and the two callers need different halves of the client.
func explainerDependency(baseURL string, tokens *oidc.ClientCredentials) handsservice.Explainer {
	if baseURL == "" || tokens == nil {
		return nil
	}
	return platformv1connect.NewIntelligenceServiceClient(
		&http.Client{
			Timeout:   10 * time.Minute,
			Transport: &oidc.Bearer{Source: tokens},
		}, baseURL)
}

func agentRunsDependency(store *postgres.AgentStore) ingest.RunRecorder {
	if store == nil {
		return nil
	}
	return store
}

// The same typed-nil guard, for the narrator's read and write of findings
// (ENT-245).
func evidenceDependency(store *postgres.AgentStore) ingest.EvidenceRecorder {
	if store == nil {
		return nil
	}
	return store
}

func narrativesDependency(store *postgres.AgentStore) narrative.Findings {
	if store == nil {
		return nil
	}
	return store
}

// modelChoicesDependency keeps a nil agent pool out of a non-nil interface.
//
// The same typed-nil trap every other optional dependency here guards against:
// assigning a nil *AgentStore straight into the interface field produces an
// interface that is not nil, so the guard downstream passes and the first call
// panics.
func modelChoicesDependency(store *postgres.AgentStore) modelroute.Choices {
	if store == nil {
		return nil
	}
	return store
}

// The Intelligence client, when this deployment has one (ENT-245).
//
// NIL IS A SUPPORTED DEPLOYMENT. The model service sits behind a compose
// profile, so a stack can run without it, and then findings carry the
// deterministic text the sweep wrote and nothing else. Returning nil rather
// than a client pointed at an empty URL matters: a client that exists and
// cannot connect turns every narration pass into a pile of timeouts, where a
// nil one answers `intelligence_available: false` in a millisecond.
//
// A generous timeout, because the thing on the other end is a local model doing
// prompt evaluation on a CPU. The per-run budget inside the harness is what
// should decide a run is too slow (ENT-238); a timeout here would only decide
// this client is impatient.
//
// IT CARRIES A BEARER TOKEN, AND THE FIRST VERSION DID NOT. Intelligence
// declares `internal:intelligence` on DraftNarrative and refuses an
// unauthenticated call, so the original plain client made every narration fail
// with "a bearer token is required". Nothing caught it: the service tests use a
// fake drafter, the store tests never leave the database, and the two halves
// had not met. Only driving the live stack surfaced it.
//
// A NIL TOKEN SOURCE IS ALSO NIL HERE, deliberately. A client that cannot
// authenticate is not a degraded narrator, it is one that fails every call, and
// `intelligence_available: false` is the honest answer for a deployment holding
// no credential. The alternative reports Intelligence as present and then
// refuses everything, which is the report that costs somebody an afternoon.
func drafterDependency(baseURL string, tokens *oidc.ClientCredentials) narrative.Drafter {
	if baseURL == "" || tokens == nil {
		return nil
	}
	return intelligenceClient(baseURL, tokens)
}

// The same client, for the question a person asks about a finding (ENT-270).
//
// A second function rather than one returning `any`, so each caller's
// dependency is typed as the half of Intelligence it actually calls. Nil under
// exactly the same conditions and for exactly the same reasons: see above.
//
// ONE DIFFERENCE WORTH NAMING, AND IT IS THE TIMEOUT. The ten minutes below is
// right for a narration nobody is watching and wrong for a person waiting in
// front of a page, but the timeout is not what should decide this: the
// harness's wall clock refuses a run that took too long and records it as a
// refusal a customer can read, where a client timeout leaves a run finishing in
// the background with nobody to hand it to. So the budget stays the decider and
// this stays generous, which is the same reasoning ENT-238 wrote down for the
// narration client.
func answererDependency(baseURL string, tokens *oidc.ClientCredentials) conversation.Answerer {
	if baseURL == "" || tokens == nil {
		return nil
	}
	return intelligenceClient(baseURL, tokens)
}

// intelligenceClient is the one construction both dependencies above share, so
// a change to the transport or the token source cannot reach one caller and
// miss the other. The bearer token is the whole point of it: ENT-245 shipped
// this client without one and every call failed with "a bearer token is
// required", and nothing but the live stack could see it.
func intelligenceClient(baseURL string, tokens *oidc.ClientCredentials) platformv1connect.IntelligenceServiceClient {
	return platformv1connect.NewIntelligenceServiceClient(
		&http.Client{
			Timeout:   10 * time.Minute,
			Transport: &oidc.Bearer{Source: tokens},
		}, baseURL)
}

// intelligenceTokens builds the token source for the Intelligence call, or nil
// when this deployment is not configured to make one (ENT-245).
//
// Nil rather than an error for a missing credential, matching every other
// optional dependency here: a stack with no model profile has nothing to
// authenticate to. An error is reserved for a credential that is present and
// unusable, because that one is a misconfiguration somebody meant to get right
// and should hear about at startup rather than at the first narration.
func intelligenceTokens(cfg *config.Config, provider *oidc.Provider, transport *oidc.Transport) (*oidc.ClientCredentials, error) {
	if cfg.IntelligenceURL == "" || cfg.InternalClientID == "" || cfg.InternalClientSecret == "" {
		return nil, nil
	}
	if provider.TokenEndpoint == "" {
		return nil, fmt.Errorf(
			"intelligence is configured but the authorization server advertises no token_endpoint, so core-api cannot mint a token to call it")
	}

	return oidc.NewClientCredentials(oidc.ClientCredentialsConfig{
		Endpoint: provider.TokenEndpoint,
		ClientID: cfg.InternalClientID,
		Secret:   cfg.InternalClientSecret,
		Audience: cfg.OIDCAudience,
		// The audience and the roles, and both are required. Requesting the
		// audience without `...projects:roles` yields a token that
		// authenticates perfectly and carries no scope, which Intelligence
		// reports as permission denied and sends the reader to check grants
		// that were already correct. The plural is not a typo.
		Scopes: []string{
			"openid",
			"urn:zitadel:iam:org:projects:roles",
			fmt.Sprintf("urn:zitadel:iam:org:project:id:%s:aud", cfg.OIDCAudience),
		},
		Transport: transport,
	})
}

// integrationsDependency keeps a nil gateway out of a non-nil interface.
//
// The same typed-nil guard as corpusDependency above, and it is the reason
// this is a named function rather than a clever inline conditional: assigning
// a nil *gateway.Client straight into the interface field produces an
// interface that is not nil, so the registration guard in server.New would
// pass and every integrations call would panic on a nil client.
func integrationsDependency(
	client *gateway.Client, keys *secrets.Keyring,
) corev1connect.IntegrationsServiceHandler {
	if client == nil {
		return nil
	}
	return integrationsservice.New(client, keys)
}

// fetchDependency builds the scheduled fetch, or nothing (ENT-279).
//
// Three conditions, and each of them makes the surface useless without it: a
// gateway, because core-api dials nobody itself; the agent pool, because
// listing what is stale across every organisation and writing the result both
// run on it; and the application pool, which is always there and is named
// anyway so the reader can see all three halves in one place.
//
// The same typed-nil guard as integrationsDependency above, and for the same
// reason: a nil *gateway.Client or a nil *postgres.AgentStore assigned into an
// interface is not a nil interface, so the guard has to stay on the concrete
// pointers.
func fetchDependency(
	client *gateway.Client, agent *postgres.AgentStore, store *postgres.Store,
	keys *secrets.Keyring,
) platformv1connect.FetchServiceHandler {
	if client == nil || agent == nil || store == nil {
		return nil
	}
	return fetchservice.New(agent, store, agent, client, keys)
}
