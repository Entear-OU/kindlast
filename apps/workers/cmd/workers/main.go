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
	"github.com/Entear-OU/kindlast/apps/workers/internal/server"
	"github.com/Entear-OU/kindlast/apps/workers/internal/service/gateway"
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

	handler, err := server.New(server.Dependencies{
		Gateway: gateway.New(
			allow,
			allow.Client(cfg.OutboundTimeout),
			ratelimit.New(cfg.RateLimitBurst, cfg.RateLimitWindow),
			logger,
		),
		SharedSecret: cfg.SharedSecret,
		// Ready when it is listening. There is nothing else to probe: the
		// gateway holds no connection to anything, by design, so a readiness
		// check that reached a customer's endpoint would be this deployment
		// dialling out on a timer for no reason.
		Ready: func(context.Context) error { return nil },
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
