# Provider Abstraction Layer Usage

This document demonstrates how to use the RAG service provider abstraction layer.

## Overview

The provider abstraction layer provides a unified interface for working with multiple AI providers:
- **Generation**: Claude (Anthropic), GPT-4o (OpenAI)
- **Embedding**: text-embedding-3-large (OpenAI), embed-multilingual-v3.0 (Cohere)
- **Reranking**: Cohere Rerank 3, Jina Reranker v2

## Basic Usage

### Generation

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/entear/kindlast/services/rag/internal/providers"
    "github.com/entear/kindlast/services/rag/internal/providers/generation"
    "github.com/entear/kindlast/services/rag/internal/router"
)

func main() {
    // Create providers
    claude, err := generation.NewClaudeProvider("your-api-key", "")
    if err != nil {
        log.Fatal(err)
    }

    openai, err := generation.NewOpenAIProvider("your-api-key", "")
    if err != nil {
        log.Fatal(err)
    }

    // Create router with circuit breaker
    genRouter := router.NewGenerationRouter(
        claude,  // primary
        openai,  // fallback
        router.DefaultCircuitBreakerSettings(),
    )

    // Generate response
    req := providers.GenerationRequest{
        Messages: []providers.Message{
            {Role: "user", Content: "What is GDPR?"},
        },
        MaxTokens:    1000,
        Temperature:  0.7,
        SystemPrompt: "You are a helpful assistant.",
    }

    resp, err := genRouter.Generate(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

### Streaming Generation

```go
func streamExample() {
    // ... setup router as above ...

    req := providers.GenerationRequest{
        Messages: []providers.Message{
            {Role: "user", Content: "Explain GDPR compliance."},
        },
        MaxTokens: 1000,
    }

    chunks, err := genRouter.Stream(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }

    for chunk := range chunks {
        if chunk.Error != nil {
            log.Printf("Error: %v", chunk.Error)
            continue
        }
        if chunk.Content != "" {
            fmt.Print(chunk.Content)
        }
        if chunk.FinishReason != "" {
            fmt.Printf("\n[Finished: %s]\n", chunk.FinishReason)
        }
    }
}
```

### Embeddings

```go
import (
    "github.com/entear/kindlast/services/rag/internal/providers/embedding"
)

func embeddingExample() {
    // Create providers
    openaiEmbed, _ := embedding.NewOpenAIProvider("your-api-key", "")
    cohereEmbed, _ := embedding.NewCohereProvider("your-api-key", "")

    // Create router
    embedRouter := router.NewEmbeddingRouter(
        openaiEmbed,  // primary
        cohereEmbed,  // fallback
        router.DefaultCircuitBreakerSettings(),
    )

    // Generate embeddings
    req := providers.EmbeddingRequest{
        Texts: []string{
            "GDPR compliance requires data protection by design.",
            "The right to be forgotten is a key GDPR principle.",
        },
    }

    resp, err := embedRouter.Embed(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Generated %d embeddings\n", len(resp.Embeddings))
    fmt.Printf("First embedding dimension: %d\n", len(resp.Embeddings[0]))
}
```

### Reranking

```go
import (
    "github.com/entear/kindlast/services/rag/internal/providers/reranking"
)

func rerankExample() {
    // Create providers
    cohereRerank, _ := reranking.NewCohereProvider("your-api-key", "")
    jinaRerank, _ := reranking.NewJinaProvider("your-api-key", "")

    // Create router
    rerankRouter := router.NewRerankRouter(
        cohereRerank,  // primary
        jinaRerank,    // fallback
        router.DefaultCircuitBreakerSettings(),
    )

    // Rerank documents
    req := providers.RerankRequest{
        Query: "GDPR data protection requirements",
        Documents: []providers.Document{
            {ID: "1", Content: "GDPR requires data protection by design and by default."},
            {ID: "2", Content: "The weather is nice today."},
            {ID: "3", Content: "Data controllers must implement appropriate technical measures."},
        },
        TopK: 2,
    }

    resp, err := rerankRouter.Rerank(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }

    for _, result := range resp.Results {
        fmt.Printf("Doc %s: %.4f - %s\n",
            result.Document.ID,
            result.Score,
            result.Document.Content[:50])
    }
}
```

## Circuit Breaker

The router automatically handles provider failures with circuit breakers:

- **Closed**: Normal operation, requests go through
- **Open**: Too many failures, requests fail fast
- **Half-Open**: Testing if provider has recovered

### Custom Circuit Breaker Settings

```go
settings := router.CircuitBreakerSettings{
    MaxRequests: 5,                    // Max requests in half-open state
    Interval:    2 * time.Minute,      // Reset interval
    Timeout:     60 * time.Second,     // Timeout for open state
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        // Trip if 60% of last 10 requests failed
        failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
        return counts.Requests >= 10 && failureRatio >= 0.6
    },
    OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
        log.Printf("Provider %s: %s -> %s", name, from, to)
    },
}

genRouter := router.NewGenerationRouter(primary, fallback, settings)
```

## Monitoring Provider Health

```go
// Check provider health
health := genRouter.GetProviderHealth()
for name, state := range health {
    fmt.Printf("Provider %s: %s\n", name, state)
}
```

## Error Handling

All provider errors implement the `ProviderError` type:

```go
resp, err := genRouter.Generate(ctx, req)
if err != nil {
    if provErr, ok := err.(*providers.ProviderError); ok {
        fmt.Printf("Provider %s failed: %s\n", provErr.Provider, provErr.Message)
        if provErr.Err != nil {
            fmt.Printf("Underlying error: %v\n", provErr.Err)
        }
    }
}
```

## Configuration from Environment

See `/internal/config/config.go` for environment-based configuration:

```bash
# Generation
GENERATION_PRIMARY=anthropic
GENERATION_FALLBACK=openai
ANTHROPIC_API_KEY=your-key
OPENAI_API_KEY=your-key

# Embedding
EMBEDDING_PRIMARY=openai
EMBEDDING_FALLBACK=cohere
COHERE_API_KEY=your-key

# Reranking
RERANKING_PRIMARY=cohere
RERANKING_FALLBACK=jina
JINA_API_KEY=your-key
```
