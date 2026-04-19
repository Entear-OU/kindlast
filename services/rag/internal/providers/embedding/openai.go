package embedding

import (
	"context"
	"errors"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/entear/kindlast/services/rag/internal/providers"
)

// OpenAIProvider implements the EmbeddingProvider interface for OpenAI
type OpenAIProvider struct {
	client *openai.Client
	model  openai.EmbeddingModel
}

// NewOpenAIProvider creates a new OpenAI embedding provider
func NewOpenAIProvider(apiKey string, model string) (*OpenAIProvider, error) {
	if apiKey == "" {
		return nil, errors.New("openai API key is required")
	}

	modelEnum := openai.EmbeddingModelTextEmbedding3Large
	if model != "" {
		modelEnum = openai.EmbeddingModel(model)
	}

	client := openai.NewClient(option.WithAPIKey(apiKey))

	return &OpenAIProvider{
		client: &client,
		model:  modelEnum,
	}, nil
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Embed generates embeddings for the given texts
func (p *OpenAIProvider) Embed(ctx context.Context, req providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	if len(req.Texts) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "texts are required",
		}
	}

	// Use model from request if provided
	model := p.model
	if req.Model != "" {
		model = openai.EmbeddingModel(req.Model)
	}

	// Build request parameters
	// Convert texts to InputUnion
	params := openai.EmbeddingNewParams{
		Model: model,
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: req.Texts,
		},
	}

	// Make API call
	result, err := p.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to generate embeddings",
			Err:      err,
		}
	}

	// Extract embeddings
	embeddings := make([][]float64, len(result.Data))
	for i, data := range result.Data {
		embeddings[i] = data.Embedding
	}

	return &providers.EmbeddingResponse{
		Embeddings: embeddings,
		Usage: providers.TokenUsage{
			InputTokens: int(result.Usage.PromptTokens),
		},
	}, nil
}
