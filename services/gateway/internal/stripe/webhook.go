package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/entear/kindlast/services/gateway/internal/db"
	"github.com/entear/kindlast/services/gateway/internal/models"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
)

// WebhookHandler handles Stripe webhook events
type WebhookHandler struct {
	db            *db.Client
	webhookSecret string
	logger        *slog.Logger
}

// NewWebhookHandler creates a new Stripe webhook handler
func NewWebhookHandler(db *db.Client, webhookSecret string, logger *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		db:            db,
		webhookSecret: webhookSecret,
		logger:        logger,
	}
}

// HandleWebhook processes Stripe webhook events
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.logger.Error("error reading request body", "error", err)
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}

	// Verify webhook signature
	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), h.webhookSecret)
	if err != nil {
		h.logger.Error("webhook signature verification failed", "error", err)
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	h.logger.Info("received webhook event",
		"type", event.Type,
		"id", event.ID,
	)

	// Handle the event
	switch event.Type {
	case "checkout.session.completed":
		h.handleCheckoutSessionCompleted(event)
	case "customer.subscription.created":
		h.handleSubscriptionCreated(event)
	case "customer.subscription.updated":
		h.handleSubscriptionUpdated(event)
	case "customer.subscription.deleted":
		h.handleSubscriptionDeleted(event)
	case "invoice.payment_succeeded":
		h.handleInvoicePaymentSucceeded(event)
	case "invoice.payment_failed":
		h.handleInvoicePaymentFailed(event)
	default:
		h.logger.Info("unhandled event type", "type", event.Type)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// handleCheckoutSessionCompleted handles successful checkout
func (h *WebhookHandler) handleCheckoutSessionCompleted(event stripe.Event) {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		h.logger.Error("error parsing checkout session", "error", err)
		return
	}

	h.logger.Info("checkout session completed",
		"session_id", session.ID,
		"customer_id", session.Customer.ID,
		"subscription_id", session.Subscription.ID,
	)

	// Extract user ID from metadata
	userID, ok := session.Metadata["user_id"]
	if !ok {
		h.logger.Error("user_id not found in checkout session metadata")
		return
	}

	// Extract plan from metadata
	planName, ok := session.Metadata["plan"]
	if !ok {
		h.logger.Error("plan not found in checkout session metadata")
		return
	}

	// Convert plan name to plan constant
	var plan string
	switch planName {
	case "professional":
		plan = models.PlanProfessional
	case "team":
		plan = models.PlanTeam
	default:
		h.logger.Error("unknown plan in metadata", "plan", planName)
		return
	}

	// Update user plan in database
	ctx := context.Background()
	if err := h.db.UpdateUserPlan(ctx, userID, plan); err != nil {
		h.logger.Error("failed to update user plan", "error", err, "user_id", userID, "plan", plan)
		return
	}

	h.logger.Info("user plan updated successfully",
		"user_id", userID,
		"plan", plan,
	)
}

// handleSubscriptionCreated handles new subscription
func (h *WebhookHandler) handleSubscriptionCreated(event stripe.Event) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		h.logger.Error("error parsing subscription", "error", err)
		return
	}

	h.logger.Info("subscription created",
		"subscription_id", subscription.ID,
		"customer_id", subscription.Customer.ID,
		"status", subscription.Status,
	)
}

// handleSubscriptionUpdated handles subscription changes
func (h *WebhookHandler) handleSubscriptionUpdated(event stripe.Event) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		h.logger.Error("error parsing subscription", "error", err)
		return
	}

	h.logger.Info("subscription updated",
		"subscription_id", subscription.ID,
		"customer_id", subscription.Customer.ID,
		"status", subscription.Status,
	)

	// If subscription was canceled, downgrade to free
	if subscription.Status == stripe.SubscriptionStatusCanceled ||
		subscription.Status == stripe.SubscriptionStatusIncomplete ||
		subscription.Status == stripe.SubscriptionStatusIncompleteExpired {

		// Get user by Stripe customer ID
		// Note: You'd need to add a method to look up user by customer_id
		// For now, log the event
		h.logger.Info("subscription canceled, should downgrade user",
			"customer_id", subscription.Customer.ID,
		)
	}
}

// handleSubscriptionDeleted handles subscription cancellation
func (h *WebhookHandler) handleSubscriptionDeleted(event stripe.Event) {
	var subscription stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &subscription); err != nil {
		h.logger.Error("error parsing subscription", "error", err)
		return
	}

	h.logger.Info("subscription deleted",
		"subscription_id", subscription.ID,
		"customer_id", subscription.Customer.ID,
	)

	// Downgrade user to free plan
	// Note: You'd need to add a method to look up user by customer_id
	h.logger.Info("subscription deleted, should downgrade user to free",
		"customer_id", subscription.Customer.ID,
	)
}

// handleInvoicePaymentSucceeded handles successful payment
func (h *WebhookHandler) handleInvoicePaymentSucceeded(event stripe.Event) {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		h.logger.Error("error parsing invoice", "error", err)
		return
	}

	h.logger.Info("invoice payment succeeded",
		"invoice_id", invoice.ID,
		"customer_id", invoice.Customer.ID,
		"amount_paid", invoice.AmountPaid,
	)
}

// handleInvoicePaymentFailed handles failed payment
func (h *WebhookHandler) handleInvoicePaymentFailed(event stripe.Event) {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		h.logger.Error("error parsing invoice", "error", err)
		return
	}

	h.logger.Error("invoice payment failed",
		"invoice_id", invoice.ID,
		"customer_id", invoice.Customer.ID,
		"amount_due", invoice.AmountDue,
	)

	// Send notification to user about failed payment
	// You'd implement email/notification logic here
}

// CreateCheckoutSession creates a Stripe checkout session
func CreateCheckoutSession(userID, email, plan string, successURL, cancelURL string) (*stripe.CheckoutSession, error) {
	// Map plan to Stripe price ID (these would be environment variables)
	var priceID string
	switch plan {
	case "professional":
		priceID = "price_professional" // Replace with actual Stripe price ID
	case "team":
		priceID = "price_team" // Replace with actual Stripe price ID
	default:
		return nil, fmt.Errorf("invalid plan: %s", plan)
	}

	params := &stripe.CheckoutSessionParams{
		CustomerEmail: stripe.String(email),
		Mode:          stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
	}

	// Add metadata to track user
	params.AddMetadata("user_id", userID)
	params.AddMetadata("plan", plan)

	sess, err := session.New(params)
	if err != nil {
		return nil, fmt.Errorf("failed to create checkout session: %w", err)
	}

	return sess, nil
}
