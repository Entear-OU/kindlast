// Command workers is the policy gateway: the only process in this system that
// opens a connection to an address a customer supplied (ENT-231, §26.4).
//
// Wiring only, no logic, the same rule core-api's main follows. This is the
// one file that knows an allow-list, a shared secret and a listen address all
// exist together; everything else receives what it needs.
//
// # WHY THIS IS A SEPARATE BINARY AND NOT A PACKAGE IN core-api
//
// core-api holds the database credential and the key that seals third-party
// credentials. Putting an HTTP client pointed at a customer-supplied URL in
// that process puts request forgery where it has the most to reach. Here it
// reaches a shared secret and whatever fetch is currently in flight.
//
// # WHAT DOES NOT LIVE HERE
//
// No database handle, no Postgres role, and no persistence of any kind. The
// gateway is handed one fetch at a time and returns what it found; core-api
// writes the row. That is what 00020 anticipated when it said the integrations
// gateway "calls core-api rather than the database, so it needs no role of its
// own", and it is why this binary can be restarted, scaled or compromised
// without any of that touching a tenant's data at rest.
//
// # THE SECOND HALF: THE TEMPORAL WORKER (ENT-256)
//
// The same binary also polls the `core` task queue and runs the schedules
// that used to be pg_cron jobs and Vercel cron routes (§16.4 puts the Go
// worker here). It inherits the rule above exactly: an activity is an RPC on
// core-api's internal surface, made with the same service credential
// Intelligence presents, and core-api does the work. Optional, like the
// gateway's allow-list: no KINDLAST_TEMPORAL_ADDR, no worker, and the gateway
// half runs as before.
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

	"github.com/Entear-OU/kindlast/apps/workers/internal/config"
	"github.com/Entear-OU/kindlast/apps/workers/internal/egress"
	"github.com/Entear-OU/kindlast/apps/workers/internal/ratelimit"
	"github.com/Entear-OU/kindlast/apps/workers/internal/schedule"
	"github.com/Entear-OU/kindlast/apps/workers/internal/server"
	"github.com/Entear-OU/kindlast/apps/workers/internal/service/gateway"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// The runtime image is distroless: no shell, no curl, no wget. A compose
	// healthcheck therefore has to be the binary itself, which is the standard
	// way round this and cheaper than reintroducing a package manager into the
	// image to probe a port. Same shape as core-api's.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			logger.Error("healthcheck failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(logger); err != nil {
		logger.Error("workers stopped", "error", err)
		os.Exit(1)
	}
}

func healthcheck() error {
	addr := os.Getenv("KINDLAST_GATEWAY_LISTEN")
	if addr == "" {
		addr = ":8100"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

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

	allow := egress.Parse(cfg.EgressAllowList, cfg.AllowPrivateDestinations)

	// Loud rather than fatal, and the distinction is deliberate.
	//
	// A gateway with no allow-list starts and refuses every fetch, which is a
	// working deployment of a feature nobody has configured. Refusing to start
	// would take down a stack over a setting its operator may never use. What
	// is not acceptable is silence: the symptom of a missing allow-list is
	// every connection failing with a permission error, which reads like a bug
	// in the product rather than a line somebody has not written.
	if allow.Empty() {
		logger.Warn("no egress allow-list: KINDLAST_GATEWAY_EGRESS_ALLOWLIST is not set, " +
			"so every fetch will be refused. Set it to the hosts this deployment may reach, " +
			"comma separated, with a leading dot for a whole domain")
	}
	if allow.AllowsPrivateDestinations() {
		// Worth a line every boot. It is the right setting for a self-hoster
		// and the wrong one for a hosted deployment, and the difference is
		// invisible from inside the process.
		logger.Warn("private and loopback destinations are permitted; " +
			"correct for a deployment whose MCP servers are on its own network, " +
			"and wrong for one where a customer supplies the address")
	}

	// The worker half, if this deployment runs one. Started before the HTTP
	// server so that a readiness probe can say something true: with a worker,
	// "ready" means the engine answers; without one, it means listening.
	ready := func(context.Context) error { return nil }
	if cfg.Temporal.Addr != "" {
		w, err := startWorker(ctx, logger, cfg)
		if err != nil {
			return err
		}
		defer w.Stop()
		ready = schedule.Ready(w.Client())
	} else {
		// Loud rather than fatal, for the same reason as the allow-list
		// above: a gateway-only deployment is a supported shape, and the
		// symptom of forgetting the worker is "nothing ever runs on a
		// schedule", which should be a log line rather than a mystery.
		logger.Warn("no temporal worker: KINDLAST_TEMPORAL_ADDR is not set, " +
			"so nothing in this deployment runs on a schedule " +
			"(snoozed findings will not come back, and invitation mail will not leave)")
	}

	handler, err := server.New(server.Dependencies{
		Gateway: gateway.New(
			allow,
			allow.Client(cfg.OutboundTimeout),
			ratelimit.New(cfg.RateLimitBurst, cfg.RateLimitWindow),
			logger,
		),
		SharedSecret: cfg.SharedSecret,
		// With no worker, ready when listening. There is nothing else to
		// probe: the gateway holds no connection to anything, by design, so a
		// readiness check that reached a customer's endpoint would be this
		// deployment dialling out on a timer for no reason. With a worker, it
		// is the engine's health.
		Ready: ready,
	})
	if err != nil {
		return err
	}

	httpServer := newHTTPServer(cfg.ListenAddr, handler)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Info("gateway listening",
		"addr", cfg.ListenAddr,
		"outbound_timeout", cfg.OutboundTimeout.String(),
		"rate_burst", cfg.RateLimitBurst,
		"rate_window", cfg.RateLimitWindow.String())

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// newHTTPServer serves HTTP/1.1 and unencrypted HTTP/2 on the same port.
//
// The same pair core-api serves, and for the same reason: Connect's own
// protocol and the health probe speak HTTP/1.1, gRPC needs unencrypted HTTP/2
// on an internal network, and dropping either is invisible to a compiler. See
// core-api's equivalent for the two failure modes.
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

// startWorker connects to the engine and starts polling, with an activity set
// that can call core-api as the service principal.
//
// The token source is the same shape core-api uses to call Intelligence and
// for the same reason: the audience and the roles scope are both required, and
// a token with the audience alone authenticates perfectly and carries no
// scope, which core-api reports as permission denied. The plural in
// `projects:roles` is not a typo.
func startWorker(ctx context.Context, logger *slog.Logger, cfg *config.Config) (*schedule.Worker, error) {
	t := cfg.Temporal

	transport := &oidc.Transport{Host: t.OIDCHostHeader}
	discoveryURL := t.OIDCDiscoveryURL
	if discoveryURL == "" {
		discoveryURL = strings.TrimSuffix(t.OIDCIssuer, "/") + oidc.DiscoveryPath
	}
	provider, err := discoverWithRetry(ctx, logger, transport, discoveryURL, t.OIDCIssuer)
	if err != nil {
		return nil, err
	}
	if provider.TokenEndpoint == "" {
		return nil, errors.New("the authorization server advertises no token_endpoint, " +
			"so the worker cannot mint a token to call core-api")
	}

	tokens, err := oidc.NewClientCredentials(oidc.ClientCredentialsConfig{
		Endpoint: provider.TokenEndpoint,
		ClientID: t.ClientID,
		Secret:   t.ClientSecret,
		Audience: t.OIDCAudience,
		Scopes: []string{
			"openid",
			"urn:zitadel:iam:org:projects:roles",
			fmt.Sprintf("urn:zitadel:iam:org:project:id:%s:aud", t.OIDCAudience),
		},
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("building the worker's token source: %w", err)
	}

	// One HTTP client, one token source, two generated clients: the sweep
	// surface for the snooze expiry and the delivery surface for the outbox.
	// Both are the same service principal presenting the same credential to
	// the same door.
	httpClient := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: &oidc.Bearer{Source: tokens},
	}
	coreAPI := platformv1connect.NewSweepServiceClient(httpClient, t.CoreAPIURL)
	mail := platformv1connect.NewDeliveryServiceClient(httpClient, t.CoreAPIURL)

	opts := schedule.Options{
		Addr:                  t.Addr,
		Namespace:             t.Namespace,
		TaskQueue:             t.TaskQueue,
		SnoozeExpirySchedule:  t.SnoozeExpirySchedule,
		OutboxRelayInterval:   t.OutboxRelayInterval,
		OutboxReclaimSchedule: t.OutboxReclaimSchedule,
		Activities:            &schedule.Activities{CoreAPI: coreAPI, Mail: mail},
		Logger:                logger,
	}
	c, err := schedule.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}
	w, err := schedule.Start(ctx, c, opts)
	if err != nil {
		c.Close()
		return nil, err
	}
	return w, nil
}

// discoverWithRetry is core-api's, for the same race: `auth` and this binary
// start together and the first discovery often lands before Zitadel answers.
func discoverWithRetry(
	ctx context.Context, logger *slog.Logger,
	transport *oidc.Transport, discoveryURL, expectedIssuer string,
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
