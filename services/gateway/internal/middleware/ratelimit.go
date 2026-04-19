package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

// RateLimit returns a middleware that enforces rate limiting using Redis
// Uses token bucket algorithm with hourly windows
// Rate limits per plan:
//   - Free: 20 requests/hour
//   - Professional: 500 requests/hour
//   - Team: 5000 requests/hour
func RateLimit(redisClient *redis.Client, logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from context (set by Auth middleware)
			user, ok := GetUser(r.Context())
			if !ok {
				// Should not happen if Auth middleware is used before this
				respondError(w, http.StatusInternalServerError, "User not found in context", "INTERNAL_ERROR")
				return
			}

			// Get rate limit for plan
			limit := getRateLimitForPlan(user.Plan)

			// Generate Redis key: ratelimit:{userID}:{hour}
			now := time.Now()
			hour := now.Truncate(time.Hour).Unix()
			key := fmt.Sprintf("ratelimit:%s:%d", user.ID, hour)

			// Increment counter with 1 hour + 5 minute TTL (buffer)
			pipe := redisClient.Pipeline()
			incr := pipe.Incr(r.Context(), key)
			pipe.Expire(r.Context(), key, time.Hour+5*time.Minute)
			_, err := pipe.Exec(r.Context())

			if err != nil {
				logger.Error("redis error", slog.String("error", err.Error()))
				// Allow request to proceed on Redis error (fail open)
				next.ServeHTTP(w, r)
				return
			}

			count := incr.Val()
			reset := now.Truncate(time.Hour).Add(time.Hour)
			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}

			// Add rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset.Unix()))

			// Check if limit exceeded
			if count > int64(limit) {
				retryAfter := int64(time.Until(reset).Seconds())
				if retryAfter < 0 {
					retryAfter = 0
				}

				logger.Warn("rate limit exceeded",
					slog.String("user_id", user.ID),
					slog.String("plan", user.Plan),
					slog.Int("limit", limit),
					slog.Int64("current", count),
				)

				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(models.ErrorResponse{
					Error:   "Too many requests",
					Message: fmt.Sprintf("Rate limit of %d requests per hour exceeded. Please try again in %d seconds.", limit, retryAfter),
					Code:    "RATE_LIMIT_EXCEEDED",
				})
				return
			}

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// getRateLimitForPlan returns the hourly rate limit for a given plan
func getRateLimitForPlan(plan string) int {
	switch plan {
	case "free":
		return 20
	case "professional":
		return 500
	case "team", "enterprise":
		return 5000
	default:
		return 20 // Default to free plan
	}
}
