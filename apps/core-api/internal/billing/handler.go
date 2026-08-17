package billing

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// MaxBodyBytes bounds what will be read from an unauthenticated caller.
//
// Provider events are a few kilobytes. The cap is not about the provider, it is
// about everybody else: without one, an endpoint anybody can POST to will
// happily read a body until memory runs out, and that is a denial of service
// requiring no credential at all.
const MaxBodyBytes = 64 * 1024

// Handler serves the provider webhook.
//
// # WHAT THIS RETURNS, AND WHY IT SAYS SO LITTLE
//
// Every refusal is a bare status code. A webhook endpoint that explains why it
// refused is one an attacker can use to find a working forgery: "bad
// signature" and "unknown customer" are different answers, and the difference
// tells them whether the signature check passed.
//
// The one distinction that IS made is 2xx versus 4xx, and it is made for the
// provider rather than for a person. Providers retry on non-2xx, for days. So
// anything that will never succeed on retry answers 2xx and is logged:
// a duplicate event has already been applied, and a customer this deployment
// does not know about is not going to become known by resending. Answering
// 4xx to those buys a retry storm and an alert on the provider's dashboard for
// something working as designed.
func Handler(applier Applier, secret string, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		// Verified BEFORE the body is parsed. A handler that unmarshals first
		// has already let an attacker choose what its parser sees, and parsers
		// are where the interesting bugs live.
		if err := VerifySignature(r.Header.Get("Stripe-Signature"), raw, secret, time.Now()); err != nil {
			// Logged at warn, not error: an unauthenticated endpoint on the
			// internet receives forgeries as a matter of course, and paging
			// somebody for each one trains them to ignore the alert.
			logger.Warn("a billing webhook failed signature verification",
				"error", err, "remote", r.RemoteAddr)
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		event, err := parse(raw)
		if err != nil {
			logger.Warn("a signed billing webhook could not be read", "error", err)
			// 400 rather than 200: this one IS worth the provider's attention,
			// because a correctly signed body this system cannot read means the
			// two disagree about the format.
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		if !handled[event.Type] {
			// Providers send far more event types than any integration wants.
			// Acknowledged rather than refused, because a retry will not make
			// this deployment interested in it.
			w.WriteHeader(http.StatusOK)
			return
		}

		if err := event.Validate(); err != nil {
			logger.Warn("a billing webhook named something unusable", "error", err)
			http.Error(w, "", http.StatusBadRequest)
			return
		}

		switch err := applier.Apply(r.Context(), event); {
		case err == nil:
			logger.Info("applied a billing change",
				"event", event.ID, "type", event.Type, "plan", event.Plan, "status", event.Status)
			w.WriteHeader(http.StatusOK)

		case errors.Is(err, ErrAlreadyApplied):
			// The idempotency criterion, seen from outside: a replayed delivery
			// changes nothing and is acknowledged, so the provider stops.
			logger.Info("a billing event was already applied", "event", event.ID)
			w.WriteHeader(http.StatusOK)

		case errors.Is(err, ErrUnknownCustomer):
			// Acknowledged rather than retried. Worth a line at warn, because
			// the honest reading is either a checkout that never completed or a
			// webhook pointed at the wrong deployment, and both want a human
			// eventually without wanting one now.
			logger.Warn("a billing webhook named a customer this deployment does not know",
				"event", event.ID, "customer", event.CustomerID)
			w.WriteHeader(http.StatusOK)

		default:
			// A real fault. 500 so the provider retries, because this one might
			// succeed next time.
			logger.Error("applying a billing webhook failed", "event", event.ID, "error", err)
			http.Error(w, "", http.StatusInternalServerError)
		}
	}
}

// handled is the set of event types this system acts on.
//
// An allow-list rather than a switch with a default, so a provider adding an
// event type cannot silently start changing subscriptions. Everything absent
// here is acknowledged and ignored.
var handled = map[string]bool{
	"customer.subscription.created": true,
	"customer.subscription.updated": true,
	"customer.subscription.deleted": true,
}

// priceItem is the one part of a provider's line item this system reads.
type priceItem struct {
	Price struct {
		Lookup string `json:"lookup_key"`
	} `json:"price"`
}

// parse reads the four fields this system acts on out of a provider payload.
//
// Deliberately narrow, and mapped rather than mirrored. A provider sends a
// large object describing everything it knows; reading four fields means a
// change to the rest cannot break this, and means the code states plainly what
// a payment provider is allowed to influence: which plan, what status, until
// when.
func parse(raw []byte) (Event, error) {
	var payload struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object struct {
				Customer         string `json:"customer"`
				Status           string `json:"status"`
				CurrentPeriodEnd int64  `json:"current_period_end"`
				Items            struct {
					Data []priceItem `json:"data"`
				} `json:"items"`
			} `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(raw, &payload); err != nil {
		return Event{}, err
	}

	object := payload.Data.Object

	event := Event{
		ID:         payload.ID,
		Type:       payload.Type,
		CustomerID: object.Customer,
		Status:     mapStatus(object.Status),
		Plan:       mapPlan(object.Items.Data),
	}
	if object.CurrentPeriodEnd > 0 {
		event.PeriodEnd = time.Unix(object.CurrentPeriodEnd, 0).UTC()
	}

	// A deletion is a cancellation whatever the object says, because the event
	// type is the more reliable signal: Stripe sends `deleted` with the
	// subscription's last known status still attached.
	if payload.Type == "customer.subscription.deleted" {
		event.Status = "canceled"
		event.Plan = "free"
	}

	return event, nil
}

// mapStatus narrows the provider's vocabulary to the three this schema stores.
//
// Anything unrecognised becomes `canceled`, and that direction is deliberate.
// The alternative, defaulting to `active`, means a status this code does not
// understand grants a paid entitlement, which is the wrong way for an unknown
// to fail when money is involved.
func mapStatus(provider string) string {
	switch provider {
	case "active", "trialing":
		return "active"
	case "past_due", "unpaid", "incomplete":
		return "past_due"
	default:
		return "canceled"
	}
}

// mapPlan reads the plan from the price's lookup key.
//
// A lookup key rather than a price id, because a price id is generated per
// environment and would make this deployment-specific; a lookup key is set by
// whoever configures the product and is stable across test and live.
//
// Anything unrecognised is `free`, for the same reason mapStatus defaults to
// canceled: an unknown must not grant.
func mapPlan(items []priceItem) string {
	for _, item := range items {
		if item.Price.Lookup == "pro" {
			return "pro"
		}
	}
	return "free"
}
