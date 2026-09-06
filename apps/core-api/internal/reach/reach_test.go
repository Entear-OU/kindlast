package reach_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/reach"
)

func TestNoURLIsNotConfiguredAndProbesNothing(t *testing.T) {
	// A deployment that runs no Intelligence is supported rather than broken,
	// and it must not be reported as an outage. It must also not cost a
	// request: there is nothing to ask.
	p := reach.New("", time.Second, nil)

	got := p.Probe(context.Background())

	if got.State != reach.NotConfigured {
		t.Fatalf("state = %v, want NotConfigured", got.State)
	}
	if !got.At.IsZero() {
		t.Fatalf("At = %v, want zero: nothing was measured", got.At)
	}
}

func TestAnAnsweringServiceIsReachable(t *testing.T) {
	var path atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path.Store(r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	got := reach.New(server.URL, time.Second, nil).Probe(context.Background())

	if got.State != reach.Reachable {
		t.Fatalf("state = %v, want Reachable", got.State)
	}
	if got.At.IsZero() {
		t.Fatal("At is zero: a measured answer must carry when it was measured")
	}
	if p, _ := path.Load().(string); p != "/healthz" {
		t.Fatalf("probed %q, want /healthz", p)
	}
}

func TestAServiceAnsweringBadlyIsUnreachable(t *testing.T) {
	// 200 or nothing. A process answering 500 on its liveness path is not one
	// a console should draw as present.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	got := reach.New(server.URL, time.Second, nil).Probe(context.Background())

	if got.State != reach.Unreachable {
		t.Fatalf("state = %v, want Unreachable", got.State)
	}
}

func TestAServiceThatIsNotThereIsUnreachableRatherThanAnError(t *testing.T) {
	// THE POINT OF THE WHOLE FEATURE. A stopped container must produce a
	// grey dot, not a failed page render.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	got := reach.New(url, time.Second, nil).Probe(context.Background())

	if got.State != reach.Unreachable {
		t.Fatalf("state = %v, want Unreachable", got.State)
	}
	if got.At.IsZero() {
		t.Fatal("At is zero: a failed probe was still a measurement")
	}
}

func TestTheAnswerIsHeldForTheCacheWindow(t *testing.T) {
	// The rail is chrome on every console page, so an uncached probe would put
	// one request on Intelligence per navigation per reader.
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := reach.New(server.URL, time.Minute, nil)
	first := p.Probe(context.Background())
	second := p.Probe(context.Background())

	if n := calls.Load(); n != 1 {
		t.Fatalf("probed %d times, want 1: the second answer should be held", n)
	}
	if !second.At.Equal(first.At) {
		t.Fatal("the held answer must report the time it was actually measured")
	}
}

func TestTheAnswerIsRemeasuredAfterTheWindow(t *testing.T) {
	// A cache that never expires is a status that cannot recover, which is
	// worse than no status at all.
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := reach.New(server.URL, time.Nanosecond, nil)
	p.Probe(context.Background())
	time.Sleep(time.Millisecond)
	p.Probe(context.Background())

	if n := calls.Load(); n != 2 {
		t.Fatalf("probed %d times, want 2: the window had passed", n)
	}
}

func TestAFailedProbeRecoversOnTheNextWindow(t *testing.T) {
	// The failure must not be sticky. An operator restarting Intelligence
	// should see the dot come back without restarting core-api.
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	p := reach.New(server.URL, time.Nanosecond, nil)
	if got := p.Probe(context.Background()); got.State != reach.Unreachable {
		t.Fatalf("state = %v, want Unreachable", got.State)
	}

	healthy.Store(true)
	time.Sleep(time.Millisecond)

	if got := p.Probe(context.Background()); got.State != reach.Reachable {
		t.Fatalf("state = %v, want Reachable after recovery", got.State)
	}
}

func TestConcurrentProbesDoNotStampede(t *testing.T) {
	// Twenty readers navigating at once is one probe, not twenty. Without the
	// lock this is the shape that turns a slow dependency into an outage.
	var calls atomic.Int64
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := reach.New(server.URL, time.Minute, nil)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Probe(context.Background())
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("probed %d times, want 1", n)
	}
}

func TestASlowServiceIsUnreachableRatherThanASlowPage(t *testing.T) {
	// A console page must not hang on a wedged dependency. The probe's own
	// timeout is what bounds it, independent of the caller's context.
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
		w.WriteHeader(http.StatusOK)
	}))
	defer func() {
		close(block)
		server.Close()
	}()

	p := reach.New(server.URL, time.Minute, nil)
	p.Timeout = 20 * time.Millisecond

	start := time.Now()
	got := p.Probe(context.Background())
	elapsed := time.Since(start)

	if got.State != reach.Unreachable {
		t.Fatalf("state = %v, want Unreachable", got.State)
	}
	if elapsed > time.Second {
		t.Fatalf("probe took %v: it must be bounded by its own timeout", elapsed)
	}
}
