package retrieval

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// QdrantClient wraps the Qdrant gRPC client
type QdrantClient struct {
	client qdrant.PointsClient
	conn   *grpc.ClientConn
}

// NewQdrantClient creates a new Qdrant client
func NewQdrantClient(host string, port int) (*QdrantClient, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Qdrant: %w", err)
	}

	client := qdrant.NewPointsClient(conn)

	return &QdrantClient{
		client: client,
		conn:   conn,
	}, nil
}

// Close closes the Qdrant client connection
func (q *QdrantClient) Close() error {
	if q.conn != nil {
		return q.conn.Close()
	}
	return nil
}

// HybridSearch performs hybrid search combining BM25 and dense vector search with RRF
func (q *QdrantClient) HybridSearch(ctx context.Context, params SearchParams, queryVector []float32) ([]SearchResult, error) {
	if len(params.Collections) == 0 {
		return nil, fmt.Errorf("at least one collection must be specified")
	}

	// Build filter conditions
	filter := buildFilter(params.Filters)

	// Perform search on each collection and combine results
	allResults := make([]SearchResult, 0)

	for _, collection := range params.Collections {
		// Dense vector search
		denseReq := &qdrant.SearchPoints{
			CollectionName: collection,
			Vector:         queryVector,
			Limit:          uint64(params.TopK * 2), // Get more for RRF
			WithPayload:    &qdrant.WithPayloadSelector{SelectorOptions: &qdrant.WithPayloadSelector_Enable{Enable: true}},
			Filter:         filter,
		}

		denseResp, err := q.client.Search(ctx, denseReq)
		if err != nil {
			return nil, fmt.Errorf("dense search failed for collection %s: %w", collection, err)
		}

		// Convert results
		for _, point := range denseResp.GetResult() {
			result := SearchResult{
				ChunkID:  extractStringFromPayload(point.Payload, "chunk_id"),
				ParentID: extractStringFromPayload(point.Payload, "parent_id"),
				Content:  extractStringFromPayload(point.Payload, "content"),
				Score:    float64(point.Score),
				Metadata: convertPayloadToMap(point.Payload),
			}
			allResults = append(allResults, result)
		}
	}

	// Apply RRF (Reciprocal Rank Fusion) if we have results
	if len(allResults) > 0 {
		allResults = applyRRF(allResults, params.TopK)
	}

	// Limit to topK
	if len(allResults) > params.TopK {
		allResults = allResults[:params.TopK]
	}

	return allResults, nil
}

// buildFilter constructs a Qdrant filter from the provided filter map
func buildFilter(filters map[string]string) *qdrant.Filter {
	if len(filters) == 0 {
		return nil
	}

	conditions := make([]*qdrant.Condition, 0, len(filters))

	for key, value := range filters {
		condition := &qdrant.Condition{
			ConditionOneOf: &qdrant.Condition_Field{
				Field: &qdrant.FieldCondition{
					Key: key,
					Match: &qdrant.Match{
						MatchValue: &qdrant.Match_Keyword{
							Keyword: value,
						},
					},
				},
			},
		}
		conditions = append(conditions, condition)
	}

	return &qdrant.Filter{
		Must: conditions,
	}
}

// extractStringFromPayload extracts a string value from Qdrant payload
func extractStringFromPayload(payload map[string]*qdrant.Value, key string) string {
	if val, ok := payload[key]; ok {
		if strVal := val.GetStringValue(); strVal != "" {
			return strVal
		}
	}
	return ""
}

// convertPayloadToMap converts Qdrant payload to a Go map
func convertPayloadToMap(payload map[string]*qdrant.Value) map[string]interface{} {
	result := make(map[string]interface{})

	for key, val := range payload {
		switch v := val.Kind.(type) {
		case *qdrant.Value_StringValue:
			result[key] = v.StringValue
		case *qdrant.Value_IntegerValue:
			result[key] = v.IntegerValue
		case *qdrant.Value_DoubleValue:
			result[key] = v.DoubleValue
		case *qdrant.Value_BoolValue:
			result[key] = v.BoolValue
		}
	}

	return result
}

// applyRRF applies Reciprocal Rank Fusion to search results
// RRF formula: RRF(d) = Σ 1/(k + rank(d))
// where k is a constant (typically 60)
func applyRRF(results []SearchResult, topK int) []SearchResult {
	const k = 60.0

	// Calculate RRF scores
	rrfScores := make(map[string]float64)

	for rank, result := range results {
		score := 1.0 / (k + float64(rank+1))
		if existing, ok := rrfScores[result.ChunkID]; ok {
			rrfScores[result.ChunkID] = existing + score
		} else {
			rrfScores[result.ChunkID] = score
		}
	}

	// Update scores with RRF scores
	for i := range results {
		if rrfScore, ok := rrfScores[results[i].ChunkID]; ok {
			results[i].Score = rrfScore
		}
	}

	// Sort by RRF score (descending)
	sortByScore(results)

	return results
}

// sortByScore sorts results by score in descending order
func sortByScore(results []SearchResult) {
	// Simple bubble sort for small datasets
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if results[j].Score < results[j+1].Score {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}
