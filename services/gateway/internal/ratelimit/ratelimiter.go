package ratelimit

import (
	"context"
	"fmt"
	"time"
)

// Plan rate limits (requests per hour)
const (
	FreePlanLimit         = 20
	ProfessionalPlanLimit = 500
	TeamPlanLimit         = 5000
	EnterprisePlanLimit   = 5000 // Same as team for now
)

// RateLimiter implements token bucket algorithm using Redis
type RateLimiter struct {
	redis *RedisClient
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(redis *RedisClient) *RateLimiter {
	return &RateLimiter{
		redis: redis,
	}
}

// RateLimitResult contains the result of a rate limit check
type RateLimitResult struct {
	Allowed    bool
	Limit      int64
	Remaining  int64
	Reset      time.Time
	RetryAfter time.Duration
}

// Allow checks if a request is allowed based on the user's plan
func (rl *RateLimiter) Allow(ctx context.Context, userID, plan string) (*RateLimitResult, error) {
	// Get rate limit for plan
	limit := rl.getLimitForPlan(plan)

	// Generate Redis key: ratelimit:{userID}:{hour}
	now := time.Now()
	hour := now.Truncate(time.Hour).Unix()
	key := fmt.Sprintf("ratelimit:%s:%d", userID, hour)

	// Increment counter with 1 hour TTL
	count, err := rl.redis.Increment(ctx, key, time.Hour+5*time.Minute) // Add buffer to TTL
	if err != nil {
		return nil, fmt.Errorf("failed to check rate limit: %w", err)
	}

	// Calculate reset time (start of next hour)
	reset := now.Truncate(time.Hour).Add(time.Hour)

	// Check if limit exceeded
	allowed := count <= int64(limit)
	remaining := int64(limit) - count
	if remaining < 0 {
		remaining = 0
	}

	result := &RateLimitResult{
		Allowed:   allowed,
		Limit:     int64(limit),
		Remaining: remaining,
		Reset:     reset,
	}

	// Calculate retry after duration if limit exceeded
	if !allowed {
		result.RetryAfter = time.Until(reset)
		if result.RetryAfter < 0 {
			result.RetryAfter = 0
		}
	}

	return result, nil
}

// GetUsage returns the current usage for a user
func (rl *RateLimiter) GetUsage(ctx context.Context, userID, plan string) (*RateLimitResult, error) {
	limit := rl.getLimitForPlan(plan)

	// Get current hour's key
	now := time.Now()
	hour := now.Truncate(time.Hour).Unix()
	key := fmt.Sprintf("ratelimit:%s:%d", userID, hour)

	// Get current count
	count, err := rl.redis.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage: %w", err)
	}

	// Calculate reset time
	reset := now.Truncate(time.Hour).Add(time.Hour)

	remaining := int64(limit) - count
	if remaining < 0 {
		remaining = 0
	}

	return &RateLimitResult{
		Allowed:   count <= int64(limit),
		Limit:     int64(limit),
		Remaining: remaining,
		Reset:     reset,
	}, nil
}

// Reset clears the rate limit for a user (admin operation)
func (rl *RateLimiter) Reset(ctx context.Context, userID string) error {
	now := time.Now()
	hour := now.Truncate(time.Hour).Unix()
	key := fmt.Sprintf("ratelimit:%s:%d", userID, hour)

	if err := rl.redis.Delete(ctx, key); err != nil {
		return fmt.Errorf("failed to reset rate limit: %w", err)
	}

	return nil
}

// getLimitForPlan returns the rate limit for a given plan
func (rl *RateLimiter) getLimitForPlan(plan string) int {
	switch plan {
	case "free":
		return FreePlanLimit
	case "professional":
		return ProfessionalPlanLimit
	case "team":
		return TeamPlanLimit
	case "enterprise":
		return EnterprisePlanLimit
	default:
		return FreePlanLimit // Default to free plan
	}
}
