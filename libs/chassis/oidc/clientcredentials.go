package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// refreshMargin is how long before expiry a cached token is replaced.
//
// A token that expires mid-request is a retry every caller would otherwise
// have to write. Thirty seconds against a ten-minute token spends 5% of its
// life to remove that class of failure entirely.
const refreshMargin = 30 * time.Second

// fallbackLifetime is assumed when the authorization server omits expires_in.
//
// Short rather than long, deliberately: minting more often than necessary
// costs one request, where trusting an absent expiry costs every request after
// it.
const fallbackLifetime = 5 * time.Minute

// ClientCredentials mints and caches a service's own access token.
//
// # WHY A TOKEN SOURCE RATHER THAN A CONFIGURED TOKEN
//
// Access tokens live minutes. A static token in a deployment's environment is
// a service that works until the first expiry and then reports that the far
// side refused it, which gets diagnosed as a network problem two or three
// times before somebody checks an `exp` claim. So a caller holds credentials
// and mints, which is the same grant every other machine principal in this
// system uses.
//
// It is in the chassis rather than in a service because it carries no business
// type: an endpoint, a credential, an audience, and a cache.
type ClientCredentials struct {
	endpoint  string
	clientID  string
	secret    string
	audience  string
	scopes    []string
	transport *Transport

	// One lock, so a burst of requests arriving on an expired token mints once
	// rather than once each. The authorization server would survive the herd;
	// the point is that it should not have to.
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	now       func() time.Time
}

// ClientCredentialsConfig is what minting a token needs.
type ClientCredentialsConfig struct {
	// Endpoint is the token endpoint, already rebased onto an address this
	// process can reach. Provider.TokenEndpoint is that value.
	Endpoint string

	// ClientID identifies the client.
	//
	// On Zitadel a service user's client id is its USERNAME rather than its
	// id. That is not guessable, it is in no specification, and it has cost an
	// afternoon before, so it is written here as well as in the Postman
	// collection: a reader debugging a refused token will be in one of the two
	// places.
	ClientID string

	// Secret is the client secret.
	Secret string

	// Audience is the resource the token is for, requested through whichever
	// scopes the provider defines for it.
	//
	// On Zitadel that is the PROJECT ID, requested through the reserved
	// `urn:zitadel:iam:org:project:id:<project>:aud` scope, and the granted
	// roles only reach the token when `urn:zitadel:iam:org:projects:roles` is
	// requested too. The plural in the second is not a typo. Without it the
	// caller authenticates perfectly and holds no authority at all, which
	// presents as a permission error rather than an authentication one and
	// sends you reading grants that are already correct.
	//
	// Callers pass the full scope list through Scopes; this field is kept
	// separate only so an error message can name what the token was for.
	Audience string

	// Scopes is the scope list sent with the request, verbatim.
	Scopes []string

	// Transport bounds the request and carries the Host override that the
	// split-horizon deployments need. Nil is fine.
	Transport *Transport
}

// NewClientCredentials builds a token source. It contacts nothing until the
// first Token call, so a process can construct one during startup without
// depending on the authorization server being up yet.
func NewClientCredentials(cfg ClientCredentialsConfig) (*ClientCredentials, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("oidc: client credentials need a token endpoint")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("oidc: client credentials need a client id")
	}
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, fmt.Errorf("oidc: client credentials need a client secret")
	}

	return &ClientCredentials{
		endpoint:  cfg.Endpoint,
		clientID:  cfg.ClientID,
		secret:    cfg.Secret,
		audience:  cfg.Audience,
		scopes:    cfg.Scopes,
		transport: cfg.Transport,
		now:       time.Now,
	}, nil
}

// Token returns a cached token, minting a new one when the cached one is spent.
func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token != "" && c.now().Before(c.expiresAt) {
		return c.token, nil
	}
	if err := c.mintLocked(ctx); err != nil {
		return "", err
	}
	return c.token, nil
}

func (c *ClientCredentials) mintLocked(ctx context.Context) error {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.secret},
	}
	if len(c.scopes) > 0 {
		form.Set("scope", strings.Join(c.scopes, " "))
	}

	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("oidc: building the token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c.transport != nil && c.transport.Host != "" {
		// net/http reads the Host header off the request field, not the map.
		request.Host = c.transport.Host
	}

	response, err := c.transport.client().Do(request)
	if err != nil {
		return fmt.Errorf("oidc: minting a token for %q: %w", c.audience, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("oidc: reading the token response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		// The body carries the provider's own error code, which is the only
		// thing that distinguishes a bad secret from an unknown audience.
		// Truncated, because it is attacker-influenced only in the sense that
		// the provider wrote it, and an unbounded body in a log is its own
		// problem.
		return fmt.Errorf("oidc: token endpoint returned %s: %s",
			response.Status, truncate(string(body), 256))
	}

	var payload struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("oidc: parsing the token response: %w", err)
	}
	if payload.AccessToken == "" {
		return fmt.Errorf("oidc: the token response carried no access_token")
	}

	lifetime := fallbackLifetime
	if payload.ExpiresIn > 0 {
		lifetime = time.Duration(payload.ExpiresIn) * time.Second
	}

	// A token whose whole life is shorter than the margin is already spent, so
	// it is used once and never cached. Subtracting the margin anyway would
	// leave a negative window, and clamping it to something positive would
	// cache a token expiring inside the window the margin exists to avoid.
	//
	// Handing it back for this one call rather than failing is deliberate: the
	// provider issued it, it is valid now, and refusing would turn a very short
	// token lifetime into an outage.
	usable := lifetime - refreshMargin
	if usable <= 0 {
		usable = 0
	}

	c.token = payload.AccessToken
	c.expiresAt = c.now().Add(usable)
	return nil
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// Bearer wraps a round tripper so every request carries a freshly minted token.
//
// A round tripper rather than a Connect interceptor, so the same token source
// serves a Connect client, a plain HTTP call and anything else a service needs
// to reach. Connect clients take an http.Client, so this composes with them
// without either side knowing about the other.
type Bearer struct {
	// Source mints the token. Required.
	Source *ClientCredentials

	// Base is the wrapped round tripper. Nil uses http.DefaultTransport.
	Base http.RoundTripper
}

// RoundTrip attaches the bearer token.
//
// It does NOT overwrite an Authorization header the caller already set. A
// caller that set one meant it, and silently replacing it would turn an
// explicit credential into a confusing one.
func (b *Bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	if b.Source == nil {
		return nil, fmt.Errorf("oidc: bearer transport has no token source")
	}
	if request.Header.Get("Authorization") != "" {
		return b.base().RoundTrip(request)
	}

	token, err := b.Source.Token(request.Context())
	if err != nil {
		return nil, err
	}

	// Cloned, because RoundTrip must not modify the request it is given.
	// Mutating it in place is a data race the moment anything retries.
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", "Bearer "+token)
	return b.base().RoundTrip(cloned)
}

func (b *Bearer) base() http.RoundTripper {
	if b.Base != nil {
		return b.Base
	}
	return http.DefaultTransport
}
