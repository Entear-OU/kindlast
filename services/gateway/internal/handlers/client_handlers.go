package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/entear/kindlast/services/gateway/internal/middleware"
	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

// ClientHandler handles client-related requests
type ClientHandler struct {
	db     *sql.DB
	redis  *redis.Client
	logger *slog.Logger
}

// NewClientHandler creates a new client handler
func NewClientHandler(db *sql.DB, redis *redis.Client, logger *slog.Logger) *ClientHandler {
	return &ClientHandler{
		db:     db,
		redis:  redis,
		logger: logger,
	}
}

// ListClients returns a paginated list of clients for the authenticated user
func (h *ClientHandler) ListClients(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
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

	// Parse status filter (default to active)
	status := r.URL.Query().Get("status")
	if status == "" {
		status = models.ClientStatusActive
	}

	offset := (page - 1) * pageSize

	// Get total count
	var total int
	err := h.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM clients WHERE user_id = $1 AND status = $2",
		user.ID, status,
	).Scan(&total)
	if err != nil {
		h.logger.Error("failed to count clients", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to count clients", "INTERNAL_ERROR")
		return
	}

	// Get clients
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT id, user_id, name, description, sector, country, employee_count,
		       tech_stack, data_subjects, processing_purposes, status, created_at, updated_at
		FROM clients
		WHERE user_id = $1 AND status = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`, user.ID, status, pageSize, offset)
	if err != nil {
		h.logger.Error("failed to query clients", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve clients", "INTERNAL_ERROR")
		return
	}
	defer rows.Close()

	clients := make([]models.Client, 0)
	for rows.Next() {
		var client models.Client
		var description, sector, country sql.NullString
		var employeeCount sql.NullInt32

		err := rows.Scan(
			&client.ID, &client.UserID, &client.Name,
			&description, &sector, &country, &employeeCount,
			pq.Array(&client.TechStack), pq.Array(&client.DataSubjects),
			pq.Array(&client.ProcessingPurposes), &client.Status,
			&client.CreatedAt, &client.UpdatedAt,
		)
		if err != nil {
			h.logger.Error("failed to scan client", slog.String("error", err.Error()))
			continue
		}

		client.Description = description.String
		client.Sector = sector.String
		client.Country = country.String
		if employeeCount.Valid {
			client.EmployeeCount = int(employeeCount.Int32)
		}

		clients = append(clients, client)
	}

	totalPages := (total + pageSize - 1) / pageSize

	respondJSON(w, http.StatusOK, models.ClientListResponse{
		Clients:    clients,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	})
}

// CreateClient creates a new client for the authenticated user
func (h *ClientHandler) CreateClient(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	// Check plan limits
	limits := models.DPOCopilotPlanLimits[user.Plan]
	if limits.MaxClients == 0 {
		respondError(w, http.StatusForbidden, "Client workspaces are not available on the free plan. Upgrade to Professional.", "PLAN_LIMIT_EXCEEDED")
		return
	}

	// Count existing clients
	var clientCount int
	err := h.db.QueryRowContext(r.Context(),
		"SELECT COUNT(*) FROM clients WHERE user_id = $1 AND status = $2",
		user.ID, models.ClientStatusActive,
	).Scan(&clientCount)
	if err != nil {
		h.logger.Error("failed to count clients", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to check client limit", "INTERNAL_ERROR")
		return
	}

	if clientCount >= limits.MaxClients {
		respondError(w, http.StatusTooManyRequests,
			"Client limit reached. Your plan allows "+strconv.Itoa(limits.MaxClients)+" clients.",
			"PLAN_LIMIT_EXCEEDED")
		return
	}

	// Parse request body
	var req models.CreateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Validate required fields
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Client name is required", "VALIDATION_ERROR")
		return
	}

	// Initialize arrays if nil
	if req.TechStack == nil {
		req.TechStack = []string{}
	}
	if req.DataSubjects == nil {
		req.DataSubjects = []string{}
	}
	if req.ProcessingPurposes == nil {
		req.ProcessingPurposes = []string{}
	}

	// Create client
	var client models.Client
	err = h.db.QueryRowContext(r.Context(), `
		INSERT INTO clients (user_id, name, description, sector, country, employee_count,
		                     tech_stack, data_subjects, processing_purposes, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, user_id, name, description, sector, country, employee_count,
		          tech_stack, data_subjects, processing_purposes, status, created_at, updated_at
	`,
		user.ID, req.Name, req.Description, req.Sector, req.Country, req.EmployeeCount,
		pq.Array(req.TechStack), pq.Array(req.DataSubjects), pq.Array(req.ProcessingPurposes),
		models.ClientStatusActive,
	).Scan(
		&client.ID, &client.UserID, &client.Name, &client.Description, &client.Sector,
		&client.Country, &client.EmployeeCount, pq.Array(&client.TechStack),
		pq.Array(&client.DataSubjects), pq.Array(&client.ProcessingPurposes),
		&client.Status, &client.CreatedAt, &client.UpdatedAt,
	)
	if err != nil {
		if isDuplicateError(err) {
			respondError(w, http.StatusConflict, "A client with this name already exists", "DUPLICATE_ERROR")
			return
		}
		h.logger.Error("failed to create client", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to create client", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("client created",
		slog.String("client_id", client.ID),
		slog.String("user_id", user.ID),
		slog.String("name", client.Name),
	)

	respondJSON(w, http.StatusCreated, client)
}

// GetClient returns a specific client by ID
func (h *ClientHandler) GetClient(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	clientID := chi.URLParam(r, "clientID")
	if clientID == "" {
		respondError(w, http.StatusBadRequest, "Client ID is required", "VALIDATION_ERROR")
		return
	}

	var client models.Client
	var description, sector, country sql.NullString
	var employeeCount sql.NullInt32

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, user_id, name, description, sector, country, employee_count,
		       tech_stack, data_subjects, processing_purposes, status, created_at, updated_at
		FROM clients
		WHERE id = $1 AND user_id = $2
	`, clientID, user.ID).Scan(
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
		h.logger.Error("failed to get client", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to retrieve client", "INTERNAL_ERROR")
		return
	}

	client.Description = description.String
	client.Sector = sector.String
	client.Country = country.String
	if employeeCount.Valid {
		client.EmployeeCount = int(employeeCount.Int32)
	}

	respondJSON(w, http.StatusOK, client)
}

// UpdateClient updates a client's information
func (h *ClientHandler) UpdateClient(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	clientID := chi.URLParam(r, "clientID")
	if clientID == "" {
		respondError(w, http.StatusBadRequest, "Client ID is required", "VALIDATION_ERROR")
		return
	}

	// Parse request body
	var req models.UpdateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	// Check client exists and belongs to user
	var existingClient models.Client
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id, user_id FROM clients WHERE id = $1 AND user_id = $2",
		clientID, user.ID,
	).Scan(&existingClient.ID, &existingClient.UserID)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Client not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to check client", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update client", "INTERNAL_ERROR")
		return
	}

	// Build update query dynamically
	query := "UPDATE clients SET updated_at = NOW()"
	args := []interface{}{}
	argIndex := 1

	if req.Name != nil {
		query += ", name = $" + strconv.Itoa(argIndex)
		args = append(args, *req.Name)
		argIndex++
	}
	if req.Description != nil {
		query += ", description = $" + strconv.Itoa(argIndex)
		args = append(args, *req.Description)
		argIndex++
	}
	if req.Sector != nil {
		query += ", sector = $" + strconv.Itoa(argIndex)
		args = append(args, *req.Sector)
		argIndex++
	}
	if req.Country != nil {
		query += ", country = $" + strconv.Itoa(argIndex)
		args = append(args, *req.Country)
		argIndex++
	}
	if req.EmployeeCount != nil {
		query += ", employee_count = $" + strconv.Itoa(argIndex)
		args = append(args, *req.EmployeeCount)
		argIndex++
	}
	if req.TechStack != nil {
		query += ", tech_stack = $" + strconv.Itoa(argIndex)
		args = append(args, pq.Array(req.TechStack))
		argIndex++
	}
	if req.DataSubjects != nil {
		query += ", data_subjects = $" + strconv.Itoa(argIndex)
		args = append(args, pq.Array(req.DataSubjects))
		argIndex++
	}
	if req.ProcessingPurposes != nil {
		query += ", processing_purposes = $" + strconv.Itoa(argIndex)
		args = append(args, pq.Array(req.ProcessingPurposes))
		argIndex++
	}

	query += " WHERE id = $" + strconv.Itoa(argIndex) + " AND user_id = $" + strconv.Itoa(argIndex+1)
	args = append(args, clientID, user.ID)

	query += ` RETURNING id, user_id, name, description, sector, country, employee_count,
	           tech_stack, data_subjects, processing_purposes, status, created_at, updated_at`

	var client models.Client
	var description, sector, country sql.NullString
	var employeeCount sql.NullInt32

	err = h.db.QueryRowContext(r.Context(), query, args...).Scan(
		&client.ID, &client.UserID, &client.Name,
		&description, &sector, &country, &employeeCount,
		pq.Array(&client.TechStack), pq.Array(&client.DataSubjects),
		pq.Array(&client.ProcessingPurposes), &client.Status,
		&client.CreatedAt, &client.UpdatedAt,
	)
	if err != nil {
		if isDuplicateError(err) {
			respondError(w, http.StatusConflict, "A client with this name already exists", "DUPLICATE_ERROR")
			return
		}
		h.logger.Error("failed to update client", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to update client", "INTERNAL_ERROR")
		return
	}

	client.Description = description.String
	client.Sector = sector.String
	client.Country = country.String
	if employeeCount.Valid {
		client.EmployeeCount = int(employeeCount.Int32)
	}

	h.logger.Info("client updated",
		slog.String("client_id", client.ID),
		slog.String("user_id", user.ID),
	)

	respondJSON(w, http.StatusOK, client)
}

// ArchiveClient soft-deletes a client by setting status to archived
func (h *ClientHandler) ArchiveClient(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found in context", "UNAUTHORIZED")
		return
	}

	clientID := chi.URLParam(r, "clientID")
	if clientID == "" {
		respondError(w, http.StatusBadRequest, "Client ID is required", "VALIDATION_ERROR")
		return
	}

	// Update client status to archived
	result, err := h.db.ExecContext(r.Context(), `
		UPDATE clients
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND user_id = $3 AND status = $4
	`, models.ClientStatusArchived, clientID, user.ID, models.ClientStatusActive)

	if err != nil {
		h.logger.Error("failed to archive client", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to archive client", "INTERNAL_ERROR")
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		respondError(w, http.StatusNotFound, "Client not found or already archived", "NOT_FOUND")
		return
	}

	h.logger.Info("client archived",
		slog.String("client_id", clientID),
		slog.String("user_id", user.ID),
	)

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "Client archived successfully",
		"id":      clientID,
	})
}

// GetClientFromContext retrieves the client from context (set by RequireClientOwnership middleware)
func GetClientFromContext(r *http.Request) (*models.Client, bool) {
	return middleware.GetClient(r.Context())
}
