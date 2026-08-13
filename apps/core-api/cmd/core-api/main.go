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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/config"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	"github.com/Entear-OU/kindlast/libs/chassis/denylist"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("core-api stopped", "error", err)
		os.Exit(1)
	}
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
	provider, err := discoverWithRetry(ctx, logger, cfg.OIDCIssuer)
	if err != nil {
		return err
	}
	logger.Info("discovered the authorization server",
		"issuer", provider.Issuer, "jwks_uri", provider.JWKSURI)

	keys := oidc.NewKeySet(provider.JWKSURI, nil)
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

	store, err := postgres.New(ctx, cfg.DatabaseURL, provider.Issuer)
	if err != nil {
		return err
	}
	defer store.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer redisClient.Close()

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
		Ready: func(ctx context.Context) error {
			return store.Ping(ctx)
		},
	})
	if err != nil {
		return err
	}

	// h2c, so gRPC clients work over plaintext on the internal network. There
	// is no TLS here because there is no public listener: `edge` terminates
	// TLS, and core-api binds no published port at all (§0.4).
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           h2c.NewHandler(handler, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

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

// discoverWithRetry waits for the authorization server rather than crash
// looping against it.
func discoverWithRetry(ctx context.Context, logger *slog.Logger, issuer string) (*oidc.Provider, error) {
	const attempts = 30

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		provider, err := oidc.Discover(ctx, nil, issuer)
		if err == nil {
			return provider, nil
		}
		lastErr = err

		logger.Info("waiting for the authorization server",
			"issuer", issuer, "attempt", attempt, "error", err)

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
