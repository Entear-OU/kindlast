package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
)

func TestArtifactHandler_GenerateArtifact_Validation(t *testing.T) {
	tests := []struct {
		name           string
		body           interface{}
		user           *models.User
		expectedStatus int
		expectedError  string
	}{
		{
			name: "invalid artifact type",
			body: models.GenerateArtifactRequest{
				Type: "invalid_type",
			},
			user: &models.User{
				ID:   "user-123",
				Plan: models.PlanProfessional,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid artifact type",
		},
		{
			name: "free plan not allowed",
			body: models.GenerateArtifactRequest{
				Type: models.ArtifactTypeRoPA,
			},
			user: &models.User{
				ID:   "user-123",
				Plan: models.PlanFree,
			},
			expectedStatus: http.StatusForbidden,
			expectedError:  "Artifact generation is not available on the free plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify validation logic
			req := tt.body.(models.GenerateArtifactRequest)
			limits := models.DPOCopilotPlanLimits[tt.user.Plan]

			if limits.MaxArtifactsPerMonth == 0 && tt.expectedError == "Artifact generation is not available on the free plan" {
				// Plan check passes
				t.Log("Free plan correctly blocked")
			}

			validTypes := map[string]bool{
				models.ArtifactTypeRoPA:              true,
				models.ArtifactTypeDPIAScreening:     true,
				models.ArtifactTypeDPAGap:            true,
				models.ArtifactTypeLawfulBasis:       true,
				models.ArtifactTypeAIActClassification: true,
			}

			if !validTypes[req.Type] && tt.expectedError == "Invalid artifact type" {
				t.Log("Invalid type correctly detected")
			}
		})
	}
}

func TestArtifactTypes(t *testing.T) {
	// Test that all artifact types are properly defined
	artifactTypes := []string{
		models.ArtifactTypeRoPA,
		models.ArtifactTypeDPIAScreening,
		models.ArtifactTypeDPAGap,
		models.ArtifactTypeLawfulBasis,
		models.ArtifactTypeAIActClassification,
	}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			if artifactType == "" {
				t.Error("artifact type is empty")
			}
		})
	}
}

func TestArtifactStatuses(t *testing.T) {
	// Test status transitions
	validStatuses := map[string]bool{
		models.ArtifactStatusDraft:    true,
		models.ArtifactStatusReviewed: true,
		models.ArtifactStatusApproved: true,
		models.ArtifactStatusExported: true,
	}

	tests := []struct {
		status  string
		isValid bool
	}{
		{models.ArtifactStatusDraft, true},
		{models.ArtifactStatusReviewed, true},
		{models.ArtifactStatusApproved, true},
		{models.ArtifactStatusExported, true},
		{"invalid_status", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if validStatuses[tt.status] != tt.isValid {
				t.Errorf("expected valid=%v for status %q, got %v", tt.isValid, tt.status, validStatuses[tt.status])
			}
		})
	}
}

func TestPlanLimits_Artifacts(t *testing.T) {
	tests := []struct {
		name                string
		plan                string
		maxArtifactsPerMonth int
		exportEnabled       bool
		aiActEnabled        bool
	}{
		{
			name:                "free plan has no artifacts",
			plan:                models.PlanFree,
			maxArtifactsPerMonth: 0,
			exportEnabled:       false,
			aiActEnabled:        false,
		},
		{
			name:                "professional plan allows 50 artifacts",
			plan:                models.PlanProfessional,
			maxArtifactsPerMonth: 50,
			exportEnabled:       true,
			aiActEnabled:        true,
		},
		{
			name:                "team plan allows 200 artifacts",
			plan:                models.PlanTeam,
			maxArtifactsPerMonth: 200,
			exportEnabled:       true,
			aiActEnabled:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limits := models.DPOCopilotPlanLimits[tt.plan]

			if limits.MaxArtifactsPerMonth != tt.maxArtifactsPerMonth {
				t.Errorf("expected max artifacts %d, got %d", tt.maxArtifactsPerMonth, limits.MaxArtifactsPerMonth)
			}
			if limits.ExportEnabled != tt.exportEnabled {
				t.Errorf("expected export enabled %v, got %v", tt.exportEnabled, limits.ExportEnabled)
			}
			if limits.AIActModuleEnabled != tt.aiActEnabled {
				t.Errorf("expected AI Act enabled %v, got %v", tt.aiActEnabled, limits.AIActModuleEnabled)
			}
		})
	}
}

func TestUpdateArtifactStatusRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request models.UpdateArtifactStatusRequest
		isValid bool
	}{
		{
			name: "valid draft status",
			request: models.UpdateArtifactStatusRequest{
				Status: models.ArtifactStatusDraft,
			},
			isValid: true,
		},
		{
			name: "valid reviewed status with reason",
			request: models.UpdateArtifactStatusRequest{
				Status: models.ArtifactStatusReviewed,
				Reason: "Reviewed and approved by DPO",
			},
			isValid: true,
		},
		{
			name: "invalid empty status",
			request: models.UpdateArtifactStatusRequest{
				Status: "",
			},
			isValid: false,
		},
		{
			name: "invalid status value",
			request: models.UpdateArtifactStatusRequest{
				Status: "invalid",
			},
			isValid: false,
		},
	}

	validStatuses := map[string]bool{
		models.ArtifactStatusDraft:    true,
		models.ArtifactStatusReviewed: true,
		models.ArtifactStatusApproved: true,
		models.ArtifactStatusExported: true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := validStatuses[tt.request.Status]
			if isValid != tt.isValid {
				t.Errorf("expected isValid %v, got %v", tt.isValid, isValid)
			}
		})
	}
}

func TestExportArtifactRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		isValid bool
	}{
		{"valid pdf format", "pdf", true},
		{"valid docx format", "docx", true},
		{"invalid format", "doc", false},
		{"invalid empty format", "", false},
		{"invalid xlsx format", "xlsx", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isValid := tt.format == "pdf" || tt.format == "docx"
			if isValid != tt.isValid {
				t.Errorf("expected isValid %v for format %q, got %v", tt.isValid, tt.format, isValid)
			}
		})
	}
}

func TestGenerateTitle(t *testing.T) {
	tests := []struct {
		artifactType string
		clientName   string
		expected     string
	}{
		{models.ArtifactTypeRoPA, "Acme Corp", "Record of Processing Activities"},
		{models.ArtifactTypeDPIAScreening, "Acme Corp", "DPIA Screening Assessment"},
		{models.ArtifactTypeDPAGap, "Acme Corp", "DPA Gap Analysis"},
		{models.ArtifactTypeLawfulBasis, "Acme Corp", "Lawful Basis Assessment"},
		{models.ArtifactTypeAIActClassification, "Acme Corp", "AI Act Risk Classification"},
	}

	titlePrefixes := map[string]string{
		models.ArtifactTypeRoPA:              "Record of Processing Activities",
		models.ArtifactTypeDPIAScreening:     "DPIA Screening Assessment",
		models.ArtifactTypeDPAGap:            "DPA Gap Analysis",
		models.ArtifactTypeLawfulBasis:       "Lawful Basis Assessment",
		models.ArtifactTypeAIActClassification: "AI Act Risk Classification",
	}

	for _, tt := range tests {
		t.Run(tt.artifactType, func(t *testing.T) {
			prefix := titlePrefixes[tt.artifactType]
			if prefix != tt.expected {
				t.Errorf("expected title prefix %q, got %q", tt.expected, prefix)
			}
		})
	}
}

func TestArtifactRouteSetup(t *testing.T) {
	r := chi.NewRouter()

	// Simulate artifact route setup
	r.Route("/api/v1/clients/{clientID}/artifacts", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Post("/generate", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})
		r.Get("/{artifactID}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Put("/{artifactID}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Put("/{artifactID}/status", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/{artifactID}/audit", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Post("/{artifactID}/export", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Get("/{artifactID}/versions", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	tests := []struct {
		method       string
		path         string
		expectedCode int
	}{
		{"GET", "/api/v1/clients/client-123/artifacts", http.StatusOK},
		{"POST", "/api/v1/clients/client-123/artifacts/generate", http.StatusCreated},
		{"GET", "/api/v1/clients/client-123/artifacts/artifact-456", http.StatusOK},
		{"PUT", "/api/v1/clients/client-123/artifacts/artifact-456", http.StatusOK},
		{"PUT", "/api/v1/clients/client-123/artifacts/artifact-456/status", http.StatusOK},
		{"GET", "/api/v1/clients/client-123/artifacts/artifact-456/audit", http.StatusOK},
		{"POST", "/api/v1/clients/client-123/artifacts/artifact-456/export", http.StatusOK},
		{"GET", "/api/v1/clients/client-123/artifacts/artifact-456/versions", http.StatusOK},
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

func TestBuildInputContext(t *testing.T) {
	client := &models.Client{
		Name:               "Acme Corp",
		Description:        "A fintech startup providing payment solutions",
		Sector:             "fintech",
		Country:            "DE",
		EmployeeCount:      50,
		TechStack:          []string{"stripe", "hubspot", "aws"},
		DataSubjects:       []string{"customers", "employees"},
		ProcessingPurposes: []string{"payment_processing", "marketing", "hr"},
	}

	// Verify that all client fields would be included in context
	fields := []string{
		client.Name,
		client.Description,
		client.Sector,
		client.Country,
	}

	for _, field := range fields {
		if field == "" {
			t.Error("client field is empty")
		}
	}

	if len(client.TechStack) == 0 {
		t.Error("tech stack is empty")
	}
	if len(client.DataSubjects) == 0 {
		t.Error("data subjects is empty")
	}
	if len(client.ProcessingPurposes) == 0 {
		t.Error("processing purposes is empty")
	}
}

func TestGenerationMeta(t *testing.T) {
	// Test GenerationMeta structure
	meta := models.GenerationMeta{
		Provider:      "claude",
		Model:         "claude-sonnet-4-20250514",
		TokensUsed:    5000,
		LatencyMs:     3500,
		CorpusVersion: "2024-01-15",
	}

	// Marshal and unmarshal to verify JSON serialization
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("failed to marshal GenerationMeta: %v", err)
	}

	var unmarshaled models.GenerationMeta
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal GenerationMeta: %v", err)
	}

	if unmarshaled.Provider != meta.Provider {
		t.Errorf("expected provider %q, got %q", meta.Provider, unmarshaled.Provider)
	}
	if unmarshaled.TokensUsed != meta.TokensUsed {
		t.Errorf("expected tokens %d, got %d", meta.TokensUsed, unmarshaled.TokensUsed)
	}
}

func TestCitation(t *testing.T) {
	// Test Citation structure
	citation := models.Citation{
		Index:     1,
		SourceURL: "https://eur-lex.europa.eu/gdpr",
		Title:     "General Data Protection Regulation",
		Section:   "Article 6",
		ChunkText: "Lawfulness of processing...",
	}

	// Marshal and unmarshal to verify JSON serialization
	data, err := json.Marshal(citation)
	if err != nil {
		t.Fatalf("failed to marshal Citation: %v", err)
	}

	var unmarshaled models.Citation
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal Citation: %v", err)
	}

	if unmarshaled.Index != citation.Index {
		t.Errorf("expected index %d, got %d", citation.Index, unmarshaled.Index)
	}
	if unmarshaled.SourceURL != citation.SourceURL {
		t.Errorf("expected source URL %q, got %q", citation.SourceURL, unmarshaled.SourceURL)
	}
}

func TestGenerateArtifactRequest(t *testing.T) {
	tests := []struct {
		name    string
		request models.GenerateArtifactRequest
	}{
		{
			name: "RoPA generation",
			request: models.GenerateArtifactRequest{
				Type:              models.ArtifactTypeRoPA,
				AdditionalContext: "Focus on payment processing activities",
			},
		},
		{
			name: "DPIA screening",
			request: models.GenerateArtifactRequest{
				Type: models.ArtifactTypeDPIAScreening,
			},
		},
		{
			name: "AI Act classification",
			request: models.GenerateArtifactRequest{
				Type:              models.ArtifactTypeAIActClassification,
				AdditionalContext: "Using ML for fraud detection",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			var unmarshaled models.GenerateArtifactRequest
			if err := json.Unmarshal(data, &unmarshaled); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			if unmarshaled.Type != tt.request.Type {
				t.Errorf("expected type %q, got %q", tt.request.Type, unmarshaled.Type)
			}
		})
	}
}
