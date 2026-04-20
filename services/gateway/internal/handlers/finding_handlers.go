package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
)

// FindingHandler handles finding endpoints
type FindingHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewFindingHandler creates a new finding handler
func NewFindingHandler(db *sql.DB, logger *slog.Logger) *FindingHandler {
	return &FindingHandler{
		db:     db,
		logger: logger,
	}
}

// ListFindings handles GET /api/v1/assessments/{id}/findings
func (h *FindingHandler) ListFindings(w http.ResponseWriter, r *http.Request) {
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

	// Verify assessment ownership
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
		respondError(w, http.StatusInternalServerError, "Failed to list findings", "INTERNAL_ERROR")
		return
	}
	if ownerID != user.ID {
		respondError(w, http.StatusForbidden, "Access denied", "FORBIDDEN")
		return
	}

	// Parse pagination params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	// Get total count
	var total int
	err = h.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM findings WHERE assessment_id = $1",
		assessmentID,
	).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count findings", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to list findings", "INTERNAL_ERROR")
		return
	}

	// Get findings
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, assessment_id, user_id, category, severity, title, description,
			   recommendation, gdpr_article, ai_act_article, is_resolved, resolved_at, created_at
		FROM findings WHERE assessment_id = $1
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'high' THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END,
			created_at DESC
		LIMIT $2 OFFSET $3
	`, assessmentID, pageSize, offset)
	if err != nil {
		h.logger.Error("failed to list findings", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to list findings", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	findings := make([]models.Finding, 0)
	for rows.Next() {
		var f models.Finding
		var category, severity, description, recommendation, gdprArticle, aiActArticle sql.NullString
		var resolvedAt sql.NullTime

		err := rows.Scan(
			&f.ID, &f.AssessmentID, &f.UserID, &category, &severity, &f.Title,
			&description, &recommendation, &gdprArticle, &aiActArticle,
			&f.IsResolved, &resolvedAt, &f.CreatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan finding", slog.String("error", err.Error()))
			continue
		}

		f.Category = category.String
		f.Severity = severity.String
		f.Description = description.String
		f.Recommendation = recommendation.String
		f.GDPRArticle = gdprArticle.String
		f.AIActArticle = aiActArticle.String
		if resolvedAt.Valid {
			f.ResolvedAt = &resolvedAt.Time
		}

		findings = append(findings, f)
	}

	totalPages := (total + pageSize - 1) / pageSize

	respondJSON(w, http.StatusOK, models.FindingListResponse{
		Findings:   findings,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// ListUserFindings handles GET /api/v1/findings (list all findings for user)
func (h *FindingHandler) ListUserFindings(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	// Parse pagination and filter params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	// Optional filter by resolved status
	resolvedFilter := r.URL.Query().Get("resolved")

	// Build query
	baseQuery := "FROM findings WHERE user_id = $1"
	args := []interface{}{user.ID}
	argCount := 1

	if resolvedFilter != "" {
		argCount++
		baseQuery += " AND is_resolved = $" + strconv.Itoa(argCount)
		args = append(args, resolvedFilter == "true")
	}

	// Get total count
	var total int
	err := h.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) "+baseQuery,
		args...,
	).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count findings", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to list findings", "INTERNAL_ERROR")
		return
	}

	// Get findings with pagination
	paginatedArgs := append(args, pageSize, offset)
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, assessment_id, user_id, category, severity, title, description,
			   recommendation, gdpr_article, ai_act_article, is_resolved, resolved_at, created_at
		`+baseQuery+`
		ORDER BY
			CASE severity
				WHEN 'critical' THEN 1
				WHEN 'high' THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low' THEN 4
				ELSE 5
			END,
			created_at DESC
		LIMIT $`+strconv.Itoa(argCount+1)+` OFFSET $`+strconv.Itoa(argCount+2),
		paginatedArgs...,
	)
	if err != nil {
		h.logger.Error("failed to list findings", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to list findings", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	findings := make([]models.Finding, 0)
	for rows.Next() {
		var f models.Finding
		var category, severity, description, recommendation, gdprArticle, aiActArticle sql.NullString
		var resolvedAt sql.NullTime

		err := rows.Scan(
			&f.ID, &f.AssessmentID, &f.UserID, &category, &severity, &f.Title,
			&description, &recommendation, &gdprArticle, &aiActArticle,
			&f.IsResolved, &resolvedAt, &f.CreatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan finding", slog.String("error", err.Error()))
			continue
		}

		f.Category = category.String
		f.Severity = severity.String
		f.Description = description.String
		f.Recommendation = recommendation.String
		f.GDPRArticle = gdprArticle.String
		f.AIActArticle = aiActArticle.String
		if resolvedAt.Valid {
			f.ResolvedAt = &resolvedAt.Time
		}

		findings = append(findings, f)
	}

	totalPages := (total + pageSize - 1) / pageSize

	respondJSON(w, http.StatusOK, models.FindingListResponse{
		Findings:   findings,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// UpdateFinding handles PATCH /api/v1/findings/{id}
func (h *FindingHandler) UpdateFinding(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	findingID := r.PathValue("id")
	if findingID == "" {
		respondError(w, http.StatusBadRequest, "Finding ID required", "BAD_REQUEST")
		return
	}

	// Verify ownership
	var ownerID string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT user_id FROM findings WHERE id = $1",
		findingID,
	).Scan(&ownerID)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Finding not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to check finding ownership", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update finding", "INTERNAL_ERROR")
		return
	}
	if ownerID != user.ID {
		respondError(w, http.StatusForbidden, "Access denied", "FORBIDDEN")
		return
	}

	var req models.UpdateFindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	if req.IsResolved == nil {
		respondError(w, http.StatusBadRequest, "No fields to update", "BAD_REQUEST")
		return
	}

	// Update finding
	var resolvedAt interface{}
	if *req.IsResolved {
		resolvedAt = time.Now()
	} else {
		resolvedAt = nil
	}

	_, err = h.db.ExecContext(r.Context(),
		"UPDATE findings SET is_resolved = $1, resolved_at = $2 WHERE id = $3",
		*req.IsResolved, resolvedAt, findingID,
	)
	if err != nil {
		h.logger.Error("failed to update finding", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update finding", "INTERNAL_ERROR")
		return
	}

	// Fetch and return updated finding
	var f models.Finding
	var category, severity, description, recommendation, gdprArticle, aiActArticle sql.NullString
	var resolvedAtTime sql.NullTime

	err = h.db.QueryRowContext(r.Context(), `
		SELECT id, assessment_id, user_id, category, severity, title, description,
			   recommendation, gdpr_article, ai_act_article, is_resolved, resolved_at, created_at
		FROM findings WHERE id = $1
	`, findingID).Scan(
		&f.ID, &f.AssessmentID, &f.UserID, &category, &severity, &f.Title,
		&description, &recommendation, &gdprArticle, &aiActArticle,
		&f.IsResolved, &resolvedAtTime, &f.CreatedAt,
	)
	if err != nil {
		h.logger.Error("failed to fetch updated finding", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to fetch finding", "INTERNAL_ERROR")
		return
	}

	f.Category = category.String
	f.Severity = severity.String
	f.Description = description.String
	f.Recommendation = recommendation.String
	f.GDPRArticle = gdprArticle.String
	f.AIActArticle = aiActArticle.String
	if resolvedAtTime.Valid {
		f.ResolvedAt = &resolvedAtTime.Time
	}

	respondJSON(w, http.StatusOK, f)
}
