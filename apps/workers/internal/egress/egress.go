// Package egress decides which addresses this deployment may dial, and builds
// an HTTP client that cannot dial anything else (ENT-231; OWASP LLM04).
//
// # WHY AN ALLOW-LIST AND NOT A DENY-LIST
//
// The endpoint of an MCP connection is a URL a customer typed into a form. A
// deny-list has to enumerate everything dangerous: the cloud metadata address,
// every loopback spelling, every private range, every link-local range, and
// then every name that resolves into one of them. Each of those is a thing
// somebody has to remember. An allow-list enumerates what is permitted, which
// is a short list an operator wrote on purpose, and its failure mode is a
// refusal rather than a request.
//
// # WHY THE CHECK IS IN THE DIALER AND NOT ONLY BEFORE THE REQUEST
//
// Checking the URL before calling the endpoint is necessary and not
// sufficient. Two things get past it, and this deployment is a bad place for
// either: the gateway sits on a network where postgres, Redis, the
// authorization server's management API and the model server all answer, some
// of them unauthenticated.
//
// A REDIRECT. `http.Client` follows redirects by default, so a permitted host
// answering `302 Location: http://169.254.169.254/` turns one allowed request
// into one forbidden request with the check already passed. This package
// refuses redirects outright, which is the right answer for a JSON-RPC
// endpoint: MCP has no reason to redirect, and following one would mean
// sending the customer's credential to whatever address the response named.
//
// DNS. A permitted host name can resolve to an address inside this
// deployment's own network, deliberately or by accident, and a name that
// resolved to a public address for a check can resolve to a private one a
// moment later for the dial.
//
// # RESOLVE ONCE, CHECK, THEN DIAL THE ADDRESS THAT WAS CHECKED
//
// The dialer below looks the name up itself, refuses every address that comes
// back if any of them is somewhere this deployment does not permit, and then
// connects to a LITERAL ADDRESS from that same answer. No second lookup
// happens, so there is no window for a rebind to move the target between the
// check and the connection.
//
// The weaker version, dialling by name and inspecting `conn.RemoteAddr()`
// afterwards, is nearly as safe and is not the same: it has already completed
// a TCP handshake with the forbidden address by the time it refuses, which is
// a port scanner for anybody who can time the difference. Refusing before the
// connection exists costs one explicit resolve and removes that entirely.
//
// # WHAT "REFUSED BEFORE ANY REQUEST LEAVES" MEANS HERE
//
// `Check` returns an error and nothing has been sent: no DNS lookup, no TCP
// connection, no bytes. The gateway calls it before it builds a request at
// all, and the test for that property asserts against a transport that records
// every attempt and expects to have recorded none.
package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotAllowed is returned for any address this deployment may not reach.
//
// One error for every reason, and the message names the host rather than the
// rule it broke. A caller learning which rule refused them is a caller
// learning the shape of the allow-list, and the person on the other end of
// this is a customer who should be told "that host is not permitted" and then
// told the permitted ones by their operator rather than by probing.
var ErrNotAllowed = errors.New("that address is not on this deployment's egress allow-list")

// AllowList is the set of hosts a deployment permits.
//
// The zero value permits nothing, and that is the important default. A gateway
// configured with no allow-list must refuse every fetch rather than permit
// every fetch: the two are one typo apart and only one of them is recoverable.
type AllowList struct {
	// Exact host names, lower-cased, without a port.
	hosts map[string]struct{}
	// Suffix rules, written `.example.com`, matching any subdomain and not the
	// bare domain. A rule that matched the bare domain too would make
	// `.example.com` and `example.com` the same entry, and an operator who
	// wrote the first meaning subdomains only would silently get both.
	suffixes []string

	// allowPrivate lets a permitted name resolve into a private or loopback
	// address.
	//
	// FALSE IS THE DEFAULT AND IT IS THE ONE THAT MATTERS FOR A HOSTED
	// DEPLOYMENT: without it, an account holder who can type a URL can point
	// this gateway at the instance metadata service, and the allow-list is the
	// only thing between them and a set of cloud credentials.
	//
	// TRUE IS A DELIBERATE DECISION AN OPERATOR MAKES, and that is why it
	// exists rather than being a hard refusal. A self-hoster's MCP server is on
	// their own network by definition. It is NOT set on the bundled compose
	// stack, because that stack is exactly the network this flag would open: a
	// name like `postgres-app` resolves into 172.16/12, and refusing private
	// destinations is what stops a customer-supplied URL reaching it.
	allowPrivate bool

	// resolve looks a host name up. A field so a test can present a name that
	// resolves into this deployment's own network without needing a DNS server
	// that will answer that way, which is the attack worth demonstrating and
	// the one that is otherwise untestable.
	//
	// Never nil in practice: Parse fills it in. A nil one falls back to the
	// real resolver rather than skipping the check, because a zero value that
	// disabled the check would be the worst possible default.
	resolve func(ctx context.Context, host string) ([]net.IP, error)
}

// Parse builds an allow-list from a comma-separated setting.
//
// Entries are host names, optionally with a leading dot for "and every
// subdomain". A port is not part of an entry: an operator permitting a host
// permits it, and a port would invite the belief that a different port on the
// same host is a different trust decision, which it is not.
func Parse(setting string, allowPrivate bool) AllowList {
	list := AllowList{
		hosts:        map[string]struct{}{},
		allowPrivate: allowPrivate,
		resolve:      lookup,
	}
	for _, raw := range strings.Split(setting, ",") {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, ".") {
			list.suffixes = append(list.suffixes, entry)
			continue
		}
		list.hosts[entry] = struct{}{}
	}
	return list
}

// Empty reports whether this allow-list permits nothing.
//
// Worth asking at boot rather than at the first fetch, so an operator who has
// not configured one hears about it from a log line rather than from a
// customer.
func (a AllowList) Empty() bool { return len(a.hosts) == 0 && len(a.suffixes) == 0 }

// AllowsPrivateDestinations reports whether a permitted name may resolve into
// a private or loopback address.
func (a AllowList) AllowsPrivateDestinations() bool { return a.allowPrivate }

// WithResolver replaces how names are looked up.
//
// FOR TESTS ONLY, and it does not weaken anything: what it substitutes is the
// source of the addresses, not the check applied to them. A test uses it to
// present a name that resolves into this deployment's own network, which is
// the attack worth demonstrating and which no real DNS server will answer with
// on demand.
func (a AllowList) WithResolver(resolve func(ctx context.Context, host string) ([]net.IP, error)) AllowList {
	a.resolve = resolve
	return a
}

// Permits reports whether a host name is on the list.
func (a AllowList) Permits(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if _, ok := a.hosts[host]; ok {
		return true
	}
	for _, suffix := range a.suffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// Check refuses an endpoint before anything is sent.
//
// Everything decidable from the URL alone is decided here: the scheme, the
// host, and the allow-list. What is not decidable here, which is where the
// name actually resolves to, is decided in the dialer.
func (a AllowList) Check(endpoint string) error {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return fmt.Errorf("%w: that is not a URL", ErrNotAllowed)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		// `file:`, `gopher:` and friends. Named rather than assumed, because a
		// default branch letting an unknown scheme through would be the one
		// case nobody tests.
		return fmt.Errorf("%w: %q is not a scheme this gateway speaks", ErrNotAllowed, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: that URL names no host", ErrNotAllowed)
	}
	if !a.Permits(host) {
		return fmt.Errorf("%w: %s", ErrNotAllowed, host)
	}
	return nil
}

// Client builds an HTTP client that can reach the allow-list and nothing else.
//
// # THE THREE THINGS THIS CLIENT DOES DIFFERENTLY FROM http.DefaultClient
//
// It refuses redirects, for the reason in the package comment: following one
// would send the customer's credential to an address the response chose.
//
// It checks the resolved IP in the dialer, so a permitted name resolving
// somewhere this deployment does not permit is refused at the moment the
// destination is finally known.
//
// And it has a timeout, because a gateway whose outbound call can hang forever
// can be made to hold all of its own goroutines by anybody running a slow
// server.
func (a AllowList) Client(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("%w: %s", ErrNotAllowed, address)
			}
			if !a.Permits(host) {
				// Reachable when a redirect or a proxy setting changed the
				// destination behind the check above. Refusing here rather
				// than trusting the earlier check is the belt to that
				// particular pair of braces.
				return nil, fmt.Errorf("%w: %s", ErrNotAllowed, host)
			}

			// RESOLVE ONCE, HERE, AND DIAL WHAT CAME BACK.
			//
			// Everything after this line uses a literal address from this one
			// answer, so a second lookup cannot move the target between the
			// check and the connection.
			addresses, err := a.resolveHost(ctx, host)
			if err != nil {
				return nil, err
			}

			// EVERY ADDRESS IS CHECKED, NOT ONLY THE ONE DIALLED. A name
			// answering with one public address and one private one is the
			// rebinding attack with the two answers delivered at once, and
			// picking the first would make which one arrives first decide
			// whether this deployment is safe.
			for _, ip := range addresses {
				if err := a.checkAddress(ip); err != nil {
					return nil, err
				}
			}

			var lastErr error
			for _, ip := range addresses {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("%w: %s resolved to nothing", ErrNotAllowed, host)
			}
			return nil, lastErr
		},
		// A small pool, because the population of endpoints is small and a
		// large set of idle connections into customer systems is a liability
		// rather than a saving.
		MaxIdleConns:          8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			return fmt.Errorf("%w: %s redirected, and this gateway does not follow redirects",
				ErrNotAllowed, request.URL.Hostname())
		},
	}
}

// resolveHost looks a name up, or reads a literal address straight out of the
// URL.
//
// A literal skips the resolver rather than being handed to it, because a
// resolver asked for "169.254.169.254" would answer with it and the extra hop
// buys nothing.
func (a AllowList) resolveHost(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	resolve := a.resolve
	if resolve == nil {
		// A zero-value AllowList reaching this far permits no host at all, so
		// this is unreachable. Falling back to the real resolver rather than
		// to nil, because "no resolver configured" must never read as "no
		// addresses to check".
		resolve = lookup
	}

	addresses, err := resolve(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s could not be resolved", ErrNotAllowed, host)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: %s resolved to nothing", ErrNotAllowed, host)
	}
	return addresses, nil
}

func lookup(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// checkAddress refuses an address a customer's endpoint has no business being
// at.
//
// The ranges named here are the ones that turn an outbound fetch into a read of
// this deployment's own infrastructure. On the bundled stack that is not
// abstract: `postgres-app`, `redis`, `auth` and `model` all answer on
// 172.16/12, some of them with no authentication worth the name, and the
// gateway sits on the same network.
//
//	loopback      127.0.0.0/8 and ::1
//	private       10/8, 172.16/12, 192.168/16 and fc00::/7 (Go's IsPrivate
//	              covers the IPv6 unique-local range as well as the three
//	              RFC 1918 ones)
//	link-local    169.254/16 and fe80::/10, which is where every cloud
//	              provider's instance metadata service lives
//	unspecified   0.0.0.0 and ::
func (a AllowList) checkAddress(ip net.IP) error {
	// Link-local is refused even with private destinations allowed, and that
	// asymmetry is the point. A self-hoster genuinely means 10.0.0.0/8 when
	// they turn private destinations on; nobody means 169.254.169.254.
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("%w: %s is link-local", ErrNotAllowed, ip)
	}
	if ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("%w: %s is multicast", ErrNotAllowed, ip)
	}
	if a.allowPrivate {
		return nil
	}
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("%w: %s is loopback", ErrNotAllowed, ip)
	case ip.IsPrivate():
		return fmt.Errorf("%w: %s is a private address", ErrNotAllowed, ip)
	case ip.IsUnspecified():
		return fmt.Errorf("%w: %s is unspecified", ErrNotAllowed, ip)
	}
	return nil
}
