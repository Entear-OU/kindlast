package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/entear/kindlast/services/gateway/internal/models"
	_ "github.com/lib/pq"
)

// TestProfileHandler_GetProfile_Authorization tests that users can only access their own profiles
func TestProfileHandler_GetProfile_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewProfileHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "profile-test-user-1", "user1@test.com")
	user2 := createTestUser(t, db, "profile-test-user-2", "user2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	// Create profile for user1
	profile1 := createTestProfile(t, db, user1.ID, "profile-1")

	tests := []struct {
		name           string
		requestingUser *models.User
		ownerUser      *models.User
		expectedStatus int
		expectProfile  bool
		description    string
	}{
		{
			name:           "user can access own profile",
			requestingUser: user1,
			ownerUser:      user1,
			expectedStatus: http.StatusOK,
			expectProfile:  true,
			description:    "User1 accessing their own profile should succeed",
		},
		{
			name:           "user cannot access another user's profile - returns 404",
			requestingUser: user2,
			ownerUser:      user1,
			expectedStatus: http.StatusNotFound,
			expectProfile:  false,
			description:    "User2 accessing User1's profile should return 404 (profile lookup by user_id)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)
			ctx := withUserContext(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.GetProfile(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, rr.Code)
			}

			if tt.expectProfile {
				var respProfile models.BusinessProfile
				if err := json.NewDecoder(rr.Body).Decode(&respProfile); err != nil {
					t.Errorf("failed to decode response: %v", err)
				}
				if respProfile.ID != profile1.ID {
					t.Errorf("expected profile ID %s, got %s", profile1.ID, respProfile.ID)
				}
			}
		})
	}
}

// TestProfileHandler_CreateOrUpdateProfile_Authorization tests profile creation/update authorization
func TestProfileHandler_CreateOrUpdateProfile_Authorization(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewProfileHandler(db, logger)

	// Create two test users
	user1 := createTestUser(t, db, "profile-create-user-1", "createuser1@test.com")
	user2 := createTestUser(t, db, "profile-create-user-2", "createuser2@test.com")
	defer cleanupTestUser(t, db, user1.ID)
	defer cleanupTestUser(t, db, user2.ID)

	tests := []struct {
		name           string
		requestingUser *models.User
		requestBody    models.CreateBusinessProfileRequest
		expectedStatus int
		description    string
	}{
		{
			name:           "user can create own profile",
			requestingUser: user1,
			requestBody: models.CreateBusinessProfileRequest{
				CompanyName:           "My Company",
				Country:               "DE",
				Industry:              "Technology",
				ProcessesPersonalData: true,
			},
			expectedStatus: http.StatusOK,
			description:    "User creating their own profile should succeed",
		},
		{
			name:           "another user creating profile creates their own",
			requestingUser: user2,
			requestBody: models.CreateBusinessProfileRequest{
				CompanyName:           "Another Company",
				Country:               "FR",
				Industry:              "Finance",
				ProcessesPersonalData: true,
			},
			expectedStatus: http.StatusOK,
			description:    "Profile is always created/updated for the authenticated user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/profile", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := withUserContext(req.Context(), tt.requestingUser)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.CreateOrUpdateProfile(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: expected status %d, got %d", tt.description, tt.expectedStatus, rr.Code)
			}

			// Verify the profile was created for the requesting user, not someone else
			if rr.Code == http.StatusOK {
				var respProfile models.BusinessProfile
				if err := json.NewDecoder(rr.Body).Decode(&respProfile); err != nil {
					t.Errorf("failed to decode response: %v", err)
				}
				if respProfile.UserID != tt.requestingUser.ID {
					t.Errorf("profile created for wrong user: expected %s, got %s",
						tt.requestingUser.ID, respProfile.UserID)
				}
			}
		})
	}
}

// TestProfileHandler_Unauthorized tests that unauthenticated requests are rejected
func TestProfileHandler_Unauthorized(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewProfileHandler(db, logger)

	tests := []struct {
		name           string
		method         string
		path           string
		handlerFunc    func(http.ResponseWriter, *http.Request)
		expectedStatus int
	}{
		{
			name:           "get profile without auth",
			method:         http.MethodGet,
			path:           "/api/v1/profile",
			handlerFunc:    handler.GetProfile,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "create profile without auth",
			method:         http.MethodPost,
			path:           "/api/v1/profile",
			handlerFunc:    handler.CreateOrUpdateProfile,
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

// TestProfileHandler_UserIsolation verifies complete user data isolation
func TestProfileHandler_UserIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	handler := NewProfileHandler(db, logger)

	// Create multiple test users with profiles
	users := make([]*models.User, 3)
	profiles := make([]*models.BusinessProfile, 3)

	for i := 0; i < 3; i++ {
		userID := "isolation-test-user-" + string(rune('A'+i))
		email := "isolation" + string(rune('a'+i)) + "@test.com"
		profileID := "isolation-profile-" + string(rune('A'+i))

		users[i] = createTestUser(t, db, userID, email)
		profiles[i] = createTestProfile(t, db, userID, profileID)
		defer cleanupTestUser(t, db, userID)
	}

	// Each user should only see their own profile
	for i, user := range users {
		t.Run("user_"+user.ID+"_sees_only_own_profile", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil)
			ctx := withUserContext(req.Context(), user)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			handler.GetProfile(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", rr.Code)
				return
			}

			var respProfile models.BusinessProfile
			if err := json.NewDecoder(rr.Body).Decode(&respProfile); err != nil {
				t.Errorf("failed to decode response: %v", err)
				return
			}

			// Verify the profile belongs to the requesting user
			if respProfile.UserID != user.ID {
				t.Errorf("user %s received profile for user %s", user.ID, respProfile.UserID)
			}
			if respProfile.ID != profiles[i].ID {
				t.Errorf("expected profile %s, got %s", profiles[i].ID, respProfile.ID)
			}
		})
	}
}
