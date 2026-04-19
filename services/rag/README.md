# RAG Service

The RAG (Retrieval-Augmented Generation) service provides the retrieval layer for Kindlast's AI compliance operating system.

## Architecture

The service implements a hybrid RAG pipeline with the following components:

### Core Components

1. **Qdrant Client** (`internal/retrieval/qdrant.go`)
   - Hybrid search combining BM25 sparse and dense vector search
   - Reciprocal Rank Fusion (RRF) for result combination
   - Configurable filters (source, tier, topic)
   - Multi-collection support (GDPR, AI Act)

2. **Parent Chunk Fetcher** (`internal/retrieval/parents.go`)
   - PostgreSQL client for fetching parent chunks
   - Connection pooling for performance
   - Batch operations for efficiency
   - Statistics and monitoring

3. **Redis Cache** (`internal/cache/redis.go`)
   - Response caching with configurable TTL (default 24h)
   - Support for both single instance and cluster mode
   - Hash-based cache keys for consistency
   - Cache invalidation patterns

4. **Citation Builder** (`internal/retrieval/citations.go`)
   - Creates citation objects from parent chunks
   - Context-aware excerpt generation
   - Deduplication and sorting
   - Multiple output formats (inline, markdown)

### Data Models

See `internal/retrieval/types.go` for core data structures:
- `SearchParams` - Search query parameters
- `SearchResult` - Individual search results
- `ParentChunk` - Parent chunk data
- `Citation` - Citation objects
- `CachedResponse` - Cached response data

## Configuration

Copy `.env.example` to `.env` and configure:

```bash
cp .env.example .env
```

### Required Configuration

- `POSTGRES_DSN` - PostgreSQL connection string

### Optional Configuration

- `QDRANT_HOST` - Qdrant host (default: localhost)
- `QDRANT_PORT` - Qdrant port (default: 6334)
- `REDIS_ADDR` - Redis address (default: localhost:6379)
- `CACHE_TTL` - Cache TTL (default: 24h)
- `DEFAULT_TOP_K` - Default number of results (default: 10)
- `DEFAULT_RERANK_K` - Number of results to rerank (default: 20)

## Development

### Prerequisites

- Go 1.23+
- Qdrant (for vector search)
- PostgreSQL 16+ (for parent chunks)
- Redis 7+ (for caching)

### Running Tests

Run all tests:
```bash
go test ./...
```

Run tests with coverage:
```bash
go test -cover ./...
```

Run tests for a specific package:
```bash
go test ./internal/retrieval
go test ./internal/cache
```

### Test Structure

Tests use:
- `sqlmock` for PostgreSQL testing
- `miniredis` for Redis testing
- Standard Go testing patterns

Integration tests (marked with `t.Skip()`) require running services.

## Usage Example

```go
package main

import (
    "context"
    "github.com/entear/kindlast/services/rag/internal/cache"
    "github.com/entear/kindlast/services/rag/internal/config"
    "github.com/entear/kindlast/services/rag/internal/retrieval"
)

func main() {
    // Load configuration
    cfg, err := config.LoadConfig()
    if err != nil {
        panic(err)
    }

    // Initialize clients
    qdrant, err := retrieval.NewQdrantClient(cfg.QdrantHost, cfg.QdrantPort)
    if err != nil {
        panic(err)
    }
    defer qdrant.Close()

    parentFetcher, err := retrieval.NewParentFetcher(cfg.PostgresDSN)
    if err != nil {
        panic(err)
    }
    defer parentFetcher.Close()

    cache, err := cache.NewRedisCache(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
    if err != nil {
        panic(err)
    }
    defer cache.Close()

    // Perform hybrid search
    params := retrieval.SearchParams{
        Query:       "What are GDPR consent requirements?",
        TopK:        cfg.DefaultTopK,
        RerankTopK:  cfg.DefaultRerankK,
        Collections: []string{cfg.GDPRCollection},
        Filters: map[string]string{
            "tier": "primary",
        },
    }

    queryVector := []float32{} // Get from embedding model
    results, err := qdrant.HybridSearch(context.Background(), params, queryVector)
    if err != nil {
        panic(err)
    }

    // Fetch parent chunks
    parentIDs := make([]string, len(results))
    for i, r := range results {
        parentIDs[i] = r.ParentID
    }

    parents, err := parentFetcher.FetchParentsByIDs(context.Background(), parentIDs)
    if err != nil {
        panic(err)
    }

    // Build citations
    citationBuilder := retrieval.NewCitationBuilder()
    citations := citationBuilder.BuildCitations(parents)

    // Format output
    formatted := citationBuilder.FormatCitations(citations)
    println(formatted)
}
```

## Database Schema

### Parent Chunks Table

```sql
CREATE TABLE parent_chunks (
    id VARCHAR(255) PRIMARY KEY,
    content TEXT NOT NULL,
    source_url TEXT NOT NULL,
    source_name VARCHAR(500) NOT NULL,
    tier VARCHAR(50) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_parent_chunks_tier ON parent_chunks(tier);
CREATE INDEX idx_parent_chunks_source_name ON parent_chunks(source_name);
```

### Child Chunks Table

```sql
CREATE TABLE child_chunks (
    id VARCHAR(255) PRIMARY KEY,
    parent_id VARCHAR(255) NOT NULL REFERENCES parent_chunks(id),
    content TEXT NOT NULL,
    chunk_index INT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_child_chunks_parent_id ON child_chunks(parent_id);
```

## Qdrant Collections

Collections should match the names in ingestion:

- `gdpr_chunks` - GDPR regulatory content
- `ai_act_chunks` - EU AI Act regulatory content

Each collection should have:
- Dense vector field for semantic search
- Sparse vector field for BM25 search (optional)
- Payload fields: `chunk_id`, `parent_id`, `content`, `tier`, `source`

## Performance Considerations

1. **Connection Pooling**: PostgreSQL and Redis clients use connection pools
2. **Caching**: Redis cache with 24-hour TTL reduces redundant searches
3. **Batch Operations**: Fetch multiple parent chunks in single query
4. **Index Usage**: Ensure proper database indexes on frequently queried fields

## Monitoring

Key metrics to monitor:
- Cache hit rate (Redis)
- Query latency (Qdrant)
- Database connection pool usage
- Error rates by component

## Future Enhancements

- [ ] Reranking integration (Cohere Rerank 3)
- [ ] BM25 sparse vector support in Qdrant
- [ ] Advanced filtering (date ranges, multiple tiers)
- [ ] Streaming responses
- [ ] Query expansion and reformulation
- [ ] Metrics and observability
