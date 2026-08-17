package billing

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The handler's status codes (ENT-210).
//
// These are read by a machine, not a person, and the machine retries on
// anything that is not 2xx, for days. So the interesting assertions are not
// "did it work" but "does a provider stop asking", and getting them wrong
// produces either a retry storm or a change that never lands.

type recorder struct {
	applied []Event
	err     error
}

func (r *recorder) Apply(_ context.Context, e Event) error {
	r.applied = append(r.applied, e)
	return r.err
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func post(t *testing.T, h http.HandlerFunc, body string, signed bool) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		"/api/v1/billing/webhook", strings.NewReader(body))
	if signed {
		req.Header.Set("Stripe-Signature", sign(t, []byte(body), time.Now(), secret))
	}

	w := httptest.NewRecorder()
	h(w, req)
	return w
}

const subscriptionUpdated = `{
  "id": "evt_1",
  "type": "customer.subscription.updated",
  "data": {"object": {
    "customer": "cus_1",
    "status": "active",
    "current_period_end": 1800000000,
    "items": {"data": [{"price": {"lookup_key": "pro"}}]}
  }}
}`

func TestAValidEventIsApplied(t *testing.T) {
	store := &recorder{}
	w := post(t, Handler(store, secret, quiet()), subscriptionUpdated, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	if len(store.applied) != 1 {
		t.Fatalf("applied %d events, want 1", len(store.applied))
	}

	got := store.applied[0]
	if got.CustomerID != "cus_1" || got.Plan != "pro" || got.Status != "active" {
		t.Fatalf("read %+v out of the payload", got)
	}
}

// The whole authentication of the endpoint, asserted from outside.
func TestAnUnsignedRequestIsRefusedAndAppliesNothing(t *testing.T) {
	store := &recorder{}
	w := post(t, Handler(store, secret, quiet()), subscriptionUpdated, false)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if len(store.applied) != 0 {
		t.Fatal("an unsigned request reached the store")
	}
}

func TestATamperedBodyIsRefusedAndAppliesNothing(t *testing.T) {
	store := &recorder{}
	h := Handler(store, secret, quiet())

	// Signed correctly, then altered. This is the shape of an attacker
	// replaying a captured delivery with the plan changed.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/billing/webhook",
		strings.NewReader(strings.Replace(subscriptionUpdated, `"pro"`, `"enterprise"`, 1)))
	req.Header.Set("Stripe-Signature", sign(t, []byte(subscriptionUpdated), time.Now(), secret))

	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if len(store.applied) != 0 {
		t.Fatal("a tampered body reached the store")
	}
}

// Acknowledged rather than retried. A replay will never succeed, so answering
// non-2xx buys a retry storm for something working exactly as designed.
func TestADuplicateEventIsAcknowledged(t *testing.T) {
	store := &recorder{err: ErrAlreadyApplied}
	w := post(t, Handler(store, secret, quiet()), subscriptionUpdated, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: a provider must stop retrying a replay", w.Code)
	}
}

func TestAnUnknownCustomerIsAcknowledged(t *testing.T) {
	// Also never going to succeed on retry: resending will not make this
	// deployment know the customer.
	store := &recorder{err: ErrUnknownCustomer}
	w := post(t, Handler(store, secret, quiet()), subscriptionUpdated, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
}

// The one case that SHOULD be retried.
func TestARealFaultAsksTheProviderToRetry(t *testing.T) {
	store := &recorder{err: context.DeadlineExceeded}
	w := post(t, Handler(store, secret, quiet()), subscriptionUpdated, true)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: a transient fault must be retried", w.Code)
	}
}

// An allow-list rather than a switch with a default, so a provider adding an
// event type cannot silently start changing subscriptions.
func TestAnUnhandledEventTypeChangesNothing(t *testing.T) {
	store := &recorder{}
	body := strings.Replace(subscriptionUpdated,
		"customer.subscription.updated", "invoice.payment_succeeded", 1)

	w := post(t, Handler(store, secret, quiet()), body, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	if len(store.applied) != 0 {
		t.Fatal("an event type this system does not handle reached the store")
	}
}

// A deletion is a cancellation whatever the object says, because Stripe sends
// `deleted` with the subscription's last known status still attached.
func TestADeletionCancelsRegardlessOfTheStatusOnTheObject(t *testing.T) {
	store := &recorder{}
	body := strings.Replace(subscriptionUpdated,
		"customer.subscription.updated", "customer.subscription.deleted", 1)

	if w := post(t, Handler(store, secret, quiet()), body, true); w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}

	got := store.applied[0]
	if got.Status != "canceled" {
		t.Errorf("status is %q, want canceled: the object still said active", got.Status)
	}
	if got.Plan != "free" {
		t.Errorf("plan is %q, want free", got.Plan)
	}
}

// An unknown must not grant. Both mappings default towards less entitlement,
// which is the right direction when money is involved.
func TestAnUnrecognisedStatusOrPlanDoesNotGrant(t *testing.T) {
	if got := mapStatus("something_new"); got != "canceled" {
		t.Errorf("an unrecognised status mapped to %q, want canceled", got)
	}
	if got := mapStatus("trialing"); got != "active" {
		t.Errorf("trialing mapped to %q, want active", got)
	}
	if got := mapStatus("unpaid"); got != "past_due" {
		t.Errorf("unpaid mapped to %q, want past_due", got)
	}

	if got := mapPlan(nil); got != "free" {
		t.Errorf("a subscription with no line items mapped to %q, want free", got)
	}
	if got := mapPlan([]priceItem{{}}); got != "free" {
		t.Errorf("an unrecognised lookup key mapped to %q, want free", got)
	}
}

// An endpoint anybody can POST to will read a body until memory runs out
// without a cap, which is a denial of service requiring no credential.
func TestAnOversizedBodyIsRefused(t *testing.T) {
	store := &recorder{}
	w := post(t, Handler(store, secret, quiet()), strings.Repeat("x", MaxBodyBytes+1024), true)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if len(store.applied) != 0 {
		t.Fatal("an oversized body reached the store")
	}
}
