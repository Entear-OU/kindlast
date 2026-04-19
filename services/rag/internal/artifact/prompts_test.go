package artifact

import (
	"strings"
	"testing"

	"github.com/entear/kindlast/services/rag/internal/providers"
)

// sampleClientContext returns a sample client context for testing.
func sampleClientContext() ClientContext {
	return ClientContext{
		Name:              "TechStartup GmbH",
		Description:       "A B2B SaaS company providing HR management software with AI-powered recruitment features",
		Sector:            "hr_technology",
		Country:           "DE",
		EmployeeCount:     50,
		TechStack:         []string{"stripe", "hubspot", "aws", "intercom"},
		DataSubjects:      []string{"employees", "job_applicants", "customers"},
		ProcessingPurposes: []string{"payment_processing", "crm", "email_marketing", "customer_support", "recruitment"},
	}
}

// sampleProcessors returns sample processor profiles for testing.
func sampleProcessors() []ProcessorProfileData {
	return []ProcessorProfileData{
		{
			Name:              "Stripe",
			Slug:              "stripe",
			Category:          "payment",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "payment_card", "billing_address"},
			ProcessingPurposes: []string{"payment_processing", "fraud_detection"},
			DataLocations:     []string{"us", "eu"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://stripe.com/legal/dpa",
		},
		{
			Name:              "HubSpot",
			Slug:              "hubspot",
			Category:          "crm",
			Headquarters:      "US",
			DataCategories:    []string{"name", "email", "phone", "company", "website_activity"},
			ProcessingPurposes: []string{"crm", "email_marketing", "analytics"},
			DataLocations:     []string{"us", "eu"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://legal.hubspot.com/dpa",
		},
		{
			Name:              "Amazon Web Services",
			Slug:              "aws",
			Category:          "cloud_infrastructure",
			Headquarters:      "US",
			DataCategories:    []string{"varies_by_service"},
			ProcessingPurposes: []string{"hosting", "storage", "compute"},
			DataLocations:     []string{"global"},
			TransferMechanism: "dpf",
			DPAStatus:         "in_place",
			DPAURL:            "https://d1.awsstatic.com/legal/aws-gdpr/AWS_GDPR_DPA.pdf",
		},
	}
}

// sampleRegulatoryDocs returns sample regulatory documents for testing.
func sampleRegulatoryDocs() []RegulatoryDocument {
	return []RegulatoryDocument{
		{
			Title:     "GDPR Article 6 - Lawfulness of processing",
			SourceURL: "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679#article6",
			Text:      "Processing shall be lawful only if and to the extent that at least one of the following applies: (a) the data subject has given consent...",
			Tier:      "primary",
		},
		{
			Title:     "EDPB Guidelines on DPIA (wp248rev.01)",
			SourceURL: "https://ec.europa.eu/newsroom/article29/items/611236",
			Text:      "The EDPB has identified nine criteria to consider when assessing whether a DPIA is required...",
			Tier:      "primary",
		},
		{
			Title:     "GDPR Article 28 - Processor",
			SourceURL: "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679#article28",
			Text:      "Where processing is to be carried out on behalf of a controller, the controller shall use only processors providing sufficient guarantees...",
			Tier:      "primary",
		},
	}
}

func TestBuildArtifactPrompt_ReturnsValidPromptPair(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	artifactTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification"}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// System prompt should not be empty
			if result.System == "" {
				t.Error("Expected non-empty system prompt")
			}

			// Messages should not be empty
			if len(result.Messages) == 0 {
				t.Error("Expected at least one message")
			}

			// First message should be from user
			if result.Messages[0].Role != "user" {
				t.Errorf("Expected first message role to be 'user', got %q", result.Messages[0].Role)
			}

			// User message should not be empty
			if result.Messages[0].Content == "" {
				t.Error("Expected non-empty user message content")
			}
		})
	}
}

func TestBuildArtifactPrompt_UnknownTypeDefaultsToRoPA(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("unknown_type", client, processors, docs)
	ropaResult := BuildArtifactPrompt("ropa", client, processors, docs)

	// Should have same system prompt as RoPA
	if result.System != ropaResult.System {
		t.Error("Expected unknown type to default to RoPA prompt")
	}
}

func TestBuildArtifactPrompt_IncludesJSONOnlyInstruction(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	artifactTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification"}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// System prompt should include JSON-only output instruction
			jsonInstructions := []string{
				"ONLY valid JSON",
				"No markdown",
			}

			for _, instruction := range jsonInstructions {
				if !strings.Contains(result.System, instruction) {
					t.Errorf("System prompt should contain %q for JSON-only output", instruction)
				}
			}
		})
	}
}

func TestBuildArtifactPrompt_IncludesSourceNotation(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	artifactTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification"}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// System prompt should mention [N] notation for citations
			if !strings.Contains(result.System, "[N]") {
				t.Error("System prompt should include [N] notation instruction for citations")
			}
		})
	}
}

func TestRopaPrompt_IncludesArticle30Requirements(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ropa", client, processors, docs)

	// Should reference Article 30 requirements
	// Note: "Categories of personal data" is the GDPR terminology
	article30Refs := []string{
		"Art. 30",
		"Art. 6(1)",
		"purpose",
		"lawful basis",
		"categories of personal data",
		"data subjects",
		"recipients",
		"transfers",
		"retention",
		"security measures",
	}

	for _, ref := range article30Refs {
		if !strings.Contains(strings.ToLower(result.System), strings.ToLower(ref)) {
			t.Errorf("RoPA system prompt should reference %q", ref)
		}
	}
}

func TestDpiaPrompt_IncludesAll9EDPBCriteria(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("dpia_screening", client, processors, docs)

	// Should reference all 9 EDPB criteria
	criteria := []string{
		"Evaluation or scoring",
		"Automated decision-making",
		"Systematic monitoring",
		"Sensitive data",
		"large scale",
		"Matching or combining",
		"vulnerable",
		"Innovative",
		"prevents data subjects",
	}

	for _, criterion := range criteria {
		if !strings.Contains(result.System, criterion) {
			t.Errorf("DPIA system prompt should reference EDPB criterion: %q", criterion)
		}
	}
}

func TestDpiaPrompt_IncludesTwoOrMoreRule(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("dpia_screening", client, processors, docs)

	// Should mention the 2+ criteria = required rule
	if !strings.Contains(result.System, "2 or more") {
		t.Error("DPIA system prompt should mention '2 or more criteria' rule")
	}
}

func TestDpaGapPrompt_IncludesArticle28Requirements(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("dpa_gap", client, processors, docs)

	// Should reference Article 28 requirements
	article28Refs := []string{
		"Art. 28",
		"documented instructions",
		"confidentiality",
		"security",
		"sub-processors",
		"data subject rights",
		"deletion or return",
		"audit",
	}

	for _, ref := range article28Refs {
		if !strings.Contains(strings.ToLower(result.System), strings.ToLower(ref)) {
			t.Errorf("DPA gap system prompt should reference %q", ref)
		}
	}
}

func TestDpaGapPrompt_IncludesTransferMechanisms(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("dpa_gap", client, processors, docs)

	// Should reference transfer mechanisms
	mechanisms := []string{
		"Adequacy",
		"Data Privacy Framework",
		"DPF",
		"Standard Contractual Clauses",
		"SCC",
	}

	foundCount := 0
	for _, mechanism := range mechanisms {
		if strings.Contains(result.System, mechanism) {
			foundCount++
		}
	}

	if foundCount < 3 {
		t.Error("DPA gap system prompt should reference major transfer mechanisms")
	}
}

func TestLawfulBasisPrompt_IncludesAllArticle6Bases(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("lawful_basis", client, processors, docs)

	// Should reference all 6 lawful bases
	bases := []string{
		"6(1)(a)",  // consent
		"6(1)(b)",  // contract
		"6(1)(c)",  // legal obligation
		"6(1)(d)",  // vital interests
		"6(1)(e)",  // public task
		"6(1)(f)",  // legitimate interests
	}

	for _, basis := range bases {
		if !strings.Contains(result.System, basis) {
			t.Errorf("Lawful basis system prompt should reference Art. %s", basis)
		}
	}
}

func TestLawfulBasisPrompt_IncludesLIARequirement(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("lawful_basis", client, processors, docs)

	// Should mention LIA for legitimate interests
	if !strings.Contains(result.System, "Legitimate Interests Assessment") && !strings.Contains(result.System, "LIA") {
		t.Error("Lawful basis system prompt should mention LIA requirement")
	}

	// Should mention balancing test
	if !strings.Contains(strings.ToLower(result.System), "balancing") {
		t.Error("Lawful basis system prompt should mention balancing test")
	}
}

func TestAiActPrompt_IncludesRiskCategories(t *testing.T) {
	client := sampleClientContext()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ai_act_classification", client, nil, docs)

	// Should reference all risk categories
	categories := []string{
		"UNACCEPTABLE",
		"HIGH-RISK",
		"LIMITED",
		"MINIMAL",
	}

	for _, category := range categories {
		if !strings.Contains(result.System, category) {
			t.Errorf("AI Act system prompt should reference risk category: %s", category)
		}
	}
}

func TestAiActPrompt_IncludesAnnexIII(t *testing.T) {
	client := sampleClientContext()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ai_act_classification", client, nil, docs)

	// Should reference Annex III high-risk areas
	annexIIIAreas := []string{
		"Biometric",
		"critical infrastructure",
		"Education",
		"Employment",
		"essential services",
	}

	foundCount := 0
	for _, area := range annexIIIAreas {
		if strings.Contains(result.System, area) {
			foundCount++
		}
	}

	if foundCount < 3 {
		t.Error("AI Act system prompt should reference major Annex III high-risk areas")
	}
}

func TestAiActPrompt_IncludesTimelines(t *testing.T) {
	client := sampleClientContext()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ai_act_classification", client, nil, docs)

	// Should include compliance timelines
	if !strings.Contains(result.System, "2025") || !strings.Contains(result.System, "2026") {
		t.Error("AI Act system prompt should include compliance timelines")
	}
}

func TestFormatRegulatoryContext_NumbersSources(t *testing.T) {
	docs := sampleRegulatoryDocs()
	result := formatRegulatoryContext(docs)

	// Should contain numbered sources
	if !strings.Contains(result, "[1]") {
		t.Error("Should contain [1] source marker")
	}
	if !strings.Contains(result, "[2]") {
		t.Error("Should contain [2] source marker")
	}
	if !strings.Contains(result, "[3]") {
		t.Error("Should contain [3] source marker")
	}

	// Should include source titles
	if !strings.Contains(result, "GDPR Article 6") {
		t.Error("Should include source titles")
	}

	// Should include source URLs
	if !strings.Contains(result, "eur-lex.europa.eu") {
		t.Error("Should include source URLs")
	}
}

func TestFormatRegulatoryContext_EmptyDocs(t *testing.T) {
	result := formatRegulatoryContext([]RegulatoryDocument{})

	if !strings.Contains(result, "No regulatory sources available") {
		t.Error("Should indicate no sources available when docs is empty")
	}
}

func TestFormatProcessorContext_FormatsProcessors(t *testing.T) {
	processors := sampleProcessors()
	result := formatProcessorContext(processors)

	// Should include processor names
	if !strings.Contains(result, "Stripe") {
		t.Error("Should include Stripe")
	}
	if !strings.Contains(result, "HubSpot") {
		t.Error("Should include HubSpot")
	}

	// Should include categories
	if !strings.Contains(result, "payment") {
		t.Error("Should include processor categories")
	}

	// Should include headquarters
	if !strings.Contains(result, "US") {
		t.Error("Should include processor headquarters")
	}

	// Should include transfer mechanism
	if !strings.Contains(result, "dpf") {
		t.Error("Should include transfer mechanism")
	}

	// Should include DPA status
	if !strings.Contains(result, "in_place") {
		t.Error("Should include DPA status")
	}
}

func TestFormatProcessorContext_EmptyProcessors(t *testing.T) {
	result := formatProcessorContext([]ProcessorProfileData{})

	if !strings.Contains(result, "No processor profiles available") {
		t.Error("Should indicate no processors available")
	}
	if !strings.Contains(result, "manual review") {
		t.Error("Should mention manual review needed")
	}
}

func TestBuildArtifactPrompt_IncludesClientDetails(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	artifactTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification"}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// User message should include client name
			if !strings.Contains(result.Messages[0].Content, client.Name) {
				t.Error("User message should include client name")
			}

			// User message should include sector
			if !strings.Contains(result.Messages[0].Content, client.Sector) {
				t.Error("User message should include sector")
			}

			// User message should include country
			if !strings.Contains(result.Messages[0].Content, client.Country) {
				t.Error("User message should include country")
			}
		})
	}
}

func TestBuildArtifactPrompt_IncludesProcessorContext(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	// Test types that should include processor context
	processorTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis"}

	for _, artifactType := range processorTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// User message should include processor info
			if !strings.Contains(result.Messages[0].Content, "Stripe") {
				t.Errorf("%s user message should include processor names", artifactType)
			}
		})
	}
}

func TestBuildArtifactPrompt_IncludesRegulatoryContext(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	artifactTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification"}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// User message should include regulatory context
			if !strings.Contains(result.Messages[0].Content, "[1]") {
				t.Errorf("%s user message should include numbered regulatory sources", artifactType)
			}
		})
	}
}

func TestRopaPrompt_IncludesSpecialCategoryGuidance(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ropa", client, processors, docs)

	// Should mention Article 9 special categories
	if !strings.Contains(result.System, "Art. 9") && !strings.Contains(result.System, "Article 9") {
		t.Error("RoPA system prompt should reference Article 9 special categories")
	}

	// Should list some special category types
	specialCategories := []string{
		"health",
		"racial",
		"ethnic",
		"genetic",
		"biometric",
	}

	foundCount := 0
	for _, cat := range specialCategories {
		if strings.Contains(strings.ToLower(result.System), cat) {
			foundCount++
		}
	}

	if foundCount < 3 {
		t.Error("RoPA system prompt should list special category data types")
	}
}

func TestLawfulBasisPrompt_IncludesArticle9Derogations(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("lawful_basis", client, processors, docs)

	// Should mention Article 9(2) derogations
	if !strings.Contains(result.System, "Art. 9(2)") && !strings.Contains(result.System, "Article 9(2)") {
		t.Error("Lawful basis system prompt should reference Article 9(2) derogations")
	}

	// Should mention explicit consent for special categories
	if !strings.Contains(strings.ToLower(result.System), "explicit consent") {
		t.Error("Lawful basis system prompt should mention explicit consent for special categories")
	}
}

func TestPromptPair_HasCorrectStructure(t *testing.T) {
	// Test that PromptPair has the expected fields
	pp := PromptPair{
		System: "test system prompt",
		Messages: []providers.Message{
			{Role: "user", Content: "test user message"},
		},
	}

	if pp.System != "test system prompt" {
		t.Error("PromptPair System field not working correctly")
	}

	if len(pp.Messages) != 1 {
		t.Error("PromptPair Messages field not working correctly")
	}

	if pp.Messages[0].Role != "user" {
		t.Error("Message Role field not working correctly")
	}

	if pp.Messages[0].Content != "test user message" {
		t.Error("Message Content field not working correctly")
	}
}

func TestClientContext_AllFieldsUsable(t *testing.T) {
	ctx := ClientContext{
		Name:              "Test Company",
		Description:       "A test company description",
		Sector:            "technology",
		Country:           "DE",
		EmployeeCount:     100,
		TechStack:         []string{"tool1", "tool2"},
		DataSubjects:      []string{"customers", "employees"},
		ProcessingPurposes: []string{"marketing", "hr"},
	}

	if ctx.Name != "Test Company" {
		t.Error("ClientContext Name field not working correctly")
	}
	if ctx.EmployeeCount != 100 {
		t.Error("ClientContext EmployeeCount field not working correctly")
	}
	if len(ctx.TechStack) != 2 {
		t.Error("ClientContext TechStack field not working correctly")
	}
}

func TestProcessorProfileData_AllFieldsUsable(t *testing.T) {
	processor := ProcessorProfileData{
		Name:              "Test Processor",
		Slug:              "test-processor",
		Category:          "analytics",
		Headquarters:      "US",
		DataCategories:    []string{"email", "name"},
		ProcessingPurposes: []string{"analytics"},
		DataLocations:     []string{"us", "eu"},
		TransferMechanism: "dpf",
		DPAStatus:         "in_place",
		DPAURL:            "https://example.com/dpa",
	}

	if processor.Name != "Test Processor" {
		t.Error("ProcessorProfileData Name field not working correctly")
	}
	if processor.TransferMechanism != "dpf" {
		t.Error("ProcessorProfileData TransferMechanism field not working correctly")
	}
}

func TestRegulatoryDocument_AllFieldsUsable(t *testing.T) {
	doc := RegulatoryDocument{
		Title:     "Test Document",
		SourceURL: "https://example.com/doc",
		Text:      "Document content here",
		Tier:      "primary",
	}

	if doc.Title != "Test Document" {
		t.Error("RegulatoryDocument Title field not working correctly")
	}
	if doc.Tier != "primary" {
		t.Error("RegulatoryDocument Tier field not working correctly")
	}
}

func TestDpaGapPrompt_IncludesTIARequirement(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("dpa_gap", client, processors, docs)

	// Should mention Transfer Impact Assessment
	if !strings.Contains(result.System, "Transfer Impact Assessment") && !strings.Contains(result.System, "TIA") {
		t.Error("DPA gap system prompt should mention Transfer Impact Assessment")
	}

	// Should mention Schrems II
	if !strings.Contains(result.System, "Schrems") {
		t.Error("DPA gap system prompt should reference Schrems II")
	}
}

func TestAiActPrompt_IncludesGPAI(t *testing.T) {
	client := sampleClientContext()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ai_act_classification", client, nil, docs)

	// Should mention General-Purpose AI
	if !strings.Contains(result.System, "General-Purpose AI") && !strings.Contains(result.System, "GPAI") {
		t.Error("AI Act system prompt should mention GPAI")
	}
}

func TestAiActPrompt_IncludesGDPRInteraction(t *testing.T) {
	client := sampleClientContext()
	docs := sampleRegulatoryDocs()

	result := BuildArtifactPrompt("ai_act_classification", client, nil, docs)

	// Should mention GDPR interaction
	if !strings.Contains(result.System, "GDPR") {
		t.Error("AI Act system prompt should mention GDPR interaction")
	}

	// Should mention that both regulations apply
	if !strings.Contains(result.System, "both regulations") {
		t.Error("AI Act system prompt should clarify both regulations may apply")
	}
}

func TestPrompts_ConservativeApproach(t *testing.T) {
	client := sampleClientContext()
	processors := sampleProcessors()
	docs := sampleRegulatoryDocs()

	artifactTypes := []string{"ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification"}

	for _, artifactType := range artifactTypes {
		t.Run(artifactType, func(t *testing.T) {
			result := BuildArtifactPrompt(artifactType, client, processors, docs)

			// Should include conservative approach guidance
			conservativeTerms := []string{"conservative", "uncertain", "review", "flag"}

			foundCount := 0
			lowerSystem := strings.ToLower(result.System)
			for _, term := range conservativeTerms {
				if strings.Contains(lowerSystem, term) {
					foundCount++
				}
			}

			if foundCount < 2 {
				t.Errorf("%s system prompt should include conservative approach guidance", artifactType)
			}
		})
	}
}
