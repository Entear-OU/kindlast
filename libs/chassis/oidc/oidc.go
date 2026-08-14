// Package oidc verifies OAuth 2.0 access tokens locally against a JWKS
// discovered from an OpenID Connect issuer.
//
// It lives in the chassis because it is pure OAuth plumbing: it knows nothing
// about findings, organisations or compliance, and the second resource server
// needs the identical thing (core-api-surface §1.6, §21.5). It passes the
// §21.5 test, which is that it could be open-sourced without mentioning what
// this product does.
//
// Two properties drive the whole design, and both come from §1.4:
//
//   - Verification is local and in-process. Introspection (RFC 7662) would put
//     the authorization server in the hot path of every request and make it a
//     single point of failure for every page render. The trade is revocation
//     latency, which ten-minute access tokens bound and the jti deny-list
//     closes.
//   - Nothing about the issuer is hard-coded. The JWKS endpoint comes from the
//     discovery document, so a self-hoster points at their own Keycloak,
//     Authentik, Dex or Entra rather than running a Zitadel they did not want
//     (§18.2).
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"
)

// DiscoveryPath is fixed by RFC 8414 and OIDC Discovery. It is the one path
// in this package that is allowed to be a constant, because it is the path
// that lets every other one be discovered.
const DiscoveryPath = "/.well-known/openid-configuration"

// Provider is the subset of the discovery document this system needs.
//
// Deliberately small: an issuer and a JWKS URI is the whole contract a
// resource server has with an authorization server (§18.2). Anything more
// would be a reason for some other component to hard-code a Zitadel-shaped
// assumption.
type Provider struct {
	Issuer  string
	JWKSURI string

	// UserInfoURI is the OIDC Core §5.3 endpoint, and it is empty when the
	// document declares none.
	//
	// It is here rather than in the "anything more would be a Zitadel-shaped
	// assumption" category above because it is the opposite of one: an access
	// token is not obliged to carry `name` or `email`, several providers do not
	// carry them, and this is the standard place to ask. A caller that needs a
	// human-readable identity has exactly two conformant options, this or an id
	// token it was never given, so leaving it undiscovered would push every
	// caller into provider-specific guesswork.
	UserInfoURI string
}

// Transport is how this package reaches the authorization server, as distinct
// from the identity that server claims.
//
// The separation exists because the two are genuinely different in a container
// deployment, and conflating them is what makes "just configure an issuer URL"
// insufficient in practice (§18.2 is right about the principle and quiet about
// this). The bundled Zitadel advertises `http://localhost:8300` as its issuer,
// because that is where a browser reaches it for the redirect flow. From
// inside the compose network there is no such address: the container answers
// at `auth:8080`, and it routes by Host, so a request without the right Host
// header reaches the wrong virtual server.
//
// So `core-api` needs to say: fetch from here, send this Host, and expect the
// document to claim that issuer. Three facts, not one.
type Transport struct {
	// Client bounds the requests. Nil uses a sensible default.
	Client *http.Client

	// Host overrides the Host header. Empty sends the URL's own host, which is
	// correct everywhere the issuer is reachable at the address it advertises.
	Host string
}

func (t *Transport) client() *http.Client {
	if t == nil || t.Client == nil {
		return defaultClient()
	}
	return t.Client
}

func (t *Transport) get(ctx context.Context, url string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: building request for %s: %w", url, err)
	}
	if t != nil && t.Host != "" {
		// net/http reads the Host header off the request field, not the map.
		request.Host = t.Host
	}
	return t.client().Do(request)
}

// Discover fetches and validates the discovery document for an issuer that is
// reachable at the address it advertises.
func Discover(ctx context.Context, transport *Transport, issuer string) (*Provider, error) {
	return DiscoverAt(ctx, transport, strings.TrimSuffix(issuer, "/")+DiscoveryPath, issuer)
}

// DiscoverAt fetches the discovery document from an address that need not be
// the issuer's own, and requires the document to claim the issuer expected.
//
// The issuer comparison is not ceremony: without it, anyone who can influence
// where this service fetches its configuration can hand it a document naming
// their issuer and their JWKS, and every subsequent token verifies against
// keys they control. RFC 8414 §3.3 requires the comparison for that reason.
//
// The endpoints in the document are rebased onto the address they were fetched
// from. That sounds like a liberty and is the opposite: the alternative is to
// fetch keys from whatever host a document names, where here the only host
// ever contacted is the one an operator configured. It is also the only way
// the document is usable at all when the issuer's advertised address does not
// resolve on this network.
func DiscoverAt(ctx context.Context, transport *Transport, discoveryURL, expectedIssuer string) (*Provider, error) {
	response, err := transport.get(ctx, discoveryURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetching %s: %w", discoveryURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery at %s returned %s", discoveryURL, response.Status)
	}

	var document struct {
		Issuer      string `json:"issuer"`
		JWKSURI     string `json:"jwks_uri"`
		UserInfoURI string `json:"userinfo_endpoint"`
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: reading discovery document: %w", err)
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("oidc: parsing discovery document: %w", err)
	}

	if document.Issuer != strings.TrimSuffix(expectedIssuer, "/") && document.Issuer != expectedIssuer {
		return nil, fmt.Errorf("oidc: discovery document at %s declares issuer %q, expected %q",
			discoveryURL, document.Issuer, expectedIssuer)
	}
	if document.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: discovery document at %s declares no jwks_uri", discoveryURL)
	}

	jwksURI, err := rebase("jwks_uri", document.JWKSURI, discoveryURL)
	if err != nil {
		return nil, err
	}

	// Absent is not an error. A resource server needs the JWKS to do its job;
	// userinfo is wanted by some callers and required by none, so a provider
	// that omits it stays usable and the caller decides what to do without it.
	var userInfoURI string
	if document.UserInfoURI != "" {
		if userInfoURI, err = rebase("userinfo_endpoint", document.UserInfoURI, discoveryURL); err != nil {
			return nil, err
		}
	}

	return &Provider{
		Issuer:      document.Issuer,
		JWKSURI:     jwksURI,
		UserInfoURI: userInfoURI,
	}, nil
}

// rebase puts an advertised endpoint on the origin it was fetched from.
//
// A no-op in the ordinary case where the issuer is reachable at the address it
// advertises, which keeps this invisible for anyone not running a split
// network.
func rebase(field, advertised, fetchedFrom string) (string, error) {
	target, err := neturl.Parse(advertised)
	if err != nil {
		return "", fmt.Errorf("oidc: discovery document declares an unparseable %s %q: %w", field, advertised, err)
	}
	source, err := neturl.Parse(fetchedFrom)
	if err != nil {
		return "", fmt.Errorf("oidc: unparseable discovery url %q: %w", fetchedFrom, err)
	}

	if target.Scheme == source.Scheme && target.Host == source.Host {
		return advertised, nil
	}

	rebased := *target
	rebased.Scheme = source.Scheme
	rebased.Host = source.Host
	return rebased.String(), nil
}

// defaultClient bounds every call this package makes.
//
// A timeout matters more here than it looks: KeySet serialises refetches
// behind a mutex, so an authorization server that accepts connections and
// never answers would otherwise stall verification for every concurrent
// request rather than failing one of them.
func defaultClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}
