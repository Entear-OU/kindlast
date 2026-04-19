package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

// PlanEnforcer handles plan-based feature enforcement for DPO Copilot
type PlanEnforcer struct {
	redis  *redis.Client
	logger *slog.Logger
}

// NewPlanEnforcer creates a new plan enforcer
func NewPlanEnforcer(redis *redis.Client, logger *slog.Logger) *PlanEnforcer {
	return &PlanEnforcer{
		redis:  redis,
		logger: logger,
	}
}

// RequirePlan returns middleware that ensures the user has one of the specified plans
func RequirePlan(allowedPlans ...string) func(next http.Handler) http.Handler {
	allowedPlanSet := make(map[string]bool)
	for _, plan := range allowedPlans {
		allowedPlanSet[plan] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := GetUser(r.Context())
			if !ok {
				respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
				return
			}

			if !allowedPlanSet[user.Plan] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "Forbidden",
					"message":     fmt.Sprintf("This feature requires a %v plan. Please upgrade.", allowedPlans),
					"code":        "PLAN_REQUIRED",
					"upgrade_url": "/upgrade",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// EnforceArtifactLimit checks if the user can generate more artifacts this month
func (e *PlanEnforcer) EnforceArtifactLimit(userID, plan string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limits := models.DPOCopilotPlanLimits[plan]

			// Check if plan allows artifacts
			if limits.MaxArtifactsPerMonth == 0 {
				respondError(w, http.StatusForbidden,
					"Artifact generation is not available on the free plan. Please upgrade to Professional.",
					"PLAN_LIMIT_EXCEEDED")
				return
			}

			// Unlimited artifacts
			if limits.MaxArtifactsPerMonth == -1 {
				next.ServeHTTP(w, r)
				return
			}

			// Check monthly usage
			ctx := r.Context()
			monthKey := e.artifactMonthKey(userID)

			count, err := e.redis.Get(ctx, monthKey).Int()
			if err != nil && err != redis.Nil {
				e.logger.Error("failed to check artifact usage", slog.String("error", err.Error()))
				// Allow request on Redis error to avoid blocking users
				next.ServeHTTP(w, r)
				return
			}

			if count >= limits.MaxArtifactsPerMonth {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "Too Many Requests",
					"message":     fmt.Sprintf("Monthly artifact limit reached (%d/%d). Resets on the 1st.", count, limits.MaxArtifactsPerMonth),
					"code":        "ARTIFACT_LIMIT_EXCEEDED",
					"upgrade_url": "/upgrade",
					"limit":       limits.MaxArtifactsPerMonth,
					"used":        count,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IncrementArtifactUsage increments the monthly artifact counter for a user
func (e *PlanEnforcer) IncrementArtifactUsage(ctx context.Context, userID string) error {
	monthKey := e.artifactMonthKey(userID)

	pipe := e.redis.Pipeline()
	pipe.Incr(ctx, monthKey)
	// Set expiry to end of current month + 1 day buffer
	pipe.ExpireAt(ctx, monthKey, e.endOfMonth())

	_, err := pipe.Exec(ctx)
	return err
}

// GetArtifactUsage returns the current monthly artifact usage for a user
func (e *PlanEnforcer) GetArtifactUsage(ctx context.Context, userID string) (int, error) {
	monthKey := e.artifactMonthKey(userID)
	count, err := e.redis.Get(ctx, monthKey).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return count, err
}

// artifactMonthKey generates the Redis key for monthly artifact tracking
func (e *PlanEnforcer) artifactMonthKey(userID string) string {
	now := time.Now()
	return fmt.Sprintf("artifacts:%s:%d-%02d", userID, now.Year(), now.Month())
}

// endOfMonth returns the timestamp for the end of the current month
func (e *PlanEnforcer) endOfMonth() time.Time {
	now := time.Now()
	// First day of next month
	firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
	return firstOfNextMonth
}

// RequireFeature returns middleware that checks if a feature is enabled for the user's plan
func RequireFeature(featureCheck func(limits models.DPOCopilotLimits) bool, featureName string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := GetUser(r.Context())
			if !ok {
				respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
				return
			}

			limits := models.DPOCopilotPlanLimits[user.Plan]
			if !featureCheck(limits) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":       "Forbidden",
					"message":     fmt.Sprintf("%s is not available on your current plan. Please upgrade.", featureName),
					"code":        "FEATURE_NOT_AVAILABLE",
					"upgrade_url": "/upgrade",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuditTrail middleware ensures the user's plan includes audit trail access
func RequireAuditTrail() func(next http.Handler) http.Handler {
	return RequireFeature(func(limits models.DPOCopilotLimits) bool {
		return limits.AuditTrailEnabled
	}, "Audit trail")
}

// RequireExport middleware ensures the user's plan includes export capability
func RequireExport() func(next http.Handler) http.Handler {
	return RequireFeature(func(limits models.DPOCopilotLimits) bool {
		return limits.ExportEnabled
	}, "Document export")
}

// RequireAIActModule middleware ensures the user's plan includes AI Act module
func RequireAIActModule() func(next http.Handler) http.Handler {
	return RequireFeature(func(limits models.DPOCopilotLimits) bool {
		return limits.AIActModuleEnabled
	}, "EU AI Act module")
}
