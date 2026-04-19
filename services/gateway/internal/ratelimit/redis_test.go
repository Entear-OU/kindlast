package ratelimit

import (
	"context"
	"testing"
	"time"
)

// Note: These tests require a running Redis instance or use of miniredis for mocking
// For integration testing, set REDIS_URL environment variable
// For unit tests, we'll use miniredis (added to dependencies)

func TestRedisClient_Increment(t *testing.T) {
	// Skip if no Redis available
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	client, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	key := "test:increment:" + time.Now().Format("20060102150405")

	// First increment should return 1
	count, err := client.Increment(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("Failed to increment: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Second increment should return 2
	count, err = client.Increment(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("Failed to increment: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	// Clean up
	client.Delete(ctx, key)
}

func TestRedisClient_Get(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	client, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	key := "test:get:" + time.Now().Format("20060102150405")

	// Get non-existent key should return 0
	count, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	// Increment and get
	client.Increment(ctx, key, time.Minute)
	count, err = client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Clean up
	client.Delete(ctx, key)
}

func TestRedisClient_TTL(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	client, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	key := "test:ttl:" + time.Now().Format("20060102150405")

	// Set key with TTL
	client.Increment(ctx, key, time.Minute)

	// Check TTL
	ttl, err := client.TTL(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get TTL: %v", err)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Errorf("Expected TTL between 0 and 1 minute, got %v", ttl)
	}

	// Clean up
	client.Delete(ctx, key)
}

func TestRedisClient_Delete(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	client, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	key := "test:delete:" + time.Now().Format("20060102150405")

	// Create key
	client.Increment(ctx, key, time.Minute)

	// Verify it exists
	count, _ := client.Get(ctx, key)
	if count != 1 {
		t.Errorf("Expected count 1 before delete, got %d", count)
	}

	// Delete
	err = client.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify it's gone
	count, _ = client.Get(ctx, key)
	if count != 0 {
		t.Errorf("Expected count 0 after delete, got %d", count)
	}
}

func TestRedisClient_Ping(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	client, err := NewRedisClient(redisURL)
	if err != nil {
		t.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	err = client.Ping(ctx)
	if err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

// Helper function to get Redis URL for testing
func getTestRedisURL() string {
	// You can set REDIS_URL environment variable for integration tests
	// Or return a default for local testing
	// For CI/CD, you might want to skip these tests if Redis is not available
	return "redis://localhost:6379/1" // Use DB 1 for tests
}
