package reranking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/entear/kindlast/services/rag/internal/providers"
)

// JinaProvider implements the RerankProvider interface for Jina AI
type JinaProvider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewJinaProvider creates a new Jina reranking provider
func NewJinaProvider(apiKey string, model string) (*JinaProvider, error) {
	if apiKey == "" {
		return nil, errors.New("jina API key is required")
	}
	if model == "" {
		model = "jina-reranker-v2-base-multilingual" // Default to Jina Reranker v2
	}

	return &JinaProvider{
		apiKey:     apiKey,
		model:      model,
		baseURL:    "https://api.jina.ai/v1",
		httpClient: &http.Client{},
	}, nil
}

// Name returns the provider name
func (p *JinaProvider) Name() string {
	return "jina"
}

// jinaRerankRequest represents a Jina API rerank request
type jinaRerankRequest struct {
	Model     string                   `json:"model"`
	Query     string                   `json:"query"`
	Documents []jinaRerankDocument     `json:"documents"`
	TopN      int                      `json:"top_n,omitempty"`
}

// jinaRerankDocument represents a document in Jina format
type jinaRerankDocument struct {
	Text string `json:"text"`
}

// jinaRerankResponse represents a Jina API rerank response
type jinaRerankResponse struct {
	Results []jinaRerankResult `json:"results"`
}

// jinaRerankResult represents a single rerank result
type jinaRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Rerank reranks documents based on query relevance
func (p *JinaProvider) Rerank(ctx context.Context, req providers.RerankRequest) (*providers.RerankResponse, error) {
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

	// Convert documents to Jina format
	documents := make([]jinaRerankDocument, len(req.Documents))
	for i, doc := range req.Documents {
		documents[i] = jinaRerankDocument{
			Text: doc.Content,
		}
	}

	// Set default top K
	topK := req.TopK
	if topK == 0 {
		topK = len(documents)
	}

	// Build request
	reqBody := jinaRerankRequest{
		Model:     model,
		Query:     req.Query,
		Documents: documents,
		TopN:      topK,
	}

	// Marshal request body
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to marshal request",
			Err:      err,
		}
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		p.baseURL+"/rerank",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to create request",
			Err:      err,
		}
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Make API call
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to make request",
			Err:      err,
		}
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to read response",
			Err:      err,
		}
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  fmt.Sprintf("API error: status %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	// Parse response
	var jinaResp jinaRerankResponse
	if err := json.Unmarshal(respBody, &jinaResp); err != nil {
		return nil, &providers.ProviderError{
			Provider: p.Name(),
			Message:  "failed to parse response",
			Err:      err,
		}
	}

	// Convert results to our format and sort by score (descending)
	rankedDocs := make([]providers.RankedDocument, len(jinaResp.Results))
	for i, res := range jinaResp.Results {
		rankedDocs[i] = providers.RankedDocument{
			Document: req.Documents[res.Index],
			Score:    res.RelevanceScore,
			Index:    res.Index,
		}
	}

	// Sort by score descending
	sort.Slice(rankedDocs, func(i, j int) bool {
		return rankedDocs[i].Score > rankedDocs[j].Score
	})

	return &providers.RerankResponse{
		Results: rankedDocs,
	}, nil
}
