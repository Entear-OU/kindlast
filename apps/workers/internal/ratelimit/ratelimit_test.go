package ratelimit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/workers/internal/ratelimit"
)

// Per-organisation quotas (ENT-231; OWASP LLM06).
//
// The property worth asserting is not that a limiter limits, which every token
// bucket does. It is that the limit is PER ORGANISATION, because a global one
// would let one busy tenant starve every other, which is the failure a
// multi-tenant product least wants and the one a shared counter produces by
// accident.

func TestOneOrganisationCannotSpendAnothersBudget(t *testing.T) {
	limiter := ratelimit.New(2, time.Minute)

	if err := limiter.Allow("org-a"); err != nil {
		t.Fatalf("first call for org-a: %v", err)
	}
	if err := limiter.Allow("org-a"); err != nil {
		t.Fatalf("second call for org-a: %v", err)
	}

	var limited ratelimit.ErrLimited
	if err := limiter.Allow("org-a"); !errors.As(err, &limited) {
		t.Fatalf("third call for org-a: got %v, want it limited", err)
	}

	// org-b has spent nothing and must be unaffected.
	if err := limiter.Allow("org-b"); err != nil {
		t.Fatalf("org-b was refused because org-a was busy: %v", err)
	}
}

// The refusal names a wait, because that is the only question a caller has.
func TestARefusalSaysWhenToTryAgain(t *testing.T) {
	limiter := ratelimit.New(1, time.Minute)

	if err := limiter.Allow("org"); err != nil {
		t.Fatalf("first call: %v", err)
	}

	var limited ratelimit.ErrLimited
	err := limiter.Allow("org")
	if !errors.As(err, &limited) {
		t.Fatalf("got %v, want it limited", err)
	}
	if limited.RetryAfter <= 0 {
		t.Errorf("RetryAfter is %v; a refusal with no wait tells a caller nothing", limited.RetryAfter)
	}
	if limited.RetryAfter > time.Minute {
		t.Errorf("RetryAfter is %v, longer than the whole window", limited.RetryAfter)
	}
}

// A zero burst permits nothing, which is the fail-closed default a limiter
// configured with nonsense should take.
func TestAZeroBurstPermitsNothing(t *testing.T) {
	limiter := ratelimit.New(0, time.Minute)

	var limited ratelimit.ErrLimited
	if err := limiter.Allow("org"); !errors.As(err, &limited) {
		t.Fatalf("got %v, want a zero-burst limiter to refuse", err)
	}
}

// Tokens come back over time, which is the half that makes it a limit rather
// than a quota.
//
// Driven with a short window rather than by injecting a clock, because the
// wait is milliseconds and a test that mocks time here would be testing the
// mock. If this ever becomes flaky the answer is an injectable clock, not a
// longer sleep.
func TestTokensRefill(t *testing.T) {
	limiter := ratelimit.New(2, 40*time.Millisecond)

	for i := range 2 {
		if err := limiter.Allow("org"); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}

	var limited ratelimit.ErrLimited
	if err := limiter.Allow("org"); !errors.As(err, &limited) {
		t.Fatalf("got %v, want the budget spent", err)
	}

	time.Sleep(60 * time.Millisecond)

	if err := limiter.Allow("org"); err != nil {
		t.Fatalf("after a full window: %v, want the budget refilled", err)
	}
}
