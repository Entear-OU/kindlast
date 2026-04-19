package handlers

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/audit"
	"github.com/entear/kindlast/services/gateway/internal/models"
)

// AuditHandler handles audit trail requests
type AuditHandler struct {
	db          *sql.DB
	logger      *slog.Logger
	auditLogger *audit.Logger
}

// NewAuditHandler creates a new audit handler
func NewAuditHandler(db *sql.DB, logger *slog.Logger, auditLogger *audit.Logger) *AuditHandler {
	return &AuditHandler{
		db:          db,
		logger:      logger,
		auditLogger: auditLogger,
	}
}

// ListAuditEntries returns a paginated list of audit entries for the user's account
func (h *AuditHandler) ListAuditEntries(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check audit trail permission
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if !limits.AuditTrailEnabled {
		respondError(w, http.StatusForbidden,
			"Audit trail is not available on your plan. Please upgrade to Professional.",
			"FEATURE_NOT_AVAILABLE")
		return
	}

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
	clientID := r.URL.Query().Get("client_id")
	artifactID := r.URL.Query().Get("artifact_id")
	action := r.URL.Query().Get("action")
	startDateStr := r.URL.Query().Get("start_date")
	endDateStr := r.URL.Query().Get("end_date")

	// Parse dates
	var startDate, endDate time.Time
	var err error
	if startDateStr != "" {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid start_date format (use YYYY-MM-DD)", "VALIDATION_ERROR")
			return
		}
	}
	if endDateStr != "" {
		endDate, err = time.Parse("2006-01-02", endDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid end_date format (use YYYY-MM-DD)", "VALIDATION_ERROR")
			return
		}
		// Set to end of day
		endDate = endDate.Add(24*time.Hour - time.Second)
	}

	// Apply retention limits if applicable
	if limits.AuditRetentionMonths > 0 {
		minDate := time.Now().AddDate(0, -limits.AuditRetentionMonths, 0)
		if startDate.IsZero() || startDate.Before(minDate) {
			startDate = minDate
		}
	}

	// Validate client ownership if client_id is provided
	if clientID != "" {
		var ownerID string
		err := h.db.QueryRowContext(r.Context(),
			"SELECT user_id FROM clients WHERE id = $1",
			clientID,
		).Scan(&ownerID)
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "Client not found", "NOT_FOUND")
			return
		}
		if err != nil {
			h.logger.Error("failed to verify client", slog.String("error", err.Error()))
			respondError(w, http.StatusInternalServerError, "Failed to verify client", "INTERNAL_ERROR")
			return
		}
		if ownerID != user.ID {
			respondError(w, http.StatusForbidden, "You do not have access to this client", "FORBIDDEN")
			return
		}
	}

	// Build query options
	opts := audit.AuditQueryOptions{
		UserID:     user.ID,
		ClientID:   clientID,
		ArtifactID: artifactID,
		Action:     action,
		Page:       page,
		PageSize:   pageSize,
	}

	if !startDate.IsZero() {
		opts.StartDate = startDate
	}
	if !endDate.IsZero() {
		opts.EndDate = endDate
	}

	// Get audit entries - need to filter by user's artifacts
	query := `
		SELECT aal.id, aal.artifact_id, aal.user_id, aal.action,
		       aal.previous_state, aal.new_state, aal.metadata, aal.created_at
		FROM artifact_audit_log aal
		JOIN artifacts a ON aal.artifact_id = a.id
		JOIN clients c ON a.client_id = c.id
		WHERE c.user_id = $1
	`
	countQuery := `
		SELECT COUNT(*)
		FROM artifact_audit_log aal
		JOIN artifacts a ON aal.artifact_id = a.id
		JOIN clients c ON a.client_id = c.id
		WHERE c.user_id = $1
	`
	args := []interface{}{user.ID}
	argIndex := 2

	if clientID != "" {
		filter := " AND c.id = $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, clientID)
		argIndex++
	}

	if artifactID != "" {
		filter := " AND aal.artifact_id = $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, artifactID)
		argIndex++
	}

	if action != "" {
		filter := " AND aal.action = $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, action)
		argIndex++
	}

	if !startDate.IsZero() {
		filter := " AND aal.created_at >= $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, startDate)
		argIndex++
	}

	if !endDate.IsZero() {
		filter := " AND aal.created_at <= $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, endDate)
		argIndex++
	}

	// Get total count
	var total int
	err = h.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count audit entries", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to count audit entries", "INTERNAL_ERROR")
		return
	}

	// Add pagination
	query += " ORDER BY aal.created_at DESC LIMIT $" + strconv.Itoa(argIndex) + " OFFSET $" + strconv.Itoa(argIndex+1)
	args = append(args, pageSize, (page-1)*pageSize)

	// Execute query
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query audit entries", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve audit entries", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	entries := make([]models.ArtifactAuditEntry, 0)
	for rows.Next() {
		var entry models.ArtifactAuditEntry
		err := rows.Scan(
			&entry.ID, &entry.ArtifactID, &entry.UserID, &entry.Action,
			&entry.PreviousState, &entry.NewState, &entry.Metadata, &entry.CreatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan audit entry", slog.String("error", err.Error()))
			continue
		}
		entries = append(entries, entry)
	}

	totalPages := (total + pageSize - 1) / pageSize

	respondJSON(w, http.StatusOK, models.AuditListResponse{
		Entries:    entries,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// ExportAuditLog exports the audit log as CSV
func (h *AuditHandler) ExportAuditLog(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check audit trail permission
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if !limits.AuditTrailEnabled {
		respondError(w, http.StatusForbidden,
			"Audit trail is not available on your plan. Please upgrade to Professional.",
			"FEATURE_NOT_AVAILABLE")
		return
	}

	// Parse filter parameters
	clientID := r.URL.Query().Get("client_id")
	startDateStr := r.URL.Query().Get("start_date")
	endDateStr := r.URL.Query().Get("end_date")

	// Parse dates
	var startDate, endDate time.Time
	var err error
	if startDateStr != "" {
		startDate, err = time.Parse("2006-01-02", startDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid start_date format (use YYYY-MM-DD)", "VALIDATION_ERROR")
			return
		}
	}
	if endDateStr != "" {
		endDate, err = time.Parse("2006-01-02", endDateStr)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid end_date format (use YYYY-MM-DD)", "VALIDATION_ERROR")
			return
		}
		endDate = endDate.Add(24*time.Hour - time.Second)
	}

	// Apply retention limits
	if limits.AuditRetentionMonths > 0 {
		minDate := time.Now().AddDate(0, -limits.AuditRetentionMonths, 0)
		if startDate.IsZero() || startDate.Before(minDate) {
			startDate = minDate
		}
	}

	// Build query
	query := `
		SELECT aal.id, aal.artifact_id, a.type, a.title, c.name as client_name,
		       aal.action, aal.metadata, aal.created_at
		FROM artifact_audit_log aal
		JOIN artifacts a ON aal.artifact_id = a.id
		JOIN clients c ON a.client_id = c.id
		WHERE c.user_id = $1
	`
	args := []interface{}{user.ID}
	argIndex := 2

	if clientID != "" {
		// Validate client ownership
		var ownerID string
		err := h.db.QueryRowContext(r.Context(),
			"SELECT user_id FROM clients WHERE id = $1",
			clientID,
		).Scan(&ownerID)
		if err != nil || ownerID != user.ID {
			respondError(w, http.StatusForbidden, "You do not have access to this client", "FORBIDDEN")
			return
		}

		query += " AND c.id = $" + strconv.Itoa(argIndex)
		args = append(args, clientID)
		argIndex++
	}

	if !startDate.IsZero() {
		query += " AND aal.created_at >= $" + strconv.Itoa(argIndex)
		args = append(args, startDate)
		argIndex++
	}

	if !endDate.IsZero() {
		query += " AND aal.created_at <= $" + strconv.Itoa(argIndex)
		args = append(args, endDate)
		argIndex++
	}

	query += " ORDER BY aal.created_at DESC LIMIT 10000"

	// Execute query
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query audit entries for export", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to export audit log", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	// Set headers for CSV download
	filename := fmt.Sprintf("audit-log-%s.csv", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

	// Create CSV writer
	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()

	// Write header
	header := []string{"ID", "Timestamp", "Client", "Artifact ID", "Artifact Type", "Artifact Title", "Action", "IP Address", "User Agent"}
	if err := csvWriter.Write(header); err != nil {
		h.logger.Error("failed to write CSV header", slog.String("error", err.Error()))
		return
	}

	// Write rows
	for rows.Next() {
		var id, artifactID, artifactType, action string
		var artifactTitle, clientName sql.NullString
		var metadata []byte
		var createdAt time.Time

		err := rows.Scan(&id, &artifactID, &artifactType, &artifactTitle, &clientName, &action, &metadata, &createdAt)
		if err != nil {
			h.logger.Error("failed to scan audit entry for export", slog.String("error", err.Error()))
			continue
		}

		// Parse metadata for IP and user agent
		var meta models.AuditMetadata
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &meta)
		}

		row := []string{
			id,
			createdAt.Format(time.RFC3339),
			clientName.String,
			artifactID,
			artifactType,
			artifactTitle.String,
			action,
			meta.IP,
			meta.UserAgent,
		}

		if err := csvWriter.Write(row); err != nil {
			h.logger.Error("failed to write CSV row", slog.String("error", err.Error()))
			return
		}
	}

	h.logger.Info("audit log exported",
		slog.String("user_id", user.ID),
		slog.String("client_id", clientID),
	)
}

// GetAuditSummary returns a summary of audit activity
func (h *AuditHandler) GetAuditSummary(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check audit trail permission
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if !limits.AuditTrailEnabled {
		respondError(w, http.StatusForbidden,
			"Audit trail is not available on your plan. Please upgrade to Professional.",
			"FEATURE_NOT_AVAILABLE")
		return
	}

	// Get summary statistics
	query := `
		SELECT
			COUNT(*) as total_entries,
			COUNT(DISTINCT aal.artifact_id) as unique_artifacts,
			COUNT(DISTINCT c.id) as unique_clients,
			COUNT(CASE WHEN aal.action = 'generated' THEN 1 END) as generations,
			COUNT(CASE WHEN aal.action = 'edited' THEN 1 END) as edits,
			COUNT(CASE WHEN aal.action = 'status_changed' THEN 1 END) as status_changes,
			COUNT(CASE WHEN aal.action = 'exported' THEN 1 END) as exports,
			MIN(aal.created_at) as earliest_entry,
			MAX(aal.created_at) as latest_entry
		FROM artifact_audit_log aal
		JOIN artifacts a ON aal.artifact_id = a.id
		JOIN clients c ON a.client_id = c.id
		WHERE c.user_id = $1
	`

	var summary struct {
		TotalEntries    int        `json:"total_entries"`
		UniqueArtifacts int        `json:"unique_artifacts"`
		UniqueClients   int        `json:"unique_clients"`
		Generations     int        `json:"generations"`
		Edits           int        `json:"edits"`
		StatusChanges   int        `json:"status_changes"`
		Exports         int        `json:"exports"`
		EarliestEntry   *time.Time `json:"earliest_entry"`
		LatestEntry     *time.Time `json:"latest_entry"`
	}

	var earliest, latest sql.NullTime
	err := h.db.QueryRowContext(r.Context(), query, user.ID).Scan(
		&summary.TotalEntries, &summary.UniqueArtifacts, &summary.UniqueClients,
		&summary.Generations, &summary.Edits, &summary.StatusChanges, &summary.Exports,
		&earliest, &latest,
	)
	if err != nil {
		h.logger.Error("failed to get audit summary", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve audit summary", "INTERNAL_ERROR")
		return
	}

	if earliest.Valid {
		summary.EarliestEntry = &earliest.Time
	}
	if latest.Valid {
		summary.LatestEntry = &latest.Time
	}

	respondJSON(w, http.StatusOK, summary)
}
