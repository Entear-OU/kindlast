package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	db                   *sql.DB
	logger               *slog.Logger
	jwtSecret            string
	jwtAccessExpiration  time.Duration
	jwtRefreshExpiration time.Duration
}

func NewAuthHandler(db *sql.DB, logger *slog.Logger, jwtSecret string, accessExp, refreshExp time.Duration) *AuthHandler {
	return &AuthHandler{
		db:                   db,
		logger:               logger,
		jwtSecret:            jwtSecret,
		jwtAccessExpiration:  accessExp,
		jwtRefreshExpiration: refreshExp,
	}
}

// Register handles user registration
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "Email and password are required", "VALIDATION_ERROR")
		return
	}

	if len(req.Password) < 8 {
		respondError(w, http.StatusBadRequest, "Password must be at least 8 characters", "VALIDATION_ERROR")
		return
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.logger.Error("failed to hash password", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
		return
	}

	// Create user
	user := models.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		PasswordHash: string(passwordHash),
		FullName:     req.FullName,
		Plan:         models.PlanFree, // Default to free plan
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	// Generate email hash for lookups
	emailHash := hashEmail(req.Email)

	// Insert into database
	_, err = h.db.ExecContext(r.Context(),
		`INSERT INTO users (id, email, email_hash, password_hash, full_name, plan, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		user.ID, user.Email, emailHash, user.PasswordHash, user.FullName, user.Plan, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		// Check for unique constraint violation
		if isDuplicateError(err) {
			respondError(w, http.StatusConflict, "Email already exists", "EMAIL_EXISTS")
			return
		}
		h.logger.Error("failed to create user", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to create user", "INTERNAL_ERROR")
		return
	}

	// Generate tokens
	accessToken, refreshToken, err := h.generateTokens(&user)
	if err != nil {
		h.logger.Error("failed to generate tokens", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to generate tokens", "INTERNAL_ERROR")
		return
	}

	// Return response
	respondJSON(w, http.StatusCreated, models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: models.UserProfile{
			ID:        user.ID,
			Email:     user.Email,
			FullName:  user.FullName,
			Plan:      user.Plan,
			CreatedAt: user.CreatedAt,
		},
	})
}

// Login handles user login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "Email and password are required", "VALIDATION_ERROR")
		return
	}

	// Fetch user from database
	var user models.User
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id, email, password_hash, full_name, plan, created_at, updated_at FROM users WHERE email = $1",
		req.Email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FullName, &user.Plan, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusUnauthorized, "Invalid email or password", "INVALID_CREDENTIALS")
		return
	}
	if err != nil {
		h.logger.Error("database error", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
		return
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Invalid email or password", "INVALID_CREDENTIALS")
		return
	}

	// Generate tokens
	accessToken, refreshToken, err := h.generateTokens(&user)
	if err != nil {
		h.logger.Error("failed to generate tokens", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to generate tokens", "INTERNAL_ERROR")
		return
	}

	// Return response
	respondJSON(w, http.StatusOK, models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: models.UserProfile{
			ID:        user.ID,
			Email:     user.Email,
			FullName:  user.FullName,
			Plan:      user.Plan,
			CreatedAt: user.CreatedAt,
		},
	})
}

// Refresh handles token refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req models.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Parse refresh token
	token, err := jwt.Parse(req.RefreshToken, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.jwtSecret), nil
	})

	if err != nil || !token.Valid {
		respondError(w, http.StatusUnauthorized, "Invalid or expired refresh token", "INVALID_TOKEN")
		return
	}

	// Extract user ID from token
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		respondError(w, http.StatusUnauthorized, "Invalid token claims", "INVALID_TOKEN")
		return
	}

	userID, ok := claims["user_id"].(string)
	if !ok {
		respondError(w, http.StatusUnauthorized, "Invalid token claims", "INVALID_TOKEN")
		return
	}

	// Fetch user from database
	var user models.User
	err = h.db.QueryRowContext(r.Context(),
		"SELECT id, email, password_hash, full_name, plan, created_at, updated_at FROM users WHERE id = $1",
		userID,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.FullName, &user.Plan, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusUnauthorized, "User not found", "USER_NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("database error", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
		return
	}

	// Generate new tokens
	accessToken, refreshToken, err := h.generateTokens(&user)
	if err != nil {
		h.logger.Error("failed to generate tokens", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to generate tokens", "INTERNAL_ERROR")
		return
	}

	// Return response
	respondJSON(w, http.StatusOK, models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: models.UserProfile{
			ID:        user.ID,
			Email:     user.Email,
			FullName:  user.FullName,
			Plan:      user.Plan,
			CreatedAt: user.CreatedAt,
		},
	})
}

// Me handles get current user
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	// User is injected by Auth middleware
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

// generateTokens generates access and refresh tokens
func (h *AuthHandler) generateTokens(user *models.User) (string, string, error) {
	// Generate access token
	accessClaims := jwt.MapClaims{
		"user_id": user.ID,
		"email":   user.Email,
		"plan":    user.Plan,
		"exp":     time.Now().Add(h.jwtAccessExpiration).Unix(),
		"iat":     time.Now().Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(h.jwtSecret))
	if err != nil {
		return "", "", err
	}

	// Generate refresh token
	refreshClaims := jwt.MapClaims{
		"user_id": user.ID,
		"exp":     time.Now().Add(h.jwtRefreshExpiration).Unix(),
		"iat":     time.Now().Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(h.jwtSecret))
	if err != nil {
		return "", "", err
	}

	return accessTokenString, refreshTokenString, nil
}

// hashEmail generates a SHA256 hash of the email for secure lookups
func hashEmail(email string) string {
	// Normalize email to lowercase before hashing
	normalized := strings.ToLower(strings.TrimSpace(email))
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}
