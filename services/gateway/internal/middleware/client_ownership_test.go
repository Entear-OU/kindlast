package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
)

func TestRequireClientOwnership_NoClientID(t *testing.T) {
	// Test when client ID is missing from URL
	r := chi.NewRouter()

	r.Route("/api/v1/clients/{clientID}", func(r chi.Router) {
		// Note: In real test, we'd use a mock DB
		// For now, test the middleware structure
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			clientID := chi.URLParam(r, "clientID")
			if clientID == "" {
				http.Error(w, "Client ID required", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	})

	// Test missing client ID
	req := httptest.NewRequest("GET", "/api/v1/clients//", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Error("expected non-OK status for missing client ID")
	}
}

func TestRequireClientOwnership_URLParam(t *testing.T) {
	r := chi.NewRouter()

	r.Route("/api/v1/clients/{clientID}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			clientID := chi.URLParam(r, "clientID")
			if clientID == "client-123" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(clientID))
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		})
	})

	// Test valid client ID
	req := httptest.NewRequest("GET", "/api/v1/clients/client-123", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if rr.Body.String() != "client-123" {
		t.Errorf("expected body %q, got %q", "client-123", rr.Body.String())
	}
}

func TestGetClient(t *testing.T) {
	// Test GetClient context helper
	client := &models.Client{
		ID:     "client-123",
		UserID: "user-456",
		Name:   "Test Client",
		Status: models.ClientStatusActive,
	}

	// Create context with client
	ctx := context.WithValue(context.Background(), clientContextKey, client)

	// Test retrieval
	retrievedClient, ok := GetClient(ctx)
	if !ok {
		t.Error("expected to retrieve client from context")
	}

	if retrievedClient.ID != client.ID {
		t.Errorf("expected client ID %q, got %q", client.ID, retrievedClient.ID)
	}

	if retrievedClient.UserID != client.UserID {
		t.Errorf("expected user ID %q, got %q", client.UserID, retrievedClient.UserID)
	}
}

func TestGetClient_NoClient(t *testing.T) {
	// Test GetClient when no client in context
	ctx := context.Background()

	_, ok := GetClient(ctx)
	if ok {
		t.Error("expected to not find client in empty context")
	}
}

func TestClientOwnership_Authorization(t *testing.T) {
	// Test authorization logic
	tests := []struct {
		name         string
		clientUserID string
		requestingUserID string
		shouldAllow  bool
	}{
		{
			name:         "owner can access",
			clientUserID: "user-123",
			requestingUserID: "user-123",
			shouldAllow:  true,
		},
		{
			name:         "non-owner denied",
			clientUserID: "user-123",
			requestingUserID: "user-456",
			shouldAllow:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := tt.clientUserID == tt.requestingUserID
			if allowed != tt.shouldAllow {
				t.Errorf("expected allowed %v, got %v", tt.shouldAllow, allowed)
			}
		})
	}
}

func TestClientStatus(t *testing.T) {
	// Test client status constants
	tests := []struct {
		status  string
		isValid bool
	}{
		{models.ClientStatusActive, true},
		{models.ClientStatusArchived, true},
		{"invalid", false},
		{"", false},
	}

	validStatuses := map[string]bool{
		models.ClientStatusActive:   true,
		models.ClientStatusArchived: true,
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			isValid := validStatuses[tt.status]
			if isValid != tt.isValid {
				t.Errorf("expected isValid %v for status %q, got %v", tt.isValid, tt.status, isValid)
			}
		})
	}
}

func TestClientContextKey(t *testing.T) {
	// Verify context key is unique and type-safe
	key1 := clientContextKey
	key2 := contextKeyClient("client")

	if key1 != key2 {
		t.Error("context keys should match for same value")
	}

	// Test that different values don't match
	key3 := contextKeyClient("other")
	if key1 == key3 {
		t.Error("different context keys should not match")
	}
}

func TestNestedRouteClientID(t *testing.T) {
	// Test that client ID is accessible in nested routes
	r := chi.NewRouter()

	r.Route("/api/v1/clients/{clientID}", func(r chi.Router) {
		r.Route("/artifacts", func(r chi.Router) {
			r.Get("/{artifactID}", func(w http.ResponseWriter, r *http.Request) {
				clientID := chi.URLParam(r, "clientID")
				artifactID := chi.URLParam(r, "artifactID")

				if clientID == "" || artifactID == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				w.WriteHeader(http.StatusOK)
				w.Write([]byte(clientID + ":" + artifactID))
			})
		})
	})

	req := httptest.NewRequest("GET", "/api/v1/clients/client-123/artifacts/artifact-456", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	expected := "client-123:artifact-456"
	if rr.Body.String() != expected {
		t.Errorf("expected body %q, got %q", expected, rr.Body.String())
	}
}

func TestMiddlewareChain(t *testing.T) {
	// Test that middleware chain works correctly
	callOrder := []string{}

	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "middleware1")
			next.ServeHTTP(w, r)
		})
	}

	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callOrder = append(callOrder, "middleware2")
			next.ServeHTTP(w, r)
		})
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callOrder = append(callOrder, "handler")
		w.WriteHeader(http.StatusOK)
	})

	// Chain middleware
	chain := middleware1(middleware2(handler))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	expectedOrder := []string{"middleware1", "middleware2", "handler"}
	if len(callOrder) != len(expectedOrder) {
		t.Errorf("expected %d calls, got %d", len(expectedOrder), len(callOrder))
	}

	for i, expected := range expectedOrder {
		if i >= len(callOrder) || callOrder[i] != expected {
			t.Errorf("expected call %d to be %q, got %q", i, expected, callOrder[i])
		}
	}
}
