package freemium

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// UserIDKey is the context key for user ID
	UserIDKey contextKey = "userID"
	// PlanKey is the context key for user plan
	PlanKey contextKey = "plan"
)

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error      string `json:"error"`
	Message    string `json:"message,omitempty"`
	Plan       string `json:"plan,omitempty"`
	UpgradeURL string `json:"upgrade_url,omitempty"`
}

// RAGResponse represents a simplified RAG response for citation counting
type RAGResponse struct {
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations,omitempty"`
}

// Citation represents a citation in the response
type Citation struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

// responseInterceptor wraps http.ResponseWriter to intercept and modify responses
type responseInterceptor struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
	ctx        context.Context
	enforcer   *Enforcer
	userID     string
	plan       string
}

func (ri *responseInterceptor) Write(b []byte) (int, error) {
	// Buffer the response
	return ri.body.Write(b)
}

func (ri *responseInterceptor) WriteHeader(statusCode int) {
	ri.statusCode = statusCode
}

// FreemiumMiddleware creates middleware that enforces freemium citation limits
func FreemiumMiddleware(enforcer *Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract user ID and plan from context (set by auth middleware)
			userID, ok := ctx.Value(UserIDKey).(string)
			if !ok || userID == "" {
				// No user in context, skip enforcement
				next.ServeHTTP(w, r)
				return
			}

			plan, ok := ctx.Value(PlanKey).(string)
			if !ok || plan == "" {
				plan = "free" // Default to free plan
			}

			// Only intercept RAG query responses
			// Check if this is a RAG query endpoint
			if !strings.Contains(r.URL.Path, "/query") && !strings.Contains(r.URL.Path, "/rag") {
				next.ServeHTTP(w, r)
				return
			}

			// Create response interceptor
			interceptor := &responseInterceptor{
				ResponseWriter: w,
				body:           &bytes.Buffer{},
				statusCode:     http.StatusOK,
				ctx:            ctx,
				enforcer:       enforcer,
				userID:         userID,
				plan:           plan,
			}

			// Call the next handler with our interceptor
			next.ServeHTTP(interceptor, r)

			// Process the intercepted response
			if interceptor.statusCode == http.StatusOK {
				// Try to parse as RAG response
				var ragResponse RAGResponse
				if err := json.Unmarshal(interceptor.body.Bytes(), &ragResponse); err != nil {
					// Not a RAG response or invalid JSON, pass through
					w.WriteHeader(interceptor.statusCode)
					w.Write(interceptor.body.Bytes())
					return
				}

				// Count citations
				citationCount := len(ragResponse.Citations)

				// Enforce citation limit
				if err := enforcer.EnforceCitationLimit(ctx, userID, plan, citationCount); err != nil {
					if citationErr, ok := err.(*CitationLimitError); ok {
						// Citation limit exceeded
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusForbidden)

						response := ErrorResponse{
							Error:      "citation limit exceeded",
							Message:    citationErr.UpgradePrompt,
							Plan:       plan,
							UpgradeURL: citationErr.UpgradeURL,
						}

						json.NewEncoder(w).Encode(response)
						return
					}
					// Other error, log but pass through
					w.WriteHeader(interceptor.statusCode)
					w.Write(interceptor.body.Bytes())
					return
				}

				// Track citation usage for analytics
				if citationCount > 0 {
					go func() {
						// Use background context to avoid cancelled context issues
						bgCtx := context.Background()
						enforcer.TrackCitationUsage(bgCtx, userID, citationCount)
					}()
				}
			}

			// Write the original response
			w.WriteHeader(interceptor.statusCode)
			w.Write(interceptor.body.Bytes())
		})
	}
}

// ValidateResponseBeforeReturn validates a RAG response before returning it
// This is an alternative approach that can be called directly in handlers
func ValidateResponseBeforeReturn(ctx context.Context, enforcer *Enforcer, userID, plan string, response *RAGResponse) error {
	citationCount := len(response.Citations)

	// Enforce citation limit
	if err := enforcer.EnforceCitationLimit(ctx, userID, plan, citationCount); err != nil {
		return err
	}

	// Track citation usage
	if citationCount > 0 {
		go func() {
			bgCtx := context.Background()
			enforcer.TrackCitationUsage(bgCtx, userID, citationCount)
		}()
	}

	return nil
}

// WithUser adds user information to the request context
func WithUser(ctx context.Context, userID, plan string) context.Context {
	ctx = context.WithValue(ctx, UserIDKey, userID)
	ctx = context.WithValue(ctx, PlanKey, plan)
	return ctx
}

// GetUserFromContext extracts user information from context
func GetUserFromContext(ctx context.Context) (userID, plan string, ok bool) {
	userID, ok1 := ctx.Value(UserIDKey).(string)
	plan, ok2 := ctx.Value(PlanKey).(string)
	return userID, plan, ok1 && ok2
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// StreamingFreemiumMiddleware handles streaming responses (for SSE/streaming RAG)
func StreamingFreemiumMiddleware(enforcer *Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract user ID and plan from context
			userID, ok := ctx.Value(UserIDKey).(string)
			if !ok || userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			plan, ok := ctx.Value(PlanKey).(string)
			if !ok || plan == "" {
				plan = "free"
			}

			// For streaming responses, we need to enforce limits upfront
			// Check if user's plan allows citations
			limit := enforcer.getCitationLimitForPlan(plan)
			if limit == -1 || limit >= 3 {
				// User has sufficient citation quota
				next.ServeHTTP(w, r)
				return
			}

			// Free plan users: we can't intercept streaming responses easily,
			// so the RAG service should handle this based on plan in request headers
			// Add plan info to headers for downstream services
			r.Header.Set("X-User-Plan", plan)
			r.Header.Set("X-Citation-Limit", string(rune(limit)))

			next.ServeHTTP(w, r)
		})
	}
}
