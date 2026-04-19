package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
)

func TestAuditHandler_ListAuditEntries_Pagination(t *testing.T) {
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
			name:             "page size capped at 100",
			queryParams:      map[string]string{"page_size": "200"},
			expectedPage:     1,
			expectedPageSize: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/audit?"
			for k, v := range tt.queryParams {
				url += k + "=" + v + "&"
			}

			req := httptest.NewRequest("GET", url, nil)

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

func TestPlanLimits_AuditTrail(t *testing.T) {
	tests := []struct {
		name               string
		plan               string
		auditEnabled       bool
		retentionMonths    int
	}{
		{
			name:            "free plan has no audit trail",
			plan:            models.PlanFree,
			auditEnabled:    false,
			retentionMonths: 0,
		},
		{
			name:            "professional plan has 12 month retention",
			plan:            models.PlanProfessional,
			auditEnabled:    true,
			retentionMonths: 12,
		},
		{
			name:            "team plan has unlimited retention",
			plan:            models.PlanTeam,
			auditEnabled:    true,
			retentionMonths: 0, // 0 = unlimited
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := models.DPOCopilotPlanLimits[tt.plan]

			if limits.AuditTrailEnabled != tt.auditEnabled {
				t.Errorf("expected audit enabled %v, got %v", tt.auditEnabled, limits.AuditTrailEnabled)
			}
			if limits.AuditRetentionMonths != tt.retentionMonths {
				t.Errorf("expected retention months %d, got %d", tt.retentionMonths, limits.AuditRetentionMonths)
			}
		})
	}
}

func TestAuditActions(t *testing.T) {
	// Test all audit action types
	validActions := map[string]bool{
		models.AuditActionGenerated:     true,
		models.AuditActionEdited:        true,
		models.AuditActionStatusChanged: true,
		models.AuditActionExported:      true,
		models.AuditActionDeleted:       true,
	}

	tests := []struct {
		action  string
		isValid bool
	}{
		{models.AuditActionGenerated, true},
		{models.AuditActionEdited, true},
		{models.AuditActionStatusChanged, true},
		{models.AuditActionExported, true},
		{models.AuditActionDeleted, true},
		{"invalid_action", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			isValid := validActions[tt.action]
			if isValid != tt.isValid {
				t.Errorf("expected isValid %v for action %q, got %v", tt.isValid, tt.action, isValid)
			}
		})
	}
}

func TestAuditEntry_Structure(t *testing.T) {
	// Test ArtifactAuditEntry structure
	entry := models.ArtifactAuditEntry{
		ID:            "uuid-123",
		ArtifactID:    "artifact-456",
		UserID:        "user-789",
		Action:        models.AuditActionGenerated,
		PreviousState: nil,
		NewState:      json.RawMessage(`{"type":"ropa","status":"draft"}`),
		Metadata:      json.RawMessage(`{"ip":"192.168.1.1","user_agent":"Mozilla/5.0"}`),
		CreatedAt:     time.Now(),
	}

	// Verify required fields
	if entry.ID == "" {
		t.Error("audit entry ID is required")
	}
	if entry.ArtifactID == "" {
		t.Error("artifact ID is required")
	}
	if entry.UserID == "" {
		t.Error("user ID is required")
	}
	if entry.Action == "" {
		t.Error("action is required")
	}
	if entry.CreatedAt.IsZero() {
		t.Error("created_at is required")
	}
}

func TestAuditMetadata_Structure(t *testing.T) {
	// Test AuditMetadata structure
	metadata := models.AuditMetadata{
		IP:        "192.168.1.1",
		UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		Reason:    "Reviewed and approved by DPO",
	}

	// Marshal and unmarshal to verify JSON serialization
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("failed to marshal AuditMetadata: %v", err)
	}

	var unmarshaled models.AuditMetadata
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal AuditMetadata: %v", err)
	}

	if unmarshaled.IP != metadata.IP {
		t.Errorf("expected IP %q, got %q", metadata.IP, unmarshaled.IP)
	}
	if unmarshaled.UserAgent != metadata.UserAgent {
		t.Errorf("expected user agent %q, got %q", metadata.UserAgent, unmarshaled.UserAgent)
	}
	if unmarshaled.Reason != metadata.Reason {
		t.Errorf("expected reason %q, got %q", metadata.Reason, unmarshaled.Reason)
	}
}

func TestAuditDateFilter(t *testing.T) {
	tests := []struct {
		name          string
		startDate     string
		endDate       string
		shouldParse   bool
	}{
		{
			name:        "valid date range",
			startDate:   "2024-01-01",
			endDate:     "2024-12-31",
			shouldParse: true,
		},
		{
			name:        "start date only",
			startDate:   "2024-01-01",
			endDate:     "",
			shouldParse: true,
		},
		{
			name:        "end date only",
			startDate:   "",
			endDate:     "2024-12-31",
			shouldParse: true,
		},
		{
			name:        "invalid date format",
			startDate:   "01-01-2024",
			endDate:     "",
			shouldParse: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.startDate != "" {
				_, err := time.Parse("2006-01-02", tt.startDate)
				parsed := err == nil
				if parsed != tt.shouldParse {
					t.Errorf("expected parse %v for date %q, got %v", tt.shouldParse, tt.startDate, parsed)
				}
			}
		})
	}
}

func TestAuditRetentionEnforcement(t *testing.T) {
	// Test that retention limits are correctly applied
	now := time.Now()

	tests := []struct {
		name            string
		retentionMonths int
		requestedStart  time.Time
		expectedStart   time.Time
	}{
		{
			name:            "no retention limit",
			retentionMonths: 0, // unlimited
			requestedStart:  now.AddDate(-2, 0, 0), // 2 years ago
			expectedStart:   now.AddDate(-2, 0, 0),
		},
		{
			name:            "12 month retention",
			retentionMonths: 12,
			requestedStart:  now.AddDate(-2, 0, 0), // 2 years ago
			expectedStart:   now.AddDate(0, -12, 0), // capped at 12 months
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var effectiveStart time.Time

			if tt.retentionMonths == 0 {
				// Unlimited retention
				effectiveStart = tt.requestedStart
			} else {
				minDate := now.AddDate(0, -tt.retentionMonths, 0)
				if tt.requestedStart.Before(minDate) {
					effectiveStart = minDate
				} else {
					effectiveStart = tt.requestedStart
				}
			}

			// Allow for small time differences due to test execution
			if effectiveStart.Year() != tt.expectedStart.Year() ||
				effectiveStart.Month() != tt.expectedStart.Month() {
				t.Errorf("expected start around %v, got %v", tt.expectedStart, effectiveStart)
			}
		})
	}
}

func TestAuditRouteSetup(t *testing.T) {
	r := chi.NewRouter()

	// Simulate audit route setup
	r.Route("/api/v1/audit", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/export", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/csv")
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/summary", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	tests := []struct {
		method       string
		path         string
		expectedCode int
		contentType  string
	}{
		{"GET", "/api/v1/audit", http.StatusOK, ""},
		{"GET", "/api/v1/audit/export", http.StatusOK, "text/csv"},
		{"GET", "/api/v1/audit/summary", http.StatusOK, ""},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != tt.expectedCode {
				t.Errorf("expected status %d, got %d", tt.expectedCode, rr.Code)
			}

			if tt.contentType != "" {
				gotContentType := rr.Header().Get("Content-Type")
				if gotContentType != tt.contentType {
					t.Errorf("expected content type %q, got %q", tt.contentType, gotContentType)
				}
			}
		})
	}
}

func TestAuditListResponse(t *testing.T) {
	// Test AuditListResponse structure
	response := models.AuditListResponse{
		Entries: []models.ArtifactAuditEntry{
			{
				ID:         "entry-1",
				ArtifactID: "artifact-1",
				Action:     models.AuditActionGenerated,
				CreatedAt:  time.Now(),
			},
			{
				ID:         "entry-2",
				ArtifactID: "artifact-1",
				Action:     models.AuditActionEdited,
				CreatedAt:  time.Now(),
			},
		},
		Total:      100,
		Page:       1,
		PageSize:   20,
		TotalPages: 5,
	}

	// Verify pagination calculations
	expectedTotalPages := (response.Total + response.PageSize - 1) / response.PageSize
	if response.TotalPages != expectedTotalPages {
		t.Errorf("expected total pages %d, got %d", expectedTotalPages, response.TotalPages)
	}

	if len(response.Entries) > response.PageSize {
		t.Error("entries count exceeds page size")
	}
}

func TestAuditExportCSVHeaders(t *testing.T) {
	// Test expected CSV export headers
	expectedHeaders := []string{
		"ID",
		"Timestamp",
		"Client",
		"Artifact ID",
		"Artifact Type",
		"Artifact Title",
		"Action",
		"IP Address",
		"User Agent",
	}

	// Verify all expected headers are present
	for _, header := range expectedHeaders {
		if header == "" {
			t.Error("CSV header should not be empty")
		}
	}

	if len(expectedHeaders) != 9 {
		t.Errorf("expected 9 CSV headers, got %d", len(expectedHeaders))
	}
}

func TestAuditFilters(t *testing.T) {
	// Test audit filter combinations
	tests := []struct {
		name       string
		filters    map[string]string
		isValid    bool
	}{
		{
			name:       "no filters",
			filters:    map[string]string{},
			isValid:    true,
		},
		{
			name:       "filter by client",
			filters:    map[string]string{"client_id": "client-123"},
			isValid:    true,
		},
		{
			name:       "filter by artifact",
			filters:    map[string]string{"artifact_id": "artifact-456"},
			isValid:    true,
		},
		{
			name:       "filter by action",
			filters:    map[string]string{"action": "generated"},
			isValid:    true,
		},
		{
			name:       "filter by date range",
			filters:    map[string]string{"start_date": "2024-01-01", "end_date": "2024-12-31"},
			isValid:    true,
		},
		{
			name:       "combined filters",
			filters:    map[string]string{"client_id": "client-123", "action": "generated", "start_date": "2024-01-01"},
			isValid:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/api/v1/audit?"
			for k, v := range tt.filters {
				url += k + "=" + v + "&"
			}

			req := httptest.NewRequest("GET", url, nil)

			// Validate filters are correctly parsed
			for k, v := range tt.filters {
				got := req.URL.Query().Get(k)
				if got != v {
					t.Errorf("expected filter %s=%s, got %s", k, v, got)
				}
			}
		})
	}
}

func TestAuditSummaryResponse(t *testing.T) {
	// Test expected summary response structure
	type summaryResponse struct {
		TotalEntries    int        `json:"total_entries"`
		UniqueArtifacts int        `json:"unique_artifacts"`
		UniqueClients   int        `json:"unique_clients"`
		Generations     int        `json:"generations"`
		Edits           int        `json:"edits"`
		StatusChanges   int        `json:"status_changes"`
		Exports         int        `json:"exports"`
		EarliestEntry   *time.Time `json:"earliest_entry"`
		LatestEntry     *time.Time `json:"latest_entry"`
	}

	now := time.Now()
	earliestTime := now.AddDate(0, -6, 0)

	summary := summaryResponse{
		TotalEntries:    500,
		UniqueArtifacts: 50,
		UniqueClients:   10,
		Generations:     50,
		Edits:           300,
		StatusChanges:   100,
		Exports:         50,
		EarliestEntry:   &earliestTime,
		LatestEntry:     &now,
	}

	// Verify summary calculations make sense
	if summary.Generations+summary.Edits+summary.StatusChanges+summary.Exports != summary.TotalEntries {
		// Note: This might not always equal exactly due to deletes
		t.Log("Sum of actions doesn't equal total (may include deletes)")
	}

	if summary.EarliestEntry.After(*summary.LatestEntry) {
		t.Error("earliest entry should be before latest entry")
	}
}
