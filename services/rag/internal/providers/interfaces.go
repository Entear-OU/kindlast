package providers

import (
	"context"
	"io"
)

// Message represents a message in a conversation
type Message struct {
	Role    string // "user", "assistant", "system"
	Content string
}

// GenerationRequest represents a request to generate text
type GenerationRequest struct {
	Messages    []Message
	MaxTokens   int
	Temperature float64
	SystemPrompt string
}

// GenerationResponse represents a non-streaming generation response
type GenerationResponse struct {
	Content      string
	FinishReason string
	Usage        TokenUsage
}

// TokenUsage represents token usage statistics
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// StreamChunk represents a chunk of streaming response
type StreamChunk struct {
	Content      string
	FinishReason string
	Error        error
}

// GenerationProvider defines the interface for text generation providers
type GenerationProvider interface {
	// Generate generates a non-streaming response
	Generate(ctx context.Context, req GenerationRequest) (*GenerationResponse, error)

	// Stream generates a streaming response
	Stream(ctx context.Context, req GenerationRequest) (<-chan StreamChunk, error)

	// Name returns the provider name
	Name() string
}

// EmbeddingRequest represents a request to generate embeddings
type EmbeddingRequest struct {
	Texts []string
	Model string
}

// EmbeddingResponse represents an embedding response
type EmbeddingResponse struct {
	Embeddings [][]float64
	Usage      TokenUsage
}

// EmbeddingProvider defines the interface for embedding providers
type EmbeddingProvider interface {
	// Embed generates embeddings for the given texts
	Embed(ctx context.Context, req EmbeddingRequest) (*EmbeddingResponse, error)

	// Name returns the provider name
	Name() string
}

// Document represents a document to be reranked
type Document struct {
	ID      string
	Content string
}

// RerankRequest represents a request to rerank documents
type RerankRequest struct {
	Query     string
	Documents []Document
	TopK      int
	Model     string
}

// RankedDocument represents a reranked document with score
type RankedDocument struct {
	Document Document
	Score    float64
	Index    int // Original index in the input
}

// RerankResponse represents a rerank response
type RerankResponse struct {
	Results []RankedDocument
}

// RerankProvider defines the interface for reranking providers
type RerankProvider interface {
	// Rerank reranks documents based on query relevance
	Rerank(ctx context.Context, req RerankRequest) (*RerankResponse, error)

	// Name returns the provider name
	Name() string
}

// ProviderError represents a provider-specific error
type ProviderError struct {
	Provider string
	Message  string
	Err      error
}

func (e *ProviderError) Error() string {
	if e.Err != nil {
		return e.Provider + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Provider + ": " + e.Message
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}

// StreamWriter is a helper interface for writing streaming responses
type StreamWriter interface {
	io.Writer
	Flush() error
}
