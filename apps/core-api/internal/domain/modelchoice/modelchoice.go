// Package modelchoice holds the rules about where an organisation's model runs
// (ENT-236, §26.6).
//
// # THE TWO LAYERS, AND WHY THERE ARE TWO
//
// An operator decides what is possible; an organisation decides which of those
// possibilities it uses. `KINDLAST_BYOK_PROVIDERS` is the first layer and this
// package parses it. "Nobody at this company may point our compliance data at
// an external API" has to be a sentence somebody can enforce in configuration,
// not a policy they have to trust every owner of every organisation to follow,
// so an empty setting permits nothing and that is the default.
//
// The second layer is one row in `org_model_config`, which can only ever name
// a provider the first layer already permits, and is re-checked against it on
// every use rather than once at insert. That last part is not tidiness: a
// provider an operator withdraws must stop being reachable for organisations
// that already chose it, and a check done only at write time would leave those
// organisations sending data to a provider the deployment has removed.
//
// # A USER-SUPPLIED BASE URL IS AN SSRF UNTIL PROVED OTHERWISE
//
// Whatever holds this endpoint will make an HTTP request to it, from inside the
// deployment, with a bearer token attached. That is the classic shape: the
// interesting targets are the metadata service on 169.254.169.254, Postgres and
// Redis on the compose network, and anything else reachable from a container
// and not from the internet.
//
// So the check is on the RESOLVED ADDRESSES and not on the string. String
// checks are defeated by a hostname the attacker controls, which resolves
// wherever they like and reads as entirely ordinary. Every address a host
// resolves to has to be public, because one private answer among several is
// the attack rather than an edge case.
//
// # WHAT THIS DOES NOT CLOSE, SAID OUT LOUD
//
// DNS rebinding. Between this check and the request, an attacker controlling
// the zone can move the name. Closing it properly means checking the address at
// connect time, in the dialler that makes the call, and the dialler here is
// httpx inside the Intelligence container rather than anything this package can
// reach. What bounds it instead is the host allow-list: an operator's list
// names the hosts, so rebinding requires a name the operator already trusts.
// The PR records this as the residual risk rather than pretending otherwise.
package modelchoice

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// ErrNotPermitted is returned when a deployment does not permit a provider.
//
// Distinguished from a validation failure, because they want different
// reactions: one is an operator's decision the caller cannot argue with, the
// other is an endpoint the caller typed wrong.
var ErrNotPermitted = errors.New("this deployment does not permit that model provider")

// ErrPrivateAddress is returned when an endpoint resolves inside the
// deployment.
var ErrPrivateAddress = errors.New(
	"that endpoint resolves to an address inside this deployment, so it cannot be a model provider")

// ConsequenceNotice is what a person is told before they turn this on.
//
// ONE SENTENCE, IN ONE PLACE, AND SERVED OVER THE API. The console renders it
// rather than composing its own, so a self-hoster's alternative client shows
// the same warning and there is no second copy to drift. §26.6 makes this the
// difference between a compliance event and a settings toggle: the customer has
// to have been told what changes before they can be said to have decided it.
const ConsequenceNotice = "Findings, compliance profile facts and DSAR content for this " +
	"organisation will leave this deployment and be processed by the provider you name. " +
	"That provider becomes a sub-processor you are responsible for recording, and this " +
	"deployment stops being one that can run with no outbound internet. The change is " +
	"written to your audit log with your name on it."

// The action types this decision writes into `audit_log`.
//
// Four rather than one, because "we started sending data to a provider", "we
// moved to a different provider", "we changed the key" and "we stopped" are
// four different things to have happened, and a record calling them all
// `model_provider_updated` could not answer the question it exists for.
//
// Here rather than beside either writer, because the handler picks which one
// applies and the store writes it, and a vocabulary split across two packages
// is one that drifts.
const (
	ActionEnabled  = "model_provider_enabled"
	ActionChanged  = "model_provider_changed"
	ActionRotated  = "model_provider_rotated"
	ActionReverted = "model_provider_reverted"
)

// Provider is one option an operator permits.
type Provider struct {
	// Name is what the audit row and the customer's sub-processor list call it.
	Name string
	// Host is the endpoint's host, exactly. A leading dot makes it a suffix, so
	// `.openai.azure.com` permits a customer's own Azure resource without
	// permitting every host that happens to end in those characters.
	Host string
}

// Suffix reports whether this provider permits subdomains rather than one host.
func (p Provider) Suffix() bool { return strings.HasPrefix(p.Host, ".") }

// Lookup resolves a host. Injected so the rules above are testable without a
// network, and so the caller decides which resolver is in play.
type Lookup func(ctx context.Context, host string) ([]netip.Addr, error)

// SystemLookup resolves through the host's resolver.
func SystemLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// ParseProviders reads `name=host,name=host` from the environment.
//
// Refuses an entry it could not check rather than skipping it. An operator who
// wrote `openai` with no host has expressed an intention this package cannot
// enforce, and silently dropping it would produce a deployment that permits
// nothing while its configuration says otherwise, which is a support call at
// best and a false sense of a boundary at worst.
func ParseProviders(spec string) ([]Provider, error) {
	var providers []Provider
	seen := map[string]struct{}{}

	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		name, host, found := strings.Cut(entry, "=")
		name = strings.TrimSpace(strings.ToLower(name))
		host = strings.TrimSpace(strings.ToLower(host))

		if !found || name == "" || host == "" {
			return nil, fmt.Errorf(
				"the model provider %q must be written `name=host`, for example `openai=api.openai.com`", entry)
		}
		if strings.ContainsAny(host, "/:@") {
			// A URL rather than a host. Accepting one would make the check
			// below compare a host against a string that is not one, which
			// fails open in the direction of matching nothing, so it would
			// look like a working allow-list that permits no endpoint at all.
			return nil, fmt.Errorf("the model provider %q names %q, which is a URL rather than a host", name, host)
		}
		if _, duplicate := seen[name]; duplicate {
			// One name, two hosts, means the audit row's provider no longer
			// identifies where the data went, which is the one thing it is for.
			return nil, fmt.Errorf("the model provider %q is listed more than once", name)
		}
		seen[name] = struct{}{}
		providers = append(providers, Provider{Name: name, Host: host})
	}
	return providers, nil
}

// Permitted finds a provider by name, or refuses.
func Permitted(providers []Provider, name string) (Provider, error) {
	wanted := strings.TrimSpace(strings.ToLower(name))
	for _, provider := range providers {
		if provider.Name == wanted {
			return provider, nil
		}
	}
	return Provider{}, fmt.Errorf("%w: %q", ErrNotPermitted, name)
}

// ValidateEndpoint decides whether one base URL may be dialled as one provider.
//
// Called both when the choice is made and again every time it is used, for the
// reason in the package comment.
func ValidateEndpoint(ctx context.Context, raw string, provider Provider, lookup Lookup) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("that endpoint is not a URL: %w", err)
	}
	if parsed.Scheme != "https" {
		// HTTPS ONLY, WITH NO LOOPBACK EXCEPTION. A model endpoint inside the
		// deployment is what `KINDLAST_MODEL_URL` is for, and it is the layer
		// that already serves that case without any of this. What reaches here
		// is by definition somebody else's service, and sending a customer's
		// compliance profile to it in the clear would be a worse disclosure
		// than the one this whole feature is about.
		return fmt.Errorf("a model provider endpoint must be https, not %q", parsed.Scheme)
	}
	if parsed.User != nil {
		// A credential in a URL is a credential in every log line that URL
		// appears in, including this deployment's.
		return errors.New("that endpoint carries credentials in the URL; put the key in the key field")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return errors.New("that endpoint names no host")
	}
	if !hostAllowed(host, provider) {
		return fmt.Errorf("%w: %q is not %s's endpoint", ErrNotPermitted, host, provider.Name)
	}

	// A literal address skips resolution and is checked directly, because
	// `LookupNetIP` on a literal answers with itself and the check below would
	// still be the one doing the work. Doing it explicitly makes that visible.
	if addr, err := netip.ParseAddr(host); err == nil {
		return checkPublic([]netip.Addr{addr})
	}

	addrs, err := lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("that endpoint's host does not resolve: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("that endpoint's host resolves to nothing: %q", host)
	}
	return checkPublic(addrs)
}

// hostAllowed compares a host against one provider's entry.
func hostAllowed(host string, provider Provider) bool {
	if provider.Suffix() {
		// A subdomain, and not the parent: `.openai.azure.com` permits
		// `acme.openai.azure.com` and not `openai.azure.com` itself. The dot is
		// what stops `notopenai.azure.com` matching, which a plain
		// `strings.HasSuffix` on the undotted form would have allowed.
		return strings.HasSuffix(host, provider.Host)
	}
	return host == provider.Host
}

// checkPublic refuses if ANY address is one this deployment can reach
// privately.
func checkPublic(addrs []netip.Addr) error {
	for _, addr := range addrs {
		// Unwrapped first, so `::ffff:10.0.0.1` is judged as 10.0.0.1 rather
		// than as an unremarkable IPv6 address.
		addr = addr.Unmap()
		if !isPublic(addr) {
			return fmt.Errorf("%w: %s", ErrPrivateAddress, addr)
		}
	}
	return nil
}

// cgnat and the other ranges Go's own helpers do not cover.
//
// Written out rather than left to `IsGlobalUnicast`, which calls all of these
// global: they are routable in the sense that word means and not reachable from
// the internet, which is precisely the property that makes them SSRF targets.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),   // RFC 6598, carrier grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // RFC 6890, IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // TEST-NET-1
	netip.MustParsePrefix("198.18.0.0/15"),   // RFC 2544, benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // TEST-NET-2
	netip.MustParsePrefix("203.0.113.0/24"),  // TEST-NET-3
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved
	netip.MustParsePrefix("::/128"),          // unspecified
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
}

func isPublic(addr netip.Addr) bool {
	if !addr.IsValid() ||
		addr.IsUnspecified() ||
		addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		!addr.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// LastFour is the hint the console shows in place of a key.
//
// Empty rather than something shorter when there is not enough key to take four
// characters from, because a two-character hint of a two-character value is the
// value. Empty when the tail is not alphanumeric too: the column refuses those,
// and failing a write at the end over a display string would be a worse answer
// than showing no hint.
func LastFour(key string) string {
	key = strings.TrimSpace(key)
	if len(key) < 8 {
		return ""
	}
	tail := key[len(key)-4:]
	for _, r := range tail {
		alphanumeric := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !alphanumeric {
			return ""
		}
	}
	return tail
}
