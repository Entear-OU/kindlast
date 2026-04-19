package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/entear/kindlast/services/rag/internal/config"
	"github.com/entear/kindlast/services/rag/internal/prompts"
)

// LowConfidenceThreshold is the minimum relevance score to return results without warning
const LowConfidenceThreshold = 0.72

// QueryRequest represents a RAG query request
type QueryRequest struct {
	Query  string         `json:"query"`
	Topic  prompts.Topic  `json:"topic"`
	TopK   int            `json:"topK"`
	Stream bool           `json:"stream"`
}

// QueryResponse represents a RAG query response
type QueryResponse struct {
	Answer        string              `json:"answer"`
	Citations     []prompts.Citation  `json:"citations"`
	CacheHit      bool                `json:"cacheHit"`
	ConfidenceOK  bool                `json:"confidenceOk"`
	MaxRelevance  float64             `json:"maxRelevance"`
	ProcessingTime time.Duration      `json:"processingTime"`
}

// StreamChunk represents a streaming response chunk
type StreamChunk struct {
	Type      string             `json:"type"`      // content, citation, error, done
	Text      string             `json:"text,omitempty"`
	Citation  *prompts.Citation  `json:"citation,omitempty"`
	Error     string             `json:"error,omitempty"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
}

// Embedder interface for embedding providers
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Health(ctx context.Context) error
}

// Retriever interface for vector search
type Retriever interface {
	HybridSearch(ctx context.Context, query string, vector []float32, topK int, filters map[string]any) ([]SearchResult, error)
	Health(ctx context.Context) error
}

// SearchResult represents a search result from the retriever
type SearchResult struct {
	ID           string
	Score        float64
	Content      string
	ParentContent string
	Metadata     map[string]any
}

// Reranker interface for reranking providers
type Reranker interface {
	Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error)
	Health(ctx context.Context) error
}

// RerankResult represents a reranked result
type RerankResult struct {
	Index     int
	Score     float64
	Document  string
}

// Generator interface for text generation providers
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
	GenerateStream(ctx context.Context, prompt string) (<-chan string, <-chan error)
	Health(ctx context.Context) error
}

// Cache interface for caching results
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Health(ctx context.Context) error
}

// ParentChunkFetcher interface for fetching parent chunks
type ParentChunkFetcher interface {
	FetchParentChunks(ctx context.Context, childIDs []string) (map[string]string, error)
	Health(ctx context.Context) error
}

// Orchestrator orchestrates the full RAG pipeline
type Orchestrator struct {
	embedder     Embedder
	retriever    Retriever
	reranker     Reranker
	generator    Generator
	cache        Cache
	parentFetcher ParentChunkFetcher
	config       *config.Config
	logger       *slog.Logger
}

// NewOrchestrator creates a new RAG orchestrator
func NewOrchestrator(
	embedder Embedder,
	retriever Retriever,
	reranker Reranker,
	generator Generator,
	cache Cache,
	parentFetcher ParentChunkFetcher,
	cfg *config.Config,
	logger *slog.Logger,
) *Orchestrator {
	return &Orchestrator{
		embedder:      embedder,
		retriever:     retriever,
		reranker:      reranker,
		generator:     generator,
		cache:         cache,
		parentFetcher: parentFetcher,
		config:        cfg,
		logger:        logger,
	}
}

// Query executes a RAG query (non-streaming)
func (o *Orchestrator) Query(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	startTime := time.Now()

	// Validate request
	if err := o.validateRequest(&req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Check cache
	cacheKey := o.generateCacheKey(req)
	if cachedResult, err := o.cache.Get(ctx, cacheKey); err == nil {
		var resp QueryResponse
		if err := json.Unmarshal([]byte(cachedResult), &resp); err == nil {
			resp.CacheHit = true
			resp.ProcessingTime = time.Since(startTime)
			o.logger.Info("cache hit", "query", req.Query, "key", cacheKey)
			return &resp, nil
		}
	}

	// Execute RAG pipeline
	citations, maxRelevance, err := o.retrieveAndRerank(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("retrieve and rerank failed: %w", err)
	}

	// Check confidence
	confidenceOK := maxRelevance >= LowConfidenceThreshold

	// Build prompt
	prompt := prompts.BuildPrompt(req.Query, req.Topic, citations)

	// Add low confidence warning if needed
	var answer string
	if !confidenceOK {
		answer = prompts.LowConfidenceWarning(maxRelevance)
	}

	// Generate response
	generatedText, err := o.generator.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}
	answer += generatedText

	resp := &QueryResponse{
		Answer:         answer,
		Citations:      citations,
		CacheHit:       false,
		ConfidenceOK:   confidenceOK,
		MaxRelevance:   maxRelevance,
		ProcessingTime: time.Since(startTime),
	}

	// Cache result
	if respJSON, err := json.Marshal(resp); err == nil {
		_ = o.cache.Set(ctx, cacheKey, string(respJSON), o.config.Redis.TTL)
	}

	return resp, nil
}

// QueryStream executes a RAG query with streaming response
func (o *Orchestrator) QueryStream(ctx context.Context, req QueryRequest) (<-chan StreamChunk, error) {
	// Validate request
	if err := o.validateRequest(&req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	chunkChan := make(chan StreamChunk, 100)

	go func() {
		defer close(chunkChan)

		// Check cache (for streaming, we skip cache for now - could optimize later)
		// Execute RAG pipeline
		citations, maxRelevance, err := o.retrieveAndRerank(ctx, req)
		if err != nil {
			chunkChan <- StreamChunk{
				Type:  prompts.ChunkTypeError,
				Error: fmt.Sprintf("retrieve and rerank failed: %v", err),
			}
			return
		}

		// Send citations
		for _, cite := range citations {
			citeCopy := cite
			chunkChan <- StreamChunk{
				Type:     prompts.ChunkTypeCitation,
				Citation: &citeCopy,
			}
		}

		// Check confidence
		confidenceOK := maxRelevance >= LowConfidenceThreshold

		// Send metadata
		chunkChan <- StreamChunk{
			Type: "metadata",
			Metadata: map[string]any{
				"confidenceOk": confidenceOK,
				"maxRelevance": maxRelevance,
				"citationCount": len(citations),
			},
		}

		// Build prompt
		prompt := prompts.BuildPrompt(req.Query, req.Topic, citations)

		// Add low confidence warning if needed
		if !confidenceOK {
			chunkChan <- StreamChunk{
				Type: prompts.ChunkTypeContent,
				Text: prompts.LowConfidenceWarning(maxRelevance),
			}
		}

		// Generate streaming response
		textChan, errChan := o.generator.GenerateStream(ctx, prompt)

		for {
			select {
			case text, ok := <-textChan:
				if !ok {
					// Stream completed
					chunkChan <- StreamChunk{Type: prompts.ChunkTypeDone}
					return
				}
				chunkChan <- StreamChunk{
					Type: prompts.ChunkTypeContent,
					Text: text,
				}
			case err, ok := <-errChan:
				if ok && err != nil {
					chunkChan <- StreamChunk{
						Type:  prompts.ChunkTypeError,
						Error: err.Error(),
					}
					return
				}
			case <-ctx.Done():
				chunkChan <- StreamChunk{
					Type:  prompts.ChunkTypeError,
					Error: "context cancelled",
				}
				return
			}
		}
	}()

	return chunkChan, nil
}

// retrieveAndRerank executes the retrieval and reranking pipeline
func (o *Orchestrator) retrieveAndRerank(ctx context.Context, req QueryRequest) ([]prompts.Citation, float64, error) {
	// Embed query
	vector, err := o.embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, 0, fmt.Errorf("embedding failed: %w", err)
	}

	// Build filters for topic
	filters := o.buildTopicFilters(req.Topic)

	// Hybrid search
	topK := req.TopK
	if topK == 0 {
		topK = 20 // Default: retrieve more for reranking
	}

	results, err := o.retriever.HybridSearch(ctx, req.Query, vector, topK, filters)
	if err != nil {
		return nil, 0, fmt.Errorf("hybrid search failed: %w", err)
	}

	if len(results) == 0 {
		return []prompts.Citation{}, 0, nil
	}

	// Extract documents for reranking
	documents := make([]string, len(results))
	for i, result := range results {
		documents[i] = result.Content
	}

	// Rerank
	rerankedResults, err := o.reranker.Rerank(ctx, req.Query, documents)
	if err != nil {
		o.logger.Warn("reranking failed, using original results", "error", err)
		// Fallback to original results
		return o.buildCitations(results, topK), o.getMaxScore(results), nil
	}

	// Take top N after reranking
	topN := o.config.Providers.Reranking.TopN
	if topN > len(rerankedResults) {
		topN = len(rerankedResults)
	}
	rerankedResults = rerankedResults[:topN]

	// Fetch parent chunks
	childIDs := make([]string, len(rerankedResults))
	for i, rr := range rerankedResults {
		childIDs[i] = results[rr.Index].ID
	}

	parentChunks, err := o.parentFetcher.FetchParentChunks(ctx, childIDs)
	if err != nil {
		o.logger.Warn("failed to fetch parent chunks", "error", err)
		parentChunks = make(map[string]string)
	}

	// Build citations from reranked results
	citations := make([]prompts.Citation, len(rerankedResults))
	maxRelevance := 0.0

	for i, rr := range rerankedResults {
		originalResult := results[rr.Index]

		// Use parent chunk if available, otherwise use child chunk
		content := parentChunks[originalResult.ID]
		if content == "" {
			content = originalResult.Content
		}

		citation := prompts.Citation{
			Source:    o.extractSource(originalResult.Metadata),
			Title:     o.extractTitle(originalResult.Metadata),
			URL:       o.extractURL(originalResult.Metadata),
			Excerpt:   content,
			Relevance: rr.Score,
		}
		citations[i] = citation

		if rr.Score > maxRelevance {
			maxRelevance = rr.Score
		}
	}

	return citations, maxRelevance, nil
}

// validateRequest validates the query request
func (o *Orchestrator) validateRequest(req *QueryRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query cannot be empty")
	}
	if req.Topic != prompts.TopicGDPR && req.Topic != prompts.TopicAIAct && req.Topic != prompts.TopicBoth {
		// Default to both if invalid
		req.Topic = prompts.TopicBoth
	}
	return nil
}

// generateCacheKey generates a cache key for the request
func (o *Orchestrator) generateCacheKey(req QueryRequest) string {
	data := fmt.Sprintf("%s|%s|%d", req.Query, req.Topic, req.TopK)
	hash := sha256.Sum256([]byte(data))
	return "rag:query:" + hex.EncodeToString(hash[:])
}

// buildTopicFilters builds Qdrant filters based on topic
func (o *Orchestrator) buildTopicFilters(topic prompts.Topic) map[string]any {
	filters := make(map[string]any)

	switch topic {
	case prompts.TopicGDPR:
		filters["topic"] = "gdpr"
	case prompts.TopicAIAct:
		filters["topic"] = "ai_act"
	case prompts.TopicBoth:
		// No filter - search both
	}

	return filters
}

// buildCitations builds citations from search results (fallback when reranking fails)
func (o *Orchestrator) buildCitations(results []SearchResult, topN int) []prompts.Citation {
	if topN > len(results) {
		topN = len(results)
	}

	citations := make([]prompts.Citation, topN)
	for i := 0; i < topN; i++ {
		result := results[i]
		citations[i] = prompts.Citation{
			Source:    o.extractSource(result.Metadata),
			Title:     o.extractTitle(result.Metadata),
			URL:       o.extractURL(result.Metadata),
			Excerpt:   result.Content,
			Relevance: result.Score,
		}
	}

	return citations
}

// getMaxScore returns the maximum score from search results
func (o *Orchestrator) getMaxScore(results []SearchResult) float64 {
	if len(results) == 0 {
		return 0
	}
	maxScore := results[0].Score
	for _, result := range results[1:] {
		if result.Score > maxScore {
			maxScore = result.Score
		}
	}
	return maxScore
}

// Metadata extraction helpers

func (o *Orchestrator) extractSource(metadata map[string]any) string {
	if source, ok := metadata["source"].(string); ok {
		return source
	}
	return "Unknown Source"
}

func (o *Orchestrator) extractTitle(metadata map[string]any) string {
	if title, ok := metadata["title"].(string); ok {
		return title
	}
	return "Untitled Document"
}

func (o *Orchestrator) extractURL(metadata map[string]any) string {
	if url, ok := metadata["url"].(string); ok {
		return url
	}
	return ""
}

// Health checks all dependencies
func (o *Orchestrator) Health(ctx context.Context) map[string]error {
	healthChecks := make(map[string]error)

	healthChecks["embedder"] = o.embedder.Health(ctx)
	healthChecks["retriever"] = o.retriever.Health(ctx)
	healthChecks["reranker"] = o.reranker.Health(ctx)
	healthChecks["generator"] = o.generator.Health(ctx)
	healthChecks["cache"] = o.cache.Health(ctx)
	healthChecks["parent_fetcher"] = o.parentFetcher.Health(ctx)

	return healthChecks
}
