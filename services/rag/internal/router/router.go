package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/entear/kindlast/services/rag/internal/providers"
	"github.com/sony/gobreaker"
)

var (
	// ErrAllProvidersFailed is returned when all providers have failed
	ErrAllProvidersFailed = errors.New("all providers failed")

	// ErrNoProvidersConfigured is returned when no providers are configured
	ErrNoProvidersConfigured = errors.New("no providers configured")
)

// CircuitBreakerSettings defines circuit breaker configuration
type CircuitBreakerSettings struct {
	MaxRequests       uint32        // Max requests allowed in half-open state
	Interval          time.Duration // Period for rate limit
	Timeout           time.Duration // Timeout for open state
	ReadyToTrip       func(counts gobreaker.Counts) bool
	OnStateChange     func(name string, from gobreaker.State, to gobreaker.State)
}

// DefaultCircuitBreakerSettings returns default circuit breaker settings
func DefaultCircuitBreakerSettings() CircuitBreakerSettings {
	return CircuitBreakerSettings{
		MaxRequests: 3,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Trip if failure rate is >= 50% and we have at least 5 requests
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.5
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// Log state changes (in production, use proper logging)
			fmt.Printf("Circuit breaker '%s' changed from %s to %s\n", name, from, to)
		},
	}
}

// GenerationRouter routes generation requests with circuit breaker and failover
type GenerationRouter struct {
	primary   providers.GenerationProvider
	fallback  providers.GenerationProvider
	breakers  map[string]*gobreaker.CircuitBreaker
	mu        sync.RWMutex
}

// NewGenerationRouter creates a new generation router
func NewGenerationRouter(
	primary providers.GenerationProvider,
	fallback providers.GenerationProvider,
	settings CircuitBreakerSettings,
) *GenerationRouter {
	if settings.MaxRequests == 0 {
		settings = DefaultCircuitBreakerSettings()
	}

	router := &GenerationRouter{
		primary:  primary,
		fallback: fallback,
		breakers: make(map[string]*gobreaker.CircuitBreaker),
	}

	// Create circuit breakers for each provider
	if primary != nil {
		router.breakers[primary.Name()] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          primary.Name(),
			MaxRequests:   settings.MaxRequests,
			Interval:      settings.Interval,
			Timeout:       settings.Timeout,
			ReadyToTrip:   settings.ReadyToTrip,
			OnStateChange: settings.OnStateChange,
		})
	}

	if fallback != nil {
		router.breakers[fallback.Name()] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          fallback.Name(),
			MaxRequests:   settings.MaxRequests,
			Interval:      settings.Interval,
			Timeout:       settings.Timeout,
			ReadyToTrip:   settings.ReadyToTrip,
			OnStateChange: settings.OnStateChange,
		})
	}

	return router
}

// Generate generates a response with automatic failover
func (r *GenerationRouter) Generate(ctx context.Context, req providers.GenerationRequest) (*providers.GenerationResponse, error) {
	if r.primary == nil && r.fallback == nil {
		return nil, ErrNoProvidersConfigured
	}

	var lastErr error

	// Try primary provider
	if r.primary != nil {
		breaker := r.breakers[r.primary.Name()]
		resp, err := breaker.Execute(func() (interface{}, error) {
			return r.primary.Generate(ctx, req)
		})

		if err == nil {
			return resp.(*providers.GenerationResponse), nil
		}
		lastErr = fmt.Errorf("primary provider %s failed: %w", r.primary.Name(), err)
	}

	// Try fallback provider
	if r.fallback != nil {
		breaker := r.breakers[r.fallback.Name()]
		resp, err := breaker.Execute(func() (interface{}, error) {
			return r.fallback.Generate(ctx, req)
		})

		if err == nil {
			return resp.(*providers.GenerationResponse), nil
		}
		lastErr = fmt.Errorf("fallback provider %s failed: %w", r.fallback.Name(), err)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrAllProvidersFailed, lastErr)
	}

	return nil, ErrAllProvidersFailed
}

// Stream generates a streaming response with automatic failover
func (r *GenerationRouter) Stream(ctx context.Context, req providers.GenerationRequest) (<-chan providers.StreamChunk, error) {
	if r.primary == nil && r.fallback == nil {
		return nil, ErrNoProvidersConfigured
	}

	// Try primary provider
	if r.primary != nil {
		breaker := r.breakers[r.primary.Name()]
		result, err := breaker.Execute(func() (interface{}, error) {
			return r.primary.Stream(ctx, req)
		})

		if err == nil {
			return result.(<-chan providers.StreamChunk), nil
		}
	}

	// Try fallback provider
	if r.fallback != nil {
		breaker := r.breakers[r.fallback.Name()]
		result, err := breaker.Execute(func() (interface{}, error) {
			return r.fallback.Stream(ctx, req)
		})

		if err == nil {
			return result.(<-chan providers.StreamChunk), nil
		}
	}

	return nil, ErrAllProvidersFailed
}

// GetProviderHealth returns the current state of providers
func (r *GenerationRouter) GetProviderHealth() map[string]gobreaker.State {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]gobreaker.State)
	for name, breaker := range r.breakers {
		health[name] = breaker.State()
	}
	return health
}

// EmbeddingRouter routes embedding requests with circuit breaker and failover
type EmbeddingRouter struct {
	primary   providers.EmbeddingProvider
	fallback  providers.EmbeddingProvider
	breakers  map[string]*gobreaker.CircuitBreaker
	mu        sync.RWMutex
}

// NewEmbeddingRouter creates a new embedding router
func NewEmbeddingRouter(
	primary providers.EmbeddingProvider,
	fallback providers.EmbeddingProvider,
	settings CircuitBreakerSettings,
) *EmbeddingRouter {
	if settings.MaxRequests == 0 {
		settings = DefaultCircuitBreakerSettings()
	}

	router := &EmbeddingRouter{
		primary:  primary,
		fallback: fallback,
		breakers: make(map[string]*gobreaker.CircuitBreaker),
	}

	// Create circuit breakers for each provider
	if primary != nil {
		router.breakers[primary.Name()] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          primary.Name(),
			MaxRequests:   settings.MaxRequests,
			Interval:      settings.Interval,
			Timeout:       settings.Timeout,
			ReadyToTrip:   settings.ReadyToTrip,
			OnStateChange: settings.OnStateChange,
		})
	}

	if fallback != nil {
		router.breakers[fallback.Name()] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          fallback.Name(),
			MaxRequests:   settings.MaxRequests,
			Interval:      settings.Interval,
			Timeout:       settings.Timeout,
			ReadyToTrip:   settings.ReadyToTrip,
			OnStateChange: settings.OnStateChange,
		})
	}

	return router
}

// Embed generates embeddings with automatic failover
func (r *EmbeddingRouter) Embed(ctx context.Context, req providers.EmbeddingRequest) (*providers.EmbeddingResponse, error) {
	if r.primary == nil && r.fallback == nil {
		return nil, ErrNoProvidersConfigured
	}

	var lastErr error

	// Try primary provider
	if r.primary != nil {
		breaker := r.breakers[r.primary.Name()]
		resp, err := breaker.Execute(func() (interface{}, error) {
			return r.primary.Embed(ctx, req)
		})

		if err == nil {
			return resp.(*providers.EmbeddingResponse), nil
		}
		lastErr = fmt.Errorf("primary provider %s failed: %w", r.primary.Name(), err)
	}

	// Try fallback provider
	if r.fallback != nil {
		breaker := r.breakers[r.fallback.Name()]
		resp, err := breaker.Execute(func() (interface{}, error) {
			return r.fallback.Embed(ctx, req)
		})

		if err == nil {
			return resp.(*providers.EmbeddingResponse), nil
		}
		lastErr = fmt.Errorf("fallback provider %s failed: %w", r.fallback.Name(), err)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrAllProvidersFailed, lastErr)
	}

	return nil, ErrAllProvidersFailed
}

// GetProviderHealth returns the current state of providers
func (r *EmbeddingRouter) GetProviderHealth() map[string]gobreaker.State {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]gobreaker.State)
	for name, breaker := range r.breakers {
		health[name] = breaker.State()
	}
	return health
}

// RerankRouter routes reranking requests with circuit breaker and failover
type RerankRouter struct {
	primary   providers.RerankProvider
	fallback  providers.RerankProvider
	breakers  map[string]*gobreaker.CircuitBreaker
	mu        sync.RWMutex
}

// NewRerankRouter creates a new reranking router
func NewRerankRouter(
	primary providers.RerankProvider,
	fallback providers.RerankProvider,
	settings CircuitBreakerSettings,
) *RerankRouter {
	if settings.MaxRequests == 0 {
		settings = DefaultCircuitBreakerSettings()
	}

	router := &RerankRouter{
		primary:  primary,
		fallback: fallback,
		breakers: make(map[string]*gobreaker.CircuitBreaker),
	}

	// Create circuit breakers for each provider
	if primary != nil {
		router.breakers[primary.Name()] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          primary.Name(),
			MaxRequests:   settings.MaxRequests,
			Interval:      settings.Interval,
			Timeout:       settings.Timeout,
			ReadyToTrip:   settings.ReadyToTrip,
			OnStateChange: settings.OnStateChange,
		})
	}

	if fallback != nil {
		router.breakers[fallback.Name()] = gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:          fallback.Name(),
			MaxRequests:   settings.MaxRequests,
			Interval:      settings.Interval,
			Timeout:       settings.Timeout,
			ReadyToTrip:   settings.ReadyToTrip,
			OnStateChange: settings.OnStateChange,
		})
	}

	return router
}

// Rerank reranks documents with automatic failover
func (r *RerankRouter) Rerank(ctx context.Context, req providers.RerankRequest) (*providers.RerankResponse, error) {
	if r.primary == nil && r.fallback == nil {
		return nil, ErrNoProvidersConfigured
	}

	var lastErr error

	// Try primary provider
	if r.primary != nil {
		breaker := r.breakers[r.primary.Name()]
		resp, err := breaker.Execute(func() (interface{}, error) {
			return r.primary.Rerank(ctx, req)
		})

		if err == nil {
			return resp.(*providers.RerankResponse), nil
		}
		lastErr = fmt.Errorf("primary provider %s failed: %w", r.primary.Name(), err)
	}

	// Try fallback provider
	if r.fallback != nil {
		breaker := r.breakers[r.fallback.Name()]
		resp, err := breaker.Execute(func() (interface{}, error) {
			return r.fallback.Rerank(ctx, req)
		})

		if err == nil {
			return resp.(*providers.RerankResponse), nil
		}
		lastErr = fmt.Errorf("fallback provider %s failed: %w", r.fallback.Name(), err)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrAllProvidersFailed, lastErr)
	}

	return nil, ErrAllProvidersFailed
}

// GetProviderHealth returns the current state of providers
func (r *RerankRouter) GetProviderHealth() map[string]gobreaker.State {
	r.mu.RLock()
	defer r.mu.RUnlock()

	health := make(map[string]gobreaker.State)
	for name, breaker := range r.breakers {
		health[name] = breaker.State()
	}
	return health
}

// Name returns the router name (implements provider interfaces)
func (r *GenerationRouter) Name() string {
	if r.primary != nil {
		return r.primary.Name() + "-router"
	}
	return "generation-router"
}

// Name returns the router name (implements provider interfaces)
func (r *EmbeddingRouter) Name() string {
	if r.primary != nil {
		return r.primary.Name() + "-router"
	}
	return "embedding-router"
}

// Name returns the router name (implements provider interfaces)
func (r *RerankRouter) Name() string {
	if r.primary != nil {
		return r.primary.Name() + "-router"
	}
	return "reranking-router"
}
