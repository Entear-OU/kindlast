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
	Provider        string // "anthropic", "openai", or "local"
	Fallback        string
	AnthropicAPIKey string
	AnthropicModel  string
	OpenAIAPIKey    string
	OpenAIBaseURL   string // For local models (LMstudio)
	OpenAIModel     string
	Temperature     float32
	MaxTokens       int
	Timeout         time.Duration
}

// EmbeddingConfig holds embedding provider configuration
type EmbeddingConfig struct {
	Provider       string // "openai", "cohere", or "local"
	Fallback       string
	OpenAIAPIKey   string
	OpenAIBaseURL  string // For local models (LMstudio)
	OpenAIModel    string
	CohereAPIKey   string
	CohereModel    string
	Dimensions     int
	Timeout        time.Duration
}

// RerankingConfig holds reranking provider configuration
type RerankingConfig struct {
	Enabled      bool   // Whether reranking is enabled
	Provider     string // "cohere" or "jina"
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
				Provider:        getEnv("RAG_PROVIDER", "anthropic"),
				Fallback:        getEnv("RAG_FALLBACK", ""),
				AnthropicAPIKey: getEnv("ANTHROPIC_API_KEY", ""),
				AnthropicModel:  getEnv("ANTHROPIC_MODEL", "claude-sonnet-4-5-20250929"),
				OpenAIAPIKey:    getEnv("OPENAI_API_KEY", ""),
				OpenAIBaseURL:   getEnv("OPENAI_API_BASE_URL", ""), // For LMstudio: http://host.docker.internal:1234/v1
				OpenAIModel:     getEnv("RAG_MODEL", getEnv("OPENAI_MODEL", "gpt-4o")),
				Temperature:     getFloat32Env("RAG_TEMPERATURE", 0.3),
				MaxTokens:       getIntEnv("RAG_MAX_TOKENS", 4096),
				Timeout:         getDurationEnv("RAG_TIMEOUT", 60*time.Second),
			},
			Embedding: EmbeddingConfig{
				Provider:     getEnv("EMBEDDING_PROVIDER", "openai"),
				Fallback:     getEnv("EMBEDDING_FALLBACK", ""),
				OpenAIAPIKey: getEnv("OPENAI_API_KEY", ""),
				OpenAIBaseURL: getEnv("EMBEDDING_BASE_URL",
					getEnv("OPENAI_API_BASE_URL", "")), // Fallback to same as generation
				OpenAIModel:  getEnv("EMBEDDING_MODEL", "text-embedding-3-large"),
				CohereAPIKey: getEnv("COHERE_API_KEY", ""),
				CohereModel:  getEnv("COHERE_EMBEDDING_MODEL", "embed-multilingual-v3.0"),
				Dimensions:   getIntEnv("EMBEDDING_DIMENSION",
					getIntEnv("EMBEDDING_DIMENSIONS", 3072)),
				Timeout:      getDurationEnv("EMBEDDING_TIMEOUT", 30*time.Second),
			},
			Reranking: RerankingConfig{
				Enabled:      getBoolEnv("RERANK_ENABLED", true),
				Provider:     getEnv("RERANK_PROVIDER", "cohere"),
				Fallback:     getEnv("RERANK_FALLBACK", "jina"),
				CohereAPIKey: getEnv("COHERE_API_KEY", ""),
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

	// Provider validation - only required if not using local providers
	gen := c.Providers.Generation
	if gen.Provider == "local" {
		if gen.OpenAIBaseURL == "" {
			return fmt.Errorf("OPENAI_API_BASE_URL is required when using local generation provider")
		}
	} else {
		// Cloud providers require API keys
		if gen.Provider == "anthropic" && gen.AnthropicAPIKey == "" {
			return fmt.Errorf("ANTHROPIC_API_KEY is required when using anthropic provider")
		}
		if gen.Provider == "openai" && gen.OpenAIAPIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY is required when using openai provider")
		}
	}

	emb := c.Providers.Embedding
	if emb.Provider == "local" {
		if emb.OpenAIBaseURL == "" {
			return fmt.Errorf("EMBEDDING_BASE_URL or OPENAI_API_BASE_URL is required when using local embedding provider")
		}
	} else {
		// Cloud providers require API keys
		if emb.Provider == "openai" && emb.OpenAIAPIKey == "" {
			return fmt.Errorf("OPENAI_API_KEY is required when using openai embedding provider")
		}
		if emb.Provider == "cohere" && emb.CohereAPIKey == "" {
			return fmt.Errorf("COHERE_API_KEY is required when using cohere embedding provider")
		}
	}

	// Reranking validation - only if enabled
	if c.Providers.Reranking.Enabled {
		rerank := c.Providers.Reranking
		if rerank.Provider == "cohere" && rerank.CohereAPIKey == "" {
			return fmt.Errorf("COHERE_API_KEY is required when reranking is enabled with cohere provider")
		}
		if rerank.Provider == "jina" && rerank.JinaAPIKey == "" {
			return fmt.Errorf("JINA_API_KEY is required when reranking is enabled with jina provider")
		}
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

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
