package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
	_ "github.com/lib/pq"
)

// TestFindingHandler_ListFindings_Authorization tests that users can only list findings for their own assessments
func TestFindingHandler_ListFindings_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "finding-list-user-1", "findinglist1@test.com")
	user2 := createTestUser(t, db, "finding-list-user-2", "findinglist2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessment for user1
	assessment1 := createTestAssessment(t, db, "finding-list-assessment-1", user1.ID, models.AssessmentTypeGDPR)

	// Create findings for user1's assessment
	createTestFinding(t, db, "finding-list-1a", assessment1.ID, user1.ID)
	createTestFinding(t, db, "finding-list-1b", assessment1.ID, user1.ID)

	tests := []struct {
		name           string
		assessmentID   string
		requestingUser *models.User
		expectedStatus int
		expectedCount  int
		description    string
	}{
		{
			name:           "user can list findings for own assessment",
			assessmentID:   assessment1.ID,
			requestingUser: user1,
			expectedStatus: http.StatusOK,
			expectedCount:  2,
			description:    "User1 listing findings for their assessment should succeed",
		},
		{
			name:           "user cannot list findings for another user's assessment - returns 403 Forbidden",
			assessmentID:   assessment1.ID,
			requestingUser: user2,
			expectedStatus: http.StatusForbidden,
			expectedCount:  0,
			description:    "User2 listing findings for User1's assessment should return 403 Forbidden",
		},
		{
			name:           "non-existent assessment returns 404",
			assessmentID:   "non-existent-assessment",
			requestingUser: user1,
			expectedStatus: http.StatusNotFound,
			expectedCount:  0,
			description:    "Listing findings for non-existent assessment should return 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/"+tt.assessmentID+"/findings", nil)
			req.SetPathValue("id", tt.assessmentID)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ListFindings(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d (body: %s)",
					tt.description, tt.expectedStatus, rr.Code, rr.Body.String())
				return
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
				return
			}

			if tt.expectedStatus == http.StatusOK {
				var resp models.FindingListResponse
				if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
					t.Errorf("failed to decode response: %v", err)
					return
				}

				if resp.Total != tt.expectedCount {
					t.Errorf("%s: expected %d findings, got %d", tt.description, tt.expectedCount, resp.Total)
				}

				// Verify all returned findings belong to the requesting user
				for _, finding := range resp.Findings {
					if finding.UserID != tt.requestingUser.ID {
						t.Errorf("returned finding %s belongs to user %s, not %s",
							finding.ID, finding.UserID, tt.requestingUser.ID)
					}
				}
			}
		})
	}
}

// TestFindingHandler_ListUserFindings_Authorization tests that users can only list their own findings
func TestFindingHandler_ListUserFindings_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "user-findings-user-1", "userfindings1@test.com")
	user2 := createTestUser(t, db, "user-findings-user-2", "userfindings2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessments for both users
	assessment1 := createTestAssessment(t, db, "user-findings-assessment-1", user1.ID, models.AssessmentTypeGDPR)
	assessment2 := createTestAssessment(t, db, "user-findings-assessment-2", user2.ID, models.AssessmentTypeGDPR)

	// Create findings for user1
	createTestFinding(t, db, "user-finding-1a", assessment1.ID, user1.ID)
	createTestFinding(t, db, "user-finding-1b", assessment1.ID, user1.ID)
	createTestFinding(t, db, "user-finding-1c", assessment1.ID, user1.ID)

	// Create finding for user2
	createTestFinding(t, db, "user-finding-2a", assessment2.ID, user2.ID)

	tests := []struct {
		name           string
		requestingUser *models.User
		expectedCount  int
		description    string
	}{
		{
			name:           "user1 sees only their findings",
			requestingUser: user1,
			expectedCount:  3,
			description:    "User1 should see only their 3 findings",
		},
		{
			name:           "user2 sees only their findings",
			requestingUser: user2,
			expectedCount:  1,
			description:    "User2 should see only their 1 finding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/findings", nil)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ListUserFindings(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("%s: expected status 200, got %d", tt.description, rr.Code)
				return
			}

			var resp models.FindingListResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Errorf("failed to decode response: %v", err)
				return
			}

			if resp.Total != tt.expectedCount {
				t.Errorf("%s: expected %d findings, got %d", tt.description, tt.expectedCount, resp.Total)
			}

			// Verify all returned findings belong to the requesting user
			for _, finding := range resp.Findings {
				if finding.UserID != tt.requestingUser.ID {
					t.Errorf("returned finding %s belongs to user %s, not %s",
						finding.ID, finding.UserID, tt.requestingUser.ID)
				}
			}
		})
	}
}

// TestFindingHandler_UpdateFinding_Authorization tests that users can only update their own findings
func TestFindingHandler_UpdateFinding_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "update-finding-user-1", "updatefinding1@test.com")
	user2 := createTestUser(t, db, "update-finding-user-2", "updatefinding2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessment for user1
	assessment1 := createTestAssessment(t, db, "update-finding-assessment-1", user1.ID, models.AssessmentTypeGDPR)

	// Create finding for user1
	finding1 := createTestFinding(t, db, "update-finding-1", assessment1.ID, user1.ID)

	isResolved := true

	tests := []struct {
		name           string
		findingID      string
		requestingUser *models.User
		updateBody     models.UpdateFindingRequest
		expectedStatus int
		description    string
	}{
		{
			name:           "user can update own finding",
			findingID:      finding1.ID,
			requestingUser: user1,
			updateBody:     models.UpdateFindingRequest{IsResolved: &isResolved},
			expectedStatus: http.StatusOK,
			description:    "User1 updating their own finding should succeed",
		},
		{
			name:           "user cannot update another user's finding - returns 403 Forbidden",
			findingID:      finding1.ID,
			requestingUser: user2,
			updateBody:     models.UpdateFindingRequest{IsResolved: &isResolved},
			expectedStatus: http.StatusForbidden,
			description:    "User2 updating User1's finding should return 403 Forbidden",
		},
		{
			name:           "update non-existent finding returns 404",
			findingID:      "non-existent-finding",
			requestingUser: user1,
			updateBody:     models.UpdateFindingRequest{IsResolved: &isResolved},
			expectedStatus: http.StatusNotFound,
			description:    "Updating non-existent finding should return 404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.updateBody)
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/findings/"+tt.findingID, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", tt.findingID)
			ctx := middleware.SetUser(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.UpdateFinding(rr, req)

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

// TestFindingHandler_Unauthorized tests that unauthenticated requests are rejected
func TestFindingHandler_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	tests := []struct {
		name           string
		method         string
		path           string
		handlerFunc    func(http.ResponseWriter, *http.Request)
		expectedStatus int
	}{
		{
			name:           "list findings for assessment without auth",
			method:         http.MethodGet,
			path:           "/api/v1/assessments/some-id/findings",
			handlerFunc:    handler.ListFindings,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "list user findings without auth",
			method:         http.MethodGet,
			path:           "/api/v1/findings",
			handlerFunc:    handler.ListUserFindings,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "update finding without auth",
			method:         http.MethodPatch,
			path:           "/api/v1/findings/some-id",
			handlerFunc:    handler.UpdateFinding,
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

// TestFindingHandler_CrossUserAccess_Findings verifies complete user isolation for findings
func TestFindingHandler_CrossUserAccess_Findings(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	// Create three test users with their own assessments and findings
	users := make([]*models.User, 3)
	assessments := make([]*models.Assessment, 3)
	findings := make([]*models.Finding, 3)

	for i := 0; i < 3; i++ {
		userID := "cross-finding-user-" + string(rune('A'+i))
		email := "crossfinding" + string(rune('a'+i)) + "@test.com"
		assessmentID := "cross-finding-assessment-" + string(rune('A'+i))
		findingID := "cross-finding-" + string(rune('A'+i))

		users[i] = createTestUser(t, db, userID, email)
		assessments[i] = createTestAssessment(t, db, assessmentID, userID, models.AssessmentTypeGDPR)
		findings[i] = createTestFinding(t, db, findingID, assessmentID, userID)
		defer cleanupTestUser(t, db, userID)
	}

	// Test 1: Each user tries to list findings for all assessments
	t.Run("ListFindings_CrossUserAccess", func(t *testing.T) {
		for i, requestingUser := range users {
			for j, assessment := range assessments {
				testName := "user_" + requestingUser.ID + "_listing_findings_for_" + assessment.ID
				t.Run(testName, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/"+assessment.ID+"/findings", nil)
					req.SetPathValue("id", assessment.ID)
					ctx := middleware.SetUser(req.Context(), requestingUser)
					req = req.WithContext(ctx)

					rr := httptest.NewRecorder()
					handler.ListFindings(rr, req)

					if i == j {
						// User accessing their own assessment's findings
						if rr.Code != http.StatusOK {
							t.Errorf("user should be able to list findings for own assessment, got status %d", rr.Code)
						}
					} else {
						// User accessing another user's assessment's findings
						if rr.Code != http.StatusForbidden {
							t.Errorf("user should not be able to list findings for another user's assessment, got status %d", rr.Code)
						}
					}
				})
			}
		}
	})

	// Test 2: Each user tries to update all findings
	isResolved := true
	t.Run("UpdateFinding_CrossUserAccess", func(t *testing.T) {
		for i, requestingUser := range users {
			for j, finding := range findings {
				testName := "user_" + requestingUser.ID + "_updating_finding_" + finding.ID
				t.Run(testName, func(t *testing.T) {
					body, _ := json.Marshal(models.UpdateFindingRequest{IsResolved: &isResolved})
					req := httptest.NewRequest(http.MethodPatch, "/api/v1/findings/"+finding.ID, bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.SetPathValue("id", finding.ID)
					ctx := middleware.SetUser(req.Context(), requestingUser)
					req = req.WithContext(ctx)

					rr := httptest.NewRecorder()
					handler.UpdateFinding(rr, req)

					if i == j {
						// User updating their own finding
						if rr.Code != http.StatusOK {
							t.Errorf("user should be able to update own finding, got status %d", rr.Code)
						}
					} else {
						// User updating another user's finding
						if rr.Code != http.StatusForbidden {
							t.Errorf("user should not be able to update another user's finding, got status %d (body: %s)",
								rr.Code, rr.Body.String())
						}
					}
				})
			}
		}
	})
}

// TestFindingHandler_UserIsolation_AllFindings tests that ListUserFindings only returns the user's findings
func TestFindingHandler_UserIsolation_AllFindings(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	// Create multiple users with varying number of findings
	type testUserData struct {
		user          *models.User
		assessment    *models.Assessment
		findings      []*models.Finding
		expectedCount int
	}

	userData := []testUserData{
		{expectedCount: 5}, // User A will have 5 findings
		{expectedCount: 2}, // User B will have 2 findings
		{expectedCount: 0}, // User C will have 0 findings
	}

	for i := range userData {
		userID := "isolation-finding-user-" + string(rune('A'+i))
		email := "isolationfinding" + string(rune('a'+i)) + "@test.com"
		assessmentID := "isolation-finding-assessment-" + string(rune('A'+i))

		userData[i].user = createTestUser(t, db, userID, email)
		userData[i].assessment = createTestAssessment(t, db, assessmentID, userID, models.AssessmentTypeGDPR)
		defer cleanupTestUser(t, db, userID)

		// Create the expected number of findings for each user
		for j := 0; j < userData[i].expectedCount; j++ {
			findingID := "isolation-finding-" + string(rune('A'+i)) + "-" + string(rune('0'+j))
			finding := createTestFinding(t, db, findingID, assessmentID, userID)
			userData[i].findings = append(userData[i].findings, finding)
		}
	}

	// Each user should only see their own findings
	for _, data := range userData {
		t.Run("user_"+data.user.ID+"_sees_only_own_findings", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/findings", nil)
			ctx := middleware.SetUser(req.Context(), data.user)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.ListUserFindings(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
				return
			}

			var resp models.FindingListResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Errorf("failed to decode response: %v", err)
				return
			}

			if resp.Total != data.expectedCount {
				t.Errorf("user %s expected %d findings, got %d",
					data.user.ID, data.expectedCount, resp.Total)
			}

			// Verify all returned findings belong to the requesting user
			for _, finding := range resp.Findings {
				if finding.UserID != data.user.ID {
					t.Errorf("user %s received finding belonging to user %s",
						data.user.ID, finding.UserID)
				}
			}
		})
	}
}

// TestFindingHandler_ForbiddenError_Details tests that 403 responses have proper error details
func TestFindingHandler_ForbiddenError_Details(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewFindingHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "forbidden-detail-user-1", "forbiddendetail1@test.com")
	user2 := createTestUser(t, db, "forbidden-detail-user-2", "forbiddendetail2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create assessment and finding for user1
	assessment1 := createTestAssessment(t, db, "forbidden-detail-assessment-1", user1.ID, models.AssessmentTypeGDPR)
	finding1 := createTestFinding(t, db, "forbidden-detail-finding-1", assessment1.ID, user1.ID)

	tests := []struct {
		name         string
		handlerFunc  func(http.ResponseWriter, *http.Request)
		setupRequest func() *http.Request
		expectedCode string
		expectedMsg  string
	}{
		{
			name:        "ListFindings returns proper 403 error",
			handlerFunc: handler.ListFindings,
			setupRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/assessments/"+assessment1.ID+"/findings", nil)
				req.SetPathValue("id", assessment1.ID)
				ctx := middleware.SetUser(req.Context(), user2)
				return req.WithContext(ctx)
			},
			expectedCode: "FORBIDDEN",
			expectedMsg:  "Access denied",
		},
		{
			name:        "UpdateFinding returns proper 403 error",
			handlerFunc: handler.UpdateFinding,
			setupRequest: func() *http.Request {
				isResolved := true
				body, _ := json.Marshal(models.UpdateFindingRequest{IsResolved: &isResolved})
				req := httptest.NewRequest(http.MethodPatch, "/api/v1/findings/"+finding1.ID, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.SetPathValue("id", finding1.ID)
				ctx := middleware.SetUser(req.Context(), user2)
				return req.WithContext(ctx)
			},
			expectedCode: "FORBIDDEN",
			expectedMsg:  "Access denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.setupRequest()
			rr := httptest.NewRecorder()
			tt.handlerFunc(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("expected status 403, got %d", rr.Code)
				return
			}

			var errResp models.ErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
				t.Errorf("failed to decode error response: %v", err)
				return
			}

			if errResp.Code != tt.expectedCode {
				t.Errorf("expected error code %s, got %s", tt.expectedCode, errResp.Code)
			}

			if errResp.Message != tt.expectedMsg {
				t.Errorf("expected message %q, got %q", tt.expectedMsg, errResp.Message)
			}
		})
	}
}
