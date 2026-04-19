package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
)

// mockRedisClient is a simple mock for testing
type mockRedisClient struct {
	data map[string]int
}

func TestRequirePlan(t *testing.T) {
	tests := []struct {
		name           string
		userPlan       string
		allowedPlans   []string
		expectedStatus int
	}{
		{
			name:           "user has allowed plan",
			userPlan:       models.PlanProfessional,
			allowedPlans:   []string{models.PlanProfessional, models.PlanTeam},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "user has team plan",
			userPlan:       models.PlanTeam,
			allowedPlans:   []string{models.PlanProfessional, models.PlanTeam},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "free user denied",
			userPlan:       models.PlanFree,
			allowedPlans:   []string{models.PlanProfessional, models.PlanTeam},
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create middleware
			middleware := RequirePlan(tt.allowedPlans...)

			// Create test handler
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			// Create request with user context
			req := httptest.NewRequest("GET", "/test", nil)
			user := &models.User{ID: "user-123", Plan: tt.userPlan}
			ctx := context.WithValue(req.Context(), userContextKey, user)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestRequirePlan_NoUser(t *testing.T) {
	middleware := RequirePlan(models.PlanProfessional)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestDPOCopilotPlanLimits(t *testing.T) {
	// Test all plans have defined limits
	plans := []string{models.PlanFree, models.PlanProfessional, models.PlanTeam}

	for _, plan := range plans {
		t.Run(plan, func(t *testing.T) {
			limits, exists := models.DPOCopilotPlanLimits[plan]
			if !exists {
				t.Errorf("no limits defined for plan %s", plan)
				return
			}

			// Verify limits are sensible
			if plan == models.PlanFree {
				if limits.MaxClients != 0 {
					t.Errorf("free plan should have 0 clients, got %d", limits.MaxClients)
				}
				if limits.AuditTrailEnabled {
					t.Error("free plan should not have audit trail")
				}
			}

			if plan == models.PlanProfessional {
				if limits.MaxClients < 1 {
					t.Errorf("professional plan should have > 0 clients, got %d", limits.MaxClients)
				}
				if !limits.AuditTrailEnabled {
					t.Error("professional plan should have audit trail")
				}
			}

			if plan == models.PlanTeam {
				if limits.MaxClients < limits.MaxClients {
					t.Error("team plan should have more clients than professional")
				}
				if limits.TeamMembers < 2 {
					t.Errorf("team plan should have > 1 team members, got %d", limits.TeamMembers)
				}
			}
		})
	}
}

func TestRequireFeature_AuditTrail(t *testing.T) {
	tests := []struct {
		name           string
		userPlan       string
		expectedStatus int
	}{
		{
			name:           "free plan denied",
			userPlan:       models.PlanFree,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "professional plan allowed",
			userPlan:       models.PlanProfessional,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "team plan allowed",
			userPlan:       models.PlanTeam,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := RequireAuditTrail()

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			user := &models.User{ID: "user-123", Plan: tt.userPlan}
			ctx := context.WithValue(req.Context(), userContextKey, user)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestRequireFeature_Export(t *testing.T) {
	tests := []struct {
		name           string
		userPlan       string
		expectedStatus int
	}{
		{
			name:           "free plan denied",
			userPlan:       models.PlanFree,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "professional plan allowed",
			userPlan:       models.PlanProfessional,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := RequireExport()

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			user := &models.User{ID: "user-123", Plan: tt.userPlan}
			ctx := context.WithValue(req.Context(), userContextKey, user)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestRequireFeature_AIActModule(t *testing.T) {
	tests := []struct {
		name           string
		userPlan       string
		expectedStatus int
	}{
		{
			name:           "free plan denied",
			userPlan:       models.PlanFree,
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "professional plan allowed",
			userPlan:       models.PlanProfessional,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := RequireAIActModule()

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			user := &models.User{ID: "user-123", Plan: tt.userPlan}
			ctx := context.WithValue(req.Context(), userContextKey, user)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestArtifactMonthKey(t *testing.T) {
	enforcer := &PlanEnforcer{}

	// Test key format
	key := enforcer.artifactMonthKey("user-123")

	// Key should contain user ID and current month
	now := time.Now()
	expectedPrefix := "artifacts:user-123:"
	expectedSuffix := now.Format("2006-01")

	if len(key) == 0 {
		t.Error("key should not be empty")
	}

	if key[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("key should start with %q, got %q", expectedPrefix, key)
	}

	if key[len(key)-7:] != expectedSuffix {
		t.Errorf("key should end with %q, got %q", expectedSuffix, key)
	}
}

func TestEndOfMonth(t *testing.T) {
	enforcer := &PlanEnforcer{}

	endOfMonth := enforcer.endOfMonth()
	now := time.Now()

	// End of month should be in the future (or very close for edge cases)
	if endOfMonth.Before(now) {
		t.Error("end of month should be in the future")
	}

	// Should be first day of next month
	if endOfMonth.Day() != 1 {
		t.Errorf("end of month should be day 1, got %d", endOfMonth.Day())
	}

	// Should be next month
	expectedMonth := now.Month() + 1
	if expectedMonth > 12 {
		expectedMonth = 1
	}
	if endOfMonth.Month() != expectedMonth {
		t.Errorf("expected month %d, got %d", expectedMonth, endOfMonth.Month())
	}
}

func TestPlanEnforcement_PlanComparison(t *testing.T) {
	// Test that plan hierarchy is correct
	freeLimits := models.DPOCopilotPlanLimits[models.PlanFree]
	proLimits := models.DPOCopilotPlanLimits[models.PlanProfessional]
	teamLimits := models.DPOCopilotPlanLimits[models.PlanTeam]

	// Professional should have more than free
	if proLimits.MaxClients <= freeLimits.MaxClients {
		t.Error("professional should have more clients than free")
	}
	if proLimits.MaxArtifactsPerMonth <= freeLimits.MaxArtifactsPerMonth {
		t.Error("professional should have more artifacts than free")
	}

	// Team should have more than professional
	if teamLimits.MaxClients <= proLimits.MaxClients {
		t.Error("team should have more clients than professional")
	}
	if teamLimits.MaxArtifactsPerMonth <= proLimits.MaxArtifactsPerMonth {
		t.Error("team should have more artifacts than professional")
	}
	if teamLimits.TeamMembers <= proLimits.TeamMembers {
		t.Error("team should have more team members than professional")
	}
}
