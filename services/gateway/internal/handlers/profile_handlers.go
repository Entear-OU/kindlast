package handlers

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ProfileHandler handles business profile endpoints
type ProfileHandler struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewProfileHandler creates a new profile handler
func NewProfileHandler(db *sql.DB, logger *slog.Logger) *ProfileHandler {
	return &ProfileHandler{
		db:     db,
		logger: logger,
	}
}

// GetProfile handles GET /api/v1/profile
func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	var profile models.BusinessProfile
	var dataTypes, thirdPartyProcessors pq.StringArray

	err := h.db.QueryRowContext(r.Context(), `
		SELECT id, user_id, company_name, country, industry, employee_count,
			   processes_personal_data, data_types, uses_ai_systems, ai_system_descriptions,
			   third_party_processors, transfers_data_outside_eu, has_dpo, has_privacy_policy,
			   has_cookie_consent, has_breach_notification, has_dsr_process, created_at, updated_at
		FROM business_profiles WHERE user_id = $1
	`, user.ID).Scan(
		&profile.ID, &profile.UserID, &profile.CompanyName, &profile.Country,
		&profile.Industry, &profile.EmployeeCount, &profile.ProcessesPersonalData,
		&dataTypes, &profile.UsesAISystems, &profile.AISystemDescriptions,
		&thirdPartyProcessors, &profile.TransfersDataOutsideEU, &profile.HasDPO,
		&profile.HasPrivacyPolicy, &profile.HasCookieConsent, &profile.HasBreachNotification,
		&profile.HasDSRProcess, &profile.CreatedAt, &profile.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		respondError(w, http.StatusNotFound, "Business profile not found", "NOT_FOUND")
		return
	}
	if err != nil {
		h.logger.Error("failed to get business profile", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to get profile", "INTERNAL_ERROR")
		return
	}

	profile.DataTypes = []string(dataTypes)
	profile.ThirdPartyProcessors = []string(thirdPartyProcessors)

	respondJSON(w, http.StatusOK, profile)
}

// CreateOrUpdateProfile handles POST /api/v1/profile
func (h *ProfileHandler) CreateOrUpdateProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := getUserFromContext(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "User not found", "UNAUTHORIZED")
		return
	}

	var req models.CreateBusinessProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Check if profile exists
	var existingID string
	err := h.db.QueryRowContext(r.Context(),
		"SELECT id FROM business_profiles WHERE user_id = $1",
		user.ID,
	).Scan(&existingID)

	now := time.Now()

	// Convert flexible fields to storage format
	employeeCount := req.GetEmployeeCountString()
	aiSystemDescriptions := req.GetAISystemDescriptionsString()

	if err == sql.ErrNoRows {
		// Create new profile
		profileID := uuid.New().String()
		_, err = h.db.ExecContext(r.Context(), `
			INSERT INTO business_profiles (
				id, user_id, company_name, country, industry, employee_count,
				processes_personal_data, data_types, uses_ai_systems, ai_system_descriptions,
				third_party_processors, transfers_data_outside_eu, has_dpo, has_privacy_policy,
				has_cookie_consent, has_breach_notification, has_dsr_process, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		`,
			profileID, user.ID, req.CompanyName, req.Country, req.Industry, employeeCount,
			req.ProcessesPersonalData, pq.Array(req.DataTypes), req.UsesAISystems, aiSystemDescriptions,
			pq.Array(req.ThirdPartyProcessors), req.TransfersDataOutsideEU, req.HasDPO, req.HasPrivacyPolicy,
			req.HasCookieConsent, req.HasBreachNotification, req.HasDSRProcess, now, now,
		)
		if err != nil {
			h.logger.Error("failed to create business profile", slog.String("error", err.Error()))
			respondError(w, http.StatusInternalServerError, "Failed to create profile", "INTERNAL_ERROR")
			return
		}
		existingID = profileID
	} else if err != nil {
		h.logger.Error("failed to check existing profile", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to create profile", "INTERNAL_ERROR")
		return
	} else {
		// Update existing profile
		_, err = h.db.ExecContext(r.Context(), `
			UPDATE business_profiles SET
				company_name = $1, country = $2, industry = $3, employee_count = $4,
				processes_personal_data = $5, data_types = $6, uses_ai_systems = $7, ai_system_descriptions = $8,
				third_party_processors = $9, transfers_data_outside_eu = $10, has_dpo = $11, has_privacy_policy = $12,
				has_cookie_consent = $13, has_breach_notification = $14, has_dsr_process = $15, updated_at = $16
			WHERE id = $17
		`,
			req.CompanyName, req.Country, req.Industry, employeeCount,
			req.ProcessesPersonalData, pq.Array(req.DataTypes), req.UsesAISystems, aiSystemDescriptions,
			pq.Array(req.ThirdPartyProcessors), req.TransfersDataOutsideEU, req.HasDPO, req.HasPrivacyPolicy,
			req.HasCookieConsent, req.HasBreachNotification, req.HasDSRProcess, now, existingID,
		)
		if err != nil {
			h.logger.Error("failed to update business profile", slog.String("error", err.Error()))
			respondError(w, http.StatusInternalServerError, "Failed to update profile", "INTERNAL_ERROR")
			return
		}
	}

	// Fetch and return updated profile
	var profile models.BusinessProfile
	var dataTypes, thirdPartyProcessors pq.StringArray

	err = h.db.QueryRowContext(r.Context(), `
		SELECT id, user_id, company_name, country, industry, employee_count,
			   processes_personal_data, data_types, uses_ai_systems, ai_system_descriptions,
			   third_party_processors, transfers_data_outside_eu, has_dpo, has_privacy_policy,
			   has_cookie_consent, has_breach_notification, has_dsr_process, created_at, updated_at
		FROM business_profiles WHERE id = $1
	`, existingID).Scan(
		&profile.ID, &profile.UserID, &profile.CompanyName, &profile.Country,
		&profile.Industry, &profile.EmployeeCount, &profile.ProcessesPersonalData,
		&dataTypes, &profile.UsesAISystems, &profile.AISystemDescriptions,
		&thirdPartyProcessors, &profile.TransfersDataOutsideEU, &profile.HasDPO,
		&profile.HasPrivacyPolicy, &profile.HasCookieConsent, &profile.HasBreachNotification,
		&profile.HasDSRProcess, &profile.CreatedAt, &profile.UpdatedAt,
	)
	if err != nil {
		h.logger.Error("failed to fetch updated profile", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "Failed to fetch profile", "INTERNAL_ERROR")
		return
	}

	profile.DataTypes = []string(dataTypes)
	profile.ThirdPartyProcessors = []string(thirdPartyProcessors)

	respondJSON(w, http.StatusOK, profile)
}
