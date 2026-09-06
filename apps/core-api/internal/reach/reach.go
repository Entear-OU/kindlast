// Package reach measures whether the Intelligence service is answering
// (ENT-296).
//
// # WHY MEASURED AND NOT DECLARED
//
// core-api already had a notion of Intelligence availability, and it was a
// configuration fact: `intelligence_available` reports whether this deployment
// was given an Intelligence URL. That cannot go false for a service that was
// configured and has since stopped, so a crashed container still answered
// `true` and the failure only appeared at the call. The console's presence dot
// beside Kindy was a hardcoded green for the same reason: there was nothing
// truthful for it to read.
//
// This package is the missing half. It answers "is it answering right now",
// which is a different question from "was it configured", and both are needed:
// a deployment running no Intelligence is supported, and drawing it as an
// outage would be its own lie.
//
// # WHY IT IS HERE AND NOT IN THE CONSOLE
//
// The console talks to core-api and holds no address for anything else. core-api
// already holds the Intelligence URL and already owns the availability concept,
// so putting the probe here keeps the number of processes that know where
// Intelligence lives at one.
//
// # WHY IT IS CACHED
//
// The rail is chrome on every console page, so an uncached probe would put one
// request on Intelligence per navigation per reader, to draw a dot. The window
// is short enough that a stopped service goes grey promptly and long enough
// that a busy console costs one probe rather than hundreds.
package reach

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// State is what a console should draw.
type State int

const (
	// NotConfigured is a deployment that runs no Intelligence. Supported, not
	// broken: findings still carry the deterministic text the sweep wrote.
	NotConfigured State = iota
	// Reachable means the process answered its liveness path. It is not a
	// promise that a model will answer: that depends on the organisation's own
	// choice and is resolved per call, by the router.
	Reachable
	// Unreachable means configured and not answering. Something to fix.
	Unreachable
)

func (s State) String() string {
	switch s {
	case Reachable:
		return "reachable"
	case Unreachable:
		return "unreachable"
	default:
		return "not_configured"
	}
}

// Result is a state and when it was actually measured.
//
// `At` is the measurement's time rather than the answer's, so a held result
// reports the probe behind it. A caller that wants to say how fresh a claim is
// can, instead of assuming the answer is live.
type Result struct {
	State State
	At    time.Time
}

// Prober probes one Intelligence deployment and holds the answer briefly.
//
// The zero value is not useful; build one with New.
type Prober struct {
	url    string
	window time.Duration
	client *http.Client

	// Timeout bounds one probe independently of the caller's context, so a
	// console page cannot hang on a wedged dependency. Exported so a test can
	// shorten it; there is no reason for a deployment to change it.
	Timeout time.Duration

	// One lock over both the cache and the probe, deliberately. Twenty readers
	// navigating at once should produce one probe rather than twenty, and the
	// simplest thing that guarantees it is that they queue behind the first.
	// The probe is bounded by Timeout, so the queue is bounded too.
	mu     sync.Mutex
	cached Result
	// Separate from cached.At because a NotConfigured result carries no
	// measurement time and would otherwise re-probe forever.
	freshUntil time.Time
}

// The liveness path Intelligence serves. Unauthenticated and discloses only
// that a process is accepting connections; see apps/intelligence health.py.
const healthPath = "/healthz"

// New builds a prober. An empty url is a deployment that runs no Intelligence,
// which probes nothing and always answers NotConfigured.
//
// A nil client uses one built here rather than http.DefaultClient, because the
// default has no timeout at all and this is a call to a service that may be
// wedged rather than down.
func New(url string, window time.Duration, client *http.Client) *Prober {
	if client == nil {
		client = &http.Client{}
	}
	return &Prober{
		url:     url,
		window:  window,
		client:  client,
		Timeout: 2 * time.Second,
	}
}

// Probe returns the current state, measuring it if the held answer has aged
// out.
//
// It never returns an error. Every failure mode of a liveness probe is the
// answer rather than an exception: a console asking whether to draw a dot has
// nothing useful to do with a transport error that it would not do with
// Unreachable.
func (p *Prober) Probe(ctx context.Context) Result {
	if p.url == "" {
		return Result{State: NotConfigured}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Now().Before(p.freshUntil) {
		return p.cached
	}

	result := Result{State: p.measure(ctx), At: time.Now()}
	p.cached = result
	p.freshUntil = result.At.Add(p.window)
	return result
}

func (p *Prober) measure(ctx context.Context) State {
	// DETACHED FROM THE CALLER'S DEADLINE, not merely bounded by it. This
	// answer is shared: the reader whose request happens to drive the probe
	// should not be able to make every other reader's dot grey by navigating
	// away. Cancellation is still honoured through the parent for shutdown.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.Timeout)
	defer cancel()

	// HEAD, not GET: nothing here reads the body, and a liveness probe should
	// move as few bytes as it can.
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, p.url+healthPath, nil)
	if err != nil {
		// A malformed configured URL. Unreachable rather than a panic: an
		// operator with a typo gets a grey dot and a log line, not a console
		// that will not render.
		return Unreachable
	}

	res, err := p.client.Do(req)
	if err != nil {
		return Unreachable
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return Unreachable
	}
	return Reachable
}
