package handlers

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

// ProcessorHandler handles processor profile requests
type ProcessorHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewProcessorHandler creates a new processor handler
func NewProcessorHandler(db *sql.DB, logger *slog.Logger) *ProcessorHandler {
	return &ProcessorHandler{
		db:     db,
		logger: logger,
	}
}

// ListProcessors returns a paginated list of processor profiles
func (h *ProcessorHandler) ListProcessors(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check plan limits for processor access
	limits := models.DPOCopilotPlanLimits[user.Plan]

	// Parse pagination parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Parse filter parameters
	search := r.URL.Query().Get("search")
	category := r.URL.Query().Get("category")

	offset := (page - 1) * pageSize

	// For limited access (free tier), limit to top 10 processors
	if limits.ProcessorAccess == "limited" {
		pageSize = 10
		page = 1
		offset = 0
	}

	// Build query with optional filters
	query := `
		SELECT id, name, slug, category, description, headquarters,
		       data_categories, processing_purposes, data_locations,
		       transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
		       verified, last_verified, created_at, updated_at
		FROM processor_profiles
		WHERE 1=1
	`
	countQuery := "SELECT COUNT(*) FROM processor_profiles WHERE 1=1"
	args := []interface{}{}
	argIndex := 1

	if search != "" {
		filter := " AND (name ILIKE $" + strconv.Itoa(argIndex) + " OR description ILIKE $" + strconv.Itoa(argIndex) + ")"
		query += filter
		countQuery += filter
		args = append(args, "%"+search+"%")
		argIndex++
	}

	if category != "" {
		filter := " AND category = $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, category)
		argIndex++
	}

	// Get total count
	var total int
	err := h.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count processors", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to count processors", "INTERNAL_ERROR")
		return
	}

	// For limited access, cap the total
	if limits.ProcessorAccess == "limited" && total > 10 {
		total = 10
	}

	// Add pagination
	query += " ORDER BY name ASC LIMIT $" + strconv.Itoa(argIndex) + " OFFSET $" + strconv.Itoa(argIndex+1)
	args = append(args, pageSize, offset)

	// Get processors
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query processors", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve processors", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	processors := make([]models.ProcessorProfile, 0)
	for rows.Next() {
		var processor models.ProcessorProfile
		var description, headquarters, transferMechanism sql.NullString
		var dpaURL, subprocessorsURL, gdprPageURL sql.NullString
		var lastVerified sql.NullTime

		err := rows.Scan(
			&processor.ID, &processor.Name, &processor.Slug, &processor.Category,
			&description, &headquarters, pq.Array(&processor.DataCategories),
			pq.Array(&processor.ProcessingPurposes), pq.Array(&processor.DataLocations),
			&transferMechanism, &dpaURL, &subprocessorsURL, &gdprPageURL,
			&processor.Verified, &lastVerified,
			&processor.CreatedAt, &processor.UpdatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan processor", slog.String("error", err.Error()))
			continue
		}

		processor.Description = description.String
		processor.Headquarters = headquarters.String
		processor.TransferMechanism = transferMechanism.String
		processor.DPAURL = dpaURL.String
		processor.SubprocessorsURL = subprocessorsURL.String
		processor.GDPRPageURL = gdprPageURL.String
		if lastVerified.Valid {
			processor.LastVerified = &lastVerified.Time
		}

		processors = append(processors, processor)
	}

	totalPages := (total + pageSize - 1) / pageSize

	response := models.ProcessorListResponse{
		Processors: processors,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}

	// Add upgrade message for limited access
	if limits.ProcessorAccess == "limited" {
		w.Header().Set("X-Plan-Limited", "true")
		w.Header().Set("X-Upgrade-URL", "/upgrade")
	}

	respondJSON(w, http.StatusOK, response)
}

// GetProcessor returns a specific processor profile by slug
func (h *ProcessorHandler) GetProcessor(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	slug := chi.URLParam(r, "slug")
	if slug == "" {
		respondError(w, http.StatusBadRequest, "Processor slug is required", "VALIDATION_ERROR")
		return
	}

	// Check if user has access to this processor
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if limits.ProcessorAccess == "limited" {
		// Check if this processor is in the top 10
		var rank int
		err := h.db.QueryRowContext(r.Context(), `
			SELECT position FROM (
				SELECT slug, ROW_NUMBER() OVER (ORDER BY name ASC) as position
				FROM processor_profiles
			) ranked
			WHERE slug = $1
		`, slug).Scan(&rank)
		if err == nil && rank > 10 {
			respondError(w, http.StatusForbidden,
				"Access to this processor profile requires a Professional plan. Please upgrade.",
				"PLAN_LIMIT_EXCEEDED")
			return
		}
	}

	var processor models.ProcessorProfile
	var description, headquarters, transferMechanism sql.NullString
	var dpaURL, subprocessorsURL, gdprPageURL sql.NullString
	var lastVerified sql.NullTime

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, name, slug, category, description, headquarters,
		       data_categories, processing_purposes, data_locations,
		       transfer_mechanism, dpa_url, subprocessors_url, gdpr_page_url,
		       verified, last_verified, created_at, updated_at
		FROM processor_profiles
		WHERE slug = $1
	`, slug).Scan(
		&processor.ID, &processor.Name, &processor.Slug, &processor.Category,
		&description, &headquarters, pq.Array(&processor.DataCategories),
		pq.Array(&processor.ProcessingPurposes), pq.Array(&processor.DataLocations),
		&transferMechanism, &dpaURL, &subprocessorsURL, &gdprPageURL,
		&processor.Verified, &lastVerified,
		&processor.CreatedAt, &processor.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Processor not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get processor", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve processor", "INTERNAL_ERROR")
		return
	}

	processor.Description = description.String
	processor.Headquarters = headquarters.String
	processor.TransferMechanism = transferMechanism.String
	processor.DPAURL = dpaURL.String
	processor.SubprocessorsURL = subprocessorsURL.String
	processor.GDPRPageURL = gdprPageURL.String
	if lastVerified.Valid {
		processor.LastVerified = &lastVerified.Time
	}

	respondJSON(w, http.StatusOK, processor)
}

// SearchProcessors searches for processor profiles (used for tech stack autocomplete)
func (h *ProcessorHandler) SearchProcessors(w http.ResponseWriter, r *http.Request) {
	_, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		respondError(w, http.StatusBadRequest, "Search query is required", "VALIDATION_ERROR")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 20 {
		limit = 10
	}

	// Search processors by name (for autocomplete)
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, name, slug, category
		FROM processor_profiles
		WHERE name ILIKE $1
		ORDER BY
			CASE WHEN name ILIKE $2 THEN 0 ELSE 1 END,
			name ASC
		LIMIT $3
	`, "%"+query+"%", query+"%", limit)
	if err != nil {
		h.logger.Error("failed to search processors", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to search processors", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	results := make([]map[string]string, 0)
	for rows.Next() {
		var id, name, slug, category string
		if err := rows.Scan(&id, &name, &slug, &category); err != nil {
			continue
		}
		results = append(results, map[string]string{
			"id":       id,
			"name":     name,
			"slug":     slug,
			"category": category,
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

// GetProcessorCategories returns available processor categories
func (h *ProcessorHandler) GetProcessorCategories(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT DISTINCT category, COUNT(*) as count
		FROM processor_profiles
		WHERE category IS NOT NULL AND category != ''
		GROUP BY category
		ORDER BY count DESC
	`)
	if err != nil {
		h.logger.Error("failed to get categories", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve categories", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	categories := make([]map[string]interface{}, 0)
	for rows.Next() {
		var category string
		var count int
		if err := rows.Scan(&category, &count); err != nil {
			continue
		}
		categories = append(categories, map[string]interface{}{
			"category": category,
			"count":    count,
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"categories": categories,
	})
}
