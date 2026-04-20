package handlers

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
	_ "github.com/lib/pq"
)

// setupTestDB creates a test database connection
// Returns nil if DATABASE_URL is not set (skips integration tests)
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to connect to database: %v", err)
	}

	return db
}

// createTestUser creates a test user in the database and returns it
func createTestUser(t *testing.T, db *sql.DB, id, email string) *models.User {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO users (id, email, password_hash, full_name, plan, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO NOTHING
	`, id, email, "test_hash", "Test User", models.PlanProfessional, now, now)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	return &models.User{
		ID:        id,
		Email:     email,
		FullName:  "Test User",
		Plan:      models.PlanProfessional,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// cleanupTestUser removes a test user and related data
func cleanupTestUser(t *testing.T, db *sql.DB, userID string) {
	t.Helper()
	// Clean up in reverse dependency order
	db.Exec("DELETE FROM findings WHERE user_id = $1", userID)
	db.Exec("DELETE FROM assessments WHERE user_id = $1", userID)
	db.Exec("DELETE FROM business_profiles WHERE user_id = $1", userID)
	db.Exec("DELETE FROM users WHERE id = $1", userID)
}

// createTestProfile creates a test business profile for a user
func createTestProfile(t *testing.T, db *sql.DB, userID, profileID string) *models.BusinessProfile {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO business_profiles (
			id, user_id, company_name, country, industry, employee_count,
			processes_personal_data, data_types, uses_ai_systems, ai_system_descriptions,
			third_party_processors, transfers_data_outside_eu, has_dpo, has_privacy_policy,
			has_cookie_consent, has_breach_notification, has_dsr_process, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (id) DO NOTHING
	`, profileID, userID, "Test Company", "DE", "Technology", "11-50",
		true, []byte(`{"personal","financial"}`), false, "",
		[]byte(`{}`), false, true, true, true, true, true, now, now)
	if err != nil {
		t.Fatalf("failed to create test profile: %v", err)
	}

	return &models.BusinessProfile{
		ID:                    profileID,
		UserID:                userID,
		CompanyName:           "Test Company",
		Country:               "DE",
		Industry:              "Technology",
		EmployeeCount:         "11-50",
		ProcessesPersonalData: true,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

// createTestAssessment creates a test assessment for a user
func createTestAssessment(t *testing.T, db *sql.DB, assessmentID, userID string, assessmentType string) *models.Assessment {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO assessments (id, user_id, type, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING
	`, assessmentID, userID, assessmentType, models.AssessmentStatusComplete, now)
	if err != nil {
		t.Fatalf("failed to create test assessment: %v", err)
	}

	return &models.Assessment{
		ID:        assessmentID,
		UserID:    userID,
		Type:      assessmentType,
		Status:    models.AssessmentStatusComplete,
		CreatedAt: now,
	}
}

// createTestFinding creates a test finding for an assessment and user
func createTestFinding(t *testing.T, db *sql.DB, findingID, assessmentID, userID string) *models.Finding {
	t.Helper()
	now := time.Now()
	_, err := db.Exec(`
		INSERT INTO findings (id, assessment_id, user_id, category, severity, title, description,
			recommendation, is_resolved, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO NOTHING
	`, findingID, assessmentID, userID, "data_protection", models.FindingSeverityHigh,
		"Test Finding", "This is a test finding description",
		"Implement proper data protection measures", false, now)
	if err != nil {
		t.Fatalf("failed to create test finding: %v", err)
	}

	return &models.Finding{
		ID:             findingID,
		AssessmentID:   assessmentID,
		UserID:         userID,
		Category:       "data_protection",
		Severity:       models.FindingSeverityHigh,
		Title:          "Test Finding",
		Description:    "This is a test finding description",
		Recommendation: "Implement proper data protection measures",
		IsResolved:     false,
		CreatedAt:      now,
	}
}

// withUserContext adds a user to the request context using the middleware's SetUser function
func withUserContext(ctx context.Context, user *models.User) context.Context {
	return middleware.SetUser(ctx, user)
}
