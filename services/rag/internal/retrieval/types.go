package retrieval

import "time"

// SearchParams contains parameters for hybrid search
type SearchParams struct {
	Query       string            // The search query
	TopK        int               // Number of results to retrieve
	RerankTopK  int               // Number of results to rerank
	Filters     map[string]string // Filters: source, tier, topic
	Collections []string          // Collections to search (e.g., gdpr_chunks, ai_act_chunks)
}

// SearchResult represents a single search result from Qdrant
type SearchResult struct {
	ChunkID  string                 // Unique chunk identifier
	ParentID string                 // Parent chunk identifier
	Content  string                 // Chunk content
	Score    float64                // Relevance score
	Metadata map[string]interface{} // Additional metadata
}

// ParentChunk represents a parent chunk fetched from PostgreSQL
type ParentChunk struct {
	ID         string    // Parent chunk ID
	Content    string    // Full parent content
	SourceURL  string    // URL of the source document
	SourceName string    // Name of the source
	Tier       string    // Tier (primary/secondary/tertiary)
	CreatedAt  time.Time // Creation timestamp
}

// Citation represents a citation object for rendering
type Citation struct {
	ID         string // Citation ID (same as parent chunk ID)
	SourceName string // Name of the source document
	SourceURL  string // URL to the source
	Excerpt    string // Relevant excerpt from the source
	Tier       string // Tier of the source
}

// CachedResponse represents a cached RAG response in Redis
type CachedResponse struct {
	Query      string     // Original query
	Response   string     // Generated response
	Citations  []Citation // Associated citations
	Timestamp  time.Time  // Cache timestamp
	ParamsHash string     // Hash of search parameters
}
