package artifact

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIsValidArtifactType(t *testing.T) {
	tests := []struct {
		name         string
		artifactType string
		want         bool
	}{
		{"ropa is valid", "ropa", true},
		{"dpia_screening is valid", "dpia_screening", true},
		{"dpa_gap is valid", "dpa_gap", true},
		{"lawful_basis is valid", "lawful_basis", true},
		{"ai_act_classification is valid", "ai_act_classification", true},
		{"invalid type", "invalid", false},
		{"empty string", "", false},
		{"uppercase invalid", "ROPA", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidArtifactType(tt.artifactType); got != tt.want {
				t.Errorf("isValidArtifactType(%q) = %v, want %v", tt.artifactType, got, tt.want)
			}
		})
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase", "stripe", "stripe"},
		{"uppercase", "STRIPE", "stripe"},
		{"mixed case", "HubSpot", "hubspot"},
		{"with spaces", "Amazon Web Services", "amazon-web-services"},
		{"with underscores", "google_workspace", "google-workspace"},
		{"with leading/trailing spaces", "  Stripe  ", "stripe"},
		{"mixed special chars", "AWS_S3 Bucket", "aws-s3-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSlug(tt.input); got != tt.want {
				t.Errorf("normalizeSlug(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int // check length since we want valid JSON
	}{
		{
			name:    "raw JSON object",
			input:   `{"name": "test"}`,
			wantLen: 16,
		},
		{
			name:    "raw JSON array",
			input:   `[{"name": "test"}]`,
			wantLen: 18,
		},
		{
			name: "JSON in code block",
			input: "```json\n{\"name\": \"test\"}\n```",
			wantLen: 16,
		},
		{
			name: "JSON in generic code block",
			input: "```\n{\"name\": \"test\"}\n```",
			wantLen: 16,
		},
		{
			name: "JSON with markdown text before",
			input: "Here is the result:\n```json\n{\"name\": \"test\"}\n```",
			wantLen: 16,
		},
		{
			name:    "no JSON",
			input:   "This is just text",
			wantLen: 0,
		},
		{
			name:    "empty string",
			input:   "",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if len(got) != tt.wantLen {
				t.Errorf("extractJSON() returned %d chars, want %d chars. Got: %q", len(got), tt.wantLen, got)
			}
			// Verify extracted JSON is valid if we expect any
			if tt.wantLen > 0 {
				var js json.RawMessage
				if err := json.Unmarshal([]byte(got), &js); err != nil {
					t.Errorf("extractJSON() returned invalid JSON: %v", err)
				}
			}
		})
	}
}

func TestTruncateText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   string
	}{
		{"short text", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very short max", "hello world", 6, "hel..."},
		{"empty string", "", 10, ""},
		{"maxLen 3", "hello", 3, "..."},
		{"maxLen 0", "hello", 0, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateText(tt.text, tt.maxLen); got != tt.want {
				t.Errorf("truncateText(%q, %d) = %q, want %q", tt.text, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestTopicFilterForType(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name         string
		artifactType string
		want         []string
	}{
		{"ai_act returns ai_act filter", "ai_act_classification", []string{"ai_act"}},
		{"ropa returns gdpr filter", "ropa", []string{"gdpr"}},
		{"dpia_screening returns gdpr filter", "dpia_screening", []string{"gdpr"}},
		{"dpa_gap returns gdpr filter", "dpa_gap", []string{"gdpr"}},
		{"lawful_basis returns gdpr filter", "lawful_basis", []string{"gdpr"}},
		{"unknown type returns nil", "unknown", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.topicFilterForType(tt.artifactType)
			if tt.want == nil && got != nil {
				t.Errorf("topicFilterForType(%q) = %v, want nil", tt.artifactType, got)
			}
			if tt.want != nil {
				if len(got) != len(tt.want) {
					t.Errorf("topicFilterForType(%q) = %v, want %v", tt.artifactType, got, tt.want)
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("topicFilterForType(%q)[%d] = %v, want %v", tt.artifactType, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestMaxTokensForType(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name         string
		artifactType string
		want         int
	}{
		{"ropa gets 8000 tokens", "ropa", 8000},
		{"dpia_screening gets 6000 tokens", "dpia_screening", 6000},
		{"dpa_gap gets 4000 tokens", "dpa_gap", 4000},
		{"ai_act_classification gets 4000 tokens", "ai_act_classification", 4000},
		{"lawful_basis gets 4000 tokens", "lawful_basis", 4000},
		{"unknown gets default 4000 tokens", "unknown", 4000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.maxTokensForType(tt.artifactType); got != tt.want {
				t.Errorf("maxTokensForType(%q) = %v, want %v", tt.artifactType, got, tt.want)
			}
		})
	}
}

func TestBuildRetrievalQuery(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name         string
		artifactType string
		client       ClientContext
		wantContains []string
	}{
		{
			name:         "ropa query contains Article 30",
			artifactType: "ropa",
			client: ClientContext{
				Name:              "TestCo",
				Sector:            "fintech",
				ProcessingPurposes: []string{"payment_processing"},
			},
			wantContains: []string{"Article 30", "Record of Processing Activities", "fintech", "payment_processing"},
		},
		{
			name:         "dpia_screening query contains EDPB criteria",
			artifactType: "dpia_screening",
			client: ClientContext{
				Name:   "TestCo",
				Sector: "healthtech",
			},
			wantContains: []string{"Article 35", "EDPB", "DPIA", "healthtech"},
		},
		{
			name:         "dpa_gap query contains Article 28",
			artifactType: "dpa_gap",
			client: ClientContext{
				Name:      "TestCo",
				TechStack: []string{"Stripe", "AWS"},
			},
			wantContains: []string{"Article 28", "Data Processing Agreement", "Standard Contractual Clauses", "Stripe", "AWS"},
		},
		{
			name:         "ai_act query contains AI Act",
			artifactType: "ai_act_classification",
			client: ClientContext{
				Name:        "TestCo",
				Description: "AI chatbot for customer support",
			},
			wantContains: []string{"AI Act", "risk classification", "AI chatbot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.buildRetrievalQuery(tt.artifactType, tt.client)
			for _, want := range tt.wantContains {
				if !containsString(got, want) {
					t.Errorf("buildRetrievalQuery() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || containsString(s[1:], substr)))
}

func TestParseAndValidate_RoPA(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "valid RoPA",
			input: `{
				"organization_name": "Test Org",
				"generated_date": "2024-01-01",
				"activities": [
					{
						"id": "PA-001",
						"name": "Email Marketing",
						"purpose": "Marketing communications",
						"lawful_basis": {
							"basis": "consent",
							"article": "Art. 6(1)(a)",
							"reasoning": "Users opt-in to marketing",
							"lia_required": false
						},
						"data_categories": ["email"],
						"data_subjects": ["customers"],
						"recipients": [],
						"retention_period": "2 years",
						"retention_rationale": "Marketing best practice",
						"security_measures": ["encryption"],
						"dpia_required": false,
						"dpia_rationale": "Low risk processing",
						"citations": [1]
					}
				]
			}`,
			wantErr: false,
		},
		{
			name: "RoPA missing organization_name",
			input: `{
				"generated_date": "2024-01-01",
				"activities": [{"id": "PA-001"}]
			}`,
			wantErr: true,
		},
		{
			name: "RoPA with empty activities",
			input: `{
				"organization_name": "Test Org",
				"generated_date": "2024-01-01",
				"activities": []
			}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{not valid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.parseAndValidate("ropa", tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAndValidate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseAndValidate_DPIAScreening(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "valid DPIAScreening",
			input: `{
				"client_name": "Test Client",
				"generated_date": "2024-01-01",
				"screening_result": "required",
				"overall_rationale": "High risk processing identified",
				"activities": [],
				"edpb_criteria": [],
				"recommendations": [],
				"citations": []
			}`,
			wantErr: false,
		},
		{
			name: "DPIAScreening missing client_name",
			input: `{
				"generated_date": "2024-01-01",
				"screening_result": "required"
			}`,
			wantErr: true,
		},
		{
			name: "DPIAScreening missing screening_result",
			input: `{
				"client_name": "Test Client",
				"generated_date": "2024-01-01"
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.parseAndValidate("dpia_screening", tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAndValidate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseAndValidate_DPAGap(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "valid DPAGapAnalysis",
			input: `{
				"client_name": "Test Client",
				"generated_date": "2024-01-01",
				"processors": [],
				"summary": {
					"total_processors": 0,
					"dpas_in_place": 0,
					"dpas_needed": 0,
					"transfers_required": 0,
					"tias_required": 0
				},
				"citations": []
			}`,
			wantErr: false,
		},
		{
			name: "DPAGapAnalysis missing client_name",
			input: `{
				"generated_date": "2024-01-01",
				"processors": []
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.parseAndValidate("dpa_gap", tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAndValidate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseAndValidate_AIActClassification(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "valid AIActClassification",
			input: `{
				"client_name": "Test Client",
				"generated_date": "2024-01-01",
				"ai_components": [],
				"summary": "No high-risk AI systems identified",
				"citations": []
			}`,
			wantErr: false,
		},
		{
			name: "AIActClassification missing client_name",
			input: `{
				"generated_date": "2024-01-01",
				"ai_components": []
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.parseAndValidate("ai_act_classification", tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseAndValidate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInMemoryProcessorRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewInMemoryProcessorRepository()
	repo.SeedCommonProcessors()

	t.Run("GetBySlug", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "stripe")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p == nil {
			t.Fatal("GetBySlug() returned nil for existing processor")
		}
		if p.Name != "Stripe" {
			t.Errorf("GetBySlug() Name = %v, want Stripe", p.Name)
		}
	})

	t.Run("GetBySlug not found", func(t *testing.T) {
		p, err := repo.GetBySlug(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetBySlug() error = %v", err)
		}
		if p != nil {
			t.Error("GetBySlug() should return nil for non-existent processor")
		}
	})

	t.Run("GetByName case insensitive", func(t *testing.T) {
		p, err := repo.GetByName(ctx, "STRIPE")
		if err != nil {
			t.Fatalf("GetByName() error = %v", err)
		}
		if p == nil {
			t.Fatal("GetByName() returned nil for existing processor")
		}
		if p.Slug != "stripe" {
			t.Errorf("GetByName() Slug = %v, want stripe", p.Slug)
		}
	})

	t.Run("GetByCategory", func(t *testing.T) {
		procs, err := repo.GetByCategory(ctx, "payment")
		if err != nil {
			t.Fatalf("GetByCategory() error = %v", err)
		}
		if len(procs) == 0 {
			t.Error("GetByCategory() returned no processors for 'payment' category")
		}
		for _, p := range procs {
			if p.Category != "payment" {
				t.Errorf("GetByCategory() returned processor with wrong category: %v", p.Category)
			}
		}
	})

	t.Run("Search", func(t *testing.T) {
		procs, err := repo.Search(ctx, "stripe", 10)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if len(procs) == 0 {
			t.Error("Search() returned no results for 'stripe'")
		}
	})

	t.Run("Count", func(t *testing.T) {
		count, err := repo.Count(ctx)
		if err != nil {
			t.Fatalf("Count() error = %v", err)
		}
		if count == 0 {
			t.Error("Count() returned 0 after seeding")
		}
	})

	t.Run("List with pagination", func(t *testing.T) {
		procs, err := repo.List(ctx, 0, 3)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(procs) > 3 {
			t.Errorf("List() returned %d processors, want at most 3", len(procs))
		}
	})
}

func TestResolveProcessors(t *testing.T) {
	repo := NewInMemoryProcessorRepository()
	repo.SeedCommonProcessors()

	s := &Service{
		processorRepo: repo,
	}

	ctx := context.Background()

	t.Run("resolves known processors", func(t *testing.T) {
		procs, err := s.resolveProcessors(ctx, []string{"Stripe", "HubSpot"})
		if err != nil {
			t.Fatalf("resolveProcessors() error = %v", err)
		}
		if len(procs) != 2 {
			t.Fatalf("resolveProcessors() returned %d processors, want 2", len(procs))
		}
		if procs[0].Category == "unknown" {
			t.Error("Stripe should not be marked as unknown")
		}
		if procs[1].Category == "unknown" {
			t.Error("HubSpot should not be marked as unknown")
		}
	})

	t.Run("marks unknown processors", func(t *testing.T) {
		procs, err := s.resolveProcessors(ctx, []string{"UnknownTool"})
		if err != nil {
			t.Fatalf("resolveProcessors() error = %v", err)
		}
		if len(procs) != 1 {
			t.Fatalf("resolveProcessors() returned %d processors, want 1", len(procs))
		}
		if procs[0].Category != "unknown" {
			t.Errorf("Unknown tool should be marked as unknown, got category: %v", procs[0].Category)
		}
		if procs[0].DPAStatus != "unknown" {
			t.Errorf("Unknown tool should have unknown DPA status, got: %v", procs[0].DPAStatus)
		}
	})

	t.Run("handles empty tech stack", func(t *testing.T) {
		procs, err := s.resolveProcessors(ctx, []string{})
		if err != nil {
			t.Fatalf("resolveProcessors() error = %v", err)
		}
		if len(procs) != 0 {
			t.Errorf("resolveProcessors() returned %d processors for empty stack, want 0", len(procs))
		}
	})

	t.Run("resolves by slug normalization", func(t *testing.T) {
		procs, err := s.resolveProcessors(ctx, []string{"google-workspace"})
		if err != nil {
			t.Fatalf("resolveProcessors() error = %v", err)
		}
		if len(procs) != 1 {
			t.Fatalf("resolveProcessors() returned %d processors, want 1", len(procs))
		}
		if procs[0].Name != "Google Workspace" {
			t.Errorf("Expected Google Workspace, got: %v", procs[0].Name)
		}
	})
}

func TestBuildArtifactPrompt(t *testing.T) {
	client := ClientContext{
		Name:              "Test Company",
		Description:       "E-commerce platform",
		Sector:            "retail",
		Country:           "DE",
		EmployeeCount:     50,
		TechStack:         []string{"Stripe", "Shopify"},
		DataSubjects:      []string{"customers", "employees"},
		ProcessingPurposes: []string{"payment_processing", "marketing"},
	}

	processors := []ProcessorProfileData{
		{
			Name:              "Stripe",
			Slug:              "stripe",
			Category:          "payment",
			Headquarters:      "US",
			DataCategories:    []string{"payment_card"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
		},
	}

	regDocs := []RegulatoryDocument{
		{
			Title:     "GDPR Article 30",
			SourceURL: "https://eur-lex.europa.eu/...",
			Text:      "Record of processing activities...",
			Tier:      "primary",
		},
	}

	t.Run("ropa prompt contains required elements", func(t *testing.T) {
		pp := BuildArtifactPrompt("ropa", client, processors, regDocs)
		if pp.System == "" {
			t.Error("System prompt should not be empty")
		}
		if len(pp.Messages) == 0 {
			t.Error("Messages should not be empty")
		}
		if pp.Messages[0].Role != "user" {
			t.Errorf("First message role should be 'user', got %v", pp.Messages[0].Role)
		}
		// Check system prompt contains key instructions
		if !containsSubstring(pp.System, "Article 30") {
			t.Error("System prompt should mention Article 30")
		}
		if !containsSubstring(pp.System, "JSON") {
			t.Error("System prompt should mention JSON output")
		}
	})

	t.Run("dpia prompt contains EDPB criteria", func(t *testing.T) {
		pp := BuildArtifactPrompt("dpia_screening", client, processors, regDocs)
		if !containsSubstring(pp.System, "9 EDPB") || !containsSubstring(pp.System, "EDPB") {
			t.Error("System prompt should mention EDPB criteria")
		}
		if !containsSubstring(pp.System, "Article 35") {
			t.Error("System prompt should mention Article 35")
		}
	})

	t.Run("dpa_gap prompt contains Article 28", func(t *testing.T) {
		pp := BuildArtifactPrompt("dpa_gap", client, processors, regDocs)
		if !containsSubstring(pp.System, "Article 28") {
			t.Error("System prompt should mention Article 28")
		}
		if !containsSubstring(pp.System, "SCC") && !containsSubstring(pp.System, "Standard Contractual Clauses") {
			t.Error("System prompt should mention SCCs")
		}
	})

	t.Run("ai_act prompt contains risk categories", func(t *testing.T) {
		pp := BuildArtifactPrompt("ai_act_classification", client, processors, regDocs)
		if !containsSubstring(pp.System, "UNACCEPTABLE") && !containsSubstring(pp.System, "unacceptable") {
			t.Error("System prompt should mention unacceptable risk")
		}
		if !containsSubstring(pp.System, "HIGH-RISK") && !containsSubstring(pp.System, "high-risk") && !containsSubstring(pp.System, "High-Risk") {
			t.Error("System prompt should mention high-risk")
		}
	})
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFormatRegulatoryContext(t *testing.T) {
	docs := []RegulatoryDocument{
		{
			Title:     "GDPR Article 30",
			SourceURL: "https://example.com/gdpr",
			Text:      "Record of processing activities requirements...",
			Tier:      "primary",
		},
		{
			Title:     "EDPB Guidelines",
			SourceURL: "",
			Text:      "Guidelines on DPIA...",
			Tier:      "secondary",
		},
	}

	result := formatRegulatoryContext(docs)

	// Check that documents are numbered
	if !containsSubstring(result, "[1]") {
		t.Error("First document should be numbered [1]")
	}
	if !containsSubstring(result, "[2]") {
		t.Error("Second document should be numbered [2]")
	}

	// Check that titles are included
	if !containsSubstring(result, "GDPR Article 30") {
		t.Error("Document title should be included")
	}

	// Check that URLs are included when present
	if !containsSubstring(result, "https://example.com/gdpr") {
		t.Error("Document URL should be included")
	}

	// Check that tier is included
	if !containsSubstring(result, "primary") {
		t.Error("Document tier should be included")
	}
}

func TestFormatRegulatoryContextEmpty(t *testing.T) {
	result := formatRegulatoryContext([]RegulatoryDocument{})
	if result != "No regulatory sources available." {
		t.Errorf("Empty docs should return placeholder message, got: %v", result)
	}
}

func TestFormatProcessorContext(t *testing.T) {
	processors := []ProcessorProfileData{
		{
			Name:              "Stripe",
			Category:          "payment",
			Headquarters:      "US",
			DataCategories:    []string{"payment_card", "email"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://stripe.com/dpa",
		},
	}

	result := formatProcessorContext(processors)

	if !containsSubstring(result, "Stripe") {
		t.Error("Processor name should be included")
	}
	if !containsSubstring(result, "payment") {
		t.Error("Processor category should be included")
	}
	if !containsSubstring(result, "US") {
		t.Error("Processor headquarters should be included")
	}
	if !containsSubstring(result, "dpf") {
		t.Error("Transfer mechanism should be included")
	}
	if !containsSubstring(result, "in_place") {
		t.Error("DPA status should be included")
	}
}

func TestFormatProcessorContextEmpty(t *testing.T) {
	result := formatProcessorContext([]ProcessorProfileData{})
	if !containsSubstring(result, "No processor profiles") {
		t.Errorf("Empty processors should return placeholder message, got: %v", result)
	}
}
