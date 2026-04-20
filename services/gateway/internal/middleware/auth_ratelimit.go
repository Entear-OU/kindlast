package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

// AuthRateLimitConfig holds configuration for auth rate limiting
type AuthRateLimitConfig struct {
	// RequestsPerMinute is the maximum number of requests allowed per minute per IP
	RequestsPerMinute int
	// BlockDuration is how long to block an IP after exceeding the limit
	BlockDuration time.Duration
}

// DefaultAuthRateLimitConfig returns sensible defaults for auth rate limiting
func DefaultAuthRateLimitConfig() AuthRateLimitConfig {
	return AuthRateLimitConfig{
		RequestsPerMinute: 10,
		BlockDuration:     5 * time.Minute,
	}
}

// AuthRateLimit returns a middleware that enforces IP-based rate limiting for auth endpoints
// This is specifically designed for unauthenticated endpoints like login, register, and refresh
// to prevent brute force attacks and credential stuffing
func AuthRateLimit(redisClient *redis.Client, logger *slog.Logger, config AuthRateLimitConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get client IP address
			clientIP := getClientIP(r)
			if clientIP == "" {
				logger.Warn("could not determine client IP for auth rate limiting")
				// Allow request to proceed if we can't determine IP
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			// Check if IP is currently blocked
			blockKey := fmt.Sprintf("auth:blocked:%s", clientIP)
			blocked, err := redisClient.Get(ctx, blockKey).Result()
			if err == nil && blocked == "1" {
				ttl, _ := redisClient.TTL(ctx, blockKey).Result()
				retryAfter := int64(ttl.Seconds())
				if retryAfter < 0 {
					retryAfter = 0
				}

				logger.Warn("blocked IP attempted auth request",
					slog.String("ip", clientIP),
					slog.Int64("retry_after", retryAfter),
				)

				respondAuthRateLimitError(w, retryAfter, config.BlockDuration)
				return
			}

			// Generate Redis key for rate counting: auth:rate:{ip}:{minute}
			now := time.Now()
			minute := now.Truncate(time.Minute).Unix()
			rateKey := fmt.Sprintf("auth:rate:%s:%d", clientIP, minute)

			// Increment counter with TTL
			pipe := redisClient.Pipeline()
			incr := pipe.Incr(ctx, rateKey)
			pipe.Expire(ctx, rateKey, 2*time.Minute) // Keep for 2 minutes to allow overlap
			_, err = pipe.Exec(ctx)

			if err != nil {
				logger.Error("redis error during auth rate limit check",
					slog.String("error", err.Error()),
					slog.String("ip", clientIP),
				)
				// Allow request to proceed on Redis error (fail open)
				next.ServeHTTP(w, r)
				return
			}

			count := incr.Val()
			remaining := int64(config.RequestsPerMinute) - count
			if remaining < 0 {
				remaining = 0
			}
			reset := now.Truncate(time.Minute).Add(time.Minute)

			// Add rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", config.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset.Unix()))

			// Check if limit exceeded
			if count > int64(config.RequestsPerMinute) {
				// Block this IP for the configured duration
				if err := redisClient.Set(ctx, blockKey, "1", config.BlockDuration).Err(); err != nil {
					logger.Error("failed to set IP block in redis",
						slog.String("error", err.Error()),
						slog.String("ip", clientIP),
					)
				}

				retryAfter := int64(config.BlockDuration.Seconds())

				logger.Warn("auth rate limit exceeded, IP blocked",
					slog.String("ip", clientIP),
					slog.Int("limit", config.RequestsPerMinute),
					slog.Int64("current", count),
					slog.Duration("block_duration", config.BlockDuration),
				)

				respondAuthRateLimitError(w, retryAfter, config.BlockDuration)
				return
			}

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the real client IP from the request
// It checks X-Forwarded-For and X-Real-IP headers first (for reverse proxies)
// then falls back to RemoteAddr
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (may contain multiple IPs)
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// X-Forwarded-For can contain multiple IPs: client, proxy1, proxy2
		// The first IP is typically the original client
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	xrip := r.Header.Get("X-Real-IP")
	if xrip != "" {
		return strings.TrimSpace(xrip)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr might not have a port
		return r.RemoteAddr
	}
	return ip
}

// respondAuthRateLimitError sends a rate limit error response
func respondAuthRateLimitError(w http.ResponseWriter, retryAfterSecs int64, blockDuration time.Duration) {
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSecs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(models.ErrorResponse{
		Error:   "Too many requests",
		Message: fmt.Sprintf("Too many authentication attempts. Please try again in %v.", blockDuration),
		Code:    "AUTH_RATE_LIMIT_EXCEEDED",
	})
}
