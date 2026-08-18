// Package ratelimit bounds how often one organisation may make this gateway
// dial out (ENT-231; OWASP LLM06).
//
// # WHAT IS BEING PROTECTED, WHICH IS NOT THIS PROCESS
//
// The obvious reading is that a limiter protects the gateway. It mostly does
// not: the gateway is cheap and the calls are synchronous. What it protects is
// the customer's own server, which is a system somebody else operates and
// which this product has no right to hammer, and the deployment's egress
// budget, which is a real cost with somebody's name on it.
//
// So the limit is per organisation rather than global. A global limit would
// let one busy tenant starve every other one, which is the failure mode a
// multi-tenant product least wants.
//
// # IN MEMORY, AND THE HONEST VERSION OF WHAT THAT MEANS
//
// This is a token bucket in a map. With one gateway replica it is exactly
// right. With N replicas each enforces the limit independently, so the
// effective limit is N times what an operator configured, and a customer would
// have to read this comment to know it.
//
// Redis is already in the stack and would make this shared. It is not used
// here because the shared version wants an atomic script, a failure policy for
// when Redis is unreachable, and a decision about whether a limiter that
// cannot reach its store fails open or closed. Those are real decisions and
// making them badly in passing is worse than one replica with a bound that is
// documented. When the gateway is scaled out, this package is the seam: the
// interface stays, the implementation changes.
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// Limiter is a per-key token bucket.
//
// Keys are organisation ids. Nothing here knows that, which is why the type is
// not called OrgLimiter: it is a bucket keyed by a string.
type Limiter struct {
	mu sync.Mutex

	// burst is how many calls may happen at once, and refill is how fast
	// tokens come back.
	burst  float64
	refill float64 // tokens per second

	buckets map[string]*bucket

	// now is injectable so the tests can move time without sleeping. A test
	// that sleeps for a refill window is a test somebody deletes when the
	// suite gets slow.
	now func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// ErrLimited is returned when a caller has spent its budget.
//
// It names the wait rather than the limit. Telling a caller "you may make 30
// calls a minute" invites them to build a client that makes exactly 30 calls a
// minute; telling them "try again in 4 seconds" answers the only question they
// have.
type ErrLimited struct{ RetryAfter time.Duration }

func (e ErrLimited) Error() string {
	return fmt.Sprintf(
		"this organisation has made too many gateway calls; try again in %s",
		e.RetryAfter.Round(time.Second))
}

// New builds a limiter allowing `burst` calls at once, refilling to full over
// `window`.
//
// A zero or negative burst permits nothing, which is the same fail-closed
// default the egress allow-list takes: a limiter configured with nonsense
// should refuse rather than wave everything through.
func New(burst int, window time.Duration) *Limiter {
	if burst < 0 {
		burst = 0
	}
	if window <= 0 {
		window = time.Minute
	}
	return &Limiter{
		burst:   float64(burst),
		refill:  float64(burst) / window.Seconds(),
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// Allow spends one token for a key, or returns ErrLimited.
func (l *Limiter) Allow(key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, seen := l.buckets[key]
	if !seen {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.refill
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}

	if b.tokens < 1 {
		wait := time.Duration(0)
		if l.refill > 0 {
			wait = time.Duration((1 - b.tokens) / l.refill * float64(time.Second))
		}
		return ErrLimited{RetryAfter: wait}
	}

	b.tokens--
	return nil
}
