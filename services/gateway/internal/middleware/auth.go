package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

type contextKeyUser string

const userContextKey contextKeyUser = "user"

// Claims represents JWT claims
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Plan   string `json:"plan"`
	jwt.RegisteredClaims
}

// Auth returns a middleware that validates JWT tokens
func Auth(jwtSecret string, db *sql.DB, logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				respondError(w, http.StatusUnauthorized, "Missing authorization header", "UNAUTHORIZED")
				return
			}

			// Check Bearer prefix
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				respondError(w, http.StatusUnauthorized, "Invalid authorization header format", "INVALID_TOKEN")
				return
			}

			tokenString := parts[1]

			// Parse and validate token
			token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
				// Validate signing method
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return []byte(jwtSecret), nil
			})

			if err != nil {
				logger.Warn("token validation failed", slog.String("error", err.Error()))
				respondError(w, http.StatusUnauthorized, "Invalid or expired token", "INVALID_TOKEN")
				return
			}

			claims, ok := token.Claims.(*Claims)
			if !ok || !token.Valid {
				respondError(w, http.StatusUnauthorized, "Invalid token claims", "INVALID_TOKEN")
				return
			}

			// Fetch user from database to ensure still exists and get current data
			var user models.User
			err = db.QueryRowContext(r.Context(),
				"SELECT id, email, full_name, plan, created_at, updated_at FROM users WHERE id = $1",
				claims.UserID,
			).Scan(&user.ID, &user.Email, &user.FullName, &user.Plan, &user.CreatedAt, &user.UpdatedAt)

			if err == sql.ErrNoRows {
				respondError(w, http.StatusUnauthorized, "User not found", "USER_NOT_FOUND")
				return
			}
			if err != nil {
				logger.Error("database error", slog.String("error", err.Error()))
				respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
				return
			}

			// Add user to context
			ctx := context.WithValue(r.Context(), userContextKey, &user)

			// Continue with authenticated context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUser retrieves the authenticated user from context
func GetUser(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey).(*models.User)
	return user, ok
}

func respondError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(models.ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    code,
	})
}
