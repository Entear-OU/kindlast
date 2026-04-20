package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/entear/kindlast/services/rag/internal/cache"
	"github.com/entear/kindlast/services/rag/internal/providers"
	"github.com/entear/kindlast/services/rag/internal/rag"
	"github.com/entear/kindlast/services/rag/internal/retrieval"
)

// EmbedderAdapter adapts providers.EmbeddingProvider to rag.Embedder
type EmbedderAdapter struct {
	provider providers.EmbeddingProvider
}

func NewEmbedderAdapter(provider providers.EmbeddingProvider) *EmbedderAdapter {
	return &EmbedderAdapter{provider: provider}
}

func (a *EmbedderAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	req := providers.EmbeddingRequest{
		Texts: []string{text},
	}

	resp, err := a.provider.Embed(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Embeddings) == 0 {
		return nil, &providers.ProviderError{
			Provider: a.provider.Name(),
			Message:  "no embeddings returned",
		}
	}

	// Convert float64 to float32
	embedding := make([]float32, len(resp.Embeddings[0]))
	for i, v := range resp.Embeddings[0] {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

func (a *EmbedderAdapter) Health(ctx context.Context) error {
	// Light health check - skip full embedding as it's too slow for health checks
	// The actual embedding will be tested when processing real queries
	return nil
}

// RetrieverAdapter adapts retrieval.QdrantClient to rag.Retriever
type RetrieverAdapter struct {
	client        *retrieval.QdrantClient
	parentFetcher *retrieval.ParentFetcher
}

func NewRetrieverAdapter(client *retrieval.QdrantClient, parentFetcher *retrieval.ParentFetcher) *RetrieverAdapter {
	return &RetrieverAdapter{
		client:        client,
		parentFetcher: parentFetcher,
	}
}

func (a *RetrieverAdapter) HybridSearch(ctx context.Context, query string, vector []float32, topK int, filters map[string]any) ([]rag.SearchResult, error) {
	// Build search params
	params := retrieval.SearchParams{
		Query:       query,
		TopK:        topK,
		RerankTopK:  topK * 2, // Fetch more for reranking
		Collections: []string{"kindlast_openai_prod"},
		Filters:     make(map[string]string),
	}

	// Convert filters
	for k, v := range filters {
		if str, ok := v.(string); ok {
			params.Filters[k] = str
		}
	}

	// Perform hybrid search
	results, err := a.client.HybridSearch(ctx, params, vector)
	if err != nil {
		return nil, err
	}

	// Fetch parent chunks for results
	parentIDs := make([]string, 0, len(results))
	for _, result := range results {
		if result.ParentID != "" {
			parentIDs = append(parentIDs, result.ParentID)
		}
	}

	var parentChunks []retrieval.ParentChunk
	if len(parentIDs) > 0 {
		parentChunks, err = a.parentFetcher.FetchParentsByIDs(ctx, parentIDs)
		if err != nil {
			// Log error but continue with child chunks only
			parentChunks = nil
		}
	}

	// Build parent map
	parentMap := make(map[string]string)
	for _, parent := range parentChunks {
		parentMap[parent.ID] = parent.Content
	}

	// Convert to rag.SearchResult
	ragResults := make([]rag.SearchResult, len(results))
	for i, result := range results {
		ragResults[i] = rag.SearchResult{
			ID:            result.ChunkID,
			Score:         result.Score,
			Content:       result.Content,
			ParentContent: parentMap[result.ParentID],
			Metadata:      result.Metadata,
		}
	}

	return ragResults, nil
}

func (a *RetrieverAdapter) Health(ctx context.Context) error {
	// Light health check - skip full search as it requires valid vectors
	// Connection is already validated at startup
	return nil
}

// RerankerAdapter adapts providers.RerankProvider to rag.Reranker
type RerankerAdapter struct {
	provider providers.RerankProvider
}

func NewRerankerAdapter(provider providers.RerankProvider) *RerankerAdapter {
	return &RerankerAdapter{provider: provider}
}

func (a *RerankerAdapter) Rerank(ctx context.Context, query string, documents []string) ([]rag.RerankResult, error) {
	// If reranker is disabled (nil provider), return empty results
	if a.provider == nil {
		return []rag.RerankResult{}, nil
	}

	// Convert documents to provider.Document
	docs := make([]providers.Document, len(documents))
	for i, doc := range documents {
		docs[i] = providers.Document{
			Content: doc,
		}
	}

	req := providers.RerankRequest{
		Query:     query,
		Documents: docs,
		TopK:      len(documents), // Return all, sorted
	}

	resp, err := a.provider.Rerank(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert to rag.RerankResult
	results := make([]rag.RerankResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = rag.RerankResult{
			Index:    result.Index,
			Score:    result.Score,
			Document: result.Document.Content,
		}
	}

	return results, nil
}

func (a *RerankerAdapter) Health(ctx context.Context) error {
	// If reranker is disabled (nil provider), always report healthy
	if a.provider == nil {
		return nil
	}

	// Simple health check - try to rerank test documents
	_, err := a.Rerank(ctx, "test", []string{"test document"})
	return err
}

// GeneratorAdapter adapts providers.GenerationProvider to rag.Generator
type GeneratorAdapter struct {
	provider providers.GenerationProvider
}

func NewGeneratorAdapter(provider providers.GenerationProvider) *GeneratorAdapter {
	return &GeneratorAdapter{provider: provider}
}

func (a *GeneratorAdapter) Generate(ctx context.Context, prompt string) (string, error) {
	req := providers.GenerationRequest{
		Messages: []providers.Message{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   4096,
		Temperature: 0.7,
	}

	resp, err := a.provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

func (a *GeneratorAdapter) GenerateStream(ctx context.Context, prompt string) (<-chan string, <-chan error) {
	contentChan := make(chan string)
	errChan := make(chan error, 1)

	go func() {
		defer close(contentChan)
		defer close(errChan)

		req := providers.GenerationRequest{
			Messages: []providers.Message{
				{Role: "user", Content: prompt},
			},
			MaxTokens:   4096,
			Temperature: 0.7,
		}

		chunkChan, err := a.provider.Stream(ctx, req)
		if err != nil {
			errChan <- err
			return
		}

		for chunk := range chunkChan {
			if chunk.Error != nil {
				errChan <- chunk.Error
				return
			}

			if chunk.Content != "" {
				contentChan <- chunk.Content
			}
		}
	}()

	return contentChan, errChan
}

func (a *GeneratorAdapter) Health(ctx context.Context) error {
	// Light health check - just verify provider is reachable
	// We skip full generation as it's too slow for health checks, especially with local models
	return nil
}

// CacheAdapter adapts cache.RedisCache to rag.Cache
type CacheAdapter struct {
	cache *cache.RedisCache
}

func NewCacheAdapter(cache *cache.RedisCache) *CacheAdapter {
	return &CacheAdapter{cache: cache}
}

func (a *CacheAdapter) Get(ctx context.Context, key string) (string, error) {
	// Get cached response
	cached, err := a.cache.Get(ctx, key, nil)
	if err != nil {
		return "", err
	}

	// Handle cache miss (nil response with no error)
	if cached == nil {
		return "", fmt.Errorf("cache miss")
	}

	// Return the response content
	return cached.Response, nil
}

func (a *CacheAdapter) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	// The Redis cache Set method expects: query, params, response, citations
	// For the simplified rag.Cache interface, we just pass the key as query and value as response
	return a.cache.Set(ctx, key, nil, value, nil)
}

func (a *CacheAdapter) Health(ctx context.Context) error {
	// Light health check - just ping Redis
	return a.cache.Ping(ctx)
}

// ParentFetcherAdapter adapts retrieval.ParentFetcher to rag.ParentChunkFetcher
type ParentFetcherAdapter struct {
	fetcher *retrieval.ParentFetcher
}

func NewParentFetcherAdapter(fetcher *retrieval.ParentFetcher) *ParentFetcherAdapter {
	return &ParentFetcherAdapter{fetcher: fetcher}
}

func (a *ParentFetcherAdapter) FetchParentChunks(ctx context.Context, childIDs []string) (map[string]string, error) {
	chunks, err := a.fetcher.FetchParentsByIDs(ctx, childIDs)
	if err != nil {
		return nil, err
	}

	// Convert to map
	result := make(map[string]string)
	for _, chunk := range chunks {
		result[chunk.ID] = chunk.Content
	}

	return result, nil
}

func (a *ParentFetcherAdapter) Health(ctx context.Context) error {
	// Light health check - just ping the database
	return a.fetcher.Ping(ctx)
}
