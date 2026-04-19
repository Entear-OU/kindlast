package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

// RAGResponse represents a RAG response with citations
type RAGResponse struct {
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations,omitempty"`
}

// Citation represents a citation in the response
type Citation struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

// Freemium returns a middleware that enforces freemium citation limits
// Free plan: max 3 citations per response
// Professional/Team: unlimited citations
func Freemium(redisClient *redis.Client, logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from context (set by Auth middleware)
			user, ok := GetUser(r.Context())
			if !ok {
				respondError(w, http.StatusInternalServerError, "User not found in context", "INTERNAL_ERROR")
				return
			}

			// Only intercept RAG query responses
			if !strings.Contains(r.URL.Path, "/query") && !strings.Contains(r.URL.Path, "/rag") {
				next.ServeHTTP(w, r)
				return
			}

			// For streaming responses, add plan headers for downstream services
			if r.Header.Get("Accept") == "text/event-stream" {
				r.Header.Set("X-User-Plan", user.Plan)
				citationLimit := getCitationLimitForPlan(user.Plan)
				r.Header.Set("X-Citation-Limit", fmt.Sprintf("%d", citationLimit))
				next.ServeHTTP(w, r)
				return
			}

			// Create response interceptor
			interceptor := &responseInterceptor{
				ResponseWriter: w,
				body:           &bytes.Buffer{},
				statusCode:     http.StatusOK,
				ctx:            r.Context(),
				redisClient:    redisClient,
				logger:         logger,
				userID:         user.ID,
				plan:           user.Plan,
			}

			// Call the next handler with our interceptor
			next.ServeHTTP(interceptor, r)

			// Process the intercepted response
			if interceptor.statusCode == http.StatusOK {
				// Try to parse as RAG response
				var ragResponse RAGResponse
				if err := json.Unmarshal(interceptor.body.Bytes(), &ragResponse); err != nil {
					// Not a RAG response or invalid JSON, pass through
					w.WriteHeader(interceptor.statusCode)
					w.Write(interceptor.body.Bytes())
					return
				}

				// Count citations
				citationCount := len(ragResponse.Citations)

				// Enforce citation limit
				limit := getCitationLimitForPlan(user.Plan)
				if limit != -1 && citationCount > limit {
					logger.Warn("citation limit exceeded",
						slog.String("user_id", user.ID),
						slog.String("plan", user.Plan),
						slog.Int("limit", limit),
						slog.Int("citations", citationCount),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(models.ErrorResponse{
						Error:   "Citation limit exceeded",
						Message: fmt.Sprintf("Your %s plan is limited to %d citations per response. Upgrade to Professional for unlimited citations.", user.Plan, limit),
						Code:    "CITATION_LIMIT_EXCEEDED",
					})
					return
				}

				// Track citation usage for analytics
				if citationCount > 0 {
					go trackCitationUsage(context.Background(), redisClient, user.ID, citationCount, logger)
				}
			}

			// Write the original response
			for key, values := range interceptor.Header() {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(interceptor.statusCode)
			w.Write(interceptor.body.Bytes())
		})
	}
}

// responseInterceptor wraps http.ResponseWriter to intercept and modify responses
type responseInterceptor struct {
	http.ResponseWriter
	body        *bytes.Buffer
	statusCode  int
	ctx         context.Context
	redisClient *redis.Client
	logger      *slog.Logger
	userID      string
	plan        string
}

func (ri *responseInterceptor) Write(b []byte) (int, error) {
	return ri.body.Write(b)
}

func (ri *responseInterceptor) WriteHeader(statusCode int) {
	ri.statusCode = statusCode
}

// getCitationLimitForPlan returns the citation limit for a given plan
func getCitationLimitForPlan(plan string) int {
	switch plan {
	case "free":
		return 3 // Max 3 citations per response
	case "professional", "team", "enterprise":
		return -1 // Unlimited
	default:
		return 3 // Default to free plan
	}
}

// trackCitationUsage tracks daily citation usage for analytics
func trackCitationUsage(ctx context.Context, redisClient *redis.Client, userID string, citationCount int, logger *slog.Logger) {
	now := time.Now()
	date := now.Format("2006-01-02")
	key := fmt.Sprintf("citations:%s:%s", userID, date)

	// Increment counter with 48 hour TTL (to keep data for reporting)
	pipe := redisClient.Pipeline()
	pipe.IncrBy(ctx, key, int64(citationCount))
	pipe.Expire(ctx, key, 48*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Error("failed to track citation usage", slog.String("error", err.Error()))
	}
}
