package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/google/uuid"
)

// AssessmentHandler handles assessment endpoints
type AssessmentHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewAssessmentHandler creates a new assessment handler
func NewAssessmentHandler(db *sql.DB, logger *slog.Logger) *AssessmentHandler {
	return &AssessmentHandler{
		db:     db,
		logger: logger,
	}
}

// CreateAssessment handles POST /api/v1/assessments
func (h *AssessmentHandler) CreateAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	var req models.CreateAssessmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Validate type
	if req.Type != models.AssessmentTypeGDPR && req.Type != models.AssessmentTypeAIAct {
		respondError(w, http.StatusBadRequest, "Invalid assessment type. Must be 'gdpr' or 'ai_act'", "BAD_REQUEST")
		return
	}

	// Get user's business profile if exists
	var profileID sql.NullString
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id FROM business_profiles WHERE user_id = $1",
		user.ID,
	).Scan(&profileID)
	if err != nil && err != sql.ErrNoRows {
		h.logger.Error("failed to get profile", slog.String("error", err.Error()))
	}

	// Create assessment
	assessmentID := uuid.New().String()
	now := time.Now()

	_, err = h.db.ExecContext(r.Context(), `
		INSERT INTO assessments (id, user_id, profile_id, type, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, assessmentID, user.ID, profileID, req.Type, models.AssessmentStatusPending, now)

	if err != nil {
		h.logger.Error("failed to create assessment", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to create assessment", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusCreated, models.Assessment{
		ID:        assessmentID,
		UserID:    user.ID,
		ProfileID: profileID.String,
		Type:      req.Type,
		Status:    models.AssessmentStatusPending,
		CreatedAt: now,
	})
}

// ListAssessments handles GET /api/v1/assessments
func (h *AssessmentHandler) ListAssessments(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	// Parse pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	// Get total count
	var total int
	err := h.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM assessments WHERE user_id = $1",
		user.ID,
	).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count assessments", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to list assessments", "INTERNAL_ERROR")
		return
	}

	// Get assessments
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, user_id, profile_id, type, status, overall_score, risk_level, result, created_at
		FROM assessments WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, user.ID, pageSize, offset)
	if err != nil {
		h.logger.Error("failed to list assessments", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to list assessments", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	assessments := make([]models.Assessment, 0)
	for rows.Next() {
		var a models.Assessment
		var profileID sql.NullString
		var overallScore sql.NullInt64
		var riskLevel sql.NullString
		var result []byte

		err := rows.Scan(&a.ID, &a.UserID, &profileID, &a.Type, &a.Status, &overallScore, &riskLevel, &result, &a.CreatedAt)
		if err != nil {
			h.logger.Error("failed to scan assessment", slog.String("error", err.Error()))
			continue
		}

		a.ProfileID = profileID.String
		if overallScore.Valid {
			score := int(overallScore.Int64)
			a.OverallScore = &score
		}
		a.RiskLevel = riskLevel.String
		if result != nil {
			a.Result = result
		}

		assessments = append(assessments, a)
	}

	totalPages := (total + pageSize - 1) / pageSize

	respondJSON(w, http.StatusOK, models.AssessmentListResponse{
		Assessments: assessments,
		Total:       total,
		Page:        page,
		PageSize:    pageSize,
		TotalPages:  totalPages,
	})
}

// GetLatestAssessment handles GET /api/v1/assessments/latest
func (h *AssessmentHandler) GetLatestAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	var a models.Assessment
	var profileID sql.NullString
	var overallScore sql.NullInt64
	var riskLevel sql.NullString
	var result []byte

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, user_id, profile_id, type, status, overall_score, risk_level, result, created_at
		FROM assessments WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, user.ID).Scan(&a.ID, &a.UserID, &profileID, &a.Type, &a.Status, &overallScore, &riskLevel, &result, &a.CreatedAt)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "No assessments found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get latest assessment", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to get assessment", "INTERNAL_ERROR")
		return
	}

	a.ProfileID = profileID.String
	if overallScore.Valid {
		score := int(overallScore.Int64)
		a.OverallScore = &score
	}
	a.RiskLevel = riskLevel.String
	if result != nil {
		a.Result = result
	}

	respondJSON(w, http.StatusOK, a)
}

// GetAssessment handles GET /api/v1/assessments/{id}
func (h *AssessmentHandler) GetAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	assessmentID := r.PathValue("id")
	if assessmentID == "" {
		respondError(w, http.StatusBadRequest, "Assessment ID required", "BAD_REQUEST")
		return
	}

	var a models.Assessment
	var profileID sql.NullString
	var overallScore sql.NullInt64
	var riskLevel sql.NullString
	var result []byte

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, user_id, profile_id, type, status, overall_score, risk_level, result, created_at
		FROM assessments WHERE id = $1 AND user_id = $2
	`, assessmentID, user.ID).Scan(&a.ID, &a.UserID, &profileID, &a.Type, &a.Status, &overallScore, &riskLevel, &result, &a.CreatedAt)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Assessment not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get assessment", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to get assessment", "INTERNAL_ERROR")
		return
	}

	a.ProfileID = profileID.String
	if overallScore.Valid {
		score := int(overallScore.Int64)
		a.OverallScore = &score
	}
	a.RiskLevel = riskLevel.String
	if result != nil {
		a.Result = result
	}

	respondJSON(w, http.StatusOK, a)
}

// UpdateAssessment handles PATCH /api/v1/assessments/{id} - used internally to update assessment results
func (h *AssessmentHandler) UpdateAssessment(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	assessmentID := r.PathValue("id")
	if assessmentID == "" {
		respondError(w, http.StatusBadRequest, "Assessment ID required", "BAD_REQUEST")
		return
	}

	// Verify ownership
	var ownerID string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT user_id FROM assessments WHERE id = $1",
		assessmentID,
	).Scan(&ownerID)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Assessment not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to check assessment ownership", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update assessment", "INTERNAL_ERROR")
		return
	}
	if ownerID != user.ID {
		respondError(w, http.StatusForbidden, "Access denied", "FORBIDDEN")
		return
	}

	// Parse update request
	var update struct {
		Status       *string          `json:"status,omitempty"`
		OverallScore *int             `json:"overall_score,omitempty"`
		RiskLevel    *string          `json:"risk_level,omitempty"`
		Result       *json.RawMessage `json:"result,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Build dynamic update query
	query := "UPDATE assessments SET "
	args := []interface{}{}
	argCount := 0

	if update.Status != nil {
		argCount++
		query += "status = $" + strconv.Itoa(argCount) + ", "
		args = append(args, *update.Status)
	}
	if update.OverallScore != nil {
		argCount++
		query += "overall_score = $" + strconv.Itoa(argCount) + ", "
		args = append(args, *update.OverallScore)
	}
	if update.RiskLevel != nil {
		argCount++
		query += "risk_level = $" + strconv.Itoa(argCount) + ", "
		args = append(args, *update.RiskLevel)
	}
	if update.Result != nil {
		argCount++
		query += "result = $" + strconv.Itoa(argCount) + ", "
		args = append(args, *update.Result)
	}

	if argCount == 0 {
		respondError(w, http.StatusBadRequest, "No fields to update", "BAD_REQUEST")
		return
	}

	// Remove trailing comma and add WHERE clause
	query = query[:len(query)-2]
	argCount++
	query += " WHERE id = $" + strconv.Itoa(argCount)
	args = append(args, assessmentID)

	_, err = h.db.ExecContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to update assessment", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update assessment", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}
