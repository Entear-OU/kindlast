package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Qdrant   QdrantConfig
	Redis    RedisConfig
	Postgres PostgresConfig
	Providers ProvidersConfig
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	CORSOrigins     []string
}

// QdrantConfig holds Qdrant vector database configuration
type QdrantConfig struct {
	Host       string
	Port       int
	APIKey     string
	Collection string
	Timeout    time.Duration
}

// RedisConfig holds Redis cache configuration
type RedisConfig struct {
	URL      string
	Password string
	DB       int
	TTL      time.Duration
}

// PostgresConfig holds PostgreSQL configuration
type PostgresConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// ProvidersConfig holds AI provider configuration
type ProvidersConfig struct {
	Generation GenerationConfig
	Embedding  EmbeddingConfig
	Reranking  RerankingConfig
}

// GenerationConfig holds generation provider configuration
type GenerationConfig struct {
	Primary         string // "anthropic" or "openai"
	Fallback        string
	AnthropicAPIKey string
	AnthropicModel  string
	OpenAIAPIKey    string
	OpenAIModel     string
	Temperature     float32
	MaxTokens       int
	Timeout         time.Duration
}

// EmbeddingConfig holds embedding provider configuration
type EmbeddingConfig struct {
	Primary        string // "openai" or "cohere"
	Fallback       string
	OpenAIAPIKey   string
	OpenAIModel    string
	CohereAPIKey   string
	CohereModel    string
	Dimensions     int
	Timeout        time.Duration
}

// RerankingConfig holds reranking provider configuration
type RerankingConfig struct {
	Primary      string // "cohere" or "jina"
	Fallback     string
	CohereAPIKey string
	CohereModel  string
	JinaAPIKey   string
	JinaModel    string
	TopN         int
	Timeout      time.Duration
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:            getEnv("PORT", "8080"),
			ReadTimeout:     getDurationEnv("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDurationEnv("SERVER_WRITE_TIMEOUT", 15*time.Second),
			ShutdownTimeout: getDurationEnv("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
			CORSOrigins:     []string{getEnv("CORS_ORIGINS", "http://localhost:3000")},
		},
		Qdrant: QdrantConfig{
			Host:       getEnv("QDRANT_HOST", "localhost"),
			Port:       getIntEnv("QDRANT_PORT", 6333),
			APIKey:     getEnv("QDRANT_API_KEY", ""),
			Collection: getEnv("QDRANT_COLLECTION", "kindlast_docs"),
			Timeout:    getDurationEnv("QDRANT_TIMEOUT", 30*time.Second),
		},
		Redis: RedisConfig{
			URL:      getEnv("REDIS_URL", "redis://localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getIntEnv("REDIS_DB", 0),
			TTL:      getDurationEnv("REDIS_TTL", 24*time.Hour),
		},
		Postgres: PostgresConfig{
			DSN:             getEnv("POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/kindlast?sslmode=disable"),
			MaxOpenConns:    getIntEnv("POSTGRES_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getIntEnv("POSTGRES_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: getDurationEnv("POSTGRES_CONN_MAX_LIFETIME", 5*time.Minute),
		},
		Providers: ProvidersConfig{
			Generation: GenerationConfig{
				Primary:         getEnv("GENERATION_PRIMARY", "anthropic"),
				Fallback:        getEnv("GENERATION_FALLBACK", "openai"),
				AnthropicAPIKey: requireEnv("ANTHROPIC_API_KEY"),
				AnthropicModel:  getEnv("ANTHROPIC_MODEL", "claude-sonnet-4-5-20250929"),
				OpenAIAPIKey:    requireEnv("OPENAI_API_KEY"),
				OpenAIModel:     getEnv("OPENAI_MODEL", "gpt-4o"),
				Temperature:     getFloat32Env("GENERATION_TEMPERATURE", 0.3),
				MaxTokens:       getIntEnv("GENERATION_MAX_TOKENS", 4096),
				Timeout:         getDurationEnv("GENERATION_TIMEOUT", 60*time.Second),
			},
			Embedding: EmbeddingConfig{
				Primary:      getEnv("EMBEDDING_PRIMARY", "openai"),
				Fallback:     getEnv("EMBEDDING_FALLBACK", "cohere"),
				OpenAIAPIKey: requireEnv("OPENAI_API_KEY"),
				OpenAIModel:  getEnv("OPENAI_EMBEDDING_MODEL", "text-embedding-3-large"),
				CohereAPIKey: requireEnv("COHERE_API_KEY"),
				CohereModel:  getEnv("COHERE_EMBEDDING_MODEL", "embed-multilingual-v3.0"),
				Dimensions:   getIntEnv("EMBEDDING_DIMENSIONS", 3072),
				Timeout:      getDurationEnv("EMBEDDING_TIMEOUT", 30*time.Second),
			},
			Reranking: RerankingConfig{
				Primary:      getEnv("RERANKING_PRIMARY", "cohere"),
				Fallback:     getEnv("RERANKING_FALLBACK", "jina"),
				CohereAPIKey: requireEnv("COHERE_API_KEY"),
				CohereModel:  getEnv("COHERE_RERANKING_MODEL", "rerank-v3.5"),
				JinaAPIKey:   getEnv("JINA_API_KEY", ""),
				JinaModel:    getEnv("JINA_RERANKING_MODEL", "jina-reranker-v2-base-multilingual"),
				TopN:         getIntEnv("RERANKING_TOP_N", 5),
				Timeout:      getDurationEnv("RERANKING_TIMEOUT", 10*time.Second),
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Server validation
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}

	// Qdrant validation
	if c.Qdrant.Host == "" {
		return fmt.Errorf("qdrant host is required")
	}
	if c.Qdrant.Collection == "" {
		return fmt.Errorf("qdrant collection is required")
	}

	// Redis validation
	if c.Redis.URL == "" {
		return fmt.Errorf("redis URL is required")
	}

	// Postgres validation
	if c.Postgres.DSN == "" {
		return fmt.Errorf("postgres DSN is required")
	}

	// Provider validation
	if c.Providers.Generation.AnthropicAPIKey == "" && c.Providers.Generation.OpenAIAPIKey == "" {
		return fmt.Errorf("at least one generation provider API key is required")
	}
	if c.Providers.Embedding.OpenAIAPIKey == "" && c.Providers.Embedding.CohereAPIKey == "" {
		return fmt.Errorf("at least one embedding provider API key is required")
	}
	if c.Providers.Reranking.CohereAPIKey == "" && c.Providers.Reranking.JinaAPIKey == "" {
		return fmt.Errorf("at least one reranking provider API key is required")
	}

	return nil
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func requireEnv(key string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	// Return empty string, validation will catch this
	return ""
}

func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getFloat32Env(key string, defaultValue float32) float32 {
	if value := os.Getenv(key); value != "" {
		if floatValue, err := strconv.ParseFloat(value, 32); err == nil {
			return float32(floatValue)
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
