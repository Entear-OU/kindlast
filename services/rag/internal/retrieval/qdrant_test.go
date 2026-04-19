package retrieval

import (
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

func TestBuildFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters map[string]string
		want    bool // whether filter should be nil
	}{
		{
			name:    "empty filters",
			filters: map[string]string{},
			want:    true,
		},
		{
			name: "single filter",
			filters: map[string]string{
				"tier": "primary",
			},
			want: false,
		},
		{
			name: "multiple filters",
			filters: map[string]string{
				"tier":   "primary",
				"source": "gdpr",
				"topic":  "consent",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := buildFilter(tt.filters)
			if (filter == nil) != tt.want {
				t.Errorf("buildFilter() returned nil = %v, want nil = %v", filter == nil, tt.want)
			}
			if filter != nil && len(filter.Must) != len(tt.filters) {
				t.Errorf("buildFilter() created %d conditions, want %d", len(filter.Must), len(tt.filters))
			}
		})
	}
}

func TestApplyRRF(t *testing.T) {
	tests := []struct {
		name    string
		results []SearchResult
		topK    int
		want    int // expected number of results
	}{
		{
			name:    "empty results",
			results: []SearchResult{},
			topK:    5,
			want:    0,
		},
		{
			name: "single result",
			results: []SearchResult{
				{ChunkID: "chunk1", Score: 0.9},
			},
			topK: 5,
			want: 1,
		},
		{
			name: "multiple results",
			results: []SearchResult{
				{ChunkID: "chunk1", Score: 0.9},
				{ChunkID: "chunk2", Score: 0.8},
				{ChunkID: "chunk3", Score: 0.7},
			},
			topK: 5,
			want: 3,
		},
		{
			name: "duplicate chunks",
			results: []SearchResult{
				{ChunkID: "chunk1", Score: 0.9},
				{ChunkID: "chunk1", Score: 0.8},
				{ChunkID: "chunk2", Score: 0.7},
			},
			topK: 5,
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRRF(tt.results, tt.topK)
			if len(got) != tt.want {
				t.Errorf("applyRRF() returned %d results, want %d", len(got), tt.want)
			}

			// Verify scores are in descending order
			for i := 0; i < len(got)-1; i++ {
				if got[i].Score < got[i+1].Score {
					t.Errorf("applyRRF() results not sorted: score[%d] = %f < score[%d] = %f",
						i, got[i].Score, i+1, got[i+1].Score)
				}
			}
		})
	}
}

func TestSortByScore(t *testing.T) {
	tests := []struct {
		name    string
		results []SearchResult
		wantLen int
	}{
		{
			name:    "empty results",
			results: []SearchResult{},
			wantLen: 0,
		},
		{
			name: "already sorted",
			results: []SearchResult{
				{ChunkID: "chunk1", Score: 0.9},
				{ChunkID: "chunk2", Score: 0.8},
				{ChunkID: "chunk3", Score: 0.7},
			},
			wantLen: 3,
		},
		{
			name: "reverse sorted",
			results: []SearchResult{
				{ChunkID: "chunk1", Score: 0.7},
				{ChunkID: "chunk2", Score: 0.8},
				{ChunkID: "chunk3", Score: 0.9},
			},
			wantLen: 3,
		},
		{
			name: "random order",
			results: []SearchResult{
				{ChunkID: "chunk1", Score: 0.8},
				{ChunkID: "chunk2", Score: 0.9},
				{ChunkID: "chunk3", Score: 0.7},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortByScore(tt.results)

			if len(tt.results) != tt.wantLen {
				t.Errorf("sortByScore() resulted in %d items, want %d", len(tt.results), tt.wantLen)
			}

			// Verify descending order
			for i := 0; i < len(tt.results)-1; i++ {
				if tt.results[i].Score < tt.results[i+1].Score {
					t.Errorf("sortByScore() not sorted: score[%d] = %f < score[%d] = %f",
						i, tt.results[i].Score, i+1, tt.results[i+1].Score)
				}
			}
		})
	}
}

func TestExtractStringFromPayload(t *testing.T) {
	// This test would require creating Qdrant Value structs
	// For now, we'll test the logic with a simple map
	t.Run("basic functionality", func(t *testing.T) {
		// This is a placeholder test
		// In a real scenario, you would mock the Qdrant payload
		payload := make(map[string]*qdrant.Value)
		result := extractStringFromPayload(payload, "test_key")
		if result != "" {
			t.Errorf("extractStringFromPayload() = %v, want empty string", result)
		}
	})
}

func TestConvertPayloadToMap(t *testing.T) {
	// This test would require creating Qdrant Value structs
	t.Run("empty payload", func(t *testing.T) {
		payload := make(map[string]*qdrant.Value)
		result := convertPayloadToMap(payload)
		if len(result) != 0 {
			t.Errorf("convertPayloadToMap() returned %d items, want 0", len(result))
		}
	})
}

// Integration test placeholder
// This would require a running Qdrant instance
func TestQdrantClient_HybridSearch(t *testing.T) {
	t.Skip("Integration test - requires running Qdrant instance")

	// Example integration test structure:
	// client, err := NewQdrantClient("localhost", 6334)
	// if err != nil {
	//     t.Fatalf("Failed to create client: %v", err)
	// }
	// defer client.Close()
	//
	// params := SearchParams{
	//     Query:       "GDPR consent requirements",
	//     TopK:        10,
	//     RerankTopK:  20,
	//     Collections: []string{"gdpr_chunks"},
	// }
	//
	// results, err := client.HybridSearch(context.Background(), params, []float32{})
	// if err != nil {
	//     t.Fatalf("HybridSearch failed: %v", err)
	// }
	//
	// if len(results) == 0 {
	//     t.Error("Expected results, got none")
	// }
}
