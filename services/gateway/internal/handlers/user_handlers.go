package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/redis/go-redis/v9"
)

type UserHandler struct {
	db          *sql.DB
	redisClient *redis.Client
	logger      *slog.Logger
}

func NewUserHandler(db *sql.DB, redisClient *redis.Client, logger *slog.Logger) *UserHandler {
	return &UserHandler{
		db:          db,
		redisClient: redisClient,
		logger:      logger,
	}
}

// GetProfile handles GET /api/v1/users/me
func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	respondJSON(w, http.StatusOK, models.UserProfile{
		ID:        user.ID,
		Email:     user.Email,
		FullName:  user.FullName,
		Plan:      user.Plan,
		CreatedAt: user.CreatedAt,
	})
}

// UpdateProfile handles PATCH /api/v1/users/me
func (h *UserHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	var req models.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Update user in database
	_, err := h.db.ExecContext(r.Context(),
		"UPDATE users SET full_name = $1, updated_at = $2 WHERE id = $3",
		req.FullName, time.Now(), user.ID,
	)
	if err != nil {
		h.logger.Error("failed to update user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update profile", "INTERNAL_ERROR")
		return
	}

	// Fetch updated user
	var updatedUser models.User
	err = h.db.QueryRowContext(r.Context(),
		"SELECT id, email, full_name, plan, created_at, updated_at FROM users WHERE id = $1",
		user.ID,
	).Scan(&updatedUser.ID, &updatedUser.Email, &updatedUser.FullName, &updatedUser.Plan, &updatedUser.CreatedAt, &updatedUser.UpdatedAt)

	if err != nil {
		h.logger.Error("failed to fetch updated user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to fetch updated profile", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, models.UserProfile{
		ID:        updatedUser.ID,
		Email:     updatedUser.Email,
		FullName:  updatedUser.FullName,
		Plan:      updatedUser.Plan,
		CreatedAt: updatedUser.CreatedAt,
	})
}

// GetPlan handles GET /api/v1/users/me/plan
func (h *UserHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	// Get plan limits
	planLimit, exists := models.PlanLimits[user.Plan]
	if !exists {
		planLimit = models.PlanLimits[models.PlanFree] // Default to free plan
	}

	// Get current month's usage from Redis
	now := time.Now()
	usageKey := h.getUsageKey(user.ID, now)
	usage, err := h.redisClient.Get(r.Context(), usageKey).Int()
	if err != nil && err != redis.Nil {
		h.logger.Error("failed to get usage from redis", slog.String("error", err.Error()))
		// Continue with usage = 0 on error
		usage = 0
	}

	respondJSON(w, http.StatusOK, models.PlanDetails{
		Plan:            user.Plan,
		QueriesPerMonth: planLimit.QueriesPerMonth,
		QueriesUsed:     usage,
		RateLimitPerMin: planLimit.RateLimitPerMin,
	})
}

// getUsageKey generates Redis key for monthly usage
func (h *UserHandler) getUsageKey(userID string, t time.Time) string {
	return "usage:" + userID + ":month:" + t.Format("2006-01")
}
