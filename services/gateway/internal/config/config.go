package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the gateway service
type Config struct {
	// Server configuration
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration

	// RAG Service
	RAGServiceURL string

	// Database
	PostgresDSN string

	// Redis
	RedisURL string

	// JWT
	JWTSecret            string
	JWTRefreshSecret     string
	JWTAccessExpiration  time.Duration
	JWTRefreshExpiration time.Duration

	// CORS
	CORSOrigins []string

	// Rate Limiting
	RateLimitEnabled bool
	RateLimitPerMin  int

	// Stripe
	StripeAPIKey       string
	StripeWebhookSecret string

	// Environment
	Environment string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Port:                 getEnvAsInt("PORT", 8080),
		ReadTimeout:          getEnvAsDuration("READ_TIMEOUT", 15*time.Second),
		WriteTimeout:         getEnvAsDuration("WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:          getEnvAsDuration("IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout:      getEnvAsDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		RAGServiceURL:        getEnv("RAG_SERVICE_URL", "http://rag-service:8080"),
		PostgresDSN:          getEnv("POSTGRES_DSN", ""),
		RedisURL:             getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:            getEnv("JWT_SECRET", ""),
		JWTRefreshSecret:     getEnv("JWT_REFRESH_SECRET", ""),
		JWTAccessExpiration:  getEnvAsDuration("JWT_ACCESS_EXPIRATION", 24*time.Hour),
		JWTRefreshExpiration: getEnvAsDuration("JWT_REFRESH_EXPIRATION", 30*24*time.Hour),
		CORSOrigins:          getEnvAsSlice("CORS_ORIGINS", ",", []string{"http://localhost:3000"}),
		RateLimitEnabled:     getEnvAsBool("RATE_LIMIT_ENABLED", true),
		RateLimitPerMin:      getEnvAsInt("RATE_LIMIT_PER_MIN", 60),
		StripeAPIKey:         getEnv("STRIPE_API_KEY", ""),
		StripeWebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),
		Environment:          getEnv("ENVIRONMENT", "development"),
	}

	// Validate required fields
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if cfg.JWTRefreshSecret == "" {
		return nil, fmt.Errorf("JWT_REFRESH_SECRET is required")
	}

	return cfg, nil
}

// Helper functions to parse environment variables

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.ParseBool(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}

func getEnvAsSlice(key, separator string, defaultValue []string) []string {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}
	return strings.Split(valueStr, separator)
}
