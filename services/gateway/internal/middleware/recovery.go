package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/entear/kindlast/services/gateway/internal/models"
)

// Recovery returns a middleware that recovers from panics
func Recovery(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log panic with stack trace
					logger.Error("panic recovered",
						slog.Any("error", err),
						slog.String("stack", string(debug.Stack())),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("request_id", r.Header.Get("X-Request-ID")),
					)

					// Return 500 error
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(models.ErrorResponse{
						Error:   "Internal server error",
						Message: "An unexpected error occurred",
						Code:    "INTERNAL_ERROR",
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
