package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/audit"
	"github.com/entear/kindlast/services/gateway/internal/config"
	"github.com/entear/kindlast/services/gateway/internal/db"
	"github.com/entear/kindlast/services/gateway/internal/handlers"
	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/proxy"
	stripeHandler "github.com/entear/kindlast/services/gateway/internal/stripe"
	"github.com/go-chi/chi/v5"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stripe/stripe-go/v81"
)

const version = "1.0.0"

func main() {
	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("starting gateway service", slog.String("version", version))

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("configuration loaded",
		slog.String("environment", cfg.Environment),
		slog.Int("port", cfg.Port),
		slog.String("rag_service_url", cfg.RAGServiceURL),
	)

	// Initialize Stripe
	if cfg.StripeAPIKey != "" {
		stripe.Key = cfg.StripeAPIKey
		logger.Info("stripe initialized")
	}

	// Initialize database client
	dbClient, err := db.NewClient(cfg.PostgresDSN)
	if err != nil {
		logger.Error("failed to create database client", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbClient.Close()

	// Verify database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := dbClient.Health(ctx); err != nil {
		logger.Error("failed to ping database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("database connection established")

	// Also create raw SQL DB for handlers that need it
	dbConn, err := sql.Open("postgres", cfg.PostgresDSN)
	if err != nil {
		logger.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbConn.Close()

	// Configure database connection pool
	dbConn.SetMaxOpenConns(25)
	dbConn.SetMaxIdleConns(5)
	dbConn.SetConnMaxLifetime(5 * time.Minute)

	// Initialize Redis client
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		logger.Error("failed to parse Redis URL", slog.String("error", err.Error()))
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	// Verify Redis connection
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to ping Redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("redis connection established")

	// Initialize RAG proxy
	ragProxy := proxy.NewRAGProxy(cfg.RAGServiceURL, logger)
	logger.Info("RAG proxy initialized")

	// Initialize audit logger
	auditLogger := audit.NewLogger(dbConn, logger)

	// Initialize plan enforcer
	planEnforcer := middleware.NewPlanEnforcer(redisClient, logger)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(dbConn, logger, cfg.JWTSecret, cfg.JWTAccessExpiration, cfg.JWTRefreshExpiration)
	userHandler := handlers.NewUserHandler(dbConn, redisClient, logger)
	queryHandler := handlers.NewQueryHandler(ragProxy, logger)
	healthHandler := handlers.NewHealthHandler(dbConn, redisClient, cfg.RAGServiceURL, logger, version)
	stripeHandlers := handlers.NewStripeHandler(logger)
	stripeWebhook := stripeHandler.NewWebhookHandler(dbClient, cfg.StripeWebhookSecret, logger)

	// SME Assessment handlers
	profileHandler := handlers.NewProfileHandler(dbConn, logger)
	assessmentHandler := handlers.NewAssessmentHandler(dbConn, logger)
	findingHandler := handlers.NewFindingHandler(dbConn, logger)

	// DPO Copilot handlers
	clientHandler := handlers.NewClientHandler(dbConn, redisClient, logger)
	artifactHandler := handlers.NewArtifactHandler(dbConn, redisClient, logger, auditLogger, planEnforcer, cfg.RAGServiceURL)
	processorHandler := handlers.NewProcessorHandler(dbConn, logger)
	auditHandler := handlers.NewAuditHandler(dbConn, logger, auditLogger)

	// Initialize router
	r := chi.NewRouter()

	// Global middleware stack
	r.Use(middleware.RequestID())
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Recovery(logger))
	r.Use(middleware.CORS(middleware.CORSConfig{
		Origins:     cfg.CORSOrigins,
		Environment: cfg.Environment,
	}))

	// Health endpoints (no auth required)
	r.Get("/health", healthHandler.Health)

	// Initialize auth rate limiting config
	authRateLimitConfig := middleware.DefaultAuthRateLimitConfig()

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Status endpoint (no auth required)
		r.Get("/status", healthHandler.Status)

		// Auth endpoints
		r.Route("/auth", func(r chi.Router) {
			// Rate-limited endpoints (login/register only - brute force protection)
			r.Group(func(r chi.Router) {
				if cfg.RateLimitEnabled {
					r.Use(middleware.AuthRateLimit(redisClient, logger, authRateLimitConfig))
				}
				r.Post("/register", authHandler.Register)
				r.Post("/login", authHandler.Login)
			})

			// Token refresh (not rate limited - requires valid refresh token)
			r.Post("/refresh", authHandler.Refresh)

			// Protected auth endpoints (requires valid access token)
			r.Group(func(r chi.Router) {
				r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))
				r.Get("/me", authHandler.Me)
			})
		})

		// User endpoints (auth required)
		r.Route("/users", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))

			r.Get("/me", userHandler.GetProfile)
			r.Patch("/me", userHandler.UpdateProfile)
			r.Get("/me/plan", userHandler.GetPlan)
		})

		// Stripe endpoints
		r.Route("/stripe", func(r chi.Router) {
			// Webhook endpoint (no auth - verified by Stripe signature)
			r.Post("/webhook", stripeWebhook.HandleWebhook)

			// Protected endpoints
			r.Group(func(r chi.Router) {
				r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))
				r.Post("/checkout", stripeHandlers.HandleCreateCheckoutSession)
				r.Post("/portal", stripeHandlers.HandleCreatePortalSession)
			})
		})

		// Query endpoint (auth + rate limit + freemium required)
		r.Route("/query", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))

			// Apply rate limiting if enabled
			if cfg.RateLimitEnabled {
				r.Use(middleware.RateLimit(redisClient, logger))
			}

			r.Use(middleware.Freemium(redisClient, logger))
			r.Post("/", queryHandler.Query)
		})

		// RAG endpoint (legacy route for backward compatibility)
		r.Route("/rag", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))
			r.Post("/query", queryHandler.Query)
		})

		// =============================================
		// SME ASSESSMENT ROUTES
		// =============================================

		// Business profile (auth required)
		r.Route("/profile", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))

			r.Get("/", profileHandler.GetProfile)
			r.Post("/", profileHandler.CreateOrUpdateProfile)
		})

		// Assessments (auth required)
		r.Route("/assessments", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))

			r.Get("/", assessmentHandler.ListAssessments)
			r.Post("/", assessmentHandler.CreateAssessment)
			r.Get("/latest", assessmentHandler.GetLatestAssessment)
			r.Get("/{id}", assessmentHandler.GetAssessment)
			r.Patch("/{id}", assessmentHandler.UpdateAssessment)
			r.Get("/{id}/findings", findingHandler.ListFindings)
		})

		// Findings (auth required)
		r.Route("/findings", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))

			r.Get("/", findingHandler.ListUserFindings)
			r.Patch("/{id}", findingHandler.UpdateFinding)
		})

		// =============================================
		// DPO COPILOT ROUTES
		// =============================================

		// Client management (requires professional or team plan)
		r.Route("/clients", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))
			r.Use(middleware.RequirePlan("professional", "team"))

			r.Get("/", clientHandler.ListClients)
			r.Post("/", clientHandler.CreateClient)
			r.Get("/{clientID}", clientHandler.GetClient)
			r.Put("/{clientID}", clientHandler.UpdateClient)
			r.Delete("/{clientID}", clientHandler.ArchiveClient)
		})

		// Artifact management (requires professional or team plan + client ownership)
		r.Route("/clients/{clientID}/artifacts", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))
			r.Use(middleware.RequirePlan("professional", "team"))
			r.Use(middleware.RequireClientOwnership(dbConn, logger))

			r.Get("/", artifactHandler.ListArtifacts)
			r.Post("/generate", artifactHandler.GenerateArtifact)
			r.Get("/{artifactID}", artifactHandler.GetArtifact)
			r.Put("/{artifactID}", artifactHandler.UpdateArtifact)
			r.Put("/{artifactID}/status", artifactHandler.UpdateStatus)
			r.Get("/{artifactID}/audit", artifactHandler.GetAuditTrail)
			r.Post("/{artifactID}/export", artifactHandler.ExportArtifact)
			r.Get("/{artifactID}/versions", artifactHandler.ListVersions)
		})

		// Processor profiles (read-only for users)
		r.Route("/processors", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))

			r.Get("/", processorHandler.ListProcessors)
			r.Get("/search", processorHandler.SearchProcessors)
			r.Get("/categories", processorHandler.GetProcessorCategories)
			r.Get("/{slug}", processorHandler.GetProcessor)
		})

		// Audit trail (account-level, requires professional or team plan)
		r.Route("/audit", func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret, dbConn, logger))
			r.Use(middleware.RequirePlan("professional", "team"))

			r.Get("/", auditHandler.ListAuditEntries)
			r.Get("/export", auditHandler.ExportAuditLog)
			r.Get("/summary", auditHandler.GetAuditSummary)
		})
	})

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.Int("port", cfg.Port))
		serverErrors <- srv.ListenAndServe()
	}()

	// Wait for interrupt signal or server error
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("server error", slog.String("error", err.Error()))
		os.Exit(1)

	case sig := <-shutdown:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))

		// Create shutdown context with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer shutdownCancel()

		// Gracefully shutdown server
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
			if err := srv.Close(); err != nil {
				logger.Error("server close failed", slog.String("error", err.Error()))
			}
			os.Exit(1)
		}

		logger.Info("server stopped gracefully")
	}
}
