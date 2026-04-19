package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
)

// respondJSON writes a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// respondError writes an error response
func respondError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    code,
	})
}

// getUserFromContext retrieves user from request context
func getUserFromContext(ctx context.Context) (*models.User, bool) {
	return middleware.GetUser(ctx)
}

// isDuplicateError checks if error is a duplicate key violation
func isDuplicateError(err error) bool {
	// PostgreSQL duplicate key error code: 23505
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "unique constraint")
}
