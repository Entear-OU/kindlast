package freemium

import (
	"context"
	"testing"
	"time"
)

func getTestRedisURL() string {
	return "redis://localhost:6379/1" // Use DB 1 for tests
}

func TestEnforcer_EnforceCitationLimit_FreePlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-free-" + time.Now().Format("20060102150405")

	// Test within limit (3 citations)
	err = enforcer.EnforceCitationLimit(ctx, userID, "free", 3)
	if err != nil {
		t.Errorf("Should allow 3 citations for free plan, got error: %v", err)
	}

	// Test within limit (2 citations)
	err = enforcer.EnforceCitationLimit(ctx, userID, "free", 2)
	if err != nil {
		t.Errorf("Should allow 2 citations for free plan, got error: %v", err)
	}

	// Test exceeding limit (4 citations)
	err = enforcer.EnforceCitationLimit(ctx, userID, "free", 4)
	if err == nil {
		t.Error("Should deny 4 citations for free plan")
	}

	// Check error type
	if _, ok := err.(*CitationLimitError); !ok {
		t.Errorf("Expected CitationLimitError, got %T", err)
	}

	// Check error details
	if citationErr, ok := err.(*CitationLimitError); ok {
		if citationErr.Plan != "free" {
			t.Errorf("Expected plan 'free', got '%s'", citationErr.Plan)
		}
		if citationErr.Limit != 3 {
			t.Errorf("Expected limit 3, got %d", citationErr.Limit)
		}
		if citationErr.Requested != 4 {
			t.Errorf("Expected requested 4, got %d", citationErr.Requested)
		}
	}
}

func TestEnforcer_EnforceCitationLimit_ProfessionalPlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-pro-" + time.Now().Format("20060102150405")

	// Professional plan has unlimited citations
	for _, count := range []int{1, 5, 10, 50, 100} {
		err = enforcer.EnforceCitationLimit(ctx, userID, "professional", count)
		if err != nil {
			t.Errorf("Professional plan should allow %d citations, got error: %v", count, err)
		}
	}
}

func TestEnforcer_EnforceCitationLimit_TeamPlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-team-" + time.Now().Format("20060102150405")

	// Team plan has unlimited citations
	for _, count := range []int{1, 5, 10, 50, 100} {
		err = enforcer.EnforceCitationLimit(ctx, userID, "team", count)
		if err != nil {
			t.Errorf("Team plan should allow %d citations, got error: %v", count, err)
		}
	}
}

func TestEnforcer_TrackCitationUsage(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-track-" + time.Now().Format("20060102150405")
	today := time.Now()

	// Track some usage
	err = enforcer.TrackCitationUsage(ctx, userID, 5)
	if err != nil {
		t.Fatalf("Failed to track usage: %v", err)
	}

	// Track more usage
	err = enforcer.TrackCitationUsage(ctx, userID, 3)
	if err != nil {
		t.Fatalf("Failed to track usage: %v", err)
	}

	// Get daily usage
	usage, err := enforcer.GetDailyCitationUsage(ctx, userID, today)
	if err != nil {
		t.Fatalf("Failed to get usage: %v", err)
	}

	if usage != 8 {
		t.Errorf("Expected usage 8, got %d", usage)
	}
}

func TestEnforcer_GetDailyCitationUsage_NoData(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-nodata-" + time.Now().Format("20060102150405")
	today := time.Now()

	// Get usage for user with no data
	usage, err := enforcer.GetDailyCitationUsage(ctx, userID, today)
	if err != nil {
		t.Fatalf("Failed to get usage: %v", err)
	}

	if usage != 0 {
		t.Errorf("Expected usage 0 for user with no data, got %d", usage)
	}
}

func TestEnforcer_GetCitationUsageRange(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-range-" + time.Now().Format("20060102150405")

	// Track usage for today
	today := time.Now()
	err = enforcer.TrackCitationUsage(ctx, userID, 10)
	if err != nil {
		t.Fatalf("Failed to track usage: %v", err)
	}

	// Get usage range (today to today)
	usageMap, err := enforcer.GetCitationUsageRange(ctx, userID, today, today)
	if err != nil {
		t.Fatalf("Failed to get usage range: %v", err)
	}

	todayStr := today.Format("2006-01-02")
	if usage, ok := usageMap[todayStr]; !ok {
		t.Error("Expected usage data for today")
	} else if usage != 10 {
		t.Errorf("Expected usage 10 for today, got %d", usage)
	}

	// Get usage range (7 days)
	startDate := today.AddDate(0, 0, -6)
	usageMap, err = enforcer.GetCitationUsageRange(ctx, userID, startDate, today)
	if err != nil {
		t.Fatalf("Failed to get usage range: %v", err)
	}

	if len(usageMap) != 7 {
		t.Errorf("Expected 7 days of data, got %d", len(usageMap))
	}

	// Check that today has data and other days have 0
	for date, usage := range usageMap {
		if date == todayStr {
			if usage != 10 {
				t.Errorf("Expected usage 10 for today, got %d", usage)
			}
		} else {
			if usage != 0 {
				t.Errorf("Expected usage 0 for %s, got %d", date, usage)
			}
		}
	}
}

func TestCitationLimitError_Error(t *testing.T) {
	err := &CitationLimitError{
		Plan:          "free",
		Limit:         3,
		Requested:     5,
		UpgradeURL:    "/upgrade",
		UpgradePrompt: "Your free plan is limited to 3 citations per response.",
	}

	errStr := err.Error()
	if errStr != "Your free plan is limited to 3 citations per response." {
		t.Errorf("Unexpected error message: %s", errStr)
	}
}

func TestEnforcer_DefaultPlan(t *testing.T) {
	redisURL := getTestRedisURL()
	if redisURL == "" {
		t.Skip("REDIS_URL not set, skipping integration test")
	}

	enforcer, err := NewEnforcer(redisURL)
	if err != nil {
		t.Fatalf("Failed to create enforcer: %v", err)
	}
	defer enforcer.Close()

	ctx := context.Background()
	userID := "test-user-default-" + time.Now().Format("20060102150405")

	// Test with unknown plan (should default to free plan limit)
	err = enforcer.EnforceCitationLimit(ctx, userID, "unknown-plan", 4)
	if err == nil {
		t.Error("Unknown plan should default to free plan and deny 4 citations")
	}

	err = enforcer.EnforceCitationLimit(ctx, userID, "unknown-plan", 3)
	if err != nil {
		t.Errorf("Unknown plan should default to free plan and allow 3 citations, got error: %v", err)
	}
}
