package oidc_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// Minting a service's own token (ENT-245).
//
// core-api calls Intelligence, and Intelligence requires a bearer token. The
// first wiring sent none, so every narration failed with "a bearer token is
// required" and the feature could not work in any deployment. These assert the
// half that fix depends on.

// tokenServer stands in for the authorization server and records what it saw.
type tokenServer struct {
	*httptest.Server
	minted   int
	lastForm url.Values
	lastHost string
}

func newTokenServer(t *testing.T, body string) *tokenServer {
	t.Helper()

	server := &tokenServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			server.minted++
			server.lastForm = r.PostForm
			server.lastHost = r.Host
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
	t.Cleanup(server.Close)
	return server
}

func credentials(t *testing.T, endpoint string, transport *oidc.Transport) *oidc.ClientCredentials {
	t.Helper()

	source, err := oidc.NewClientCredentials(oidc.ClientCredentialsConfig{
		Endpoint:  endpoint,
		ClientID:  "core-api-client",
		Secret:    "a-secret",
		Audience:  "386706826638393350",
		Scopes:    []string{"openid", "urn:zitadel:iam:org:projects:roles"},
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("building the token source: %v", err)
	}
	return source
}

func TestATokenIsMintedWithTheGrantAndScopesConfigured(t *testing.T) {
	server := newTokenServer(t, `{"access_token":"minted","expires_in":600}`)
	source := credentials(t, server.URL, nil)

	token, err := source.Token(t.Context())
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if token != "minted" {
		t.Fatalf("got token %q, want the one the server issued", token)
	}

	if got := server.lastForm.Get("grant_type"); got != "client_credentials" {
		t.Fatalf("grant_type was %q", got)
	}
	// THE SCOPE LIST IS THE AUTHORITY, and sending it is not optional. A
	// caller that authenticates without requesting its roles gets a valid token
	// carrying nothing, which presents as a permission error and sends the
	// reader to check grants that were already correct.
	if got := server.lastForm.Get("scope"); !strings.Contains(got, "urn:zitadel:iam:org:projects:roles") {
		t.Fatalf("scope was %q, want the roles scope included", got)
	}
}

func TestATokenIsCachedRatherThanMintedPerRequest(t *testing.T) {
	server := newTokenServer(t, `{"access_token":"minted","expires_in":600}`)
	source := credentials(t, server.URL, nil)

	for range 5 {
		if _, err := source.Token(t.Context()); err != nil {
			t.Fatalf("minting: %v", err)
		}
	}

	// Without caching, every narrated finding costs a round trip to the
	// authorization server, and a narration pass over a backlog turns into a
	// burst nobody asked for.
	if server.minted != 1 {
		t.Fatalf("minted %d times for five calls, want 1", server.minted)
	}
}

func TestAnExpiredTokenIsReplaced(t *testing.T) {
	// `expires_in` under the refresh margin means the cached token is already
	// considered spent, so the next call must mint again rather than hand back
	// something the far side will refuse.
	server := newTokenServer(t, `{"access_token":"minted","expires_in":1}`)
	source := credentials(t, server.URL, nil)

	for range 3 {
		if _, err := source.Token(t.Context()); err != nil {
			t.Fatalf("minting: %v", err)
		}
	}

	if server.minted != 3 {
		t.Fatalf("minted %d times, want one per call for an already-spent token", server.minted)
	}
}

func TestARefusedTokenSaysWhatTheProviderSaid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
		}))
	t.Cleanup(server.Close)

	source := credentials(t, server.URL, nil)
	_, err := source.Token(t.Context())
	if err == nil {
		t.Fatal("a refused token was reported as success")
	}
	// The provider's own code is the only thing that separates a bad secret
	// from an unknown audience, and losing it makes the failure unreadable.
	if !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("the error lost what the provider said: %v", err)
	}
}

func TestAResponseWithNoTokenIsAnError(t *testing.T) {
	// A 200 carrying no token would otherwise cache an empty string and send
	// an "Authorization: Bearer " header, which the far side reports as
	// unauthenticated: the confusing failure this whole type exists to avoid.
	server := newTokenServer(t, `{"expires_in":600}`)
	source := credentials(t, server.URL, nil)

	if _, err := source.Token(t.Context()); err == nil {
		t.Fatal("a response with no access_token was accepted")
	}
}

func TestTheHostOverrideReachesTheTokenEndpoint(t *testing.T) {
	// The same split horizon the discovery and JWKS fetches have: the endpoint
	// is reached at one address and routes by a Host it advertises elsewhere.
	server := newTokenServer(t, `{"access_token":"minted","expires_in":600}`)
	source := credentials(t, server.URL, &oidc.Transport{Host: "localhost:8300"})

	if _, err := source.Token(t.Context()); err != nil {
		t.Fatalf("minting: %v", err)
	}
	if server.lastHost != "localhost:8300" {
		t.Fatalf("the token endpoint saw Host %q, want the override", server.lastHost)
	}
}

func TestCredentialsAreRequired(t *testing.T) {
	for _, missing := range []struct {
		name string
		cfg  oidc.ClientCredentialsConfig
	}{
		{"endpoint", oidc.ClientCredentialsConfig{ClientID: "c", Secret: "s"}},
		{"client id", oidc.ClientCredentialsConfig{Endpoint: "http://x", Secret: "s"}},
		{"secret", oidc.ClientCredentialsConfig{Endpoint: "http://x", ClientID: "c"}},
	} {
		t.Run(missing.name, func(t *testing.T) {
			// Refusing at construction rather than at the first call, so a
			// misconfigured deployment fails at startup instead of the first
			// time somebody narrates a finding.
			if _, err := oidc.NewClientCredentials(missing.cfg); err == nil {
				t.Fatalf("built a token source with no %s", missing.name)
			}
		})
	}
}

func TestTheBearerTransportAttachesTheToken(t *testing.T) {
	tokens := newTokenServer(t, `{"access_token":"minted","expires_in":600}`)

	var seen string
	resource := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
	t.Cleanup(resource.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, resource.URL, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}

	client := &http.Client{Transport: &oidc.Bearer{Source: credentials(t, tokens.URL, nil)}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("calling the resource: %v", err)
	}
	_ = response.Body.Close()

	if seen != "Bearer minted" {
		t.Fatalf("the resource saw Authorization %q", seen)
	}
}

func TestTheBearerTransportLeavesAnExplicitHeaderAlone(t *testing.T) {
	tokens := newTokenServer(t, `{"access_token":"minted","expires_in":600}`)

	var seen string
	resource := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
	t.Cleanup(resource.Close)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, resource.URL, nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer chosen-by-the-caller")

	client := &http.Client{Transport: &oidc.Bearer{Source: credentials(t, tokens.URL, nil)}}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("calling the resource: %v", err)
	}
	_ = response.Body.Close()

	// A caller that set a credential meant it. Silently replacing one is how a
	// delegated call quietly becomes a service call.
	if seen != "Bearer chosen-by-the-caller" {
		t.Fatalf("the transport replaced an explicit credential with %q", seen)
	}
	if tokens.minted != 0 {
		t.Fatal("it minted a token it had no use for")
	}
}
