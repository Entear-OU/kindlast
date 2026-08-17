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
	"github.com/Entear-OU/kindlast/apps/core-api/internal/dispatch"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/identity"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/sweep"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
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

	handler, err := server.New(server.Dependencies{
		Verifier: verifier,
		DenyList: revocations,
		Tenants:  tenantOpener{store},
		Profiles: identity.NewUserInfo(provider.UserInfoURI, transport),
		Ready: func(ctx context.Context) error {
			return store.Ping(ctx)
		},
		HumanClientID:  cfg.HumanClientID,
		Producer:       producer,
		BillingEnabled: cfg.BillingEnabled,
		AppBaseURL:     cfg.AppBaseURL,
		SMTPConfigured: cfg.SMTPAddr != "",
		Tokens:         store,
		BillingWebhook: billingWebhook,
	})
	if err != nil {
		return err
	}

	startDispatcher(ctx, logger, cfg, outbox)

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

// startDispatcher runs the outbox drain in the background, if this deployment
// is configured to deliver mail.
//
// # WHY EVERY MISSING PIECE IS A WARNING AND NOT A STARTUP FAILURE
//
// Three things have to be present before a message can leave: the agent pool,
// an SMTP address, and a sender. Any of them absent means messages queue up as
// `pending` and nothing is delivered, and that is a supported state rather than
// a broken one. The outbox exists precisely so that the write and the delivery
// are separable: rows are safe on disk, they are not lost, and configuring the
// missing piece later drains the backlog.
//
// So refusing to start would be the wrong trade. It would take a deployment
// that is serving every request correctly and stop it over a channel it may not
// use. What is not acceptable is silence, because the symptom of a missing
// dispatcher is an invitation that never arrives, which nobody reports for days
// and which reads like a spam filter problem. Hence a warning naming the
// setting, at boot, every time.
//
// InviteMember still refuses without KINDLAST_APP_BASE_URL, and that asymmetry
// is deliberate: an undelivered message is recoverable, an unbuildable link is
// not.
func startDispatcher(ctx context.Context, logger *slog.Logger, cfg *config.Config, outbox *postgres.AgentStore) {
	if outbox == nil {
		logger.Warn("no outbox dispatcher: KINDLAST_AGENT_DATABASE_URL is not set, " +
			"so transactional messages such as invitation emails will queue and not be delivered")
		return
	}
	if cfg.SMTPAddr == "" {
		logger.Warn("no outbox dispatcher: KINDLAST_SMTP_ADDR is not set, " +
			"so transactional messages such as invitation emails will queue and not be delivered")
		return
	}

	channel, err := delivery.NewSMTP(cfg.SMTPAddr, cfg.EmailFrom)
	if err != nil {
		logger.Error("no outbox dispatcher: the SMTP channel could not be built; "+
			"transactional messages will queue and not be delivered", "error", err)
		return
	}

	go dispatch.New(outbox, channel, logger, 0, 0).Run(ctx)

	// The doorbell path (ENT-209), on the same channel and a separate loop.
	//
	// Two loops rather than one, because the two queues resolve their recipient
	// at different times: a transactional message carries one decided at mint,
	// a notification's is worked out now from memberships and preferences.
	// Sharing the channel is what keeps this one delivery mechanism rather than
	// two (§23.6); sharing the drain would have meant forcing a recipient into
	// the notification row at enqueue, which is precisely what ENT-192's
	// as-built note warns against.
	//
	// Skipped without a base URL, because every notification carries a link
	// into `/o/{slug}/` and an email whose only actionable content is broken is
	// worse than one that has not been sent.
	if cfg.AppBaseURL == "" {
		logger.Warn("no doorbell dispatcher: KINDLAST_APP_BASE_URL is not set, " +
			"so finding notifications will queue and not be delivered")
		return
	}
	go dispatch.NewDoorbell(outbox, channel, logger, cfg.AppBaseURL, 0, 0).Run(ctx)
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
