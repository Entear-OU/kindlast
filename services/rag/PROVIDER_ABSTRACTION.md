# RAG Service Provider Abstraction Layer

This document describes the provider abstraction layer built for the Kindlast RAG service.

## Overview

The provider abstraction layer implements a flexible, vendor-agnostic interface for AI services with automatic failover and circuit breaking. All providers can be swapped via configuration without code changes.

## Directory Structure

```
services/rag/internal/
├── providers/
│   ├── interfaces.go           # Core provider interfaces
│   ├── generation/
│   │   ├── claude.go           # Anthropic Claude Sonnet provider
│   │   └── openai.go           # OpenAI GPT-4o provider
│   ├── embedding/
│   │   ├── openai.go           # OpenAI text-embedding-3-large
│   │   └── cohere.go           # Cohere embed-multilingual-v3.0
│   ├── reranking/
│   │   ├── cohere.go           # Cohere Rerank 3
│   │   └── jina.go             # Jina Reranker v2 (HTTP client)
│   └── example_usage.md        # Usage examples
└── router/
    └── router.go               # Circuit breaker and routing logic
```

## Core Interfaces

### GenerationProvider

Generates text responses from conversational input.

**Methods:**
- `Generate(ctx, req) (*GenerationResponse, error)` - Non-streaming generation
- `Stream(ctx, req) (<-chan StreamChunk, error)` - Streaming generation
- `Name() string` - Returns provider name

**Implementations:**
- `ClaudeProvider` - Uses Anthropic SDK
- `OpenAIProvider` - Uses OpenAI SDK

**Features:**
- System prompts
- Temperature control
- Token limits
- Streaming support
- Token usage tracking

### EmbeddingProvider

Converts text to dense vector embeddings.

**Methods:**
- `Embed(ctx, req) (*EmbeddingResponse, error)` - Generate embeddings
- `Name() string` - Returns provider name

**Implementations:**
- `OpenAIProvider` - text-embedding-3-large (3072 dimensions)
- `CohereProvider` - embed-multilingual-v3.0 (variable dimensions)

**Features:**
- Batch embedding
- Token usage tracking
- Model selection

### RerankProvider

Reranks search results by relevance to query.

**Methods:**
- `Rerank(ctx, req) (*RerankResponse, error)` - Rerank documents
- `Name() string` - Returns provider name

**Implementations:**
- `CohereProvider` - Uses Cohere SDK
- `JinaProvider` - Custom HTTP client (no official SDK)

**Features:**
- Top-K selection
- Relevance scoring
- Document metadata preservation

## Router Layer

### Circuit Breaker

Each router uses `github.com/sony/gobreaker` for circuit breaking:

**States:**
- **Closed** - Normal operation, all requests pass through
- **Open** - Too many failures, fails fast without calling provider
- **Half-Open** - Testing recovery, limited requests allowed

**Default Settings:**
- Max requests in half-open: 3
- Interval: 1 minute
- Timeout: 30 seconds
- Trip condition: ≥50% failure rate with ≥5 requests

### GenerationRouter

Routes generation requests with primary/fallback providers.

**Features:**
- Automatic failover from primary to fallback
- Circuit breaker per provider
- Health status monitoring
- Streaming support

**Usage:**
```go
router := router.NewGenerationRouter(
    claudeProvider,  // primary
    openaiProvider,  // fallback
    router.DefaultCircuitBreakerSettings(),
)

resp, err := router.Generate(ctx, req)
```

### EmbeddingRouter

Routes embedding requests with primary/fallback providers.

**Features:**
- Automatic failover
- Circuit breaker per provider
- Health status monitoring

**Usage:**
```go
router := router.NewEmbeddingRouter(
    openaiProvider,  // primary
    cohereProvider,  // fallback
    router.DefaultCircuitBreakerSettings(),
)

resp, err := router.Embed(ctx, req)
```

### RerankRouter

Routes reranking requests with primary/fallback providers.

**Features:**
- Automatic failover
- Circuit breaker per provider
- Health status monitoring

**Usage:**
```go
router := router.NewRerankRouter(
    cohereProvider,  // primary
    jinaProvider,    // fallback
    router.DefaultCircuitBreakerSettings(),
)

resp, err := router.Rerank(ctx, req)
```

## Provider Implementations

### Claude (Anthropic)

**SDK:** `github.com/anthropics/anthropic-sdk-go`
**Model:** Claude Sonnet 4.5 (configurable)
**Features:**
- Full message history support
- System prompts
- Streaming via Server-Sent Events
- Token usage tracking

**Notes:**
- Uses `MessageNewParams` for requests
- Streaming via `Messages.NewStreaming()`
- Content extracted from `ContentBlockUnion`

### OpenAI (Generation)

**SDK:** `github.com/openai/openai-go`
**Model:** GPT-4o (configurable)
**Features:**
- Full message history support
- System prompts
- Streaming via Server-Sent Events
- Token usage tracking

**Notes:**
- Uses `ChatCompletionNewParams` for requests
- Streaming via `Chat.Completions.NewStreaming()`
- Content in `Choices[0].Message.Content`

### OpenAI (Embedding)

**SDK:** `github.com/openai/openai-go`
**Model:** text-embedding-3-large (configurable)
**Dimensions:** 3072
**Features:**
- Batch embedding
- Token usage tracking

**Notes:**
- Uses `EmbeddingNewParams` for requests
- Input accepts `[]string` directly
- Returns `[][]float64` embeddings

### Cohere (Embedding)

**SDK:** `github.com/cohere-ai/cohere-go/v2`
**Model:** embed-multilingual-v3.0 (configurable)
**Dimensions:** Variable (default 1024)
**Features:**
- Batch embedding
- Multiple input types (search, classification, clustering)
- Estimated token usage

**Notes:**
- Uses `EmbedRequest` with `InputType` parameter
- Returns `EmbedFloatsResponse`
- Token usage estimated (no API field)

### Cohere (Reranking)

**SDK:** `github.com/cohere-ai/cohere-go/v2`
**Model:** rerank-v3.5 (configurable)
**Features:**
- Top-K filtering
- Relevance scores
- Document metadata preservation

**Notes:**
- Uses `RerankRequest` with `RerankRequestDocumentsItem`
- Returns sorted results by relevance
- Preserves original document indices

### Jina (Reranking)

**SDK:** Custom HTTP client (no official SDK)
**Model:** jina-reranker-v2-base-multilingual (configurable)
**API:** `https://api.jina.ai/v1/rerank`
**Features:**
- Top-K filtering
- Relevance scores
- Document metadata preservation

**Notes:**
- Custom HTTP client implementation
- Bearer token authentication
- Results sorted by score descending

## Error Handling

All providers return `ProviderError` with:
- Provider name
- Error message
- Underlying error (if any)

**Example:**
```go
if err != nil {
    if provErr, ok := err.(*providers.ProviderError); ok {
        log.Printf("Provider %s failed: %s", provErr.Provider, provErr.Message)
    }
}
```

## Configuration Integration

The provider layer integrates with `internal/config/config.go`:

```go
type ProvidersConfig struct {
    Generation GenerationConfig
    Embedding  EmbeddingConfig
    Reranking  RerankingConfig
}
```

**Environment Variables:**
- `GENERATION_PRIMARY`, `GENERATION_FALLBACK`
- `EMBEDDING_PRIMARY`, `EMBEDDING_FALLBACK`
- `RERANKING_PRIMARY`, `RERANKING_FALLBACK`
- `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `COHERE_API_KEY`, `JINA_API_KEY`
- Model names, timeouts, etc.

## Dependencies

```
go 1.26.2

require (
    github.com/anthropics/anthropic-sdk-go v1.37.0
    github.com/openai/openai-go v1.12.0
    github.com/cohere-ai/cohere-go/v2 v2.18.0
    github.com/sony/gobreaker v1.0.0
)
```

## Next Steps

### Remaining Work

1. **Fix SDK compatibility issues** - Some provider implementations have minor API mismatches that need adjustment based on actual SDK versions
2. **Add unit tests** - Test each provider with mocked SDK clients
3. **Add integration tests** - Test routers with real API calls (optional, API keys required)
4. **Add provider metrics** - Track latency, tokens, errors per provider
5. **Add retry logic** - Exponential backoff for transient errors

### Integration Points

The provider abstraction layer is designed to be used by:
- `internal/rag/orchestrator.go` - RAG pipeline orchestration
- `cmd/server/main.go` - HTTP server initialization
- Future services requiring AI providers

### Performance Considerations

- **Connection pooling**: SDKs handle this internally
- **Request timeouts**: Set via context
- **Rate limiting**: Should be added at gateway level
- **Caching**: Query results cached in Redis, not provider responses

## Testing Strategy

### Unit Tests (TODO)

```go
func TestClaudeProvider_Generate(t *testing.T) {
    // Mock Anthropic client
    // Test Generate method
}

func TestGenerationRouter_Failover(t *testing.T) {
    // Mock primary provider to fail
    // Verify fallback is called
}
```

### Integration Tests (TODO)

```go
func TestProviders_RealAPI(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    // Test with real API keys
}
```

## References

- [Anthropic SDK](https://github.com/anthropics/anthropic-sdk-go)
- [OpenAI SDK](https://github.com/openai/openai-go)
- [Cohere SDK](https://github.com/cohere-ai/cohere-go)
- [Circuit Breaker Pattern](https://github.com/sony/gobreaker)
- [Example Usage](internal/providers/example_usage.md)
