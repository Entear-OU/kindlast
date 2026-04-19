package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/audit"
	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

// ArtifactHandler handles artifact-related requests
type ArtifactHandler struct {
	db            *sql.DB
	redis         *redis.Client
	logger        *slog.Logger
	auditLogger   *audit.Logger
	planEnforcer  *middleware.PlanEnforcer
	ragServiceURL string
}

// NewArtifactHandler creates a new artifact handler
func NewArtifactHandler(
	db *sql.DB,
	redis *redis.Client,
	logger *slog.Logger,
	auditLogger *audit.Logger,
	planEnforcer *middleware.PlanEnforcer,
	ragServiceURL string,
) *ArtifactHandler {
	return &ArtifactHandler{
		db:            db,
		redis:         redis,
		logger:        logger,
		auditLogger:   auditLogger,
		planEnforcer:  planEnforcer,
		ragServiceURL: ragServiceURL,
	}
}

// ListArtifacts returns a paginated list of artifacts for a client
func (h *ArtifactHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	// Parse pagination and filter parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	artifactType := r.URL.Query().Get("type")
	status := r.URL.Query().Get("status")

	offset := (page - 1) * pageSize

	// Build query with optional filters
	query := `
		SELECT id, client_id, user_id, type, status, title, input_context,
		       generated_content, edited_content, citations, generation_meta,
		       version, created_at, updated_at
		FROM artifacts
		WHERE client_id = $1
	`
	countQuery := "SELECT COUNT(*) FROM artifacts WHERE client_id = $1"
	args := []interface{}{client.ID}
	argIndex := 2

	if artifactType != "" {
		filter := " AND type = $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, artifactType)
		argIndex++
	}

	if status != "" {
		filter := " AND status = $" + strconv.Itoa(argIndex)
		query += filter
		countQuery += filter
		args = append(args, status)
		argIndex++
	}

	// Get total count
	var total int
	err := h.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count artifacts", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to count artifacts", "INTERNAL_ERROR")
		return
	}

	// Add pagination
	query += " ORDER BY created_at DESC LIMIT $" + strconv.Itoa(argIndex) + " OFFSET $" + strconv.Itoa(argIndex+1)
	args = append(args, pageSize, offset)

	// Get artifacts
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to query artifacts", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve artifacts", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	artifacts := make([]models.Artifact, 0)
	for rows.Next() {
		var artifact models.Artifact
		var title sql.NullString
		var editedContent []byte

		err := rows.Scan(
			&artifact.ID, &artifact.ClientID, &artifact.UserID, &artifact.Type,
			&artifact.Status, &title, &artifact.InputContext,
			&artifact.GeneratedContent, &editedContent, &artifact.Citations,
			&artifact.GenerationMeta, &artifact.Version,
			&artifact.CreatedAt, &artifact.UpdatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan artifact", slog.String("error", err.Error()))
			continue
		}

		artifact.Title = title.String
		if editedContent != nil {
			artifact.EditedContent = editedContent
		}

		artifacts = append(artifacts, artifact)
	}

	totalPages := (total + pageSize - 1) / pageSize

	respondJSON(w, http.StatusOK, models.ArtifactListResponse{
		Artifacts:  artifacts,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// GenerateArtifact generates a new compliance artifact
func (h *ArtifactHandler) GenerateArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	// Check plan limits for artifact generation
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if limits.MaxArtifactsPerMonth == 0 {
		respondError(w, http.StatusForbidden,
			"Artifact generation is not available on the free plan. Please upgrade to Professional.",
			"PLAN_LIMIT_EXCEEDED")
		return
	}

	// Check monthly usage (unless unlimited)
	if limits.MaxArtifactsPerMonth > 0 {
		usage, err := h.planEnforcer.GetArtifactUsage(r.Context(), user.ID)
		if err != nil {
			h.logger.Error("failed to check artifact usage", slog.String("error", err.Error()))
			// Continue on error - don't block user
		} else if usage >= limits.MaxArtifactsPerMonth {
			respondError(w, http.StatusTooManyRequests,
				fmt.Sprintf("Monthly artifact limit reached (%d/%d). Resets on the 1st.", usage, limits.MaxArtifactsPerMonth),
				"ARTIFACT_LIMIT_EXCEEDED")
			return
		}
	}

	// Parse request body
	var req models.GenerateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Validate artifact type
	validTypes := map[string]bool{
		models.ArtifactTypeRoPA:              true,
		models.ArtifactTypeDPIAScreening:     true,
		models.ArtifactTypeDPAGap:            true,
		models.ArtifactTypeLawfulBasis:       true,
		models.ArtifactTypeAIActClassification: true,
	}
	if !validTypes[req.Type] {
		respondError(w, http.StatusBadRequest, "Invalid artifact type", "VALIDATION_ERROR")
		return
	}

	// Check AI Act module access for AI Act artifacts
	if req.Type == models.ArtifactTypeAIActClassification && !limits.AIActModuleEnabled {
		respondError(w, http.StatusForbidden,
			"EU AI Act module is not available on your plan. Please upgrade.",
			"FEATURE_NOT_AVAILABLE")
		return
	}

	// Build input context from client data
	inputContext := h.buildInputContext(client, req.AdditionalContext)

	// Call RAG service to generate artifact
	generateResult, err := h.callRAGService(r.Context(), req.Type, client, inputContext)
	if err != nil {
		h.logger.Error("failed to generate artifact",
			slog.String("error", err.Error()),
			slog.String("type", req.Type),
			slog.String("client_id", client.ID),
		)
		respondError(w, http.StatusInternalServerError, "Failed to generate artifact", "GENERATION_ERROR")
		return
	}

	// Generate title based on type
	title := h.generateTitle(req.Type, client.Name)

	// Insert artifact into database
	var artifact models.Artifact
	err = h.db.QueryRowContext(r.Context(), `
		INSERT INTO artifacts (client_id, user_id, type, status, title, input_context,
		                       generated_content, citations, generation_meta, version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1)
		RETURNING id, client_id, user_id, type, status, title, input_context,
		          generated_content, edited_content, citations, generation_meta,
		          version, created_at, updated_at
	`,
		client.ID, user.ID, req.Type, models.ArtifactStatusDraft,
		title, inputContext, generateResult.Content,
		generateResult.Citations, generateResult.Meta,
	).Scan(
		&artifact.ID, &artifact.ClientID, &artifact.UserID, &artifact.Type,
		&artifact.Status, &artifact.Title, &artifact.InputContext,
		&artifact.GeneratedContent, &artifact.EditedContent, &artifact.Citations,
		&artifact.GenerationMeta, &artifact.Version,
		&artifact.CreatedAt, &artifact.UpdatedAt,
	)
	if err != nil {
		h.logger.Error("failed to insert artifact", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to save artifact", "INTERNAL_ERROR")
		return
	}

	// Increment artifact usage counter
	if err := h.planEnforcer.IncrementArtifactUsage(r.Context(), user.ID); err != nil {
		h.logger.Error("failed to increment artifact usage", slog.String("error", err.Error()))
		// Don't fail the request for counter increment failure
	}

	// Log audit entry
	if err := h.auditLogger.LogGenerated(r.Context(), artifact.ID, user.ID, &artifact, r); err != nil {
		h.logger.Error("failed to log audit entry", slog.String("error", err.Error()))
		// Don't fail the request for audit logging failure
	}

	h.logger.Info("artifact generated",
		slog.String("artifact_id", artifact.ID),
		slog.String("client_id", client.ID),
		slog.String("user_id", user.ID),
		slog.String("type", req.Type),
	)

	respondJSON(w, http.StatusCreated, artifact)
}

// GetArtifact returns a specific artifact
func (h *ArtifactHandler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	artifactID := chi.URLParam(r, "artifactID")
	if artifactID == "" {
		respondError(w, http.StatusBadRequest, "Artifact ID is required", "VALIDATION_ERROR")
		return
	}

	var artifact models.Artifact
	var title sql.NullString
	var editedContent []byte

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, client_id, user_id, type, status, title, input_context,
		       generated_content, edited_content, citations, generation_meta,
		       version, created_at, updated_at
		FROM artifacts
		WHERE id = $1 AND client_id = $2
	`, artifactID, client.ID).Scan(
		&artifact.ID, &artifact.ClientID, &artifact.UserID, &artifact.Type,
		&artifact.Status, &title, &artifact.InputContext,
		&artifact.GeneratedContent, &editedContent, &artifact.Citations,
		&artifact.GenerationMeta, &artifact.Version,
		&artifact.CreatedAt, &artifact.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Artifact not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get artifact", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve artifact", "INTERNAL_ERROR")
		return
	}

	artifact.Title = title.String
	if editedContent != nil {
		artifact.EditedContent = editedContent
	}

	respondJSON(w, http.StatusOK, artifact)
}

// UpdateArtifact updates an artifact's content
func (h *ArtifactHandler) UpdateArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	artifactID := chi.URLParam(r, "artifactID")
	if artifactID == "" {
		respondError(w, http.StatusBadRequest, "Artifact ID is required", "VALIDATION_ERROR")
		return
	}

	// Parse request body
	var req models.UpdateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Get current artifact for audit logging
	var previousContent []byte
	err := h.db.QueryRowContext(r.Context(),
		"SELECT COALESCE(edited_content, generated_content) FROM artifacts WHERE id = $1 AND client_id = $2",
		artifactID, client.ID,
	).Scan(&previousContent)
	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Artifact not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get artifact", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve artifact", "INTERNAL_ERROR")
		return
	}

	// Build update query
	query := "UPDATE artifacts SET updated_at = NOW(), version = version + 1"
	args := []interface{}{}
	argIndex := 1

	if req.Title != nil {
		query += ", title = $" + strconv.Itoa(argIndex)
		args = append(args, *req.Title)
		argIndex++
	}

	if req.EditedContent != nil {
		query += ", edited_content = $" + strconv.Itoa(argIndex)
		args = append(args, req.EditedContent)
		argIndex++
	}

	query += " WHERE id = $" + strconv.Itoa(argIndex) + " AND client_id = $" + strconv.Itoa(argIndex+1)
	args = append(args, artifactID, client.ID)

	query += ` RETURNING id, client_id, user_id, type, status, title, input_context,
	           generated_content, edited_content, citations, generation_meta,
	           version, created_at, updated_at`

	var artifact models.Artifact
	var title sql.NullString
	var editedContent []byte

	err = h.db.QueryRowContext(r.Context(), query, args...).Scan(
		&artifact.ID, &artifact.ClientID, &artifact.UserID, &artifact.Type,
		&artifact.Status, &title, &artifact.InputContext,
		&artifact.GeneratedContent, &editedContent, &artifact.Citations,
		&artifact.GenerationMeta, &artifact.Version,
		&artifact.CreatedAt, &artifact.UpdatedAt,
	)
	if err != nil {
		h.logger.Error("failed to update artifact", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update artifact", "INTERNAL_ERROR")
		return
	}

	artifact.Title = title.String
	if editedContent != nil {
		artifact.EditedContent = editedContent
	}

	// Save version history
	if req.EditedContent != nil {
		_, err = h.db.ExecContext(r.Context(), `
			INSERT INTO artifact_versions (artifact_id, version, content, edited_by)
			VALUES ($1, $2, $3, $4)
		`, artifactID, artifact.Version, req.EditedContent, user.ID)
		if err != nil {
			h.logger.Error("failed to save version history", slog.String("error", err.Error()))
			// Don't fail the request for version history failure
		}
	}

	// Log audit entry
	if req.EditedContent != nil {
		if err := h.auditLogger.LogEdited(r.Context(), artifactID, user.ID, previousContent, req.EditedContent, r); err != nil {
			h.logger.Error("failed to log audit entry", slog.String("error", err.Error()))
		}
	}

	h.logger.Info("artifact updated",
		slog.String("artifact_id", artifact.ID),
		slog.String("user_id", user.ID),
	)

	respondJSON(w, http.StatusOK, artifact)
}

// UpdateStatus updates an artifact's status
func (h *ArtifactHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	artifactID := chi.URLParam(r, "artifactID")
	if artifactID == "" {
		respondError(w, http.StatusBadRequest, "Artifact ID is required", "VALIDATION_ERROR")
		return
	}

	// Parse request body
	var req models.UpdateArtifactStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Validate status
	validStatuses := map[string]bool{
		models.ArtifactStatusDraft:    true,
		models.ArtifactStatusReviewed: true,
		models.ArtifactStatusApproved: true,
		models.ArtifactStatusExported: true,
	}
	if !validStatuses[req.Status] {
		respondError(w, http.StatusBadRequest, "Invalid status", "VALIDATION_ERROR")
		return
	}

	// Get current status for audit logging
	var previousStatus string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT status FROM artifacts WHERE id = $1 AND client_id = $2",
		artifactID, client.ID,
	).Scan(&previousStatus)
	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Artifact not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get artifact", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve artifact", "INTERNAL_ERROR")
		return
	}

	// Update status
	var artifact models.Artifact
	var title sql.NullString
	var editedContent []byte

	err = h.db.QueryRowContext(r.Context(), `
		UPDATE artifacts
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND client_id = $3
		RETURNING id, client_id, user_id, type, status, title, input_context,
		          generated_content, edited_content, citations, generation_meta,
		          version, created_at, updated_at
	`, req.Status, artifactID, client.ID).Scan(
		&artifact.ID, &artifact.ClientID, &artifact.UserID, &artifact.Type,
		&artifact.Status, &title, &artifact.InputContext,
		&artifact.GeneratedContent, &editedContent, &artifact.Citations,
		&artifact.GenerationMeta, &artifact.Version,
		&artifact.CreatedAt, &artifact.UpdatedAt,
	)
	if err != nil {
		h.logger.Error("failed to update artifact status", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update status", "INTERNAL_ERROR")
		return
	}

	artifact.Title = title.String
	if editedContent != nil {
		artifact.EditedContent = editedContent
	}

	// Log audit entry
	if err := h.auditLogger.LogStatusChanged(r.Context(), artifactID, user.ID, previousStatus, req.Status, req.Reason, r); err != nil {
		h.logger.Error("failed to log audit entry", slog.String("error", err.Error()))
	}

	h.logger.Info("artifact status updated",
		slog.String("artifact_id", artifact.ID),
		slog.String("previous_status", previousStatus),
		slog.String("new_status", req.Status),
		slog.String("user_id", user.ID),
	)

	respondJSON(w, http.StatusOK, artifact)
}

// ExportArtifact exports an artifact to PDF or DOCX format
func (h *ArtifactHandler) ExportArtifact(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check export permission
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if !limits.ExportEnabled {
		respondError(w, http.StatusForbidden,
			"Document export is not available on your plan. Please upgrade.",
			"FEATURE_NOT_AVAILABLE")
		return
	}

	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	artifactID := chi.URLParam(r, "artifactID")
	if artifactID == "" {
		respondError(w, http.StatusBadRequest, "Artifact ID is required", "VALIDATION_ERROR")
		return
	}

	// Parse request body
	var req models.ExportArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Validate format
	if req.Format != "pdf" && req.Format != "docx" {
		respondError(w, http.StatusBadRequest, "Format must be 'pdf' or 'docx'", "VALIDATION_ERROR")
		return
	}

	// Get artifact
	var artifact models.Artifact
	var title sql.NullString
	var editedContent []byte

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, client_id, user_id, type, status, title, input_context,
		       generated_content, edited_content, citations, generation_meta,
		       version, created_at, updated_at
		FROM artifacts
		WHERE id = $1 AND client_id = $2
	`, artifactID, client.ID).Scan(
		&artifact.ID, &artifact.ClientID, &artifact.UserID, &artifact.Type,
		&artifact.Status, &title, &artifact.InputContext,
		&artifact.GeneratedContent, &editedContent, &artifact.Citations,
		&artifact.GenerationMeta, &artifact.Version,
		&artifact.CreatedAt, &artifact.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Artifact not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get artifact", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve artifact", "INTERNAL_ERROR")
		return
	}

	artifact.Title = title.String
	if editedContent != nil {
		artifact.EditedContent = editedContent
	}

	// Log audit entry
	if err := h.auditLogger.LogExported(r.Context(), artifactID, user.ID, req.Format, r); err != nil {
		h.logger.Error("failed to log audit entry", slog.String("error", err.Error()))
	}

	// TODO: Implement actual PDF/DOCX generation
	// For now, return a placeholder response indicating the export was triggered
	h.logger.Info("artifact export requested",
		slog.String("artifact_id", artifact.ID),
		slog.String("format", req.Format),
		slog.String("user_id", user.ID),
	)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "Export initiated",
		"artifact_id": artifact.ID,
		"format":      req.Format,
		"status":      "processing",
	})
}

// GetAuditTrail returns the audit trail for an artifact
func (h *ArtifactHandler) GetAuditTrail(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check audit trail permission
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if !limits.AuditTrailEnabled {
		respondError(w, http.StatusForbidden,
			"Audit trail is not available on your plan. Please upgrade.",
			"FEATURE_NOT_AVAILABLE")
		return
	}

	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	artifactID := chi.URLParam(r, "artifactID")
	if artifactID == "" {
		respondError(w, http.StatusBadRequest, "Artifact ID is required", "VALIDATION_ERROR")
		return
	}

	// Verify artifact belongs to client
	var exists bool
	err := h.db.QueryRowContext(r.Context(),
		"SELECT EXISTS(SELECT 1 FROM artifacts WHERE id = $1 AND client_id = $2)",
		artifactID, client.ID,
	).Scan(&exists)
	if err != nil || !exists {
		respondError(w, http.StatusNotFound, "Artifact not found", "NOT_FOUND")
		return
	}

	// Parse pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Get audit entries
	entries, total, err := h.auditLogger.GetAuditEntries(r.Context(), audit.AuditQueryOptions{
		ArtifactID: artifactID,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		h.logger.Error("failed to get audit entries", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve audit trail", "INTERNAL_ERROR")
		return
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

// ListVersions returns the version history for an artifact
func (h *ArtifactHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	client, ok := middleware.GetClient(r.Context())
	if !ok {
		respondError(w, http.StatusBadRequest, "Client context not found", "INTERNAL_ERROR")
		return
	}

	artifactID := chi.URLParam(r, "artifactID")
	if artifactID == "" {
		respondError(w, http.StatusBadRequest, "Artifact ID is required", "VALIDATION_ERROR")
		return
	}

	// Verify artifact belongs to client
	var exists bool
	err := h.db.QueryRowContext(r.Context(),
		"SELECT EXISTS(SELECT 1 FROM artifacts WHERE id = $1 AND client_id = $2)",
		artifactID, client.ID,
	).Scan(&exists)
	if err != nil || !exists {
		respondError(w, http.StatusNotFound, "Artifact not found", "NOT_FOUND")
		return
	}

	// Get versions
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, artifact_id, version, content, edited_by, created_at
		FROM artifact_versions
		WHERE artifact_id = $1
		ORDER BY version DESC
	`, artifactID)
	if err != nil {
		h.logger.Error("failed to get versions", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve versions", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	versions := make([]models.ArtifactVersion, 0)
	for rows.Next() {
		var version models.ArtifactVersion
		err := rows.Scan(
			&version.ID, &version.ArtifactID, &version.Version,
			&version.Content, &version.EditedBy, &version.CreatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan version", slog.String("error", err.Error()))
			continue
		}
		versions = append(versions, version)
	}

	respondJSON(w, http.StatusOK, versions)
}

// Helper functions

func (h *ArtifactHandler) buildInputContext(client *models.Client, additionalContext string) string {
	context := fmt.Sprintf(`Organization: %s
Sector: %s
Country: %s
Employee Count: %d
Description: %s
Tech Stack: %v
Data Subjects: %v
Processing Purposes: %v`,
		client.Name,
		client.Sector,
		client.Country,
		client.EmployeeCount,
		client.Description,
		client.TechStack,
		client.DataSubjects,
		client.ProcessingPurposes,
	)

	if additionalContext != "" {
		context += "\n\nAdditional Context: " + additionalContext
	}

	return context
}

func (h *ArtifactHandler) generateTitle(artifactType, clientName string) string {
	titles := map[string]string{
		models.ArtifactTypeRoPA:              "Record of Processing Activities",
		models.ArtifactTypeDPIAScreening:     "DPIA Screening Assessment",
		models.ArtifactTypeDPAGap:            "DPA Gap Analysis",
		models.ArtifactTypeLawfulBasis:       "Lawful Basis Assessment",
		models.ArtifactTypeAIActClassification: "AI Act Risk Classification",
	}

	title := titles[artifactType]
	if title == "" {
		title = "Compliance Artifact"
	}

	return fmt.Sprintf("%s - %s - %s", title, clientName, time.Now().Format("2006-01-02"))
}

// RAGGenerateResult represents the result from the RAG service
type RAGGenerateResult struct {
	Content   json.RawMessage `json:"content"`
	Citations json.RawMessage `json:"citations"`
	Meta      json.RawMessage `json:"meta"`
}

func (h *ArtifactHandler) callRAGService(ctx context.Context, artifactType string, client *models.Client, inputContext string) (*RAGGenerateResult, error) {
	// Build request to RAG service
	reqBody := map[string]interface{}{
		"artifact_type": artifactType,
		"client_context": map[string]interface{}{
			"name":                client.Name,
			"description":         client.Description,
			"sector":              client.Sector,
			"country":             client.Country,
			"employee_count":      client.EmployeeCount,
			"tech_stack":          client.TechStack,
			"data_subjects":       client.DataSubjects,
			"processing_purposes": client.ProcessingPurposes,
		},
		"input_context": inputContext,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make request to RAG service
	req, err := http.NewRequestWithContext(ctx, "POST", h.ragServiceURL+"/api/v1/artifact/generate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call RAG service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("RAG service error: %s - %s", resp.Status, string(body))
	}

	var result RAGGenerateResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}
