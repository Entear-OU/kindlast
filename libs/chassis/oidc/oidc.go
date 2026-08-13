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
}

// Discover fetches and validates the discovery document for an issuer.
//
// The issuer in the response must equal the issuer that was asked for. That
// check is not ceremony: without it, an attacker who can influence the
// configured issuer URL can point the resource server at a document naming
// someone else's issuer and JWKS, and every subsequent token verifies against
// keys they control. RFC 8414 §3.3 requires the comparison for exactly this
// reason.
func Discover(ctx context.Context, client *http.Client, issuer string) (*Provider, error) {
	if client == nil {
		client = defaultClient()
	}
	url := strings.TrimSuffix(issuer, "/") + DiscoveryPath

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: building discovery request: %w", err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetching %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery at %s returned %s", url, response.Status)
	}

	var document struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: reading discovery document: %w", err)
	}
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("oidc: parsing discovery document: %w", err)
	}

	if document.Issuer != strings.TrimSuffix(issuer, "/") && document.Issuer != issuer {
		return nil, fmt.Errorf("oidc: discovery document declares issuer %q, asked for %q",
			document.Issuer, issuer)
	}
	if document.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: discovery document for %s declares no jwks_uri", issuer)
	}

	return &Provider{Issuer: document.Issuer, JWKSURI: document.JWKSURI}, nil
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
