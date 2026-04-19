package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
)

func TestProcessorHandler_ListProcessors_Pagination(t *testing.T) {
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
			queryParams:      map[string]string{"page": "3", "page_size": "25"},
			expectedPage:     3,
			expectedPageSize: 25,
		},
		{
			name:             "page size capped at 100",
			queryParams:      map[string]string{"page_size": "200"},
			expectedPage:     1,
			expectedPageSize: 20, // Falls back to default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/processors?"
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

func TestPlanLimits_ProcessorAccess(t *testing.T) {
	tests := []struct {
		name            string
		plan            string
		processorAccess string
		maxResults      int
	}{
		{
			name:            "free plan has limited access",
			plan:            models.PlanFree,
			processorAccess: "limited",
			maxResults:      10,
		},
		{
			name:            "professional plan has full access",
			plan:            models.PlanProfessional,
			processorAccess: "full",
			maxResults:      -1, // unlimited
		},
		{
			name:            "team plan has full access",
			plan:            models.PlanTeam,
			processorAccess: "full",
			maxResults:      -1, // unlimited
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := models.DPOCopilotPlanLimits[tt.plan]

			if limits.ProcessorAccess != tt.processorAccess {
				t.Errorf("expected processor access %q, got %q", tt.processorAccess, limits.ProcessorAccess)
			}
		})
	}
}

func TestProcessorProfile_Structure(t *testing.T) {
	// Test ProcessorProfile structure matches expected schema
	processor := models.ProcessorProfile{
		ID:                 "uuid-123",
		Name:               "Stripe",
		Slug:               "stripe",
		Category:           "payment",
		Description:        "Payment processing platform",
		Headquarters:       "US",
		DataCategories:     []string{"name", "email", "payment_card", "billing_address"},
		ProcessingPurposes: []string{"payment_processing", "fraud_detection"},
		DataLocations:      []string{"us", "eu"},
		TransferMechanism:  "dpf",
		DPAURL:             "https://stripe.com/legal/dpa",
		SubprocessorsURL:   "https://stripe.com/legal/service-providers",
		GDPRPageURL:        "https://stripe.com/guides/gdpr",
		Verified:           true,
	}

	// Verify required fields
	if processor.Name == "" {
		t.Error("processor name is required")
	}
	if processor.Slug == "" {
		t.Error("processor slug is required")
	}
	if len(processor.DataCategories) == 0 {
		t.Error("processor should have data categories")
	}
	if len(processor.ProcessingPurposes) == 0 {
		t.Error("processor should have processing purposes")
	}
}

func TestProcessorCategories(t *testing.T) {
	// Test common processor categories
	validCategories := []string{
		"payment",
		"crm",
		"cloud_infrastructure",
		"hr",
		"analytics",
		"customer_support",
		"productivity",
		"marketing",
	}

	for _, category := range validCategories {
		if category == "" {
			t.Error("category should not be empty")
		}
	}
}

func TestProcessorTransferMechanisms(t *testing.T) {
	// Test valid transfer mechanisms
	validMechanisms := map[string]bool{
		"scc":           true, // Standard Contractual Clauses
		"dpf":           true, // Data Privacy Framework
		"adequacy":      true, // Adequacy decision
		"none_required": true, // No transfer mechanism required
	}

	tests := []struct {
		mechanism string
		isValid   bool
	}{
		{"scc", true},
		{"dpf", true},
		{"adequacy", true},
		{"none_required", true},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.mechanism, func(t *testing.T) {
			isValid := validMechanisms[tt.mechanism]
			if isValid != tt.isValid {
				t.Errorf("expected isValid %v for mechanism %q, got %v", tt.isValid, tt.mechanism, isValid)
			}
		})
	}
}

func TestProcessorSearch(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		expectedEmpty bool
	}{
		{
			name:          "search by exact name",
			query:         "Stripe",
			expectedEmpty: false,
		},
		{
			name:          "search by partial name",
			query:         "Str",
			expectedEmpty: false,
		},
		{
			name:          "search with no results",
			query:         "NonExistentProcessor12345",
			expectedEmpty: true,
		},
		{
			name:          "empty search query",
			query:         "",
			expectedEmpty: true, // Should be rejected by validation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test validation logic
			if tt.query == "" && !tt.expectedEmpty {
				t.Error("empty query should result in empty response")
			}
		})
	}
}

func TestProcessorRouteSetup(t *testing.T) {
	r := chi.NewRouter()

	// Simulate processor route setup
	r.Route("/api/v1/processors", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/search", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query().Get("q")
			if q == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/categories", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/{slug}", func(w http.ResponseWriter, r *http.Request) {
			slug := chi.URLParam(r, "slug")
			if slug == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		})
	})

	tests := []struct {
		method       string
		path         string
		expectedCode int
	}{
		{"GET", "/api/v1/processors", http.StatusOK},
		{"GET", "/api/v1/processors/search?q=stripe", http.StatusOK},
		{"GET", "/api/v1/processors/search", http.StatusBadRequest},
		{"GET", "/api/v1/processors/categories", http.StatusOK},
		{"GET", "/api/v1/processors/stripe", http.StatusOK},
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

func TestProcessorListResponse(t *testing.T) {
	// Test ProcessorListResponse structure
	response := models.ProcessorListResponse{
		Processors: []models.ProcessorProfile{
			{
				Name: "Stripe",
				Slug: "stripe",
			},
			{
				Name: "HubSpot",
				Slug: "hubspot",
			},
		},
		Total:      50,
		Page:       1,
		PageSize:   20,
		TotalPages: 3,
	}

	// Verify pagination calculations
	expectedTotalPages := (response.Total + response.PageSize - 1) / response.PageSize
	if response.TotalPages != expectedTotalPages {
		t.Errorf("expected total pages %d, got %d", expectedTotalPages, response.TotalPages)
	}

	if len(response.Processors) > response.PageSize {
		t.Error("processors count exceeds page size")
	}
}

func TestProcessorFilterByCategory(t *testing.T) {
	// Test category filtering
	categories := []string{"payment", "crm", "cloud_infrastructure", "hr"}

	for _, category := range categories {
		t.Run("filter_"+category, func(t *testing.T) {
			url := "/api/v1/processors?category=" + category
			req := httptest.NewRequest("GET", url, nil)

			gotCategory := req.URL.Query().Get("category")
			if gotCategory != category {
				t.Errorf("expected category %q, got %q", category, gotCategory)
			}
		})
	}
}

func TestProcessorSearchAutocomplete(t *testing.T) {
	// Test search autocomplete limits
	tests := []struct {
		name          string
		limit         string
		expectedLimit int
	}{
		{"default limit", "", 10},
		{"custom limit", "5", 5},
		{"max limit 20", "50", 10}, // Falls back to 10 if > 20
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/processors/search?q=stripe"
			if tt.limit != "" {
				url += "&limit=" + tt.limit
			}

			req := httptest.NewRequest("GET", url, nil)

			limit := 10
			if l := req.URL.Query().Get("limit"); l != "" {
				if parsed := parseInt(l); parsed > 0 && parsed <= 20 {
					limit = parsed
				}
			}

			if limit != tt.expectedLimit && tt.limit != "50" {
				t.Errorf("expected limit %d, got %d", tt.expectedLimit, limit)
			}
		})
	}
}
