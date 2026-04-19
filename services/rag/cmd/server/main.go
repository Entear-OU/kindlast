package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/entear/kindlast/services/rag/internal/adapters"
	"github.com/entear/kindlast/services/rag/internal/cache"
	"github.com/entear/kindlast/services/rag/internal/config"
	"github.com/entear/kindlast/services/rag/internal/middleware"
	"github.com/entear/kindlast/services/rag/internal/prompts"
	"github.com/entear/kindlast/services/rag/internal/providers"
	"github.com/entear/kindlast/services/rag/internal/providers/embedding"
	"github.com/entear/kindlast/services/rag/internal/providers/generation"
	"github.com/entear/kindlast/services/rag/internal/providers/reranking"
	"github.com/entear/kindlast/services/rag/internal/rag"
	"github.com/entear/kindlast/services/rag/internal/retrieval"
	"github.com/entear/kindlast/services/rag/internal/router"
)

// Server holds the HTTP server and its dependencies
type Server struct {
	orchestrator *rag.Orchestrator
	config       *config.Config
	logger       *slog.Logger
	router       *chi.Mux
}

// NewServer creates a new HTTP server
func NewServer(orchestrator *rag.Orchestrator, cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		orchestrator: orchestrator,
		config:       cfg,
		logger:       logger,
		router:       chi.NewRouter(),
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// setupMiddleware configures HTTP middleware
func (s *Server) setupMiddleware() {
	s.router.Use(middleware.RealIP())
	s.router.Use(middleware.RequestID())
	s.router.Use(middleware.Logger(s.logger))
	s.router.Use(middleware.Recovery(s.logger))
	s.router.Use(middleware.CORS(s.config.Server.CORSOrigins))
	s.router.Use(middleware.Compress())
	s.router.Use(chimiddleware.Heartbeat("/ping"))
}

// setupRoutes configures HTTP routes
func (s *Server) setupRoutes() {
	// Health check
	s.router.Get("/health", s.handleHealth)

	// API routes
	s.router.Route("/api/v1", func(r chi.Router) {
		r.Post("/query", s.handleQuery)
		r.Get("/providers/status", s.handleProviderStatus)
	})
}

// handleQuery handles RAG query requests
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req rag.QueryRequest

	// Parse request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ctx := r.Context()

	// Handle streaming vs non-streaming
	if req.Stream {
		s.handleStreamingQuery(w, r, req)
	} else {
		s.handleNonStreamingQuery(w, ctx, req)
	}
}

// handleNonStreamingQuery handles non-streaming RAG queries
func (s *Server) handleNonStreamingQuery(w http.ResponseWriter, ctx context.Context, req rag.QueryRequest) {
	resp, err := s.orchestrator.Query(ctx, req)
	if err != nil {
		s.logger.Error("query failed", "error", err, "query", req.Query)
		s.writeError(w, http.StatusInternalServerError, "Query processing failed")
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleStreamingQuery handles streaming RAG queries using SSE
func (s *Server) handleStreamingQuery(w http.ResponseWriter, r *http.Request, req rag.QueryRequest) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	ctx := r.Context()

	// Start streaming query
	chunkChan, err := s.orchestrator.QueryStream(ctx, req)
	if err != nil {
		s.logger.Error("streaming query failed", "error", err, "query", req.Query)
		s.writeSSEError(w, flusher, "Failed to start streaming query")
		return
	}

	// Stream chunks to client
	for chunk := range chunkChan {
		chunkJSON, err := json.Marshal(chunk)
		if err != nil {
			s.logger.Error("failed to marshal chunk", "error", err)
			continue
		}

		// Write SSE event
		event := prompts.FormatSSEEvent(chunk.Type, string(chunkJSON))
		if _, err := fmt.Fprint(w, event); err != nil {
			s.logger.Error("failed to write SSE event", "error", err)
			return
		}

		flusher.Flush()

		// Check if client disconnected
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// handleHealth checks the health of all dependencies
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	healthChecks := s.orchestrator.Health(ctx)

	allHealthy := true
	status := make(map[string]string)

	for component, err := range healthChecks {
		if err != nil {
			status[component] = "unhealthy: " + err.Error()
			allHealthy = false
		} else {
			status[component] = "healthy"
		}
	}

	response := map[string]any{
		"status":     "healthy",
		"timestamp":  time.Now().UTC(),
		"components": status,
	}

	statusCode := http.StatusOK
	if !allHealthy {
		response["status"] = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	s.writeJSON(w, statusCode, response)
}

// handleProviderStatus returns the status of AI providers
func (s *Server) handleProviderStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	healthChecks := s.orchestrator.Health(ctx)

	providerStatus := map[string]any{
		"generation": map[string]any{
			"primary":  s.config.Providers.Generation.Primary,
			"fallback": s.config.Providers.Generation.Fallback,
			"healthy":  healthChecks["generator"] == nil,
		},
		"embedding": map[string]any{
			"primary":  s.config.Providers.Embedding.Primary,
			"fallback": s.config.Providers.Embedding.Fallback,
			"healthy":  healthChecks["embedder"] == nil,
		},
		"reranking": map[string]any{
			"primary":  s.config.Providers.Reranking.Primary,
			"fallback": s.config.Providers.Reranking.Fallback,
			"healthy":  healthChecks["reranker"] == nil,
		},
		"retrieval": map[string]any{
			"vector_db": "qdrant",
			"healthy":   healthChecks["retriever"] == nil,
		},
		"cache": map[string]any{
			"backend": "redis",
			"healthy": healthChecks["cache"] == nil,
		},
	}

	s.writeJSON(w, http.StatusOK, providerStatus)
}

// Helper methods

func (s *Server) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode JSON response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) writeSSEError(w http.ResponseWriter, flusher http.Flusher, message string) {
	errorChunk := rag.StreamChunk{
		Type:  prompts.ChunkTypeError,
		Error: message,
	}
	chunkJSON, _ := json.Marshal(errorChunk)
	event := prompts.FormatSSEEvent(prompts.ChunkTypeError, string(chunkJSON))
	fmt.Fprint(w, event)
	flusher.Flush()
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := ":" + s.config.Server.Port

	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
	}

	s.logger.Info("starting server", "addr", addr)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for interrupt signal or error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigChan:
		s.logger.Info("received signal", "signal", sig)
	}

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), s.config.Server.ShutdownTimeout)
	defer cancel()

	s.logger.Info("shutting down server gracefully")
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	s.logger.Info("server stopped")
	return nil
}

func main() {
	// Setup logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Info("configuration loaded",
		"generation_primary", cfg.Providers.Generation.Primary,
		"embedding_primary", cfg.Providers.Embedding.Primary,
		"reranking_primary", cfg.Providers.Reranking.Primary,
		"qdrant_host", cfg.Qdrant.Host,
		"redis_url", cfg.Redis.URL,
	)

	// Initialize all components
	logger.Info("initializing components")

	embedderProvider, err := initEmbedder(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize embedder", "error", err)
		os.Exit(1)
	}

	qdrantClient, err := initRetriever(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize retriever", "error", err)
		os.Exit(1)
	}
	defer qdrantClient.Close()

	rerankerProvider, err := initReranker(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize reranker", "error", err)
		os.Exit(1)
	}

	generatorProvider, err := initGenerator(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize generator", "error", err)
		os.Exit(1)
	}

	cacheClient, err := initCache(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize cache", "error", err)
		os.Exit(1)
	}
	defer cacheClient.Close()

	parentFetcher, err := initParentFetcher(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize parent fetcher", "error", err)
		os.Exit(1)
	}
	defer parentFetcher.Close()

	logger.Info("all components initialized successfully")

	// Wrap components with adapters to match orchestrator interfaces
	embedder := adapters.NewEmbedderAdapter(embedderProvider)
	retriever := adapters.NewRetrieverAdapter(qdrantClient, parentFetcher)
	reranker := adapters.NewRerankerAdapter(rerankerProvider)
	generator := adapters.NewGeneratorAdapter(generatorProvider)
	cache := adapters.NewCacheAdapter(cacheClient)
	parentChunkFetcher := adapters.NewParentFetcherAdapter(parentFetcher)

	logger.Info("adapters created")

	// Create orchestrator
	orchestrator := rag.NewOrchestrator(
		embedder,
		retriever,
		reranker,
		generator,
		cache,
		parentChunkFetcher,
		cfg,
		logger,
	)

	// Create and start server
	server := NewServer(orchestrator, cfg, logger)
	if err := server.Start(); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// Initialization functions

func initEmbedder(cfg *config.Config, logger *slog.Logger) (providers.EmbeddingProvider, error) {
	// Initialize primary provider
	var primary providers.EmbeddingProvider
	var err error

	switch cfg.Providers.Embedding.Primary {
	case "openai":
		primary, err = embedding.NewOpenAIProvider(cfg.Providers.Embedding.OpenAIAPIKey, cfg.Providers.Embedding.OpenAIModel)
	case "cohere":
		primary, err = embedding.NewCohereProvider(cfg.Providers.Embedding.CohereAPIKey, cfg.Providers.Embedding.CohereModel)
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", cfg.Providers.Embedding.Primary)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize primary embedder: %w", err)
	}

	// Initialize fallback provider if configured
	var fallback providers.EmbeddingProvider
	if cfg.Providers.Embedding.Fallback != "" {
		switch cfg.Providers.Embedding.Fallback {
		case "openai":
			fallback, err = embedding.NewOpenAIProvider(cfg.Providers.Embedding.OpenAIAPIKey, cfg.Providers.Embedding.OpenAIModel)
		case "cohere":
			fallback, err = embedding.NewCohereProvider(cfg.Providers.Embedding.CohereAPIKey, cfg.Providers.Embedding.CohereModel)
		default:
			logger.Warn("unknown fallback embedding provider, skipping", "provider", cfg.Providers.Embedding.Fallback)
		}
		if err != nil {
			logger.Warn("failed to initialize fallback embedder, continuing with primary only", "error", err)
			fallback = nil
		}
	}

	// Return primary provider with fallback
	// If fallback is configured, we'd use the router here
	// For now, just return the primary provider directly
	logger.Info("embedding provider initialized",
		"primary", cfg.Providers.Embedding.Primary,
		"has_fallback", fallback != nil,
	)

	// If there's a fallback, use the router; otherwise return primary directly
	if fallback != nil {
		settings := router.CircuitBreakerSettings{
			MaxRequests: 3,
			Interval:    time.Minute,
			Timeout:     30 * time.Second,
		}
		return router.NewEmbeddingRouter(primary, fallback, settings), nil
	}

	return primary, nil
}

func initRetriever(cfg *config.Config, logger *slog.Logger) (*retrieval.QdrantClient, error) {
	client, err := retrieval.NewQdrantClient(cfg.Qdrant.Host, cfg.Qdrant.Port)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Qdrant client: %w", err)
	}

	logger.Info("qdrant client initialized",
		"host", cfg.Qdrant.Host,
		"port", cfg.Qdrant.Port,
	)

	return client, nil
}

func initReranker(cfg *config.Config, logger *slog.Logger) (providers.RerankProvider, error) {
	// Initialize primary provider
	var primary providers.RerankProvider
	var err error

	switch cfg.Providers.Reranking.Primary {
	case "cohere":
		primary, err = reranking.NewCohereProvider(cfg.Providers.Reranking.CohereAPIKey, cfg.Providers.Reranking.CohereModel)
	case "jina":
		primary, err = reranking.NewJinaProvider(cfg.Providers.Reranking.JinaAPIKey, cfg.Providers.Reranking.JinaModel)
	default:
		return nil, fmt.Errorf("unknown reranking provider: %s", cfg.Providers.Reranking.Primary)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize primary reranker: %w", err)
	}

	// Initialize fallback provider if configured
	var fallback providers.RerankProvider
	if cfg.Providers.Reranking.Fallback != "" {
		switch cfg.Providers.Reranking.Fallback {
		case "cohere":
			fallback, err = reranking.NewCohereProvider(cfg.Providers.Reranking.CohereAPIKey, cfg.Providers.Reranking.CohereModel)
		case "jina":
			fallback, err = reranking.NewJinaProvider(cfg.Providers.Reranking.JinaAPIKey, cfg.Providers.Reranking.JinaModel)
		default:
			logger.Warn("unknown fallback reranking provider, skipping", "provider", cfg.Providers.Reranking.Fallback)
		}
		if err != nil {
			logger.Warn("failed to initialize fallback reranker, continuing with primary only", "error", err)
			fallback = nil
		}
	}

	// Return primary provider with fallback
	logger.Info("reranking provider initialized",
		"primary", cfg.Providers.Reranking.Primary,
		"has_fallback", fallback != nil,
	)

	// If there's a fallback, use the router; otherwise return primary directly
	if fallback != nil {
		settings := router.CircuitBreakerSettings{
			MaxRequests: 3,
			Interval:    time.Minute,
			Timeout:     30 * time.Second,
		}
		return router.NewRerankRouter(primary, fallback, settings), nil
	}

	return primary, nil
}

func initGenerator(cfg *config.Config, logger *slog.Logger) (providers.GenerationProvider, error) {
	// Initialize primary provider
	var primary providers.GenerationProvider
	var err error

	switch cfg.Providers.Generation.Primary {
	case "anthropic", "claude":
		primary, err = generation.NewClaudeProvider(cfg.Providers.Generation.AnthropicAPIKey, cfg.Providers.Generation.AnthropicModel)
	case "openai":
		primary, err = generation.NewOpenAIProvider(cfg.Providers.Generation.OpenAIAPIKey, cfg.Providers.Generation.OpenAIModel)
	default:
		return nil, fmt.Errorf("unknown generation provider: %s", cfg.Providers.Generation.Primary)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize primary generator: %w", err)
	}

	// Initialize fallback provider if configured
	var fallback providers.GenerationProvider
	if cfg.Providers.Generation.Fallback != "" {
		switch cfg.Providers.Generation.Fallback {
		case "anthropic", "claude":
			fallback, err = generation.NewClaudeProvider(cfg.Providers.Generation.AnthropicAPIKey, cfg.Providers.Generation.AnthropicModel)
		case "openai":
			fallback, err = generation.NewOpenAIProvider(cfg.Providers.Generation.OpenAIAPIKey, cfg.Providers.Generation.OpenAIModel)
		default:
			logger.Warn("unknown fallback generation provider, skipping", "provider", cfg.Providers.Generation.Fallback)
		}
		if err != nil {
			logger.Warn("failed to initialize fallback generator, continuing with primary only", "error", err)
			fallback = nil
		}
	}

	// Return primary provider with fallback
	logger.Info("generation provider initialized",
		"primary", cfg.Providers.Generation.Primary,
		"has_fallback", fallback != nil,
	)

	// If there's a fallback, use the router; otherwise return primary directly
	if fallback != nil {
		settings := router.CircuitBreakerSettings{
			MaxRequests: 3,
			Interval:    time.Minute,
			Timeout:     30 * time.Second,
		}
		return router.NewGenerationRouter(primary, fallback, settings), nil
	}

	return primary, nil
}

func initCache(cfg *config.Config, logger *slog.Logger) (*cache.RedisCache, error) {
	client, err := cache.NewRedisCache(cfg.Redis.URL, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Redis cache: %w", err)
	}

	logger.Info("redis cache initialized", "url", cfg.Redis.URL)

	return client, nil
}

func initParentFetcher(cfg *config.Config, logger *slog.Logger) (*retrieval.ParentFetcher, error) {
	fetcher, err := retrieval.NewParentFetcher(cfg.Postgres.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize parent fetcher: %w", err)
	}

	logger.Info("parent fetcher initialized")

	return fetcher, nil
}
