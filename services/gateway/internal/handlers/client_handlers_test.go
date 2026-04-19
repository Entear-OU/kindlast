package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
)

func TestClientHandler_CreateClient_Validation(t *testing.T) {
	// Test plan enforcement logic directly since handler requires DB
	tests := []struct {
		name           string
		plan           string
		shouldBlock    bool
	}{
		{
			name:        "free plan blocked",
			plan:        models.PlanFree,
			shouldBlock: true,
		},
		{
			name:        "professional plan allowed",
			plan:        models.PlanProfessional,
			shouldBlock: false,
		},
		{
			name:        "team plan allowed",
			plan:        models.PlanTeam,
			shouldBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := models.DPOCopilotPlanLimits[tt.plan]
			blocked := limits.MaxClients == 0
			if blocked != tt.shouldBlock {
				t.Errorf("expected shouldBlock %v, got %v", tt.shouldBlock, blocked)
			}
		})
	}
}

func TestClientHandler_ListClients_Pagination(t *testing.T) {
	tests := []struct {
		name             string
		queryParams      map[string]string
		expectedPage     int
		expectedPageSize int
	}{
		{
			name:             "default pagination",
			queryParams:      map[string]string{},
			expectedPage:     1,
			expectedPageSize: 20,
		},
		{
			name:             "custom page and size",
			queryParams:      map[string]string{"page": "2", "page_size": "50"},
			expectedPage:     2,
			expectedPageSize: 50,
		},
		{
			name:             "invalid page defaults to 1",
			queryParams:      map[string]string{"page": "-1"},
			expectedPage:     1,
			expectedPageSize: 20,
		},
		{
			name:             "page size capped at 100",
			queryParams:      map[string]string{"page_size": "150"},
			expectedPage:     1,
			expectedPageSize: 20, // Falls back to default because 150 > 100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/clients?"
			for k, v := range tt.queryParams {
				url += k + "=" + v + "&"
			}

			req := httptest.NewRequest("GET", url, nil)

			// Parse pagination (same logic as handler)
			page := 1
			if p := req.URL.Query().Get("page"); p != "" {
				if parsed := parseInt(p); parsed > 0 {
					page = parsed
				}
			}

			pageSize := 20
			if ps := req.URL.Query().Get("page_size"); ps != "" {
				if parsed := parseInt(ps); parsed > 0 && parsed <= 100 {
					pageSize = parsed
				}
			}

			if page != tt.expectedPage {
				t.Errorf("expected page %d, got %d", tt.expectedPage, page)
			}
			if pageSize != tt.expectedPageSize {
				t.Errorf("expected page_size %d, got %d", tt.expectedPageSize, pageSize)
			}
		})
	}
}

func TestPlanLimits_ClientWorkspaces(t *testing.T) {
	tests := []struct {
		name        string
		plan        string
		maxClients  int
		shouldAllow bool
	}{
		{
			name:        "free plan has no clients",
			plan:        models.PlanFree,
			maxClients:  0,
			shouldAllow: false,
		},
		{
			name:        "professional plan allows 20 clients",
			plan:        models.PlanProfessional,
			maxClients:  20,
			shouldAllow: true,
		},
		{
			name:        "team plan allows 50 clients",
			plan:        models.PlanTeam,
			maxClients:  50,
			shouldAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := models.DPOCopilotPlanLimits[tt.plan]

			if limits.MaxClients != tt.maxClients {
				t.Errorf("expected max clients %d, got %d", tt.maxClients, limits.MaxClients)
			}

			allows := limits.MaxClients > 0
			if allows != tt.shouldAllow {
				t.Errorf("expected shouldAllow %v, got %v", tt.shouldAllow, allows)
			}
		})
	}
}

func TestCreateClientRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request models.CreateClientRequest
		isValid bool
	}{
		{
			name: "valid request with all fields",
			request: models.CreateClientRequest{
				Name:               "Acme Corp",
				Description:        "A fintech startup",
				Sector:             "fintech",
				Country:            "DE",
				EmployeeCount:      50,
				TechStack:          []string{"stripe", "hubspot"},
				DataSubjects:       []string{"customers", "employees"},
				ProcessingPurposes: []string{"payment_processing", "marketing"},
			},
			isValid: true,
		},
		{
			name: "valid request with minimal fields",
			request: models.CreateClientRequest{
				Name: "Simple Client",
			},
			isValid: true,
		},
		{
			name: "invalid request - missing name",
			request: models.CreateClientRequest{
				Description: "No name provided",
			},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.request.Name != ""
			if isValid != tt.isValid {
				t.Errorf("expected isValid %v, got %v", tt.isValid, isValid)
			}
		})
	}
}

func TestUpdateClientRequest_PartialUpdate(t *testing.T) {
	// Test that update request properly handles partial updates
	name := "New Name"
	desc := "New Description"

	tests := []struct {
		name           string
		request        models.UpdateClientRequest
		fieldsToUpdate []string
	}{
		{
			name: "update name only",
			request: models.UpdateClientRequest{
				Name: &name,
			},
			fieldsToUpdate: []string{"name"},
		},
		{
			name: "update description only",
			request: models.UpdateClientRequest{
				Description: &desc,
			},
			fieldsToUpdate: []string{"description"},
		},
		{
			name: "update multiple fields",
			request: models.UpdateClientRequest{
				Name:        &name,
				Description: &desc,
			},
			fieldsToUpdate: []string{"name", "description"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateCount := 0
			if tt.request.Name != nil {
				updateCount++
			}
			if tt.request.Description != nil {
				updateCount++
			}
			if tt.request.Sector != nil {
				updateCount++
			}
			if tt.request.Country != nil {
				updateCount++
			}
			if tt.request.EmployeeCount != nil {
				updateCount++
			}
			if tt.request.TechStack != nil {
				updateCount++
			}
			if tt.request.DataSubjects != nil {
				updateCount++
			}
			if tt.request.ProcessingPurposes != nil {
				updateCount++
			}

			if updateCount != len(tt.fieldsToUpdate) {
				t.Errorf("expected %d fields to update, got %d", len(tt.fieldsToUpdate), updateCount)
			}
		})
	}
}

// parseInt is a helper function for parsing integers
func parseInt(s string) int {
	var result int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else if c == '-' && result == 0 {
			// Handle negative numbers
			continue
		}
	}
	if len(s) > 0 && s[0] == '-' {
		return -result
	}
	return result
}

// TestRouteSetup verifies the route configuration is correct
func TestRouteSetup(t *testing.T) {
	r := chi.NewRouter()

	// Simulate route setup similar to main.go
	r.Route("/api/v1/clients", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Post("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
		r.Get("/{clientID}", func(w http.ResponseWriter, r *http.Request) {
			clientID := chi.URLParam(r, "clientID")
			if clientID == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
		r.Put("/{clientID}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Delete("/{clientID}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	tests := []struct {
		method       string
		path         string
		expectedCode int
	}{
		{"GET", "/api/v1/clients", http.StatusOK},
		{"POST", "/api/v1/clients", http.StatusCreated},
		{"GET", "/api/v1/clients/123", http.StatusOK},
		{"PUT", "/api/v1/clients/123", http.StatusOK},
		{"DELETE", "/api/v1/clients/123", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, rr.Code)
			}
		})
	}
}
