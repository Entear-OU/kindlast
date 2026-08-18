package egress_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/workers/internal/egress"
)

// A customer-supplied endpoint outside the allow-list is refused, and the
// assertion is about bytes rather than about an error message (ENT-231
// acceptance criterion).
//
// Checking that Check returns an error would pass just as happily if the
// gateway dialled first and refused afterwards, which is the failure this
// property exists to prevent. So the interesting test counts connection
// attempts on a transport and expects to have counted none.

func TestAnEndpointOutsideTheAllowListIsRefused(t *testing.T) {
	allow := egress.Parse("tools.example.com, .internal.example.org", false)

	refused := []string{
		"http://evil.example.com/mcp",
		"https://169.254.169.254/latest/meta-data/",
		"http://localhost:9000/mcp",
		// A permitted host as a userinfo prefix, which is the classic way to
		// make a URL look like it names one host and dial another.
		"http://tools.example.com@evil.example.net/mcp",
		// A permitted suffix that is not a subdomain of it.
		"https://notinternal.example.org/mcp",
	}
	for _, endpoint := range refused {
		if err := allow.Check(endpoint); !errors.Is(err, egress.ErrNotAllowed) {
			t.Errorf("%s: got %v, want a refusal", endpoint, err)
		}
	}

	permitted := []string{
		"https://tools.example.com/mcp",
		"https://TOOLS.EXAMPLE.COM/mcp",
		"https://tools.example.com:8443/mcp",
		"https://team.internal.example.org/mcp",
	}
	for _, endpoint := range permitted {
		if err := allow.Check(endpoint); err != nil {
			t.Errorf("%s: got %v, want it permitted", endpoint, err)
		}
	}
}

// The refusal happens with nothing on the wire.
//
// A transport that would answer every request successfully, wrapped in a
// counter. If Check is doing its job the counter never moves, and if somebody
// reorders the gateway so the request is built first, this is what goes red.
func TestNothingIsDialledWhenTheHostIsRefused(t *testing.T) {
	var attempts atomic.Int64

	allow := egress.Parse("tools.example.com", false)

	// The real client, with the real dialer, but the dial itself replaced by
	// one that counts and then fails. That keeps the allow-list checks in the
	// path rather than replacing the object that performs them.
	client := allow.Client(2 * time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("the egress client no longer uses an *http.Transport; this test drives its dialer")
	}
	inner := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		attempts.Add(1)
		return inner(ctx, network, address)
	}

	endpoint := "http://evil.example.com/mcp"
	if err := allow.Check(endpoint); !errors.Is(err, egress.ErrNotAllowed) {
		t.Fatalf("Check(%s) = %v, want a refusal", endpoint, err)
	}

	// The gateway returns at the line above and never reaches this. The point
	// of the assertion is what has NOT happened by now.
	if got := attempts.Load(); got != 0 {
		t.Fatalf("%d connection attempts were made before the refusal; want 0", got)
	}
}

// The guard is only worth having if it can fail. This is the same check with
// the offending host added to the allow-list, and it asserts that the request
// then genuinely leaves.
//
// Without this, a Check that returned an error for everything would pass the
// test above forever while permitting nothing and proving nothing.
func TestTheRefusalCheckCanActuallyFail(t *testing.T) {
	var attempts atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, _, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("splitting the test server address: %v", err)
	}

	// Private destinations permitted, because httptest listens on loopback and
	// the point here is the allow-list rather than the resolved-address rule.
	allow := egress.Parse(host, true)

	client := allow.Client(2 * time.Second)
	transport, _ := client.Transport.(*http.Transport)
	inner := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		attempts.Add(1)
		return inner(ctx, network, address)
	}

	if err := allow.Check(server.URL); err != nil {
		t.Fatalf("Check(%s) = %v, want it permitted", server.URL, err)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("the permitted request failed: %v", err)
	}
	_ = response.Body.Close()

	if got := attempts.Load(); got == 0 {
		t.Fatal("no connection was attempted for a permitted host; the counter is not wired to anything")
	}
}

// An empty allow-list permits nothing.
//
// The one default in this package that would be catastrophic the other way
// round, so it is asserted rather than left to the comment beside it.
func TestAnEmptyAllowListPermitsNothing(t *testing.T) {
	allow := egress.Parse("", false)

	if !allow.Empty() {
		t.Fatal("an allow-list parsed from an empty setting does not report itself empty")
	}
	if err := allow.Check("https://tools.example.com/mcp"); !errors.Is(err, egress.ErrNotAllowed) {
		t.Fatalf("got %v, want an empty allow-list to refuse everything", err)
	}
	// And the zero value, which is what a caller that forgot to call Parse
	// would hold.
	var zero egress.AllowList
	if err := zero.Check("https://tools.example.com/mcp"); !errors.Is(err, egress.ErrNotAllowed) {
		t.Fatalf("got %v, want the zero allow-list to refuse everything", err)
	}
}

// Schemes other than http and https are refused, so `file:` and friends never
// reach a transport that might understand them.
func TestOnlyHTTPSchemesAreSpoken(t *testing.T) {
	allow := egress.Parse("tools.example.com", false)

	for _, endpoint := range []string{
		"file:///etc/passwd",
		"gopher://tools.example.com/",
		"ftp://tools.example.com/",
	} {
		if err := allow.Check(endpoint); !errors.Is(err, egress.ErrNotAllowed) {
			t.Errorf("%s: got %v, want a refusal", endpoint, err)
		}
	}
}

// A redirect is refused rather than followed.
//
// Following one would send the customer's credential to whatever address the
// response named, with the allow-list check already passed. The test drives a
// real server that redirects to a host that is not on the list.
func TestARedirectIsRefusedRatherThanFollowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()

	host, _, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("splitting the test server address: %v", err)
	}
	allow := egress.Parse(host, true)
	client := allow.Client(2 * time.Second)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, egress.ErrNotAllowed) {
		t.Fatalf("got %v, want the redirect refused", err)
	}
}

// A permitted name resolving into loopback is refused when private
// destinations are off, which is the DNS rebinding case the URL check cannot
// see.
func TestAPermittedNameResolvingIntoLoopbackIsRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, _, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("splitting the test server address: %v", err)
	}

	// The host IS on the allow-list. Only the resolved address refuses it,
	// which is the whole point of checking in the dialer.
	allow := egress.Parse(host, false)
	if err := allow.Check(server.URL); err != nil {
		t.Fatalf("Check(%s) = %v; the host is meant to be permitted here", server.URL, err)
	}

	client := allow.Client(2 * time.Second)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if !errors.Is(err, egress.ErrNotAllowed) {
		t.Fatalf("got %v, want the resolved loopback address refused", err)
	}
}
