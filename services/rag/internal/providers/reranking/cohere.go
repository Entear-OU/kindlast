package reranking

import (
	"context"
	"errors"

	coherego "github.com/cohere-ai/cohere-go/v2"
	cohereclient "github.com/cohere-ai/cohere-go/v2/client"
	"github.com/entear/kindlast/services/rag/internal/providers"
)

// CohereProvider implements the RerankProvider interface for Cohere
type CohereProvider struct {
	client *cohereclient.Client
	model  string
}

// NewCohereProvider creates a new Cohere reranking provider
func NewCohereProvider(apiKey string, model string) (*CohereProvider, error) {
	if apiKey == "" {
		return nil, errors.New("cohere API key is required")
	}
	if model == "" {
		model = "rerank-v3.5" // Default to Rerank 3.5
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

// Rerank reranks documents based on query relevance
func (p *CohereProvider) Rerank(ctx context.Context, req providers.RerankRequest) (*providers.RerankResponse, error) {
	if req.Query == "" {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "query is required",
		}
	}

	if len(req.Documents) == 0 {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "documents are required",
		}
	}

	// Use model from request if provided
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	// Convert documents to Cohere format
	documents := make([]*coherego.RerankRequestDocumentsItem, len(req.Documents))
	for i, doc := range req.Documents {
		documents[i] = &coherego.RerankRequestDocumentsItem{
			String: doc.Content,
		}
	}

	// Set default top K
	topK := req.TopK
	if topK == 0 {
		topK = len(req.Documents)
	}

	// Build request parameters
	params := &coherego.RerankRequest{
		Query:     req.Query,
		Documents: documents,
		Model:     &model,
		TopN:      &topK,
	}

	// Make API call
	result, err := p.client.Rerank(ctx, params)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to rerank documents",
			Err:      err,
		}
	}

	// Convert results to our format
	rankedDocs := make([]providers.RankedDocument, len(result.Results))
	for i, res := range result.Results {
		rankedDocs[i] = providers.RankedDocument{
			Document: req.Documents[res.Index],
			Score:    res.RelevanceScore,
			Index:    res.Index,
		}
	}

	return &providers.RerankResponse{
		Results: rankedDocs,
	}, nil
}
