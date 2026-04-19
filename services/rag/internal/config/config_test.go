package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Set required environment variables
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	os.Setenv("OPENAI_API_KEY", "test-key")
	os.Setenv("COHERE_API_KEY", "test-key")
	defer func() {
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("COHERE_API_KEY")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Test defaults
	if cfg.Server.Port != "8080" {
		t.Errorf("Server.Port = %v, want 8080", cfg.Server.Port)
	}
	if cfg.Qdrant.Host != "localhost" {
		t.Errorf("Qdrant.Host = %v, want localhost", cfg.Qdrant.Host)
	}
	if cfg.Providers.Generation.Primary != "anthropic" {
		t.Errorf("Providers.Generation.Primary = %v, want anthropic", cfg.Providers.Generation.Primary)
	}
}

func TestLoadWithCustomValues(t *testing.T) {
	// Set custom environment variables
	os.Setenv("PORT", "9090")
	os.Setenv("QDRANT_HOST", "qdrant.example.com")
	os.Setenv("GENERATION_PRIMARY", "openai")
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	os.Setenv("OPENAI_API_KEY", "test-key")
	os.Setenv("COHERE_API_KEY", "test-key")
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("QDRANT_HOST")
		os.Unsetenv("GENERATION_PRIMARY")
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("COHERE_API_KEY")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.Port != "9090" {
		t.Errorf("Server.Port = %v, want 9090", cfg.Server.Port)
	}
	if cfg.Qdrant.Host != "qdrant.example.com" {
		t.Errorf("Qdrant.Host = %v, want qdrant.example.com", cfg.Qdrant.Host)
	}
	if cfg.Providers.Generation.Primary != "openai" {
		t.Errorf("Providers.Generation.Primary = %v, want openai", cfg.Providers.Generation.Primary)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		cleanup func()
		wantErr bool
	}{
		{
			name: "missing API keys",
			setup: func() {
				os.Unsetenv("ANTHROPIC_API_KEY")
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("COHERE_API_KEY")
			},
			cleanup: func() {
				os.Setenv("ANTHROPIC_API_KEY", "test-key")
				os.Setenv("OPENAI_API_KEY", "test-key")
				os.Setenv("COHERE_API_KEY", "test-key")
			},
			wantErr: true,
		},
		{
			name: "valid config",
			setup: func() {
				os.Setenv("ANTHROPIC_API_KEY", "test-key")
				os.Setenv("OPENAI_API_KEY", "test-key")
				os.Setenv("COHERE_API_KEY", "test-key")
			},
			cleanup: func() {
				os.Unsetenv("ANTHROPIC_API_KEY")
				os.Unsetenv("OPENAI_API_KEY")
				os.Unsetenv("COHERE_API_KEY")
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer tt.cleanup()

			_, err := Load()
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetDurationEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		value        string
		defaultValue time.Duration
		want         time.Duration
	}{
		{
			name:         "valid duration",
			key:          "TEST_DURATION",
			value:        "30s",
			defaultValue: 10 * time.Second,
			want:         30 * time.Second,
		},
		{
			name:         "invalid duration uses default",
			key:          "TEST_DURATION",
			value:        "invalid",
			defaultValue: 10 * time.Second,
			want:         10 * time.Second,
		},
		{
			name:         "empty uses default",
			key:          "TEST_DURATION",
			value:        "",
			defaultValue: 10 * time.Second,
			want:         10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			}

			got := getDurationEnv(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getDurationEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetIntEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		value        string
		defaultValue int
		want         int
	}{
		{
			name:         "valid int",
			key:          "TEST_INT",
			value:        "42",
			defaultValue: 10,
			want:         42,
		},
		{
			name:         "invalid int uses default",
			key:          "TEST_INT",
			value:        "invalid",
			defaultValue: 10,
			want:         10,
		},
		{
			name:         "empty uses default",
			key:          "TEST_INT",
			value:        "",
			defaultValue: 10,
			want:         10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != "" {
				os.Setenv(tt.key, tt.value)
				defer os.Unsetenv(tt.key)
			}

			got := getIntEnv(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getIntEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}
