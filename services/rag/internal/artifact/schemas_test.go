package artifact

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRoPA_JSONRoundTrip(t *testing.T) {
	original := RoPA{
		OrganizationName: "Acme Corp",
		DPOName:          "Jane Doe",
		GeneratedDate:    "2024-01-15",
		Activities: []ProcessingActivity{
			{
				ID:      "PA-001",
				Name:    "Email marketing via HubSpot",
				Purpose: "Direct marketing communications to customers",
				LawfulBasis: LawfulBasisEntry{
					Basis:       "consent",
					Article:     "Art. 6(1)(a)",
					Reasoning:   "Explicit consent obtained at signup via opt-in checkbox",
					LIARequired: false,
				},
				DataCategories: []string{"email", "name", "purchase_history"},
				DataSubjects:   []string{"customers"},
				Recipients: []Recipient{
					{
						Name:      "HubSpot Inc.",
						Role:      "processor",
						Purpose:   "Email marketing automation",
						DPAStatus: "in_place",
					},
				},
				Transfers: []Transfer{
					{
						Destination: "US",
						Mechanism:   "dpf",
						Notes:       "HubSpot is certified under EU-US Data Privacy Framework",
					},
				},
				RetentionPeriod:    "24 months after last interaction",
				RetentionRationale: "Based on marketing best practices and legitimate business need",
				SecurityMeasures:   []string{"encryption at rest", "TLS in transit", "access controls"},
				DPIARequired:       false,
				DPIARationale:      "Standard marketing activity, no high-risk processing",
				Notes:              "Review consent mechanisms annually",
				Citations:          []int{1, 3, 5},
			},
		},
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal RoPA: %v", err)
	}

	// Unmarshal back
	var decoded RoPA
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal RoPA: %v", err)
	}

	// Verify key fields
	if decoded.OrganizationName != original.OrganizationName {
		t.Errorf("OrganizationName mismatch: got %q, want %q", decoded.OrganizationName, original.OrganizationName)
	}
	if decoded.DPOName != original.DPOName {
		t.Errorf("DPOName mismatch: got %q, want %q", decoded.DPOName, original.DPOName)
	}
	if len(decoded.Activities) != len(original.Activities) {
		t.Errorf("Activities length mismatch: got %d, want %d", len(decoded.Activities), len(original.Activities))
	}
	if decoded.Activities[0].LawfulBasis.Basis != original.Activities[0].LawfulBasis.Basis {
		t.Errorf("LawfulBasis.Basis mismatch: got %q, want %q",
			decoded.Activities[0].LawfulBasis.Basis, original.Activities[0].LawfulBasis.Basis)
	}
}

func TestDPIAScreening_JSONRoundTrip(t *testing.T) {
	original := DPIAScreening{
		ClientName:       "TechStartup Ltd",
		GeneratedDate:    "2024-01-15",
		ScreeningResult:  "required",
		OverallRationale: "Processing involves systematic monitoring and profiling of individuals at large scale",
		Activities: []DPIAActivityCheck{
			{
				ActivityName:    "User behavior analytics",
				RiskLevel:       "high",
				TriggerCriteria: []string{"systematic monitoring", "evaluation or scoring"},
				Rationale:       "Systematic collection and analysis of user behavior patterns across the platform",
				RequiresDPIA:    true,
			},
		},
		EDPBCriteria: []CriterionCheck{
			{Number: 1, Name: "Evaluation or scoring", Triggered: true, Evidence: "User scoring for personalization"},
			{Number: 2, Name: "Automated decision-making with legal effects", Triggered: false, Evidence: "No automated decisions with legal effect"},
			{Number: 3, Name: "Systematic monitoring", Triggered: true, Evidence: "Continuous tracking of user activity"},
			{Number: 4, Name: "Sensitive data or highly personal data", Triggered: false, Evidence: "No special categories processed"},
			{Number: 5, Name: "Large scale processing", Triggered: true, Evidence: "Over 100,000 users processed"},
			{Number: 6, Name: "Matching or combining datasets", Triggered: false, Evidence: "No dataset matching"},
			{Number: 7, Name: "Vulnerable data subjects", Triggered: false, Evidence: "No children or vulnerable groups targeted"},
			{Number: 8, Name: "Innovative use of technology", Triggered: true, Evidence: "Machine learning for behavior prediction"},
			{Number: 9, Name: "Processing preventing data subjects from exercising rights", Triggered: false, Evidence: "All rights exercisable"},
		},
		Recommendations: []string{
			"Conduct full DPIA before processing begins",
			"Consult with supervisory authority if high residual risk remains",
			"Implement privacy by design measures",
		},
		Citations: []int{2, 4, 7},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal DPIAScreening: %v", err)
	}

	var decoded DPIAScreening
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DPIAScreening: %v", err)
	}

	if decoded.ClientName != original.ClientName {
		t.Errorf("ClientName mismatch: got %q, want %q", decoded.ClientName, original.ClientName)
	}
	if decoded.ScreeningResult != original.ScreeningResult {
		t.Errorf("ScreeningResult mismatch: got %q, want %q", decoded.ScreeningResult, original.ScreeningResult)
	}
	if len(decoded.EDPBCriteria) != 9 {
		t.Errorf("EDPBCriteria length mismatch: got %d, want 9", len(decoded.EDPBCriteria))
	}

	// Count triggered criteria
	triggered := 0
	for _, c := range decoded.EDPBCriteria {
		if c.Triggered {
			triggered++
		}
	}
	if triggered != 4 {
		t.Errorf("Triggered criteria count mismatch: got %d, want 4", triggered)
	}
}

func TestDPAGapAnalysis_JSONRoundTrip(t *testing.T) {
	original := DPAGapAnalysis{
		ClientName:    "FinTech Solutions",
		GeneratedDate: "2024-01-15",
		Processors: []DPACheck{
			{
				ProcessorName:     "Stripe",
				Category:          "payment",
				DataCategories:    []string{"name", "email", "payment_card", "billing_address"},
				Headquarters:      "US",
				DPAStatus:         "in_place",
				DPAPublicURL:      "https://stripe.com/legal/dpa",
				TransferRequired:  true,
				TransferMechanism: "dpf",
				TIARequired:       false,
				SCCType:           "",
				Actions:           []string{"Verify DPF certification annually"},
			},
			{
				ProcessorName:     "Custom Analytics Tool",
				Category:          "analytics",
				DataCategories:    []string{"user_behavior", "ip_address"},
				Headquarters:      "US",
				DPAStatus:         "needed",
				DPAPublicURL:      "",
				TransferRequired:  true,
				TransferMechanism: "scc",
				TIARequired:       true,
				SCCType:           "module_2",
				Actions:           []string{"Execute DPA", "Conduct TIA", "Implement SCCs Module 2"},
			},
		},
		Summary: DPAGapSummary{
			TotalProcessors:   2,
			DPAsInPlace:       1,
			DPAsNeeded:        1,
			TransfersRequired: 2,
			TIAsRequired:      1,
		},
		Citations: []int{1, 6},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal DPAGapAnalysis: %v", err)
	}

	var decoded DPAGapAnalysis
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DPAGapAnalysis: %v", err)
	}

	if decoded.ClientName != original.ClientName {
		t.Errorf("ClientName mismatch: got %q, want %q", decoded.ClientName, original.ClientName)
	}
	if decoded.Summary.TotalProcessors != original.Summary.TotalProcessors {
		t.Errorf("Summary.TotalProcessors mismatch: got %d, want %d",
			decoded.Summary.TotalProcessors, original.Summary.TotalProcessors)
	}
	if decoded.Summary.DPAsNeeded != original.Summary.DPAsNeeded {
		t.Errorf("Summary.DPAsNeeded mismatch: got %d, want %d",
			decoded.Summary.DPAsNeeded, original.Summary.DPAsNeeded)
	}
}

func TestAIActClassification_JSONRoundTrip(t *testing.T) {
	original := AIActClassification{
		ClientName:    "AI Startup GmbH",
		GeneratedDate: "2024-01-15",
		AIComponents: []AIComponent{
			{
				Name:                "Resume Screening Tool",
				Description:         "AI system that ranks job applicants based on CV analysis",
				RiskCategory:        "high",
				ClassificationBasis: "Annex III, point 4(a) - AI systems for recruitment",
				Obligations: []string{
					"Establish risk management system",
					"Ensure data governance and data quality",
					"Maintain technical documentation",
					"Implement logging capabilities",
					"Ensure human oversight",
					"Achieve appropriate accuracy, robustness, and cybersecurity",
				},
				Timeline:         "Compliance required from August 2025",
				TransparencyReqs: []string{"Inform candidates that AI is used in screening", "Provide meaningful information about logic involved"},
				Recommendations:  []string{"Conduct conformity assessment", "Register in EU AI database", "Appoint authorized representative if non-EU provider"},
			},
			{
				Name:                "Customer Service Chatbot",
				Description:         "AI chatbot for answering customer queries",
				RiskCategory:        "limited",
				ClassificationBasis: "Article 50 - AI systems interacting with natural persons",
				Obligations:         []string{"Transparency obligation - inform users they are interacting with AI"},
				Timeline:            "Compliance required from August 2025",
				TransparencyReqs:    []string{"Clear disclosure that user is interacting with AI system"},
				Recommendations:     []string{"Update user interface to clearly indicate AI interaction"},
			},
		},
		Summary:   "Two AI systems identified: one high-risk (recruitment), one limited-risk (chatbot). Priority action needed for recruitment tool compliance.",
		Citations: []int{8, 9, 10},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal AIActClassification: %v", err)
	}

	var decoded AIActClassification
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal AIActClassification: %v", err)
	}

	if decoded.ClientName != original.ClientName {
		t.Errorf("ClientName mismatch: got %q, want %q", decoded.ClientName, original.ClientName)
	}
	if len(decoded.AIComponents) != 2 {
		t.Errorf("AIComponents length mismatch: got %d, want 2", len(decoded.AIComponents))
	}
	if decoded.AIComponents[0].RiskCategory != "high" {
		t.Errorf("First AIComponent RiskCategory mismatch: got %q, want %q",
			decoded.AIComponents[0].RiskCategory, "high")
	}
	if decoded.AIComponents[1].RiskCategory != "limited" {
		t.Errorf("Second AIComponent RiskCategory mismatch: got %q, want %q",
			decoded.AIComponents[1].RiskCategory, "limited")
	}
}

func TestSampleRoPA_SerializesUnder50KB(t *testing.T) {
	// Create a RoPA with 5 processing activities as per acceptance criteria
	ropa := RoPA{
		OrganizationName: "Enterprise Solutions Ltd",
		DPOName:          "Dr. Privacy Expert",
		GeneratedDate:    "2024-01-15",
		Activities:       make([]ProcessingActivity, 5),
	}

	activityTemplates := []struct {
		name       string
		purpose    string
		basis      string
		article    string
		categories []string
		subjects   []string
	}{
		{
			name:       "Employee HR Management",
			purpose:    "Managing employment relationship including payroll, benefits, and performance",
			basis:      "contract",
			article:    "Art. 6(1)(b)",
			categories: []string{"name", "address", "salary", "bank_details", "performance_reviews"},
			subjects:   []string{"employees"},
		},
		{
			name:       "Customer CRM via Salesforce",
			purpose:    "Customer relationship management and sales pipeline tracking",
			basis:      "legitimate_interests",
			article:    "Art. 6(1)(f)",
			categories: []string{"name", "email", "company", "phone", "interaction_history"},
			subjects:   []string{"business_contacts", "prospects"},
		},
		{
			name:       "Marketing Email Campaigns",
			purpose:    "Direct marketing communications to opted-in subscribers",
			basis:      "consent",
			article:    "Art. 6(1)(a)",
			categories: []string{"email", "name", "preferences", "engagement_metrics"},
			subjects:   []string{"subscribers", "customers"},
		},
		{
			name:       "Payment Processing via Stripe",
			purpose:    "Processing customer payments for products and services",
			basis:      "contract",
			article:    "Art. 6(1)(b)",
			categories: []string{"name", "email", "payment_card", "billing_address", "transaction_history"},
			subjects:   []string{"customers"},
		},
		{
			name:       "Website Analytics via Google Analytics",
			purpose:    "Understanding website usage to improve user experience",
			basis:      "legitimate_interests",
			article:    "Art. 6(1)(f)",
			categories: []string{"ip_address", "device_info", "browsing_behavior", "cookies"},
			subjects:   []string{"website_visitors"},
		},
	}

	for i, tmpl := range activityTemplates {
		ropa.Activities[i] = ProcessingActivity{
			ID:      fmt.Sprintf("PA-%03d", i+1),
			Name:    tmpl.name,
			Purpose: tmpl.purpose,
			LawfulBasis: LawfulBasisEntry{
				Basis:       tmpl.basis,
				Article:     tmpl.article,
				Reasoning:   "Processing is necessary for the stated purpose under the identified legal basis",
				LIARequired: tmpl.basis == "legitimate_interests",
			},
			DataCategories: tmpl.categories,
			DataSubjects:   tmpl.subjects,
			Recipients: []Recipient{
				{
					Name:      "Primary Processor",
					Role:      "processor",
					Purpose:   "Service provision",
					DPAStatus: "in_place",
				},
			},
			Transfers: []Transfer{
				{
					Destination: "US",
					Mechanism:   "dpf",
					Notes:       "Processor is DPF certified",
				},
			},
			RetentionPeriod:    "Duration of relationship + 6 years for legal compliance",
			RetentionRationale: "Based on statutory limitation periods and regulatory requirements",
			SecurityMeasures:   []string{"encryption", "access_controls", "audit_logging", "backup"},
			DPIARequired:       false,
			DPIARationale:      "Standard processing, no high-risk indicators",
			Citations:          []int{1, 2, 3},
		}
	}

	data, err := json.Marshal(ropa)
	if err != nil {
		t.Fatalf("Failed to marshal sample RoPA: %v", err)
	}

	sizeKB := float64(len(data)) / 1024.0
	if sizeKB >= 50 {
		t.Errorf("Sample RoPA with 5 activities exceeds 50KB limit: %.2f KB", sizeKB)
	}

	t.Logf("Sample RoPA serialized size: %.2f KB", sizeKB)
}

func TestCitation_JSONRoundTrip(t *testing.T) {
	original := Citation{
		Index:     1,
		SourceURL: "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679",
		Title:     "General Data Protection Regulation (GDPR)",
		Section:   "Article 6 - Lawfulness of processing",
		ChunkText: "Processing shall be lawful only if and to the extent that at least one of the following applies...",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Citation: %v", err)
	}

	var decoded Citation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Citation: %v", err)
	}

	if decoded.Index != original.Index {
		t.Errorf("Index mismatch: got %d, want %d", decoded.Index, original.Index)
	}
	if decoded.SourceURL != original.SourceURL {
		t.Errorf("SourceURL mismatch: got %q, want %q", decoded.SourceURL, original.SourceURL)
	}
	if decoded.Title != original.Title {
		t.Errorf("Title mismatch: got %q, want %q", decoded.Title, original.Title)
	}
}

func TestGenerationMeta_JSONRoundTrip(t *testing.T) {
	original := GenerationMeta{
		Provider:      "anthropic",
		Model:         "claude-sonnet-4-20250514",
		TokensUsed:    4521,
		LatencyMs:     3250,
		CorpusVersion: "2024-01-15-v2",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal GenerationMeta: %v", err)
	}

	var decoded GenerationMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal GenerationMeta: %v", err)
	}

	if decoded.Provider != original.Provider {
		t.Errorf("Provider mismatch: got %q, want %q", decoded.Provider, original.Provider)
	}
	if decoded.TokensUsed != original.TokensUsed {
		t.Errorf("TokensUsed mismatch: got %d, want %d", decoded.TokensUsed, original.TokensUsed)
	}
	if decoded.LatencyMs != original.LatencyMs {
		t.Errorf("LatencyMs mismatch: got %d, want %d", decoded.LatencyMs, original.LatencyMs)
	}
}

func TestLawfulBasisEntry_AllBasisTypes(t *testing.T) {
	bases := []struct {
		basis       string
		article     string
		liaRequired bool
	}{
		{"consent", "Art. 6(1)(a)", false},
		{"contract", "Art. 6(1)(b)", false},
		{"legal_obligation", "Art. 6(1)(c)", false},
		{"vital_interests", "Art. 6(1)(d)", false},
		{"public_task", "Art. 6(1)(e)", false},
		{"legitimate_interests", "Art. 6(1)(f)", true},
	}

	for _, tc := range bases {
		t.Run(tc.basis, func(t *testing.T) {
			entry := LawfulBasisEntry{
				Basis:       tc.basis,
				Article:     tc.article,
				Reasoning:   "Test reasoning for " + tc.basis,
				LIARequired: tc.liaRequired,
			}

			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatalf("Failed to marshal LawfulBasisEntry: %v", err)
			}

			var decoded LawfulBasisEntry
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Failed to unmarshal LawfulBasisEntry: %v", err)
			}

			if decoded.Basis != tc.basis {
				t.Errorf("Basis mismatch: got %q, want %q", decoded.Basis, tc.basis)
			}
			if decoded.LIARequired != tc.liaRequired {
				t.Errorf("LIARequired mismatch: got %v, want %v", decoded.LIARequired, tc.liaRequired)
			}
		})
	}
}

func TestEmptyArtifacts_MarshalCorrectly(t *testing.T) {
	t.Run("EmptyRoPA", func(t *testing.T) {
		ropa := RoPA{
			OrganizationName: "Test Org",
			GeneratedDate:    "2024-01-15",
			Activities:       []ProcessingActivity{},
		}

		data, err := json.Marshal(ropa)
		if err != nil {
			t.Fatalf("Failed to marshal empty RoPA: %v", err)
		}

		var decoded RoPA
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal empty RoPA: %v", err)
		}

		if len(decoded.Activities) != 0 {
			t.Errorf("Expected empty activities, got %d", len(decoded.Activities))
		}
	})

	t.Run("EmptyDPAGapAnalysis", func(t *testing.T) {
		dpa := DPAGapAnalysis{
			ClientName:    "Test Client",
			GeneratedDate: "2024-01-15",
			Processors:    []DPACheck{},
			Summary: DPAGapSummary{
				TotalProcessors:   0,
				DPAsInPlace:       0,
				DPAsNeeded:        0,
				TransfersRequired: 0,
				TIAsRequired:      0,
			},
			Citations: []int{},
		}

		data, err := json.Marshal(dpa)
		if err != nil {
			t.Fatalf("Failed to marshal empty DPAGapAnalysis: %v", err)
		}

		var decoded DPAGapAnalysis
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Failed to unmarshal empty DPAGapAnalysis: %v", err)
		}

		if len(decoded.Processors) != 0 {
			t.Errorf("Expected empty processors, got %d", len(decoded.Processors))
		}
	})
}

func TestOmitEmptyFields(t *testing.T) {
	// Test that omitempty fields are excluded when empty
	ropa := RoPA{
		OrganizationName: "Test Org",
		GeneratedDate:    "2024-01-15",
		Activities:       []ProcessingActivity{},
	}

	data, err := json.Marshal(ropa)
	if err != nil {
		t.Fatalf("Failed to marshal RoPA: %v", err)
	}

	// DPOName should not appear in JSON when empty
	jsonStr := string(data)
	if strings.Contains(jsonStr, "dpo_name") {
		t.Error("Expected dpo_name to be omitted when empty")
	}

	// Test ProcessingActivity with omitempty fields
	activity := ProcessingActivity{
		ID:      "PA-001",
		Name:    "Test Activity",
		Purpose: "Test Purpose",
		LawfulBasis: LawfulBasisEntry{
			Basis:   "consent",
			Article: "Art. 6(1)(a)",
		},
		DataCategories:     []string{"email"},
		DataSubjects:       []string{"customers"},
		Recipients:         []Recipient{},
		RetentionPeriod:    "12 months",
		RetentionRationale: "Standard retention",
		SecurityMeasures:   []string{},
		DPIARequired:       false,
		DPIARationale:      "Not required",
		Citations:          []int{},
	}

	activityData, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("Failed to marshal ProcessingActivity: %v", err)
	}

	activityJSON := string(activityData)
	// Transfers and Notes should be omitted when empty
	if strings.Contains(activityJSON, `"transfers"`) {
		t.Error("Expected transfers to be omitted when nil")
	}
	if strings.Contains(activityJSON, `"notes"`) {
		t.Error("Expected notes to be omitted when empty string")
	}
}

func TestJSONFieldNames_MatchPRDSpec(t *testing.T) {
	// Verify that JSON field names match the PRD specification exactly
	ropa := RoPA{
		OrganizationName: "Test",
		DPOName:          "Test DPO",
		GeneratedDate:    "2024-01-15",
		Activities: []ProcessingActivity{
			{
				ID:      "PA-001",
				Name:    "Test",
				Purpose: "Test",
				LawfulBasis: LawfulBasisEntry{
					Basis:       "consent",
					Article:     "Art. 6(1)(a)",
					Reasoning:   "Test",
					LIARequired: true,
				},
				DataCategories:     []string{"test"},
				DataSubjects:       []string{"test"},
				Recipients:         []Recipient{{Name: "Test", Role: "processor", Purpose: "Test", DPAStatus: "in_place"}},
				Transfers:          []Transfer{{Destination: "US", Mechanism: "dpf", Notes: "Test"}},
				RetentionPeriod:    "12 months",
				RetentionRationale: "Test",
				SecurityMeasures:   []string{"test"},
				DPIARequired:       true,
				DPIARationale:      "Test",
				Notes:              "Test",
				Citations:          []int{1},
			},
		},
	}

	data, err := json.Marshal(ropa)
	if err != nil {
		t.Fatalf("Failed to marshal RoPA: %v", err)
	}

	jsonStr := string(data)

	// Check for expected field names from PRD
	expectedFields := []string{
		`"organization_name"`,
		`"dpo_name"`,
		`"generated_date"`,
		`"activities"`,
		`"id"`,
		`"name"`,
		`"purpose"`,
		`"lawful_basis"`,
		`"basis"`,
		`"article"`,
		`"reasoning"`,
		`"lia_required"`,
		`"data_categories"`,
		`"data_subjects"`,
		`"recipients"`,
		`"transfers"`,
		`"destination"`,
		`"mechanism"`,
		`"notes"`,
		`"retention_period"`,
		`"retention_rationale"`,
		`"security_measures"`,
		`"dpia_required"`,
		`"dpia_rationale"`,
		`"citations"`,
		`"role"`,
		`"dpa_status"`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("Expected JSON to contain field %s", field)
		}
	}
}

func TestDPACheck_AllFieldsPresent(t *testing.T) {
	check := DPACheck{
		ProcessorName:     "Test Processor",
		Category:          "analytics",
		DataCategories:    []string{"email", "name"},
		Headquarters:      "US",
		DPAStatus:         "needed",
		DPAPublicURL:      "https://example.com/dpa",
		TransferRequired:  true,
		TransferMechanism: "scc",
		TIARequired:       true,
		SCCType:           "module_2",
		Actions:           []string{"Execute DPA", "Conduct TIA"},
	}

	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("Failed to marshal DPACheck: %v", err)
	}

	jsonStr := string(data)

	expectedFields := []string{
		`"processor_name"`,
		`"category"`,
		`"data_categories"`,
		`"headquarters"`,
		`"dpa_status"`,
		`"dpa_public_url"`,
		`"transfer_required"`,
		`"transfer_mechanism"`,
		`"tia_required"`,
		`"scc_type"`,
		`"actions"`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("Expected JSON to contain field %s", field)
		}
	}
}

func TestAIComponent_AllFieldsPresent(t *testing.T) {
	component := AIComponent{
		Name:                "Test AI System",
		Description:         "A test AI system",
		RiskCategory:        "high",
		ClassificationBasis: "Annex III",
		Obligations:         []string{"Obligation 1", "Obligation 2"},
		Timeline:            "August 2025",
		TransparencyReqs:    []string{"Req 1", "Req 2"},
		Recommendations:     []string{"Rec 1", "Rec 2"},
	}

	data, err := json.Marshal(component)
	if err != nil {
		t.Fatalf("Failed to marshal AIComponent: %v", err)
	}

	jsonStr := string(data)

	expectedFields := []string{
		`"name"`,
		`"description"`,
		`"risk_category"`,
		`"classification_basis"`,
		`"obligations"`,
		`"timeline"`,
		`"transparency_reqs"`,
		`"recommendations"`,
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, field) {
			t.Errorf("Expected JSON to contain field %s", field)
		}
	}
}
