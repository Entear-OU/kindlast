package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/entear/kindlast/services/rag/internal/retrieval"
	"github.com/redis/go-redis/v9"
)

const (
	// DefaultTTL is the default cache TTL (24 hours)
	DefaultTTL = 24 * time.Hour

	// KeyPrefix is the prefix for all RAG cache keys
	KeyPrefix = "rag:query:"
)

// RedisCache handles caching of RAG responses
type RedisCache struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// NewRedisCache creates a new Redis cache client
func NewRedisCache(addr string, password string, db int) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisCache{
		client: client,
		ttl:    DefaultTTL,
	}, nil
}

// NewRedisClusterCache creates a new Redis cluster cache client
func NewRedisClusterCache(addrs []string, password string) (*RedisCache, error) {
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        addrs,
		Password:     password,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis cluster: %w", err)
	}

	// Wrap cluster client as universal client
	return &RedisCache{
		client: client,
		ttl:    DefaultTTL,
	}, nil
}

// Close closes the Redis client connection
func (r *RedisCache) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// SetTTL sets a custom TTL for cache entries
func (r *RedisCache) SetTTL(ttl time.Duration) {
	r.ttl = ttl
}

// Get retrieves a cached response from Redis
func (r *RedisCache) Get(ctx context.Context, query string, params map[string]string) (*retrieval.CachedResponse, error) {
	key := generateCacheKey(query, params)

	data, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil // Cache miss
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get from cache: %w", err)
	}

	var cached retrieval.CachedResponse
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached data: %w", err)
	}

	return &cached, nil
}

// Set stores a response in Redis cache
func (r *RedisCache) Set(ctx context.Context, query string, params map[string]string, response string, citations []retrieval.Citation) error {
	key := generateCacheKey(query, params)

	cached := retrieval.CachedResponse{
		Query:      query,
		Response:   response,
		Citations:  citations,
		Timestamp:  time.Now(),
		ParamsHash: hashParams(params),
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	if err := r.client.Set(ctx, key, data, r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	return nil
}

// Delete removes a cached entry
func (r *RedisCache) Delete(ctx context.Context, query string, params map[string]string) error {
	key := generateCacheKey(query, params)

	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete from cache: %w", err)
	}

	return nil
}

// Invalidate removes all cache entries matching a pattern
func (r *RedisCache) Invalidate(ctx context.Context, pattern string) error {
	fullPattern := KeyPrefix + pattern

	iter := r.client.Scan(ctx, 0, fullPattern, 0).Iterator()
	for iter.Next(ctx) {
		if err := r.client.Del(ctx, iter.Val()).Err(); err != nil {
			return fmt.Errorf("failed to delete key %s: %w", iter.Val(), err)
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan error: %w", err)
	}

	return nil
}

// GetStats returns cache statistics
func (r *RedisCache) GetStats(ctx context.Context) (map[string]interface{}, error) {
	info, err := r.client.Info(ctx, "stats").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	dbSize, err := r.client.DBSize(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get db size: %w", err)
	}

	// Count RAG-specific keys
	ragKeyCount := 0
	iter := r.client.Scan(ctx, 0, KeyPrefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		ragKeyCount++
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to count RAG keys: %w", err)
	}

	return map[string]interface{}{
		"total_keys": dbSize,
		"rag_keys":   ragKeyCount,
		"info":       info,
	}, nil
}

// generateCacheKey creates a cache key from query and parameters
func generateCacheKey(query string, params map[string]string) string {
	hash := hashQueryAndParams(query, params)
	return KeyPrefix + hash
}

// hashQueryAndParams creates a hash from query and parameters
func hashQueryAndParams(query string, params map[string]string) string {
	h := sha256.New()
	h.Write([]byte(query))

	// Sort params for consistent hashing
	paramsHash := hashParams(params)
	h.Write([]byte(paramsHash))

	return hex.EncodeToString(h.Sum(nil))
}

// hashParams creates a consistent hash from parameters
func hashParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}

	// Convert to JSON for consistent ordering
	data, _ := json.Marshal(params)
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// Ping tests the Redis connection
func (r *RedisCache) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// FlushAll removes all keys (use with caution)
func (r *RedisCache) FlushAll(ctx context.Context) error {
	return r.client.FlushAll(ctx).Err()
}
