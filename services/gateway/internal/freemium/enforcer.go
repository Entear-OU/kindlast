package freemium

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Citation limits per plan
const (
	FreePlanCitationLimit         = 3  // max 3 citations per response
	ProfessionalPlanCitationLimit = -1 // unlimited
	TeamPlanCitationLimit         = -1 // unlimited
	EnterprisePlanCitationLimit   = -1 // unlimited
)

// Enforcer enforces freemium limits (citation limits, etc.)
type Enforcer struct {
	redis *redis.Client
}

// NewEnforcer creates a new freemium enforcer
func NewEnforcer(redisURL string) (*Enforcer, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	// Configure connection pool
	opts.PoolSize = 10
	opts.MinIdleConns = 5
	opts.MaxIdleConns = 10
	opts.ConnMaxIdleTime = 5 * time.Minute
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Enforcer{
		redis: client,
	}, nil
}

// EnforceCitationLimit checks if a user can use the requested number of citations
func (e *Enforcer) EnforceCitationLimit(ctx context.Context, userID, plan string, citationCount int) error {
	// Get citation limit for plan
	limit := e.getCitationLimitForPlan(plan)

	// Unlimited plans
	if limit == -1 {
		return nil
	}

	// Check if citation count exceeds limit
	if citationCount > limit {
		return &CitationLimitError{
			Plan:          plan,
			Limit:         limit,
			Requested:     citationCount,
			UpgradeURL:    "/upgrade",
			UpgradePrompt: fmt.Sprintf("Your %s plan is limited to %d citations per response. Upgrade to Professional for unlimited citations.", plan, limit),
		}
	}

	return nil
}

// TrackCitationUsage tracks daily citation usage for analytics
func (e *Enforcer) TrackCitationUsage(ctx context.Context, userID string, citationCount int) error {
	// Generate Redis key: citations:{userID}:{date}
	now := time.Now()
	date := now.Format("2006-01-02")
	key := fmt.Sprintf("citations:%s:%s", userID, date)

	// Increment counter with 48 hour TTL (to keep data for reporting)
	pipe := e.redis.Pipeline()
	pipe.IncrBy(ctx, key, int64(citationCount))
	pipe.Expire(ctx, key, 48*time.Hour)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to track citation usage: %w", err)
	}

	return nil
}

// GetDailyCitationUsage returns the citation usage for a user on a specific date
func (e *Enforcer) GetDailyCitationUsage(ctx context.Context, userID string, date time.Time) (int, error) {
	dateStr := date.Format("2006-01-02")
	key := fmt.Sprintf("citations:%s:%s", userID, dateStr)

	count, err := e.redis.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil // No usage recorded
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get citation usage: %w", err)
	}

	return int(count), nil
}

// GetCitationUsageRange returns citation usage for a date range
func (e *Enforcer) GetCitationUsageRange(ctx context.Context, userID string, startDate, endDate time.Time) (map[string]int, error) {
	usage := make(map[string]int)

	// Iterate through each date in range
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		count, err := e.GetDailyCitationUsage(ctx, userID, d)
		if err != nil {
			return nil, err
		}
		usage[d.Format("2006-01-02")] = count
	}

	return usage, nil
}

// getCitationLimitForPlan returns the citation limit for a given plan
func (e *Enforcer) getCitationLimitForPlan(plan string) int {
	switch plan {
	case "free":
		return FreePlanCitationLimit
	case "professional":
		return ProfessionalPlanCitationLimit
	case "team":
		return TeamPlanCitationLimit
	case "enterprise":
		return EnterprisePlanCitationLimit
	default:
		return FreePlanCitationLimit // Default to free plan
	}
}

// Close closes the Redis connection
func (e *Enforcer) Close() error {
	return e.redis.Close()
}

// CitationLimitError represents a citation limit exceeded error
type CitationLimitError struct {
	Plan          string
	Limit         int
	Requested     int
	UpgradeURL    string
	UpgradePrompt string
}

func (e *CitationLimitError) Error() string {
	return e.UpgradePrompt
}
