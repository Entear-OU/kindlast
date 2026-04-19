package embedding

import (
	"context"
	"errors"

	coherego "github.com/cohere-ai/cohere-go/v2"
	cohereclient "github.com/cohere-ai/cohere-go/v2/client"
	"github.com/entear/kindlast/services/rag/internal/providers"
)

// CohereProvider implements the EmbeddingProvider interface for Cohere
type CohereProvider struct {
	client *cohereclient.Client
	model  string
}

// NewCohereProvider creates a new Cohere embedding provider
func NewCohereProvider(apiKey string, model string) (*CohereProvider, error) {
	if apiKey == "" {
		return nil, errors.New("cohere API key is required")
	}
	if model == "" {
		model = "embed-multilingual-v3.0" // Default to embed-multilingual-v3.0
	}

	client := cohereclient.NewClient(cohereclient.WithToken(apiKey))

	return &CohereProvider{
		client: client,
		model:  model,
	}, nil
}

// Name returns the provider name
func (p *CohereProvider) Name() string {
	return "cohere"
}

// Embed generates embeddings for the given texts
func (p *CohereProvider) Embed(ctx context.Context, req providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	if len(req.Texts) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "texts are required",
		}
	}

	// Use model from request if provided
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	// Build request parameters
	params := &coherego.EmbedRequest{
		Texts:         req.Texts,
		Model:         &model,
		InputType:     coherego.EmbedInputTypeSearchDocument.Ptr(),
		EmbeddingTypes: []coherego.EmbeddingType{coherego.EmbeddingTypeFloat},
	}

	// Make API call
	result, err := p.client.Embed(ctx, params)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to generate embeddings",
			Err:      err,
		}
	}

	// Extract embeddings - Cohere SDK returns EmbedFloatsResponse
	if result.EmbeddingsFloats == nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "no embeddings returned from API",
		}
	}

	// Extract embeddings from EmbedFloatsResponse
	embeddings := result.EmbeddingsFloats.Embeddings

	// Cohere doesn't provide token usage in the same way, so we estimate
	// Average of ~0.75 tokens per word, ~5 characters per word
	totalChars := 0
	for _, text := range req.Texts {
		totalChars += len(text)
	}
	estimatedTokens := (totalChars / 5) * 3 / 4

	return &providers.EmbeddingResponse{
		Embeddings: embeddings,
		Usage: providers.TokenUsage{
			InputTokens: estimatedTokens,
		},
	}, nil
}
