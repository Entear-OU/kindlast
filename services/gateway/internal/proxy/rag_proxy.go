package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/sony/gobreaker"
)

// RAGProxy handles reverse proxy to RAG service with circuit breaker
type RAGProxy struct {
	serviceURL    string
	httpClient    *http.Client
	circuitBreaker *gobreaker.CircuitBreaker
	logger        *slog.Logger
}

// NewRAGProxy creates a new RAG proxy instance
func NewRAGProxy(ragServiceURL string, logger *slog.Logger) *RAGProxy {
	// Configure circuit breaker
	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "RAG-Service",
		MaxRequests: 3,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			logger.Warn("circuit breaker state changed",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
		},
	})

	return &RAGProxy{
		serviceURL: strings.TrimSuffix(ragServiceURL, "/"),
		httpClient: &http.Client{
			Timeout: 2 * time.Minute, // Allow time for streaming responses
		},
		circuitBreaker: cb,
		logger:        logger,
	}
}

// ProxyRequest proxies a request to the RAG service
func (p *RAGProxy) ProxyRequest(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := middleware.GetUser(r.Context())
	if !ok {
		p.respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Read request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		p.logger.Error("failed to read request body", slog.String("error", err.Error()))
		p.respondError(w, http.StatusBadRequest, "Failed to read request body", "BAD_REQUEST")
		return
	}
	defer r.Body.Close()

	// Parse request to inject user context
	var requestData map[string]interface{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
			p.logger.Error("failed to parse request body", slog.String("error", err.Error()))
			p.respondError(w, http.StatusBadRequest, "Invalid JSON in request body", "BAD_REQUEST")
			return
		}
	} else {
		requestData = make(map[string]interface{})
	}

	// Inject user context into request
	requestData["user_id"] = user.ID
	requestData["user_plan"] = user.Plan

	// Re-encode request body
	modifiedBody, err := json.Marshal(requestData)
	if err != nil {
		p.logger.Error("failed to marshal modified request", slog.String("error", err.Error()))
		p.respondError(w, http.StatusInternalServerError, "Failed to prepare request", "INTERNAL_ERROR")
		return
	}

	// Check if this is a streaming request (SSE)
	isStreaming := strings.Contains(r.Header.Get("Accept"), "text/event-stream")

	// Execute request through circuit breaker
	_, err = p.circuitBreaker.Execute(func() (interface{}, error) {
		if isStreaming {
			return nil, p.proxyStreamingRequest(w, r, modifiedBody, user.ID)
		}
		return nil, p.proxyRegularRequest(w, r, modifiedBody, user.ID)
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			p.logger.Error("circuit breaker open", slog.String("user_id", user.ID))
			p.respondError(w, http.StatusServiceUnavailable, "RAG service temporarily unavailable", "SERVICE_UNAVAILABLE")
			return
		}
		// Error already handled in proxy methods
		return
	}
}

// proxyRegularRequest handles non-streaming requests
func (p *RAGProxy) proxyRegularRequest(w http.ResponseWriter, r *http.Request, body []byte, userID string) error {
	// Create target URL
	targetURL := fmt.Sprintf("%s%s", p.serviceURL, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL = fmt.Sprintf("%s?%s", targetURL, r.URL.RawQuery)
	}

	// Create new request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		p.logger.Error("failed to create proxy request", slog.String("error", err.Error()))
		p.respondError(w, http.StatusInternalServerError, "Failed to create proxy request", "INTERNAL_ERROR")
		return err
	}

	// Copy headers
	p.copyHeaders(proxyReq.Header, r.Header)
	proxyReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		p.logger.Error("proxy request failed",
			slog.String("error", err.Error()),
			slog.String("url", targetURL),
			slog.String("user_id", userID),
		)
		p.respondError(w, http.StatusBadGateway, "Failed to connect to RAG service", "SERVICE_ERROR")
		return err
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		p.logger.Error("failed to copy response body", slog.String("error", err.Error()))
		return err
	}

	return nil
}

// proxyStreamingRequest handles SSE streaming requests
func (p *RAGProxy) proxyStreamingRequest(w http.ResponseWriter, r *http.Request, body []byte, userID string) error {
	// Create target URL
	targetURL := fmt.Sprintf("%s%s", p.serviceURL, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL = fmt.Sprintf("%s?%s", targetURL, r.URL.RawQuery)
	}

	// Create new request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		p.logger.Error("failed to create proxy request", slog.String("error", err.Error()))
		p.respondError(w, http.StatusInternalServerError, "Failed to create proxy request", "INTERNAL_ERROR")
		return err
	}

	// Copy headers
	p.copyHeaders(proxyReq.Header, r.Header)
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Accept", "text/event-stream")

	// Execute request
	resp, err := p.httpClient.Do(proxyReq)
	if err != nil {
		p.logger.Error("proxy streaming request failed",
			slog.String("error", err.Error()),
			slog.String("url", targetURL),
			slog.String("user_id", userID),
		)
		p.respondError(w, http.StatusBadGateway, "Failed to connect to RAG service", "SERVICE_ERROR")
		return err
	}
	defer resp.Body.Close()

	// Check if response is actually SSE
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		p.logger.Warn("expected SSE response but got different content type",
			slog.String("content_type", contentType),
		)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Copy other response headers
	for key, values := range resp.Header {
		if key != "Content-Type" && key != "Cache-Control" && key != "Connection" {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Get flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		p.logger.Error("response writer does not support flushing")
		return fmt.Errorf("streaming not supported")
	}

	// Stream response with flushing
	buffer := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			_, writeErr := w.Write(buffer[:n])
			if writeErr != nil {
				p.logger.Error("failed to write streaming response", slog.String("error", writeErr.Error()))
				return writeErr
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			p.logger.Error("error reading streaming response", slog.String("error", err.Error()))
			return err
		}
	}

	return nil
}

// copyHeaders copies relevant headers from source to destination
func (p *RAGProxy) copyHeaders(dst, src http.Header) {
	// Headers to copy
	headersToCopy := []string{
		"Authorization",
		"X-Request-ID",
		"User-Agent",
		"Accept-Language",
	}

	for _, header := range headersToCopy {
		if value := src.Get(header); value != "" {
			dst.Set(header, value)
		}
	}
}

// respondError writes an error response
func (p *RAGProxy) respondError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    code,
	})
}

// ReverseProxy creates a simple reverse proxy (alternative implementation using httputil)
func (p *RAGProxy) ReverseProxy() (*httputil.ReverseProxy, error) {
	target, err := url.Parse(p.serviceURL)
	if err != nil {
		return nil, fmt.Errorf("invalid RAG service URL: %w", err)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		p.logger.Error("reverse proxy error",
			slog.String("error", err.Error()),
			slog.String("path", r.URL.Path),
		)
		p.respondError(w, http.StatusBadGateway, "RAG service error", "SERVICE_ERROR")
	}

	return proxy, nil
}
