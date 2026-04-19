package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/entear/kindlast/services/gateway/internal/auth"
	"github.com/entear/kindlast/services/gateway/internal/stripe"
)

// StripeHandler handles Stripe-related endpoints
type StripeHandler struct {
	logger *slog.Logger
}

// NewStripeHandler creates a new Stripe handler
func NewStripeHandler(logger *slog.Logger) *StripeHandler {
	return &StripeHandler{
		logger: logger,
	}
}

// CreateCheckoutSessionRequest represents checkout session request
type CreateCheckoutSessionRequest struct {
	Plan string `json:"plan"` // "professional" or "team"
}

// CreateCheckoutSessionResponse represents checkout session response
type CreateCheckoutSessionResponse struct {
	SessionID  string `json:"session_id"`
	SessionURL string `json:"session_url"`
}

// HandleCreateCheckoutSession creates a Stripe checkout session
func (h *StripeHandler) HandleCreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request
	var req CreateCheckoutSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate plan
	if req.Plan != "professional" && req.Plan != "team" {
		http.Error(w, "Invalid plan. Must be 'professional' or 'team'", http.StatusBadRequest)
		return
	}

	// Create checkout session
	successURL := "https://app.kindlast.com/checkout/success"
	cancelURL := "https://app.kindlast.com/checkout/cancel"

	session, err := stripe.CreateCheckoutSession(
		claims.UserID,
		claims.Email,
		req.Plan,
		successURL,
		cancelURL,
	)
	if err != nil {
		h.logger.Error("failed to create checkout session", "error", err)
		http.Error(w, "Failed to create checkout session", http.StatusInternalServerError)
		return
	}

	// Return session details
	resp := CreateCheckoutSessionResponse{
		SessionID:  session.ID,
		SessionURL: session.URL,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleCreatePortalSession creates a Stripe customer portal session
func (h *StripeHandler) HandleCreatePortalSession(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	claims := auth.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// TODO: Implement customer portal session
	// This would require storing the Stripe customer ID in the database
	// and retrieving it here to create a portal session

	h.logger.Info("customer portal requested", "user_id", claims.UserID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Customer portal not yet implemented",
	})
}
