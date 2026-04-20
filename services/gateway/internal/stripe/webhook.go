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
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		h.logger.Error("error parsing checkout session", "error", err)
		return
	}

	h.logger.Info("checkout session completed",
		"session_id", sess.ID,
		"customer_id", sess.Customer.ID,
		"subscription_id", sess.Subscription.ID,
	)

	// Extract user ID from metadata
	userID, ok := sess.Metadata["user_id"]
	if !ok {
		h.logger.Error("user_id not found in checkout session metadata")
		return
	}

	// Extract plan from metadata
	planName, ok := sess.Metadata["plan"]
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

	ctx := context.Background()

	// Update user plan in database
	if err := h.db.UpdateUserPlan(ctx, userID, plan); err != nil {
		h.logger.Error("failed to update user plan", "error", err, "user_id", userID, "plan", plan)
		return
	}

	// Store the Stripe customer ID for future subscription events
	if sess.Customer != nil && sess.Customer.ID != "" {
		if err := h.db.UpdateUserStripeCustomerID(ctx, userID, sess.Customer.ID); err != nil {
			h.logger.Error("failed to update user stripe customer id", "error", err, "user_id", userID, "customer_id", sess.Customer.ID)
			// Don't return - the plan was updated successfully
		}
	}

	h.logger.Info("user plan updated successfully",
		"user_id", userID,
		"plan", plan,
		"stripe_customer_id", sess.Customer.ID,
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

	ctx := context.Background()

	// Get user by Stripe customer ID
	customerID := subscription.Customer.ID
	user, err := h.db.GetUserByStripeCustomerID(ctx, customerID)
	if err != nil {
		h.logger.Error("failed to find user for subscription update",
			"error", err,
			"customer_id", customerID,
		)
		return
	}

	// If subscription was canceled or failed, downgrade to free
	if subscription.Status == stripe.SubscriptionStatusCanceled ||
		subscription.Status == stripe.SubscriptionStatusIncomplete ||
		subscription.Status == stripe.SubscriptionStatusIncompleteExpired ||
		subscription.Status == stripe.SubscriptionStatusPastDue ||
		subscription.Status == stripe.SubscriptionStatusUnpaid {

		h.logger.Info("subscription status requires downgrade",
			"customer_id", customerID,
			"user_id", user.ID,
			"status", subscription.Status,
		)

		if err := h.db.UpdateUserPlan(ctx, user.ID, models.PlanFree); err != nil {
			h.logger.Error("failed to downgrade user to free plan",
				"error", err,
				"user_id", user.ID,
			)
			return
		}

		h.logger.Info("user downgraded to free plan",
			"user_id", user.ID,
			"previous_plan", user.Plan,
		)
	} else if subscription.Status == stripe.SubscriptionStatusActive {
		// Subscription is active - determine plan from price ID
		plan := h.determinePlanFromSubscription(&subscription)
		if plan != "" && plan != user.Plan {
			if err := h.db.UpdateUserPlan(ctx, user.ID, plan); err != nil {
				h.logger.Error("failed to update user plan",
					"error", err,
					"user_id", user.ID,
					"new_plan", plan,
				)
				return
			}

			h.logger.Info("user plan updated from subscription change",
				"user_id", user.ID,
				"previous_plan", user.Plan,
				"new_plan", plan,
			)
		}
	}
}

// determinePlanFromSubscription extracts the plan from subscription price metadata
func (h *WebhookHandler) determinePlanFromSubscription(subscription *stripe.Subscription) string {
	if subscription.Items == nil || len(subscription.Items.Data) == 0 {
		return ""
	}

	// Check the first item's price metadata for plan info
	item := subscription.Items.Data[0]
	if item.Price != nil && item.Price.Metadata != nil {
		if plan, ok := item.Price.Metadata["plan"]; ok {
			switch plan {
			case "professional":
				return models.PlanProfessional
			case "team":
				return models.PlanTeam
			}
		}
	}

	return ""
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

	ctx := context.Background()

	// Get user by Stripe customer ID
	customerID := subscription.Customer.ID
	user, err := h.db.GetUserByStripeCustomerID(ctx, customerID)
	if err != nil {
		h.logger.Error("failed to find user for subscription deletion",
			"error", err,
			"customer_id", customerID,
		)
		return
	}

	// Downgrade user to free plan
	if err := h.db.UpdateUserPlan(ctx, user.ID, models.PlanFree); err != nil {
		h.logger.Error("failed to downgrade user to free plan",
			"error", err,
			"user_id", user.ID,
		)
		return
	}

	h.logger.Info("user downgraded to free plan after subscription deletion",
		"user_id", user.ID,
		"previous_plan", user.Plan,
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
