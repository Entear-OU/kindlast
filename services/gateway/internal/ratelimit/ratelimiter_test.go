package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_Allow_FreePlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	redis, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redis.Close()

	limiter := NewRateLimiter(redis)
	ctx := context.Background()
	userID := "test-user-free-" + time.Now().Format("20060102150405")

	// Clean up after test
	defer limiter.Reset(ctx, userID)

	// Test free plan limit (20 requests per hour)
	for i := 1; i <= 20; i++ {
		result, err := limiter.Allow(ctx, userID, "free")
		if err != nil {
			t.Fatalf("Failed to check rate limit: %v", err)
		}

		if !result.Allowed {
			t.Errorf("Request %d should be allowed", i)
		}

		if result.Limit != FreePlanLimit {
			t.Errorf("Expected limit %d, got %d", FreePlanLimit, result.Limit)
		}

		expectedRemaining := int64(FreePlanLimit - i)
		if result.Remaining != expectedRemaining {
			t.Errorf("Request %d: expected remaining %d, got %d", i, expectedRemaining, result.Remaining)
		}
	}

	// 21st request should be denied
	result, err := limiter.Allow(ctx, userID, "free")
	if err != nil {
		t.Fatalf("Failed to check rate limit: %v", err)
	}

	if result.Allowed {
		t.Error("Request 21 should be denied for free plan")
	}

	if result.RetryAfter <= 0 {
		t.Error("RetryAfter should be set when rate limit exceeded")
	}
}

func TestRateLimiter_Allow_ProfessionalPlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	redis, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redis.Close()

	limiter := NewRateLimiter(redis)
	ctx := context.Background()
	userID := "test-user-pro-" + time.Now().Format("20060102150405")

	defer limiter.Reset(ctx, userID)

	// Test professional plan limit (500 requests per hour)
	result, err := limiter.Allow(ctx, userID, "professional")
	if err != nil {
		t.Fatalf("Failed to check rate limit: %v", err)
	}

	if !result.Allowed {
		t.Error("Request should be allowed for professional plan")
	}

	if result.Limit != ProfessionalPlanLimit {
		t.Errorf("Expected limit %d, got %d", ProfessionalPlanLimit, result.Limit)
	}
}

func TestRateLimiter_Allow_TeamPlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	redis, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redis.Close()

	limiter := NewRateLimiter(redis)
	ctx := context.Background()
	userID := "test-user-team-" + time.Now().Format("20060102150405")

	defer limiter.Reset(ctx, userID)

	// Test team plan limit (5000 requests per hour)
	result, err := limiter.Allow(ctx, userID, "team")
	if err != nil {
		t.Fatalf("Failed to check rate limit: %v", err)
	}

	if !result.Allowed {
		t.Error("Request should be allowed for team plan")
	}

	if result.Limit != TeamPlanLimit {
		t.Errorf("Expected limit %d, got %d", TeamPlanLimit, result.Limit)
	}
}

func TestRateLimiter_GetUsage(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	redis, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redis.Close()

	limiter := NewRateLimiter(redis)
	ctx := context.Background()
	userID := "test-user-usage-" + time.Now().Format("20060102150405")

	defer limiter.Reset(ctx, userID)

	// Make some requests
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx, userID, "free")
	}

	// Check usage
	result, err := limiter.GetUsage(ctx, userID, "free")
	if err != nil {
		t.Fatalf("Failed to get usage: %v", err)
	}

	if result.Remaining != int64(FreePlanLimit-5) {
		t.Errorf("Expected remaining %d, got %d", FreePlanLimit-5, result.Remaining)
	}
}

func TestRateLimiter_Reset(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	redis, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redis.Close()

	limiter := NewRateLimiter(redis)
	ctx := context.Background()
	userID := "test-user-reset-" + time.Now().Format("20060102150405")

	// Make some requests
	for i := 0; i < 10; i++ {
		limiter.Allow(ctx, userID, "free")
	}

	// Verify usage
	result, err := limiter.GetUsage(ctx, userID, "free")
	if err != nil {
		t.Fatalf("Failed to get usage: %v", err)
	}
	if result.Remaining != int64(FreePlanLimit-10) {
		t.Errorf("Expected remaining %d, got %d", FreePlanLimit-10, result.Remaining)
	}

	// Reset
	err = limiter.Reset(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to reset: %v", err)
	}

	// Verify reset
	result, err = limiter.GetUsage(ctx, userID, "free")
	if err != nil {
		t.Fatalf("Failed to get usage after reset: %v", err)
	}
	if result.Remaining != int64(FreePlanLimit) {
		t.Errorf("Expected remaining %d after reset, got %d", FreePlanLimit, result.Remaining)
	}
}

func TestRateLimiter_DefaultPlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	redis, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer redis.Close()

	limiter := NewRateLimiter(redis)
	ctx := context.Background()
	userID := "test-user-default-" + time.Now().Format("20060102150405")

	defer limiter.Reset(ctx, userID)

	// Test with unknown plan (should default to free)
	result, err := limiter.Allow(ctx, userID, "unknown-plan")
	if err != nil {
		t.Fatalf("Failed to check rate limit: %v", err)
	}

	if result.Limit != FreePlanLimit {
		t.Errorf("Unknown plan should default to free plan limit %d, got %d", FreePlanLimit, result.Limit)
	}
}
