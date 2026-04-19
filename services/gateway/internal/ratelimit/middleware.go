package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// UserIDKey is the context key for user ID
	UserIDKey contextKey = "userID"
	// PlanKey is the context key for user plan
	PlanKey contextKey = "plan"
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message,omitempty"`
	RetryAfter int64  `json:"retry_after,omitempty"` // seconds
}

// RateLimitMiddleware creates middleware that enforces rate limits
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract user ID and plan from context (set by auth middleware)
			userID, ok := ctx.Value(UserIDKey).(string)
			if !ok || userID == "" {
				// No user in context, skip rate limiting (for public endpoints)
				next.ServeHTTP(w, r)
				return
			}

			plan, ok := ctx.Value(PlanKey).(string)
			if !ok || plan == "" {
				plan = "free" // Default to free plan
			}

			// Check rate limit
			result, err := limiter.Allow(ctx, userID, plan)
			if err != nil {
				// Log error but allow request to proceed
				// (failing open is better than failing closed for rate limiting)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			// Add rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.Reset.Unix(), 10))

			// Check if rate limit exceeded
			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)

				response := ErrorResponse{
					Error:      "rate limit exceeded",
					Message:    fmt.Sprintf("Rate limit of %d requests per hour exceeded. Please try again in %d seconds.", result.Limit, int64(result.RetryAfter.Seconds())),
					RetryAfter: int64(result.RetryAfter.Seconds()),
				}

				json.NewEncoder(w).Encode(response)
				return
			}

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// WithUser adds user information to the request context
func WithUser(ctx context.Context, userID, plan string) context.Context {
	ctx = context.WithValue(ctx, UserIDKey, userID)
	ctx = context.WithValue(ctx, PlanKey, plan)
	return ctx
}

// GetUserFromContext extracts user information from context
func GetUserFromContext(ctx context.Context) (userID, plan string, ok bool) {
	userID, ok1 := ctx.Value(UserIDKey).(string)
	plan, ok2 := ctx.Value(PlanKey).(string)
	return userID, plan, ok1 && ok2
}
