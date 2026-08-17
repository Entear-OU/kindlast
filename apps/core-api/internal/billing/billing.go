// Package billing verifies and applies payment provider webhooks.
//
// # WHY THE SIGNATURE CHECK IS THE WHOLE OF THE AUTHENTICATION
//
// This is an unauthenticated endpoint that changes what a customer is entitled
// to. There is no session, no token and no caller identity: anybody on the
// internet can POST to it. The only thing separating a real provider event from
// an invented one is that the body carries a signature computed with a shared
// secret, so the check runs before the body is parsed and before anything is
// read out of it.
//
// That ordering matters more than it looks. A handler that unmarshals first and
// verifies second has already let an attacker choose what its parser sees, and
// parsers are where the interesting bugs are.
//
// # WHY THIS IS NOT ON THE CONNECT SURFACE
//
// Every method there runs behind authentication, a scope check and tenancy, and
// this caller has none of them (doc §0.2 lists the webhook as one of the
// justified exceptions). It is a plain route on core-api's mux, beside the
// health probes and the unsubscribe endpoint.
package billing

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Errors a caller can distinguish. Every one of them is answered to the
// provider as a 4xx without detail: a webhook endpoint that explains why it
// refused is one an attacker can use to find a working forgery.
var (
	ErrNoSignature      = errors.New("billing: the request carries no signature")
	ErrBadSignature     = errors.New("billing: the signature does not match")
	ErrSignatureExpired = errors.New("billing: the signature is outside the tolerance window")
	ErrUnknownCustomer  = errors.New("billing: no organisation holds that provider customer id")
	ErrAlreadyApplied   = errors.New("billing: this event has already been applied")
)

// SignatureTolerance is how far a signature's timestamp may be from now.
//
// Five minutes, matching Stripe's own default. It exists to bound replay: a
// signature is valid forever without it, so an attacker who captures one body
// can resend it indefinitely. The dedup ledger catches a replay of an event
// already applied, and this catches a replay of one that never was, which the
// ledger cannot know about.
const SignatureTolerance = 5 * time.Minute

// VerifySignature checks a Stripe-style `t=...,v1=...` header against the body.
//
// Implemented here rather than taken from the provider SDK deliberately. The
// SDK is a TypeScript dependency of `apps/web` and this is Go; pulling a second
// SDK into core-api to compute one HMAC would be a supply-chain cost for
// twenty lines. The scheme is documented and stable, and the parts that are
// easy to get wrong are the ones a library would not have saved us from
// anyway: constant-time comparison and the tolerance window.
func VerifySignature(header string, body []byte, secret string, now time.Time) error {
	if strings.TrimSpace(header) == "" || strings.TrimSpace(secret) == "" {
		return ErrNoSignature
	}

	var timestamp string
	var candidates []string

	for _, part := range strings.Split(header, ",") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch key {
		case "t":
			timestamp = value
		case "v1":
			// A header may carry several v1 values during a secret rotation, and
			// any one matching is enough. Collecting them all is what makes a
			// rotation not an outage.
			candidates = append(candidates, value)
		}
	}

	if timestamp == "" || len(candidates) == 0 {
		return ErrNoSignature
	}

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrNoSignature
	}
	signedAt := time.Unix(seconds, 0)

	// Checked in both directions. A timestamp far in the future is as much a
	// forgery signal as one far in the past, and only checking the past leaves
	// an attacker free to mint something that stays valid.
	if delta := now.Sub(signedAt); delta > SignatureTolerance || delta < -SignatureTolerance {
		return ErrSignatureExpired
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := mac.Sum(nil)

	for _, candidate := range candidates {
		given, err := hex.DecodeString(candidate)
		if err != nil {
			continue
		}
		// Constant time, because a byte-by-byte comparison leaks how much of a
		// guess was right and turns forgery into a search rather than a guess.
		if hmac.Equal(given, expected) {
			return nil
		}
	}

	return ErrBadSignature
}

// Event is the part of a provider payload this system acts on.
//
// Deliberately small. A provider sends a large object describing everything it
// knows; reading four fields out of it means a change to the rest cannot break
// this, and means the code says plainly what a payment provider is allowed to
// influence: which plan, what status, until when.
type Event struct {
	ID         string
	Type       string
	CustomerID string
	Plan       string
	Status     string
	PeriodEnd  time.Time
}

// Valid plans and statuses, matching the check constraints on `subscriptions`.
//
// Checked in Go as well as by the constraint, because a provider sending
// something unexpected should produce a refusal naming the field rather than a
// 23514 from inside a write, and because a constraint violation mid-transaction
// would abort the dedup insert alongside it.
var (
	validPlans    = map[string]bool{"free": true, "pro": true}
	validStatuses = map[string]bool{"active": true, "past_due": true, "canceled": true}
)

// Validate refuses an event this system cannot act on.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("billing: the event carries no id, so it cannot be deduplicated")
	}
	if strings.TrimSpace(e.CustomerID) == "" {
		return fmt.Errorf("billing: the event names no customer")
	}
	if !validPlans[e.Plan] {
		return fmt.Errorf("billing: %q is not a plan this system sells", e.Plan)
	}
	if !validStatuses[e.Status] {
		return fmt.Errorf("billing: %q is not a subscription status", e.Status)
	}
	return nil
}

// Applier records a verified event.
//
// An interface so this package does not depend on a database driver, the same
// reason `interceptor.TenantOpener` is one.
type Applier interface {
	// Apply records the event id and updates the subscription in one
	// transaction. It must return ErrAlreadyApplied when the id has been seen
	// before, and ErrUnknownCustomer when no organisation holds the customer id.
	Apply(ctx context.Context, e Event) error
}
