package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Logger returns a logging middleware
func Logger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// Call next handler
			next.ServeHTTP(ww, r)

			// Log request
			duration := time.Since(start)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", duration.Milliseconds(),
				"remote_addr", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}

// Recovery returns a panic recovery middleware
func Recovery(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					logger.Error("panic recovered",
						"error", rvr,
						"path", r.URL.Path,
						"method", r.Method,
					)

					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"error": "Internal server error"}`))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// CORS returns a CORS middleware
func CORS(allowedOrigins []string) func(next http.Handler) http.Handler {
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any major browsers
	})

	return corsHandler.Handler
}

// ContentType returns a middleware that sets the Content-Type header
func ContentType(contentType string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestID returns a middleware that injects a request ID into the context
func RequestID() func(next http.Handler) http.Handler {
	return middleware.RequestID
}

// Timeout returns a middleware that times out long-running requests
func Timeout(timeout time.Duration) func(next http.Handler) http.Handler {
	return middleware.Timeout(timeout)
}

// Compress returns a middleware that compresses HTTP responses
func Compress() func(next http.Handler) http.Handler {
	return middleware.Compress(5) // Compression level 5
}

// RealIP returns a middleware that sets the RemoteAddr to the real client IP
func RealIP() func(next http.Handler) http.Handler {
	return middleware.RealIP
}

// NoCache returns a middleware that sets headers to prevent caching
func NoCache() func(next http.Handler) http.Handler {
	return middleware.NoCache
}
