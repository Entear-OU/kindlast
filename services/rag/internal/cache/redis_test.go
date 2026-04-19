package cache

import (
	"context"
	"testing"
	"time"

	"github.com/entear/kindlast/services/rag/internal/retrieval"
	"github.com/redis/go-redis/v9"
	"github.com/alicebob/miniredis/v2"
)

func setupMiniRedis(t *testing.T) (*RedisCache, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	cache := &RedisCache{
		client: redis.NewClient(&redis.Options{
			Addr: mr.Addr(),
		}),
		ttl: DefaultTTL,
	}

	return cache, mr
}

func TestNewRedisCache(t *testing.T) {
	t.Run("invalid address", func(t *testing.T) {
		_, err := NewRedisCache("invalid:99999", "", 0)
		if err == nil {
			t.Error("Expected error for invalid address, got nil")
		}
	})
}

func TestRedisCache_SetAndGet(t *testing.T) {
	cache, mr := setupMiniRedis(t)
	defer mr.Close()
	defer cache.Close()

	ctx := context.Background()

	t.Run("cache miss", func(t *testing.T) {
		result, err := cache.Get(ctx, "nonexistent query", nil)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if result != nil {
			t.Errorf("Expected nil result for cache miss, got %v", result)
		}
	})

	t.Run("set and get", func(t *testing.T) {
		query := "What are GDPR consent requirements?"
		params := map[string]string{
			"tier":   "primary",
			"source": "gdpr",
		}
		response := "GDPR requires explicit consent..."
		citations := []retrieval.Citation{
			{
				ID:         "citation1",
				SourceName: "GDPR Article 7",
				SourceURL:  "https://example.com/gdpr/article-7",
				Excerpt:    "Consent must be explicit...",
				Tier:       "primary",
			},
		}

		err := cache.Set(ctx, query, params, response, citations)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}

		cached, err := cache.Get(ctx, query, params)
		if err != nil {
			t.Fatalf("Failed to get from cache: %v", err)
		}

		if cached == nil {
			t.Fatal("Expected cached result, got nil")
		}

		if cached.Query != query {
			t.Errorf("Expected query '%s', got '%s'", query, cached.Query)
		}

		if cached.Response != response {
			t.Errorf("Expected response '%s', got '%s'", response, cached.Response)
		}

		if len(cached.Citations) != 1 {
			t.Fatalf("Expected 1 citation, got %d", len(cached.Citations))
		}

		if cached.Citations[0].SourceName != "GDPR Article 7" {
			t.Errorf("Expected source 'GDPR Article 7', got '%s'", cached.Citations[0].SourceName)
		}
	})

	t.Run("different params = different keys", func(t *testing.T) {
		query := "What are GDPR requirements?"
		params1 := map[string]string{"tier": "primary"}
		params2 := map[string]string{"tier": "secondary"}

		err := cache.Set(ctx, query, params1, "Response 1", nil)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}

		err = cache.Set(ctx, query, params2, "Response 2", nil)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}

		cached1, _ := cache.Get(ctx, query, params1)
		cached2, _ := cache.Get(ctx, query, params2)

		if cached1.Response == cached2.Response {
			t.Error("Expected different responses for different params")
		}
	})
}

func TestRedisCache_Delete(t *testing.T) {
	cache, mr := setupMiniRedis(t)
	defer mr.Close()
	defer cache.Close()

	ctx := context.Background()

	query := "test query"
	params := map[string]string{"tier": "primary"}

	err := cache.Set(ctx, query, params, "test response", nil)
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	// Verify it exists
	cached, err := cache.Get(ctx, query, params)
	if err != nil || cached == nil {
		t.Fatal("Cache entry should exist")
	}

	// Delete it
	err = cache.Delete(ctx, query, params)
	if err != nil {
		t.Fatalf("Failed to delete from cache: %v", err)
	}

	// Verify it's gone
	cached, err = cache.Get(ctx, query, params)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cached != nil {
		t.Error("Cache entry should be deleted")
	}
}

func TestRedisCache_Invalidate(t *testing.T) {
	cache, mr := setupMiniRedis(t)
	defer mr.Close()
	defer cache.Close()

	ctx := context.Background()

	// Set multiple entries
	queries := []string{"query1", "query2", "query3"}
	for _, q := range queries {
		err := cache.Set(ctx, q, nil, "response", nil)
		if err != nil {
			t.Fatalf("Failed to set cache: %v", err)
		}
	}

	// Invalidate all
	err := cache.Invalidate(ctx, "*")
	if err != nil {
		t.Fatalf("Failed to invalidate cache: %v", err)
	}

	// Verify all are gone
	for _, q := range queries {
		cached, err := cache.Get(ctx, q, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if cached != nil {
			t.Errorf("Cache entry for '%s' should be invalidated", q)
		}
	}
}

func TestRedisCache_TTL(t *testing.T) {
	cache, mr := setupMiniRedis(t)
	defer mr.Close()
	defer cache.Close()

	ctx := context.Background()

	// Set a short TTL
	cache.SetTTL(1 * time.Second)

	query := "test query"
	err := cache.Set(ctx, query, nil, "test response", nil)
	if err != nil {
		t.Fatalf("Failed to set cache: %v", err)
	}

	// Verify it exists
	cached, err := cache.Get(ctx, query, nil)
	if err != nil || cached == nil {
		t.Fatal("Cache entry should exist")
	}

	// Fast-forward time in miniredis
	mr.FastForward(2 * time.Second)

	// Verify it's expired
	cached, err = cache.Get(ctx, query, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if cached != nil {
		t.Error("Cache entry should be expired")
	}
}

func TestRedisCache_Ping(t *testing.T) {
	cache, mr := setupMiniRedis(t)
	defer mr.Close()
	defer cache.Close()

	ctx := context.Background()

	err := cache.Ping(ctx)
	if err != nil {
		t.Errorf("Ping should succeed, got error: %v", err)
	}
}

func TestGenerateCacheKey(t *testing.T) {
	tests := []struct {
		name    string
		query1  string
		params1 map[string]string
		query2  string
		params2 map[string]string
		wantSame bool
	}{
		{
			name:    "same query and params",
			query1:  "test query",
			params1: map[string]string{"tier": "primary"},
			query2:  "test query",
			params2: map[string]string{"tier": "primary"},
			wantSame: true,
		},
		{
			name:    "different query",
			query1:  "query 1",
			params1: map[string]string{"tier": "primary"},
			query2:  "query 2",
			params2: map[string]string{"tier": "primary"},
			wantSame: false,
		},
		{
			name:    "different params",
			query1:  "test query",
			params1: map[string]string{"tier": "primary"},
			query2:  "test query",
			params2: map[string]string{"tier": "secondary"},
			wantSame: false,
		},
		{
			name:    "nil vs empty params",
			query1:  "test query",
			params1: nil,
			query2:  "test query",
			params2: map[string]string{},
			wantSame: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := generateCacheKey(tt.query1, tt.params1)
			key2 := generateCacheKey(tt.query2, tt.params2)

			if (key1 == key2) != tt.wantSame {
				t.Errorf("generateCacheKey() keys equal = %v, want %v", key1 == key2, tt.wantSame)
			}
		})
	}
}

func TestHashParams(t *testing.T) {
	tests := []struct {
		name     string
		params1  map[string]string
		params2  map[string]string
		wantSame bool
	}{
		{
			name:     "empty params",
			params1:  map[string]string{},
			params2:  map[string]string{},
			wantSame: true,
		},
		{
			name:     "same params",
			params1:  map[string]string{"a": "1", "b": "2"},
			params2:  map[string]string{"a": "1", "b": "2"},
			wantSame: true,
		},
		{
			name:     "different order same content",
			params1:  map[string]string{"a": "1", "b": "2"},
			params2:  map[string]string{"b": "2", "a": "1"},
			wantSame: true, // JSON marshaling should produce consistent order
		},
		{
			name:     "different params",
			params1:  map[string]string{"a": "1"},
			params2:  map[string]string{"a": "2"},
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := hashParams(tt.params1)
			hash2 := hashParams(tt.params2)

			if (hash1 == hash2) != tt.wantSame {
				t.Errorf("hashParams() hashes equal = %v, want %v", hash1 == hash2, tt.wantSame)
			}
		})
	}
}
