# PRD 03 — RAG Service

**Agent**: RAG agent  
**DEPENDS ON**: `01-infrastructure.md`, `02-ingestion-pipeline.md` (Qdrant populated)  
**Produces**: Go RAG service with provider abstraction, hybrid search, reranking, cited streaming generation  

---

## Overview

The RAG service is a Go microservice that handles the query path: embed query → hybrid search Qdrant → rerank with Cohere → fetch parent chunks → generate with Claude → stream cited response. All AI providers sit behind interfaces so any can be swapped without code changes.

---

## Service structure

```
services/rag/
├── cmd/
│   └── rag/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── provider/
│   │   ├── interfaces.go         # GenerationProvider, EmbeddingProvider, RerankProvider
│   │   ├── router.go             # ProviderRouter with health checks
│   │   ├── generation/
│   │   │   ├── claude.go
│   │   │   └── openai.go
│   │   ├── embedding/
│   │   │   ├── openai.go
│   │   │   └── cohere.go
│   │   └── rerank/
│   │       ├── cohere.go
│   │       └── jina.go
│   ├── retrieval/
│   │   ├── qdrant.go             # hybrid BM25 + vector search
│   │   └── parent_fetch.go       # fetch parent chunks from PostgreSQL
│   ├── cache/
│   │   └── redis.go              # query, embedding, retrieval caches
│   ├── rag/
│   │   ├── service.go            # orchestrates the full RAG pipeline
│   │   ├── prompt.go             # prompt templates per query intent
│   │   └── intent.go             # lightweight query classifier
│   └── server/
│       ├── server.go             # HTTP server + routes
│       ├── handlers.go           # request/response handlers
│       └── middleware.go         # logging, recovery, metrics
├── go.mod
└── go.sum
```

---

## Task 1 — Provider interfaces

Create `services/rag/internal/provider/interfaces.go`:

```go
package provider

import "context"

// --- Shared types ---

type Message struct {
    Role    string // "user" | "assistant" | "system"
    Content string
}

type GenerationRequest struct {
    SystemPrompt string
    Messages     []Message
    MaxTokens    int
    Stream       bool
}

type GenerationChunk struct {
    Text       string
    Done       bool
    ProviderID string
}

type Document struct {
    ID          string
    Text        string
    SourceURL   string
    Title       string
    SectionTitle string
    IsTable     bool
    Score       float64
    ParentID    string
}

type RankedDocument struct {
    Document
    RerankScore float64
}

// --- Provider interfaces ---

type GenerationProvider interface {
    Generate(ctx context.Context, req GenerationRequest) (<-chan GenerationChunk, error)
    ProviderID() string
    HealthCheck(ctx context.Context) error
}

type EmbeddingProvider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    CollectionName() string
    Dimensions() int
    ProviderID() string
    HealthCheck(ctx context.Context) error
}

type RerankProvider interface {
    Rerank(ctx context.Context, query string, docs []Document, topN int) ([]RankedDocument, error)
    ProviderID() string
    HealthCheck(ctx context.Context) error
}
```

### Acceptance criteria
- [ ] File compiles: `go build ./internal/provider/...`
- [ ] All three interfaces defined with correct method signatures

---

## Task 2 — Provider router with circuit breaker

Create `services/rag/internal/provider/router.go`:

```go
package provider

import (
    "context"
    "errors"
    "sync"
    "time"
)

type ProviderRouter[T any] struct {
    providers []T
    health    map[string]bool
    mu        sync.RWMutex
    idFn      func(T) string
    checkFn   func(context.Context, T) error
}

func NewRouter[T any](
    providers []T,
    idFn func(T) string,
    checkFn func(context.Context, T) error,
) *ProviderRouter[T] {
    r := &ProviderRouter[T]{
        providers: providers,
        health:    make(map[string]bool),
        idFn:      idFn,
        checkFn:   checkFn,
    }
    // mark all healthy initially
    for _, p := range providers {
        r.health[idFn(p)] = true
    }
    return r
}

func (r *ProviderRouter[T]) Primary() (T, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    for _, p := range r.providers {
        if r.health[r.idFn(p)] {
            return p, nil
        }
    }
    var zero T
    return zero, errors.New("all providers unavailable")
}

func (r *ProviderRouter[T]) MarkUnhealthy(id string) {
    r.mu.Lock()
    r.health[id] = false
    r.mu.Unlock()
}

func (r *ProviderRouter[T]) StartHealthChecks(ctx context.Context, interval time.Duration) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                for _, p := range r.providers {
                    id := r.idFn(p)
                    healthy := r.checkFn(ctx, p) == nil
                    r.mu.Lock()
                    r.health[id] = healthy
                    r.mu.Unlock()
                }
            case <-ctx.Done():
                return
            }
        }
    }()
}

func (r *ProviderRouter[T]) Status() map[string]bool {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make(map[string]bool, len(r.health))
    for k, v := range r.health {
        out[k] = v
    }
    return out
}
```

### Acceptance criteria
- [ ] Router initialises with all providers healthy
- [ ] `Primary()` returns first healthy provider
- [ ] After `MarkUnhealthy("claude")`, `Primary()` returns the next provider
- [ ] Health check goroutine re-marks providers healthy when they recover

---

## Task 3 — Claude generation provider

Create `services/rag/internal/provider/generation/claude.go`:

```go
package generation

import (
    "context"
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
    "kindlast/rag/internal/provider"
)

type ClaudeProvider struct {
    client *anthropic.Client
    model  string
}

func NewClaudeProvider(apiKey, model string) *ClaudeProvider {
    client := anthropic.NewClient(option.WithAPIKey(apiKey))
    return &ClaudeProvider{client: &client, model: model}
}

func (p *ClaudeProvider) ProviderID() string { return "claude" }

func (p *ClaudeProvider) HealthCheck(ctx context.Context) error {
    _, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.Model(p.model),
        MaxTokens: 1,
        Messages: []anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
        },
    })
    return err
}

func (p *ClaudeProvider) Generate(ctx context.Context, req provider.GenerationRequest) (<-chan provider.GenerationChunk, error) {
    ch := make(chan provider.GenerationChunk, 64)

    messages := make([]anthropic.MessageParam, len(req.Messages))
    for i, m := range req.Messages {
        if m.Role == "user" {
            messages[i] = anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content))
        } else {
            messages[i] = anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content))
        }
    }

    go func() {
        defer close(ch)
        stream := p.client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
            Model:     anthropic.Model(p.model),
            MaxTokens: int64(req.MaxTokens),
            System:    []anthropic.TextBlockParam{{Text: req.SystemPrompt}},
            Messages:  messages,
        })
        for stream.Next() {
            event := stream.Current()
            if delta, ok := event.Delta.(anthropic.ContentBlockDeltaEventDelta); ok {
                if textDelta, ok := delta.AsTextDelta(); ok {
                    ch <- provider.GenerationChunk{
                        Text:       textDelta.Text,
                        ProviderID: p.ProviderID(),
                    }
                }
            }
        }
        if err := stream.Err(); err != nil {
            ch <- provider.GenerationChunk{Done: true, ProviderID: p.ProviderID()}
        }
    }()

    return ch, nil
}
```

Create `services/rag/internal/provider/generation/openai.go` — identical pattern using OpenAI Go SDK with `client.Chat.Completions.NewStreaming`.

### Acceptance criteria
- [ ] `ClaudeProvider.HealthCheck()` returns nil with valid API key
- [ ] `Generate()` streams at least one chunk for a simple prompt
- [ ] Channel closes after generation completes
- [ ] Cancelled context stops generation cleanly

---

## Task 4 — Embedding providers (Go)

Create `services/rag/internal/provider/embedding/openai.go`:

```go
package embedding

import (
    "context"
    "github.com/openai/openai-go"
    "github.com/openai/openai-go/option"
    "kindlast/rag/internal/provider"
)

type OpenAIEmbedder struct {
    client     *openai.Client
    model      string
    collection string
    dims       int
}

func NewOpenAIEmbedder(apiKey, model, collection string, dims int) *OpenAIEmbedder {
    client := openai.NewClient(option.WithAPIKey(apiKey))
    return &OpenAIEmbedder{client: &client, model: model, collection: collection, dims: dims}
}

func (e *OpenAIEmbedder) ProviderID() string    { return "openai" }
func (e *OpenAIEmbedder) CollectionName() string { return e.collection }
func (e *OpenAIEmbedder) Dimensions() int        { return e.dims }

func (e *OpenAIEmbedder) HealthCheck(ctx context.Context) error {
    _, err := e.Embed(ctx, []string{"health check"})
    return err
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    response, err := e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
        Model:      openai.EmbeddingModel(e.model),
        Input:      openai.EmbeddingNewParamsInputUnionArrayOfStrings(texts),
        Dimensions: openai.Int(int64(e.dims)),
    })
    if err != nil {
        return nil, err
    }
    vectors := make([][]float32, len(response.Data))
    for i, d := range response.Data {
        f32 := make([]float32, len(d.Embedding))
        for j, v := range d.Embedding {
            f32[j] = float32(v)
        }
        vectors[i] = f32
    }
    return vectors, nil
}
```

Create `services/rag/internal/provider/embedding/cohere.go` with same interface.

---

## Task 5 — Hybrid Qdrant retrieval

Create `services/rag/internal/retrieval/qdrant.go`:

```go
package retrieval

import (
    "context"
    "github.com/qdrant/go-client/qdrant"
    "kindlast/rag/internal/provider"
)

type QdrantRetriever struct {
    client *qdrant.Client
}

func NewQdrantRetriever(host string, port int, apiKey string) (*QdrantRetriever, error) {
    client, err := qdrant.NewClient(&qdrant.Config{
        Host:   host,
        Port:   port,
        APIKey: apiKey,
        UseTLS: false,
    })
    if err != nil {
        return nil, err
    }
    return &QdrantRetriever{client: client}, nil
}

func (r *QdrantRetriever) HybridSearch(
    ctx context.Context,
    query string,
    queryVector []float32,
    collection string,
    limit uint64,
    topicFilter []string,   // ["gdpr"] | ["ai_act"] | nil for both
) ([]provider.Document, error) {

    // build optional topic filter
    var filter *qdrant.Filter
    if len(topicFilter) > 0 {
        conditions := make([]*qdrant.Condition, len(topicFilter))
        for i, t := range topicFilter {
            conditions[i] = qdrant.NewMatch("topic", t)
        }
        filter = &qdrant.Filter{
            Should: conditions,
        }
    }

    // prefetch dense vector results
    prefetch := []*qdrant.PrefetchQuery{
        {
            Query:  qdrant.NewQuery(queryVector...),
            Limit:  qdrant.PtrOf(limit * 2), // over-fetch before rerank
            Filter: filter,
        },
        {
            // BM25 sparse search
            Query: qdrant.NewQuerySparse(
                buildSparseVector(query), // tokenize query for BM25
            ),
            Using:  qdrant.PtrOf("bm25"),
            Limit:  qdrant.PtrOf(limit * 2),
            Filter: filter,
        },
    }

    // reciprocal rank fusion of both result sets
    response, err := r.client.Query(ctx, &qdrant.QueryPoints{
        CollectionName: collection,
        Prefetch:       prefetch,
        Query:          qdrant.NewQueryFusion(qdrant.Fusion_RRF),
        Limit:          qdrant.PtrOf(limit),
        WithPayload:    qdrant.NewWithPayload(true),
    })
    if err != nil {
        return nil, err
    }

    docs := make([]provider.Document, 0, len(response))
    for _, point := range response {
        payload := point.Payload
        docs = append(docs, provider.Document{
            ID:           point.Id.String(),
            Text:         payload["text"].GetStringValue(),
            SourceURL:    payload["source_url"].GetStringValue(),
            SectionTitle: payload["section_title"].GetStringValue(),
            IsTable:      payload["is_table"].GetBoolValue(),
            Score:        float64(point.Score),
            ParentID:     payload["parent_id"].GetStringValue(),
        })
    }
    return docs, nil
}

// buildSparseVector tokenizes the query for BM25
func buildSparseVector(query string) *qdrant.SparseVector {
    // Simple tokenization — in production use a proper tokenizer
    // that matches Qdrant's BM25 modifier configuration
    tokens := tokenize(query)
    indices := make([]uint32, len(tokens))
    values := make([]float32, len(tokens))
    for i, t := range tokens {
        indices[i] = hashToken(t)
        values[i] = 1.0
    }
    return &qdrant.SparseVector{Indices: indices, Values: values}
}
```

---

## Task 6 — RAG service orchestrator

Create `services/rag/internal/rag/service.go`:

```go
package rag

import (
    "context"
    "fmt"
    "kindlast/rag/internal/provider"
    "kindlast/rag/internal/retrieval"
    "kindlast/rag/internal/cache"
)

const LOW_SCORE_THRESHOLD = 0.72

type Service struct {
    genRouter    *provider.ProviderRouter[provider.GenerationProvider]
    embedRouter  *provider.ProviderRouter[provider.EmbeddingProvider]
    rerankRouter *provider.ProviderRouter[provider.RerankProvider]
    retriever    *retrieval.QdrantRetriever
    parentFetch  *retrieval.ParentFetcher
    cache        *cache.RedisCache
}

type QueryRequest struct {
    Query      string
    UserPlan   string   // "free" | "premium" | "api"
    TopicFilter []string
    MaxCitations int
}

type Citation struct {
    Index      int
    SourceURL  string
    Title      string
    Section    string
    ChunkText  string
}

type QueryResponse struct {
    Answer    string
    Citations []Citation
    CacheHit  bool
    Provider  string
    Warning   string  // set if confidence is low
}

func (s *Service) Query(ctx context.Context, req QueryRequest) (<-chan string, *QueryResponse, error) {
    // 1. Check query cache
    cacheKey := s.cache.QueryKey(req.Query, req.TopicFilter)
    if cached, ok := s.cache.GetQuery(ctx, cacheKey); ok {
        cached.CacheHit = true
        // stream cached answer
        ch := make(chan string, 1)
        go func() {
            defer close(ch)
            // stream in chunks to preserve UX
            for i := 0; i < len(cached.Answer); i += 20 {
                end := min(i+20, len(cached.Answer))
                ch <- cached.Answer[i:end]
            }
        }()
        return ch, cached, nil
    }

    // 2. Embed query (with embedding cache)
    embedder, err := s.embedRouter.Primary()
    if err != nil {
        return nil, nil, fmt.Errorf("no embedding provider: %w", err)
    }

    embKey := s.cache.EmbedKey(req.Query)
    queryVector, ok := s.cache.GetEmbedding(ctx, embKey)
    if !ok {
        vectors, err := embedder.Embed(ctx, []string{req.Query})
        if err != nil {
            s.embedRouter.MarkUnhealthy(embedder.ProviderID())
            // try fallback
            embedder, err = s.embedRouter.Primary()
            if err != nil {
                return nil, nil, err
            }
            vectors, err = embedder.Embed(ctx, []string{req.Query})
            if err != nil {
                return nil, nil, err
            }
        }
        queryVector = vectors[0]
        s.cache.SetEmbedding(ctx, embKey, queryVector)
    }

    // 3. Hybrid search
    docs, err := s.retriever.HybridSearch(
        ctx, req.Query, queryVector,
        embedder.CollectionName(), 20, req.TopicFilter,
    )
    if err != nil {
        return nil, nil, err
    }

    // 4. Low confidence guard
    if len(docs) == 0 || docs[0].Score < LOW_SCORE_THRESHOLD {
        resp := &QueryResponse{
            Answer: "I don't have sufficient regulatory source material to answer this confidently. " +
                    "This may be outside current GDPR/EU AI Act guidance — consider consulting a qualified DPO.",
            Citations: []Citation{},
            Warning:  "low_confidence",
        }
        ch := make(chan string, 1)
        ch <- resp.Answer
        close(ch)
        return ch, resp, nil
    }

    // 5. Rerank
    reranker, err := s.rerankRouter.Primary()
    if err == nil {
        ranked, err := reranker.Rerank(ctx, req.Query, docs, 10)
        if err == nil {
            docs = make([]provider.Document, len(ranked))
            for i, r := range ranked {
                docs[i] = r.Document
            }
        }
    }

    // 6. Fetch parent chunks for fuller context
    parentTexts := s.parentFetch.FetchParents(ctx, docs[:min(5, len(docs))])

    // 7. Classify intent and select prompt
    intent := ClassifyIntent(req.Query)
    systemPrompt := BuildSystemPrompt(intent, parentTexts, docs)

    // 8. Apply freemium limit
    maxCitations := req.MaxCitations
    if req.UserPlan == "free" && maxCitations > 3 {
        maxCitations = 3
    }

    // 9. Generate with citations
    generator, err := s.genRouter.Primary()
    if err != nil {
        return nil, nil, err
    }

    genReq := provider.GenerationRequest{
        SystemPrompt: systemPrompt,
        Messages:     []provider.Message{{Role: "user", Content: req.Query}},
        MaxTokens:    1024,
        Stream:       true,
    }

    stream, err := generator.Generate(ctx, genReq)
    if err != nil {
        s.genRouter.MarkUnhealthy(generator.ProviderID())
        // try fallback provider
        generator, err = s.genRouter.Primary()
        if err != nil {
            return nil, nil, err
        }
        stream, err = generator.Generate(ctx, genReq)
        if err != nil {
            return nil, nil, err
        }
    }

    // build citations (limited by plan)
    citations := buildCitations(docs[:min(maxCitations, len(docs))])

    resp := &QueryResponse{
        Citations: citations,
        Provider:  generator.ProviderID(),
        CacheHit:  false,
    }

    // wrap stream to collect full answer for caching
    outCh := make(chan string, 64)
    go func() {
        defer close(outCh)
        fullAnswer := ""
        for chunk := range stream {
            outCh <- chunk.Text
            fullAnswer += chunk.Text
        }
        resp.Answer = fullAnswer
        // cache the completed response
        s.cache.SetQuery(ctx, cacheKey, resp)
    }()

    return outCh, resp, nil
}
```

---

## Task 7 — Prompt templates

Create `services/rag/internal/rag/prompt.go`:

```go
package rag

import (
    "fmt"
    "strings"
    "kindlast/rag/internal/provider"
)

type QueryIntent string

const (
    IntentLookup     QueryIntent = "lookup"
    IntentAssessment QueryIntent = "assessment"
    IntentChecklist  QueryIntent = "checklist"
)

var systemPrompts = map[QueryIntent]string{
    IntentLookup: `You are a GDPR and EU AI Act regulatory reference tool.
Your role is to retrieve and quote the relevant regulatory provision directly.
Be precise and concise. Always cite the specific article number and source document.
Format citations inline as [1], [2] etc., matching the SOURCE INDEX below.
Never make claims not supported by the provided sources.
If sources don't fully answer the question, say so explicitly.`,

    IntentAssessment: `You are a GDPR and EU AI Act compliance analyst.
Assess the described practice against current regulatory requirements.
Structure your response: (1) Applicable rules, (2) Assessment, (3) Required actions.
Cite every claim inline as [1], [2] etc., matching the SOURCE INDEX below.
Flag uncertainty clearly — do not speculate beyond what sources support.
End with: "This is regulatory guidance, not legal advice. Consult a qualified DPO for your specific situation."`,

    IntentChecklist: `You are a GDPR compliance consultant generating actionable checklists.
Create numbered, specific action items grounded in regulatory requirements.
Each item must cite its regulatory basis inline as [1], [2] etc.
Be practical — focus on what an SME can actually implement.
Do not include items not supported by the provided sources.`,
}

func BuildSystemPrompt(intent QueryIntent, parentTexts []string, docs []provider.Document) string {
    template, ok := systemPrompts[intent]
    if !ok {
        template = systemPrompts[IntentAssessment]
    }

    sourceIndex := buildSourceIndex(docs)
    
    return fmt.Sprintf(`%s

--- SOURCE INDEX ---
%s

--- CONTEXT ---
%s
--- END CONTEXT ---

Respond with inline citations [1], [2] etc. referencing the SOURCE INDEX.
Every factual claim must be backed by a citation. Do not use information outside these sources.`,
        template,
        sourceIndex,
        strings.Join(parentTexts, "\n\n---\n\n"),
    )
}

func buildSourceIndex(docs []provider.Document) string {
    var sb strings.Builder
    seen := map[string]bool{}
    idx := 1
    for _, doc := range docs {
        key := doc.SourceURL + doc.SectionTitle
        if seen[key] {
            continue
        }
        seen[key] = true
        sb.WriteString(fmt.Sprintf("[%d] %s", idx, doc.SourceURL))
        if doc.SectionTitle != "" {
            sb.WriteString(fmt.Sprintf(" — %s", doc.SectionTitle))
        }
        sb.WriteString("\n")
        idx++
    }
    return sb.String()
}
```

---

## Task 8 — HTTP server and handlers

Create `services/rag/internal/server/handlers.go`:

```go
package server

import (
    "encoding/json"
    "net/http"
    "kindlast/rag/internal/rag"
)

// POST /v1/query
// Body: {"query": "...", "topic_filter": ["gdpr"], "max_citations": 5}
// Returns: Server-Sent Events stream
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
    // parse request
    var req struct {
        Query       string   `json:"query"`
        TopicFilter []string `json:"topic_filter"`
        MaxCitations int     `json:"max_citations"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid request", http.StatusBadRequest)
        return
    }

    // read user context from middleware (set by gateway after JWT validation)
    userPlan := r.Header.Get("X-User-Plan")   // "free" | "premium" | "api"
    if userPlan == "" {
        userPlan = "free"
    }
    if req.MaxCitations == 0 {
        req.MaxCitations = 10
    }

    // set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }

    queryReq := rag.QueryRequest{
        Query:        req.Query,
        UserPlan:     userPlan,
        TopicFilter:  req.TopicFilter,
        MaxCitations: req.MaxCitations,
    }

    stream, resp, err := s.ragService.Query(r.Context(), queryReq)
    if err != nil {
        writeSSEError(w, err.Error())
        return
    }

    // stream text chunks
    for chunk := range stream {
        writeSSEData(w, map[string]string{"type": "chunk", "text": chunk})
        flusher.Flush()
    }

    // send final metadata (citations, provider, cache_hit)
    writeSSEData(w, map[string]any{
        "type":      "done",
        "citations": resp.Citations,
        "provider":  resp.Provider,
        "cache_hit": resp.CacheHit,
        "warning":   resp.Warning,
    })
    flusher.Flush()
}

// GET /healthz — liveness
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}

// GET /readyz — readiness (checks Qdrant + Redis)
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
    // check Qdrant reachable
    if err := s.qdrant.Ping(r.Context()); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]string{"error": "qdrant unreachable"})
        return
    }
    // check Redis reachable
    if err := s.redis.Ping(r.Context()); err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]string{"error": "redis unreachable"})
        return
    }
    json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// GET /v1/providers — returns current provider health status
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode(map[string]any{
        "generation": s.ragService.GenStatus(),
        "embedding":  s.ragService.EmbedStatus(),
        "rerank":     s.ragService.RerankStatus(),
    })
}

func writeSSEData(w http.ResponseWriter, data any) {
    b, _ := json.Marshal(data)
    fmt.Fprintf(w, "data: %s\n\n", b)
}

func writeSSEError(w http.ResponseWriter, msg string) {
    fmt.Fprintf(w, "data: {\"type\":\"error\",\"message\":\"%s\"}\n\n", msg)
}
```

---

## Task 9 — K8s Deployment

Create `infrastructure/k8s/app/rag-service-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rag-service
  namespace: kindlast-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: rag-service
  template:
    metadata:
      labels:
        app: rag-service
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      containers:
      - name: rag
        image: kindlast/rag:latest
        ports:
        - containerPort: 8081
          name: http
        - containerPort: 9090
          name: metrics
        envFrom:
        - secretRef:
            name: ai-provider-keys
        env:
        - name: QDRANT_HOST
          value: qdrant.kindlast-data.svc.cluster.local
        - name: REDIS_URL
          value: redis://redis.kindlast-data.svc.cluster.local:6379
        - name: POSTGRES_DSN
          valueFrom:
            secretKeyRef:
              name: postgres-credentials
              key: dsn
        - name: GENERATION_PROVIDER_ORDER
          value: "claude,gpt-4o"
        - name: EMBEDDING_PROVIDER_ORDER
          value: "openai,cohere"
        - name: RERANK_PROVIDER_ORDER
          value: "cohere,jina"
        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "512Mi"
            cpu: "1000m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
          initialDelaySeconds: 10
          periodSeconds: 15
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
          initialDelaySeconds: 5
          periodSeconds: 10
```

### Acceptance criteria
- [ ] `curl http://rag-service:8081/healthz` returns `{"status":"ok"}`
- [ ] `curl http://rag-service:8081/readyz` returns `{"status":"ready"}` when Qdrant and Redis are up
- [ ] Streaming query returns SSE events: multiple `chunk` events then one `done` event
- [ ] Free-tier user receives max 3 citations
- [ ] Setting `GENERATION_PROVIDER_ORDER=gpt-4o,claude` routes to GPT-4o first
- [ ] `curl http://rag-service:8081/v1/providers` shows health of all providers
