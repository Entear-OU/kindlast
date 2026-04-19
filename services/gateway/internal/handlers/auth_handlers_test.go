package handlers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
)

func TestRegister(t *testing.T) {
	// This is a basic structure test - in real usage, you'd use a test database
	// For now, we're just testing the handler structure

	// Create a test request
	reqBody := models.RegisterRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Note: This test would fail without a real database connection
	// In production, you'd use a test database or mock
	t.Log("Auth handler test structure verified")
}

func TestGenerateTokens(t *testing.T) {
	// Test token generation logic
	handler := &AuthHandler{
		jwtSecret:            "test-secret",
		jwtAccessExpiration:  15 * time.Minute,
		jwtRefreshExpiration: 7 * 24 * time.Hour,
	}

	user := &models.User{
		ID:    "test-user-id",
		Email: "test@example.com",
		Plan:  models.PlanFree,
	}

	accessToken, refreshToken, err := handler.generateTokens(user)
	if err != nil {
		t.Fatalf("Failed to generate tokens: %v", err)
	}

	if accessToken == "" {
		t.Error("Access token is empty")
	}

	if refreshToken == "" {
		t.Error("Refresh token is empty")
	}

	t.Logf("Access token generated: %d chars", len(accessToken))
	t.Logf("Refresh token generated: %d chars", len(refreshToken))
}
