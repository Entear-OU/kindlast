package handlers

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

type HealthHandler struct {
	db          *sql.DB
	redisClient *redis.Client
	ragURL      string
	logger      *slog.Logger
	version     string
}

func NewHealthHandler(db *sql.DB, redisClient *redis.Client, ragURL string, logger *slog.Logger, version string) *HealthHandler {
	return &HealthHandler{
		db:          db,
		redisClient: redisClient,
		ragURL:      ragURL,
		logger:      logger,
		version:     version,
	}
}

// Health handles GET /health
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	components := make(map[string]string)
	overallStatus := "healthy"

	// Check database
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	err := h.db.PingContext(ctx)
	if err != nil {
		components["database"] = "unhealthy"
		overallStatus = "degraded"
		h.logger.Error("database health check failed", slog.String("error", err.Error()))
	} else {
		components["database"] = "healthy"
	}

	// Check Redis
	err = h.redisClient.Ping(ctx).Err()
	if err != nil {
		components["redis"] = "unhealthy"
		overallStatus = "degraded"
		h.logger.Error("redis health check failed", slog.String("error", err.Error()))
	} else {
		components["redis"] = "healthy"
	}

	// Check RAG service (simple connectivity check)
	ragStatus := h.checkRAGService(ctx)
	components["rag_service"] = ragStatus
	if ragStatus != "healthy" {
		overallStatus = "degraded"
	}

	status := http.StatusOK
	if overallStatus == "degraded" {
		status = http.StatusServiceUnavailable
	}

	respondJSON(w, status, models.HealthResponse{
		Status:     overallStatus,
		Version:    h.version,
		Components: components,
		Timestamp:  time.Now(),
	})
}

// Status handles GET /api/v1/status
func (h *HealthHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	health := make(map[string]bool)

	// Check database
	health["database"] = h.db.PingContext(ctx) == nil

	// Check Redis
	health["redis"] = h.redisClient.Ping(ctx).Err() == nil

	// Check RAG service
	health["rag_service"] = h.checkRAGService(ctx) == "healthy"

	// Determine overall status
	status := "operational"
	for _, healthy := range health {
		if !healthy {
			status = "degraded"
			break
		}
	}

	respondJSON(w, http.StatusOK, models.StatusResponse{
		Service:   "gateway",
		Status:    status,
		Health:    health,
		Timestamp: time.Now(),
	})
}

// checkRAGService performs a simple connectivity check to RAG service
func (h *HealthHandler) checkRAGService(ctx context.Context) string {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", h.ragURL+"/health", nil)
	if err != nil {
		h.logger.Error("failed to create RAG health check request", slog.String("error", err.Error()))
		return "unhealthy"
	}

	resp, err := client.Do(req)
	if err != nil {
		h.logger.Warn("RAG service health check failed", slog.String("error", err.Error()))
		return "unhealthy"
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return "healthy"
	}

	return "unhealthy"
}
