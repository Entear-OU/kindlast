package middleware

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type contextKeyClient string

const clientContextKey contextKeyClient = "client"

// RequireClientOwnership returns middleware that verifies the authenticated user owns the client
func RequireClientOwnership(db *sql.DB, logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get user from context
			user, ok := GetUser(r.Context())
			if !ok {
				respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
				return
			}

			// Get client ID from URL
			clientID := chi.URLParam(r, "clientID")
			if clientID == "" {
				respondError(w, http.StatusBadRequest, "Client ID is required", "VALIDATION_ERROR")
				return
			}

			// Query client and verify ownership
			var client models.Client
			var description, sector, country sql.NullString
			var employeeCount sql.NullInt32

			err := db.QueryRowContext(r.Context(), `
				SELECT id, user_id, name, description, sector, country, employee_count,
				       tech_stack, data_subjects, processing_purposes, status, created_at, updated_at
				FROM clients
				WHERE id = $1
			`, clientID).Scan(
				&client.ID, &client.UserID, &client.Name,
				&description, &sector, &country, &employeeCount,
				pq.Array(&client.TechStack), pq.Array(&client.DataSubjects),
				pq.Array(&client.ProcessingPurposes), &client.Status,
				&client.CreatedAt, &client.UpdatedAt,
			)

			if err == sql.ErrNoRows {
				respondError(w, http.StatusNotFound, "Client not found", "NOT_FOUND")
				return
			}
			if err != nil {
				logger.Error("failed to query client", slog.String("error", err.Error()))
				respondError(w, http.StatusInternalServerError, "Failed to verify client ownership", "INTERNAL_ERROR")
				return
			}

			// Verify ownership
			if client.UserID != user.ID {
				logger.Warn("unauthorized client access attempt",
					slog.String("client_id", clientID),
					slog.String("client_owner", client.UserID),
					slog.String("requesting_user", user.ID),
				)
				respondError(w, http.StatusForbidden, "You do not have access to this client", "FORBIDDEN")
				return
			}

			// Set nullable fields
			client.Description = description.String
			client.Sector = sector.String
			client.Country = country.String
			if employeeCount.Valid {
				client.EmployeeCount = int(employeeCount.Int32)
			}

			// Add client to context
			ctx := context.WithValue(r.Context(), clientContextKey, &client)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetClient retrieves the client from context
func GetClient(ctx context.Context) (*models.Client, bool) {
	client, ok := ctx.Value(clientContextKey).(*models.Client)
	return client, ok
}
