package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
	_ "github.com/lib/pq"
)

// TestAssessmentHandler_GetAssessment_Authorization tests that users can only access their own assessments
func TestAssessmentHandler_GetAssessment_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "assessment-auth-user-1", "assessauth1@test.com")
	user2 := createTestUser(t, db, "assessment-auth-user-2", "assessauth2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessment for user1
	assessment1 := createTestAssessment(t, db, "assessment-auth-1", user1.ID, models.AssessmentTypeGDPR)

	tests := []struct {
		name           string
		assessmentID   string
		requestingUser *models.User
		expectedStatus int
		description    string
	}{
		{
			name:           "user can access own assessment",
			assessmentID:   assessment1.ID,
			requestingUser: user1,
			expectedStatus: http.StatusOK,
			description:    "User1 accessing their own assessment should succeed",
		},
		{
			name:           "user cannot access another user's assessment",
			assessmentID:   assessment1.ID,
			requestingUser: user2,
			expectedStatus: http.StatusNotFound,
			description:    "User2 accessing User1's assessment should return 404 (ownership check in query)",
		},
		{
			name:           "assessment not found returns 404",
			assessmentID:   "non-existent-assessment",
			requestingUser: user1,
			expectedStatus: http.StatusNotFound,
			description:    "Non-existent assessment should return 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/"+tt.assessmentID, nil)
			req.SetPathValue("id", tt.assessmentID)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.GetAssessment(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d (body: %s)",
					tt.description, tt.expectedStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestAssessmentHandler_ListAssessments_Authorization tests that users can only list their own assessments
func TestAssessmentHandler_ListAssessments_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "list-assess-user-1", "listassess1@test.com")
	user2 := createTestUser(t, db, "list-assess-user-2", "listassess2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessments for user1
	createTestAssessment(t, db, "list-assessment-1a", user1.ID, models.AssessmentTypeGDPR)
	createTestAssessment(t, db, "list-assessment-1b", user1.ID, models.AssessmentTypeAIAct)

	// Create assessment for user2
	createTestAssessment(t, db, "list-assessment-2a", user2.ID, models.AssessmentTypeGDPR)

	tests := []struct {
		name           string
		requestingUser *models.User
		expectedCount  int
		description    string
	}{
		{
			name:           "user1 sees only their assessments",
			requestingUser: user1,
			expectedCount:  2,
			description:    "User1 should see only their 2 assessments",
		},
		{
			name:           "user2 sees only their assessments",
			requestingUser: user2,
			expectedCount:  1,
			description:    "User2 should see only their 1 assessment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments", nil)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ListAssessments(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("%s: expected status 200, got %d", tt.description, rr.Code)
				return
			}

			var resp models.AssessmentListResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Errorf("failed to decode response: %v", err)
				return
			}

			if resp.Total != tt.expectedCount {
				t.Errorf("%s: expected %d assessments, got %d", tt.description, tt.expectedCount, resp.Total)
			}

			// Verify all returned assessments belong to the requesting user
			for _, assessment := range resp.Assessments {
				if assessment.UserID != tt.requestingUser.ID {
					t.Errorf("returned assessment %s belongs to user %s, not %s",
						assessment.ID, assessment.UserID, tt.requestingUser.ID)
				}
			}
		})
	}
}

// TestAssessmentHandler_UpdateAssessment_Authorization tests that users can only update their own assessments
func TestAssessmentHandler_UpdateAssessment_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "update-assess-user-1", "updateassess1@test.com")
	user2 := createTestUser(t, db, "update-assess-user-2", "updateassess2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessment for user1
	assessment1 := createTestAssessment(t, db, "update-assessment-1", user1.ID, models.AssessmentTypeGDPR)

	tests := []struct {
		name           string
		assessmentID   string
		requestingUser *models.User
		updateBody     map[string]interface{}
		expectedStatus int
		description    string
	}{
		{
			name:           "user can update own assessment",
			assessmentID:   assessment1.ID,
			requestingUser: user1,
			updateBody:     map[string]interface{}{"status": models.AssessmentStatusProcessing},
			expectedStatus: http.StatusOK,
			description:    "User1 updating their own assessment should succeed",
		},
		{
			name:           "user cannot update another user's assessment - returns 403 Forbidden",
			assessmentID:   assessment1.ID,
			requestingUser: user2,
			updateBody:     map[string]interface{}{"status": models.AssessmentStatusProcessing},
			expectedStatus: http.StatusForbidden,
			description:    "User2 updating User1's assessment should return 403 Forbidden",
		},
		{
			name:           "update non-existent assessment returns 404",
			assessmentID:   "non-existent-assessment",
			requestingUser: user1,
			updateBody:     map[string]interface{}{"status": models.AssessmentStatusProcessing},
			expectedStatus: http.StatusNotFound,
			description:    "Updating non-existent assessment should return 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.updateBody)
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/assessments/"+tt.assessmentID, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", tt.assessmentID)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.UpdateAssessment(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d (body: %s)",
					tt.description, tt.expectedStatus, rr.Code, rr.Body.String())
			}

			// Verify error code for forbidden access
			if tt.expectedStatus == http.StatusForbidden {
				var errResp models.ErrorResponse
				if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
					t.Errorf("failed to decode error response: %v", err)
				}
				if errResp.Code != "FORBIDDEN" {
					t.Errorf("expected error code FORBIDDEN, got %s", errResp.Code)
				}
			}
		})
	}
}

// TestAssessmentHandler_GetLatestAssessment_Authorization tests that users can only get their own latest assessment
func TestAssessmentHandler_GetLatestAssessment_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "latest-assess-user-1", "latestassess1@test.com")
	user2 := createTestUser(t, db, "latest-assess-user-2", "latestassess2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessments for user1
	createTestAssessment(t, db, "latest-assessment-1a", user1.ID, models.AssessmentTypeGDPR)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	latestAssessment1 := createTestAssessment(t, db, "latest-assessment-1b", user1.ID, models.AssessmentTypeAIAct)

	// Create assessment for user2
	latestAssessment2 := createTestAssessment(t, db, "latest-assessment-2", user2.ID, models.AssessmentTypeGDPR)

	tests := []struct {
		name           string
		requestingUser *models.User
		expectedID     string
		expectedStatus int
		description    string
	}{
		{
			name:           "user1 gets their latest assessment",
			requestingUser: user1,
			expectedID:     latestAssessment1.ID,
			expectedStatus: http.StatusOK,
			description:    "User1 should get their latest assessment",
		},
		{
			name:           "user2 gets their latest assessment",
			requestingUser: user2,
			expectedID:     latestAssessment2.ID,
			expectedStatus: http.StatusOK,
			description:    "User2 should get their latest assessment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/latest", nil)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.GetLatestAssessment(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, rr.Code)
				return
			}

			var assessment models.Assessment
			if err := json.NewDecoder(rr.Body).Decode(&assessment); err != nil {
				t.Errorf("failed to decode response: %v", err)
				return
			}

			// Verify the assessment belongs to the requesting user
			if assessment.UserID != tt.requestingUser.ID {
				t.Errorf("returned assessment belongs to user %s, not %s",
					assessment.UserID, tt.requestingUser.ID)
			}

			if assessment.ID != tt.expectedID {
				t.Errorf("expected assessment ID %s, got %s", tt.expectedID, assessment.ID)
			}
		})
	}
}

// TestAssessmentHandler_CreateAssessment_Authorization tests that assessments are created for the authenticated user
func TestAssessmentHandler_CreateAssessment_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	// Create test user
	user1 := createTestUser(t, db, "create-assess-user-1", "createassess1@test.com")
	defer cleanupTestUser(t, db, user1.ID)

	tests := []struct {
		name           string
		requestingUser *models.User
		requestBody    models.CreateAssessmentRequest
		expectedStatus int
		description    string
	}{
		{
			name:           "user can create assessment for themselves",
			requestingUser: user1,
			requestBody:    models.CreateAssessmentRequest{Type: models.AssessmentTypeGDPR},
			expectedStatus: http.StatusCreated,
			description:    "User creating assessment should get it assigned to themselves",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/assessments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.CreateAssessment(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, rr.Code)
				return
			}

			if tt.expectedStatus == http.StatusCreated {
				var assessment models.Assessment
				if err := json.NewDecoder(rr.Body).Decode(&assessment); err != nil {
					t.Errorf("failed to decode response: %v", err)
					return
				}

				// Verify the assessment was created for the requesting user
				if assessment.UserID != tt.requestingUser.ID {
					t.Errorf("assessment created for wrong user: expected %s, got %s",
						tt.requestingUser.ID, assessment.UserID)
				}
			}
		})
	}
}

// TestAssessmentHandler_Unauthorized tests that unauthenticated requests are rejected
func TestAssessmentHandler_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	tests := []struct {
		name           string
		method         string
		path           string
		handlerFunc    func(http.ResponseWriter, *http.Request)
		expectedStatus int
	}{
		{
			name:           "list assessments without auth",
			method:         http.MethodGet,
			path:           "/api/v1/assessments",
			handlerFunc:    handler.ListAssessments,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "get assessment without auth",
			method:         http.MethodGet,
			path:           "/api/v1/assessments/some-id",
			handlerFunc:    handler.GetAssessment,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "get latest assessment without auth",
			method:         http.MethodGet,
			path:           "/api/v1/assessments/latest",
			handlerFunc:    handler.GetLatestAssessment,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "create assessment without auth",
			method:         http.MethodPost,
			path:           "/api/v1/assessments",
			handlerFunc:    handler.CreateAssessment,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "update assessment without auth",
			method:         http.MethodPatch,
			path:           "/api/v1/assessments/some-id",
			handlerFunc:    handler.UpdateAssessment,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			// No user context set - simulating unauthenticated request

			rr := httptest.NewRecorder()
			tt.handlerFunc(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			// Verify error response
			var errResp models.ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
				t.Errorf("failed to decode error response: %v", err)
			}
			if errResp.Code != "UNAUTHORIZED" {
				t.Errorf("expected error code UNAUTHORIZED, got %s", errResp.Code)
			}
		})
	}
}

// TestAssessmentHandler_CrossUserAccess verifies that cross-user access is properly denied
func TestAssessmentHandler_CrossUserAccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewAssessmentHandler(db, logger)

	// Create three test users
	users := make([]*models.User, 3)
	assessments := make([]*models.Assessment, 3)

	for i := 0; i < 3; i++ {
		userID := "cross-user-assess-" + string(rune('A'+i))
		email := "crossuser" + string(rune('a'+i)) + "@test.com"
		assessmentID := "cross-assessment-" + string(rune('A'+i))

		users[i] = createTestUser(t, db, userID, email)
		assessments[i] = createTestAssessment(t, db, assessmentID, userID, models.AssessmentTypeGDPR)
		defer cleanupTestUser(t, db, userID)
	}

	// Each user tries to access all assessments
	for i, requestingUser := range users {
		for j, assessment := range assessments {
			testName := "user_" + requestingUser.ID + "_accessing_assessment_" + assessment.ID
			t.Run(testName, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/"+assessment.ID, nil)
				req.SetPathValue("id", assessment.ID)
				ctx := middleware.SetUser(req.Context(), requestingUser)
				req = req.WithContext(ctx)

				rr := httptest.NewRecorder()
				handler.GetAssessment(rr, req)

				if i == j {
					// User accessing their own assessment
					if rr.Code != http.StatusOK {
						t.Errorf("user should be able to access own assessment, got status %d", rr.Code)
					}
				} else {
					// User accessing another user's assessment
					if rr.Code != http.StatusNotFound {
						t.Errorf("user should not be able to access another user's assessment, got status %d", rr.Code)
					}
				}
			})
		}
	}
}
