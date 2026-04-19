// Package artifact provides the artifact generation pipeline for DPO Copilot.
// It generates compliance deliverables (RoPAs, DPIA screenings, DPA gap analyses,
// AI Act classifications) by combining regulatory corpus retrieval with structured
// LLM generation.
package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/entear/kindlast/services/rag/internal/cache"
	"github.com/entear/kindlast/services/rag/internal/providers"
	"github.com/entear/kindlast/services/rag/internal/retrieval"
	"github.com/entear/kindlast/services/rag/internal/router"
)

// Re-export prompt types from prompts.go for use in service
// ClientContext, ProcessorProfileData, PromptPair are defined in prompts.go

// Service handles artifact generation for DPO Copilot.
// It orchestrates the retrieval, reranking, and generation pipeline
// to produce structured compliance artifacts.
type Service struct {
	genRouter     *router.GenerationRouter
	embedRouter   *router.EmbeddingRouter
	rerankRouter  *router.RerankRouter
	retriever     *retrieval.QdrantClient
	parentFetcher *retrieval.ParentFetcher
	processorRepo ProcessorRepository
	cache         *cache.RedisCache
	corpusVersion string
}

// ServiceConfig contains configuration for the artifact service.
type ServiceConfig struct {
	CorpusVersion string
}

// NewService creates a new artifact generation service.
func NewService(
	genRouter *router.GenerationRouter,
	embedRouter *router.EmbeddingRouter,
	rerankRouter *router.RerankRouter,
	retriever *retrieval.QdrantClient,
	parentFetcher *retrieval.ParentFetcher,
	processorRepo ProcessorRepository,
	cache *cache.RedisCache,
	cfg ServiceConfig,
) *Service {
	corpusVersion := cfg.CorpusVersion
	if corpusVersion == "" {
		corpusVersion = "v1.0.0"
	}
	return &Service{
		genRouter:     genRouter,
		embedRouter:   embedRouter,
		rerankRouter:  rerankRouter,
		retriever:     retriever,
		parentFetcher: parentFetcher,
		processorRepo: processorRepo,
		cache:         cache,
		corpusVersion: corpusVersion,
	}
}

// GenerateRequest contains the parameters for artifact generation.
type GenerateRequest struct {
	ArtifactType  string        // "ropa" | "dpia_screening" | "dpa_gap" | "lawful_basis" | "ai_act_classification"
	ClientContext ClientContext // Client business context (defined in prompts.go)
	UserPlan      string        // "free" | "professional" | "team"
	ExistingRoPA  *RoPA         // Optional: existing RoPA for incremental updates
}

// GenerateResult contains the generated artifact and metadata.
type GenerateResult struct {
	Content       json.RawMessage   // Artifact JSON matching the type schema
	Citations     []Citation        // Regulatory source citations
	Provider      string            // Generation provider used
	Model         string            // Model used
	TokensUsed    int               // Tokens consumed
	LatencyMs     int64             // Generation latency in milliseconds
	CorpusVersion string            // Version of the regulatory corpus
}

// Generate produces a compliance artifact based on the request.
// The pipeline:
// 1. Resolves processor profiles from tech stack
// 2. Builds artifact-specific retrieval query
// 3. Embeds and searches regulatory corpus
// 4. Applies topic filter based on artifact type
// 5. Reranks results
// 6. Fetches parent chunks for context
// 7. Builds generation prompt
// 8. Generates artifact JSON via LLM
// 9. Parses and validates output
func (s *Service) Generate(ctx context.Context, req GenerateRequest) (*GenerateResult, error) {
	startTime := time.Now()

	// Validate artifact type
	if !isValidArtifactType(req.ArtifactType) {
		return nil, fmt.Errorf("invalid artifact type: %s", req.ArtifactType)
	}

	// 1. Resolve processor profiles from tech stack
	processors, err := s.resolveProcessors(ctx, req.ClientContext.TechStack)
	if err != nil {
		return nil, fmt.Errorf("processor resolution: %w", err)
	}

	// 2. Build artifact-specific query for regulatory corpus retrieval
	retrievalQuery := s.buildRetrievalQuery(req.ArtifactType, req.ClientContext)

	// 3. Embed the retrieval query
	embedReq := providers.EmbeddingRequest{
		Texts: []string{retrievalQuery},
	}
	embedResp, err := s.embedRouter.Embed(ctx, embedReq)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	if len(embedResp.Embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	// Convert float64 to float32 for Qdrant
	queryVector := make([]float32, len(embedResp.Embeddings[0]))
	for i, v := range embedResp.Embeddings[0] {
		queryVector[i] = float32(v)
	}

	// 4. Topic filter based on artifact type
	topicFilter := s.topicFilterForType(req.ArtifactType)

	// Build search params
	searchParams := retrieval.SearchParams{
		Query:       retrievalQuery,
		TopK:        30, // More docs for artifact generation
		RerankTopK:  15,
		Filters:     make(map[string]string),
		Collections: s.collectionsForType(req.ArtifactType),
	}

	// Apply topic filter
	if len(topicFilter) == 1 {
		searchParams.Filters["topic"] = topicFilter[0]
	}

	// 5. Search regulatory corpus
	searchResults, err := s.retriever.HybridSearch(ctx, searchParams, queryVector)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	if len(searchResults) == 0 {
		return nil, fmt.Errorf("no relevant regulatory documents found")
	}

	// 6. Rerank results
	rerankDocs := make([]providers.Document, len(searchResults))
	for i, result := range searchResults {
		rerankDocs[i] = providers.Document{
			ID:      result.ChunkID,
			Content: result.Content,
		}
	}

	rerankReq := providers.RerankRequest{
		Query:     retrievalQuery,
		Documents: rerankDocs,
		TopK:      15,
	}
	rerankResp, err := s.rerankRouter.Rerank(ctx, rerankReq)
	if err != nil {
		return nil, fmt.Errorf("reranking failed: %w", err)
	}

	// Get top reranked results
	topResults := make([]retrieval.SearchResult, 0, len(rerankResp.Results))
	for _, ranked := range rerankResp.Results {
		if ranked.Index < len(searchResults) {
			topResults = append(topResults, searchResults[ranked.Index])
		}
	}

	// Limit to top 10 for parent fetch
	maxParentFetch := 10
	if len(topResults) > maxParentFetch {
		topResults = topResults[:maxParentFetch]
	}

	// 7. Fetch parent chunks for context
	parentIDs := make([]string, 0, len(topResults))
	for _, result := range topResults {
		if result.ParentID != "" {
			parentIDs = append(parentIDs, result.ParentID)
		}
	}

	parentChunks, err := s.parentFetcher.FetchParentsByIDs(ctx, parentIDs)
	if err != nil {
		return nil, fmt.Errorf("parent fetch failed: %w", err)
	}

	// 8. Build generation prompt
	// Convert parent chunks to regulatory documents for prompt building
	regDocs := make([]RegulatoryDocument, len(parentChunks))
	for i, p := range parentChunks {
		regDocs[i] = RegulatoryDocument{
			Title:     p.SourceName,
			SourceURL: p.SourceURL,
			Text:      p.Content,
			Tier:      p.Tier,
		}
	}
	promptPair := BuildArtifactPrompt(req.ArtifactType, req.ClientContext, processors, regDocs)

	// 9. Generate artifact via LLM
	genReq := providers.GenerationRequest{
		SystemPrompt: promptPair.System,
		Messages:     promptPair.Messages,
		MaxTokens:    s.maxTokensForType(req.ArtifactType),
		Temperature:  0.3, // Lower temperature for structured output
	}

	genResp, err := s.genRouter.Generate(ctx, genReq)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}

	// 10. Parse and validate artifact JSON
	content, err := s.parseAndValidate(req.ArtifactType, genResp.Content)
	if err != nil {
		return nil, fmt.Errorf("artifact validation failed: %w", err)
	}

	// Build citations from parent chunks
	citations := make([]Citation, len(parentChunks))
	for i, parent := range parentChunks {
		citations[i] = Citation{
			Index:     i + 1,
			SourceURL: parent.SourceURL,
			Title:     parent.SourceName,
			Section:   parent.Tier,
			ChunkText: truncateText(parent.Content, 500),
		}
	}

	return &GenerateResult{
		Content:       content,
		Citations:     citations,
		Provider:      s.genRouter.Name(),
		Model:         "claude-sonnet", // Will be extracted from response in production
		TokensUsed:    genResp.Usage.InputTokens + genResp.Usage.OutputTokens,
		LatencyMs:     time.Since(startTime).Milliseconds(),
		CorpusVersion: s.corpusVersion,
	}, nil
}

// resolveProcessors looks up processor profiles for the given tech stack names.
func (s *Service) resolveProcessors(ctx context.Context, techStack []string) ([]ProcessorProfileData, error) {
	if len(techStack) == 0 {
		return []ProcessorProfileData{}, nil
	}

	processors := make([]ProcessorProfileData, 0, len(techStack))

	for _, name := range techStack {
		// Try exact match first
		profile, err := s.processorRepo.GetBySlug(ctx, normalizeSlug(name))
		if err == nil && profile != nil {
			processors = append(processors, *profile)
			continue
		}

		// Try fuzzy match by name
		profile, err = s.processorRepo.GetByName(ctx, name)
		if err == nil && profile != nil {
			processors = append(processors, *profile)
			continue
		}

		// Add as unknown processor
		processors = append(processors, ProcessorProfileData{
			Name:              name,
			Slug:              normalizeSlug(name),
			Category:          "unknown",
			Headquarters:      "unknown",
			DataCategories:    []string{},
			ProcessingPurposes: []string{},
			DataLocations:     []string{},
			TransferMechanism: "unknown",
			DPAStatus:         "unknown",
		})
	}

	return processors, nil
}

// buildRetrievalQuery constructs an artifact-specific query for retrieval.
func (s *Service) buildRetrievalQuery(artifactType string, client ClientContext) string {
	var sb strings.Builder

	switch artifactType {
	case "ropa":
		sb.WriteString("GDPR Article 30 Record of Processing Activities requirements. ")
		sb.WriteString("Lawful basis assessment. Data retention periods. ")
		sb.WriteString("DPIA requirements. International data transfers. ")
		if client.Sector != "" {
			sb.WriteString(fmt.Sprintf("%s sector GDPR compliance. ", client.Sector))
		}
		if len(client.ProcessingPurposes) > 0 {
			sb.WriteString(fmt.Sprintf("Processing purposes: %s. ", strings.Join(client.ProcessingPurposes, ", ")))
		}

	case "dpia_screening":
		sb.WriteString("GDPR Article 35 DPIA requirements. ")
		sb.WriteString("EDPB DPIA guidelines wp248rev.01. ")
		sb.WriteString("Nine EDPB criteria for DPIA: evaluation scoring, automated decision-making, ")
		sb.WriteString("systematic monitoring, sensitive data, large scale, matching combining datasets, ")
		sb.WriteString("vulnerable data subjects, innovative technology, preventing rights exercise. ")
		if client.Sector != "" {
			sb.WriteString(fmt.Sprintf("%s sector high risk processing. ", client.Sector))
		}

	case "dpa_gap":
		sb.WriteString("GDPR Article 28 processor requirements. ")
		sb.WriteString("Data Processing Agreement obligations. ")
		sb.WriteString("Standard Contractual Clauses. EU-US Data Privacy Framework. ")
		sb.WriteString("Transfer Impact Assessment. Third country transfers. ")
		if len(client.TechStack) > 0 {
			sb.WriteString(fmt.Sprintf("Processors: %s. ", strings.Join(client.TechStack, ", ")))
		}

	case "lawful_basis":
		sb.WriteString("GDPR Article 6 lawful basis for processing. ")
		sb.WriteString("Consent requirements Article 7. ")
		sb.WriteString("Legitimate interests assessment balancing test. ")
		sb.WriteString("Contract necessity. Legal obligation. ")
		if len(client.ProcessingPurposes) > 0 {
			sb.WriteString(fmt.Sprintf("Processing purposes: %s. ", strings.Join(client.ProcessingPurposes, ", ")))
		}

	case "ai_act_classification":
		sb.WriteString("EU AI Act risk classification. ")
		sb.WriteString("Prohibited AI practices Article 5. ")
		sb.WriteString("High-risk AI systems Annex III. ")
		sb.WriteString("Limited risk transparency obligations. ")
		sb.WriteString("General-purpose AI models GPAI. ")
		if client.Description != "" {
			sb.WriteString(fmt.Sprintf("AI use case: %s. ", client.Description))
		}

	default:
		// Generic regulatory query
		sb.WriteString("GDPR compliance requirements. ")
		sb.WriteString(client.Description)
	}

	return sb.String()
}

// topicFilterForType returns the topic filter for the given artifact type.
func (s *Service) topicFilterForType(artifactType string) []string {
	switch artifactType {
	case "ai_act_classification":
		return []string{"ai_act"}
	case "ropa", "dpia_screening", "dpa_gap", "lawful_basis":
		return []string{"gdpr"}
	default:
		return nil // search both
	}
}

// collectionsForType returns the Qdrant collections to search for the artifact type.
func (s *Service) collectionsForType(artifactType string) []string {
	switch artifactType {
	case "ai_act_classification":
		return []string{"kindlast_openai_prod"}
	default:
		return []string{"kindlast_openai_prod"}
	}
}

// maxTokensForType returns the maximum tokens for the given artifact type.
func (s *Service) maxTokensForType(artifactType string) int {
	switch artifactType {
	case "ropa":
		return 8000 // RoPAs can be lengthy
	case "dpia_screening":
		return 6000
	case "dpa_gap":
		return 4000
	case "ai_act_classification":
		return 4000
	case "lawful_basis":
		return 4000
	default:
		return 4000
	}
}

// parseAndValidate parses and validates the generated artifact JSON.
func (s *Service) parseAndValidate(artifactType, response string) (json.RawMessage, error) {
	// Extract JSON from response (may be wrapped in markdown code blocks)
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no valid JSON found in response")
	}

	// Validate that it's valid JSON
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Type-specific validation
	switch artifactType {
	case "ropa":
		var ropa RoPA
		if err := json.Unmarshal(raw, &ropa); err != nil {
			return nil, fmt.Errorf("invalid RoPA structure: %w", err)
		}
		if ropa.OrganizationName == "" {
			return nil, fmt.Errorf("RoPA missing required field: organization_name")
		}
		if len(ropa.Activities) == 0 {
			return nil, fmt.Errorf("RoPA must contain at least one processing activity")
		}

	case "dpia_screening":
		var dpia DPIAScreening
		if err := json.Unmarshal(raw, &dpia); err != nil {
			return nil, fmt.Errorf("invalid DPIAScreening structure: %w", err)
		}
		if dpia.ClientName == "" {
			return nil, fmt.Errorf("DPIAScreening missing required field: client_name")
		}
		if dpia.ScreeningResult == "" {
			return nil, fmt.Errorf("DPIAScreening missing required field: screening_result")
		}

	case "dpa_gap":
		var gap DPAGapAnalysis
		if err := json.Unmarshal(raw, &gap); err != nil {
			return nil, fmt.Errorf("invalid DPAGapAnalysis structure: %w", err)
		}
		if gap.ClientName == "" {
			return nil, fmt.Errorf("DPAGapAnalysis missing required field: client_name")
		}

	case "ai_act_classification":
		var aiAct AIActClassification
		if err := json.Unmarshal(raw, &aiAct); err != nil {
			return nil, fmt.Errorf("invalid AIActClassification structure: %w", err)
		}
		if aiAct.ClientName == "" {
			return nil, fmt.Errorf("AIActClassification missing required field: client_name")
		}

	case "lawful_basis":
		// Lawful basis uses the same RoPA structure but focused on basis analysis
		var ropa RoPA
		if err := json.Unmarshal(raw, &ropa); err != nil {
			return nil, fmt.Errorf("invalid lawful basis structure: %w", err)
		}
	}

	return raw, nil
}

// Helper functions

// isValidArtifactType checks if the artifact type is valid.
func isValidArtifactType(t string) bool {
	switch t {
	case "ropa", "dpia_screening", "dpa_gap", "lawful_basis", "ai_act_classification":
		return true
	default:
		return false
	}
}

// normalizeSlug converts a name to a slug format.
func normalizeSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

// extractJSON extracts JSON from a string that may contain markdown code blocks.
func extractJSON(s string) string {
	// Try to find JSON in code blocks first
	if start := strings.Index(s, "```json"); start != -1 {
		start += 7
		if end := strings.Index(s[start:], "```"); end != -1 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Try generic code block
	if start := strings.Index(s, "```"); start != -1 {
		start += 3
		// Skip language identifier if present
		if newline := strings.Index(s[start:], "\n"); newline != -1 {
			start += newline + 1
		}
		if end := strings.Index(s[start:], "```"); end != -1 {
			return strings.TrimSpace(s[start : start+end])
		}
	}

	// Try to find raw JSON (starts with { or [)
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		return s
	}

	return ""
}

// truncateText truncates text to the specified length.
func truncateText(text string, maxLen int) string {
	if maxLen <= 3 {
		return "..."
	}
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}
