package dispatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
)

// The retention loop (ENT-242).
//
// What is tested here is not the retention rule, which is the database's and is
// covered against it in db/tests and in the store package. It is that the rule
// is ever reached: the reclaim runs on its own timer inside the loop that
// already exists, it runs once at start rather than only on the first tick, it
// carries the configured window, and a failure does not end the loop.
//
// The last two are the ones that fail silently. A window that never arrives, or
// a loop that exited hours ago, both look exactly like a deployment with
// nothing to reclaim.
//
// PROVEN ABLE TO FAIL. Removing the `d.reclaimOnce(ctx)` call before the select
// turns "reclaims once before the first tick" red on its own; passing a
// hardcoded zero instead of `d.reclaimRetention` turns "carries the configured
// window" red on its own.

// reclaimRecorder is the Outbox surface with the drain stubbed out. Drain
// reports an empty queue, so the delivery half of the loop does nothing and the
// test is about the other half.
type reclaimRecorder struct {
	mu       sync.Mutex
	calls    []reclaimCall
	fail     error
	called   chan struct{}
	closeOne sync.Once
}

type reclaimCall struct {
	retention time.Duration
	batch     int
}

func (r *reclaimRecorder) Drain(context.Context, int, postgres.Deliver) (postgres.DrainResult, error) {
	return postgres.DrainResult{}, nil
}

func (r *reclaimRecorder) ReclaimOutbox(
	_ context.Context, retention time.Duration, batch int,
) (postgres.ReclaimResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, reclaimCall{retention: retention, batch: batch})
	r.mu.Unlock()
	r.closeOne.Do(func() { close(r.called) })

	if r.fail != nil {
		return postgres.ReclaimResult{}, r.fail
	}
	return postgres.ReclaimResult{Redacted: 1, Abandoned: 2}, nil
}

func (r *reclaimRecorder) seen() []reclaimCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]reclaimCall(nil), r.calls...)
}

// silentChannel is somewhere a message would go if there were one.
type silentChannel struct{}

func (silentChannel) Name() string                                   { return "test" }
func (silentChannel) Send(context.Context, delivery.Message) error   { return nil }

// runUntilReclaimed starts the loop, waits for the first reclaim, and stops.
func runUntilReclaimed(t *testing.T, outbox *reclaimRecorder, d *Dispatcher) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	select {
	case <-outbox.called:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("the reclaim was never called")
	}
	cancel()
	<-done
}

func newRecorder() *reclaimRecorder {
	return &reclaimRecorder{called: make(chan struct{})}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestTheReclaimRunsBeforeTheFirstTick(t *testing.T) {
	// A deployment restarted more often than the reclaim interval would never
	// reclaim at all if the first pass waited for a tick, and nothing about
	// that failure is visible: the logs are identical to a deployment with
	// nothing to clear. So the first pass is unconditional, and the interval is
	// set to an hour here to prove this call is not a tick.
	outbox := newRecorder()
	d := New(outbox, silentChannel{}, quietLogger(), time.Hour, 5)

	runUntilReclaimed(t, outbox, d)

	if got := len(outbox.seen()); got == 0 {
		t.Fatal("the reclaim did not run before the first tick")
	}
}

func TestTheReclaimCarriesTheConfiguredWindow(t *testing.T) {
	// The window is the whole decision, and it crosses two packages and a type
	// conversion into a Postgres interval to get where it is used. A window
	// that arrives as zero would redact every delivered body on the first pass,
	// which is a data loss nobody notices, and a window that arrived as
	// something enormous would reclaim nothing, which is the bug this issue
	// exists to fix arriving back in a different shape.
	outbox := newRecorder()
	d := New(outbox, silentChannel{}, quietLogger(), time.Hour, 5)

	runUntilReclaimed(t, outbox, d)

	calls := outbox.seen()
	if len(calls) == 0 {
		t.Fatal("the reclaim never ran")
	}
	if calls[0].retention != DeliveredBodyRetention {
		t.Errorf("the reclaim was given %s, want %s", calls[0].retention, DeliveredBodyRetention)
	}
	if calls[0].batch != DefaultReclaimBatch {
		t.Errorf("the reclaim was given a batch of %d, want %d", calls[0].batch, DefaultReclaimBatch)
	}
}

func TestAFailedReclaimDoesNotEndTheLoop(t *testing.T) {
	// Every reason a reclaim fails is one that resolves on its own, and a loop
	// that exits on the first of them stops delivering mail too, because the
	// drain shares it. The process stays up and healthy throughout, which is
	// what makes this worth a test rather than an assumption.
	outbox := newRecorder()
	outbox.fail = errors.New("the database is restarting")

	d := New(outbox, silentChannel{}, quietLogger(), 5*time.Millisecond, 5)
	d.reclaimInterval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		if len(outbox.seen()) >= 3 {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("the loop stopped reclaiming after %d failures", len(outbox.seen()))
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel()
	<-done
}
