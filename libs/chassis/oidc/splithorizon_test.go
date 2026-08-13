package oidc_test

import (
	"encoding/base64"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The deployment shape §18.2 does not mention, and the one the compose stack
// actually has.
//
// Zitadel advertises `http://localhost:8300` as its issuer, because that is
// where a browser reaches it for the redirect flow. `core-api` has no such
// address: it is on the compose network, where the container answers at
// `auth:8080` and routes by Host. So the document says one thing, the network
// requires another, and both are correct.
//
// This test stands in for that: a server reachable at one address, serving a
// document that names a different one, and only answering when the Host header
// matches.
func TestDiscoveryAgainstAnIssuerReachableAtAnotherAddress(t *testing.T) {
	const advertisedIssuer = "http://localhost:8300"
	const requiredHost = "localhost:8300"

	var jwksHostSeen string

	mux := http.NewServeMux()
	mux.HandleFunc(oidc.DiscoveryPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != requiredHost {
			// Zitadel's behaviour: the wrong Host reaches the wrong virtual
			// server, which is a confusing failure rather than a clean one.
			http.Error(w, "unknown host "+r.Host, http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]string{
			"issuer": advertisedIssuer,
			// Advertised at an address that does not resolve on this network.
			"jwks_uri": advertisedIssuer + "/oauth/v2/keys",
		})
	})
	mux.HandleFunc("/oauth/v2/keys", func(w http.ResponseWriter, r *http.Request) {
		jwksHostSeen = r.Host
		public := &signingKey().PublicKey
		writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "key-1", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
		}}})
	})

	internal := httptest.NewServer(mux)
	t.Cleanup(internal.Close)

	transport := &oidc.Transport{Host: requiredHost}

	provider, err := oidc.DiscoverAt(t.Context(), transport,
		internal.URL+oidc.DiscoveryPath, advertisedIssuer)
	if err != nil {
		t.Fatalf("discovery across a split network: %v", err)
	}

	// The identity is what the document claims, because that is what tokens
	// will carry in their `iss`.
	if provider.Issuer != advertisedIssuer {
		t.Fatalf("issuer = %q, want %q", provider.Issuer, advertisedIssuer)
	}

	// The address is the one an operator configured, not the one the document
	// named. Fetching from the advertised host would fail here and, worse,
	// would mean trusting a document to say where keys come from.
	if !strings.HasPrefix(provider.JWKSURI, internal.URL) {
		t.Fatalf("jwks_uri = %q, want it rebased onto %q", provider.JWKSURI, internal.URL)
	}
	if !strings.HasSuffix(provider.JWKSURI, "/oauth/v2/keys") {
		t.Fatalf("jwks_uri = %q, want the advertised path preserved", provider.JWKSURI)
	}

	// And the whole thing has to actually verify a token, or the rebasing is
	// just string manipulation that happens to look right.
	keys := oidc.NewKeySet(provider.JWKSURI, transport)
	if err := keys.Warm(t.Context()); err != nil {
		t.Fatalf("warming across the split network: %v", err)
	}
	if jwksHostSeen != requiredHost {
		t.Fatalf("the jwks fetch sent Host %q, want %q; the override does not reach key fetches", jwksHostSeen, requiredHost)
	}

	verifier, err := oidc.NewVerifier(keys, provider.Issuer, testAudience)
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}

	claims := map[string]any{
		"iss": advertisedIssuer, "aud": testAudience, "sub": "user-subject-1",
		"exp": nowPlusTenMinutes(), "jti": "split-horizon", "scope": "openid",
	}
	if _, err := verifier.Verify(t.Context(), signClaims(t, "key-1", claims)); err != nil {
		t.Fatalf("verifying a token from the advertised issuer: %v", err)
	}
}

// A document naming an issuer other than the expected one is still refused,
// and the split-horizon accommodation must not have opened that door.
func TestSplitHorizonDiscoveryStillChecksTheIssuer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc(oidc.DiscoveryPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{
			"issuer":   "http://attacker.example",
			"jwks_uri": "http://attacker.example/keys",
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	_, err := oidc.DiscoverAt(t.Context(), nil, server.URL+oidc.DiscoveryPath, "http://localhost:8300")
	if err == nil {
		t.Fatal("a document naming a different issuer was accepted; " +
			"anyone who can steer where config is fetched from could supply the keys too")
	}
}
