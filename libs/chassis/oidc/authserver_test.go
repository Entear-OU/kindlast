package oidc_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// A real authorization server double, per §13.2: an RSA keypair generated in
// the suite, a JWKS served over HTTP, and real signed tokens.
//
// The alternative, stubbing the verifier, is the thing §13.2 forbids and it is
// worth restating why. Every case in this file passes trivially against a
// mock: a stub that returns "valid" is not sensitive to the audience, the
// algorithm or the expiry, so the suite would be asserting facts about the
// stub. The bugs being hunted here live in the interaction between a real
// parser, a real key set and a real signature.

// Key generation is the slowest thing in this suite, so the two keys are
// generated once and shared. They are only ever used to sign test tokens.
var (
	signingKey  = sync.OnceValue(func() *rsa.PrivateKey { return mustGenerateKey() })
	strangerKey = sync.OnceValue(func() *rsa.PrivateKey { return mustGenerateKey() })
)

func mustGenerateKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic("generating a test signing key: " + err.Error())
	}
	return key
}

// authServer serves OIDC discovery and a JWKS, and counts how often the JWKS
// is fetched.
//
// The counter is not incidental: "exactly one refetch, not one per request" is
// an acceptance criterion, and the only way to assert it is to watch the
// server rather than to inspect the cache.
type authServer struct {
	server *httptest.Server

	mu          sync.Mutex
	keys        map[string]*rsa.PrivateKey
	jwksFetches int
}

// newAuthServer starts a server that already holds a signing key.
func newAuthServer(t *testing.T) *authServer {
	t.Helper()
	return startAuthServer(t, map[string]*rsa.PrivateKey{"key-1": signingKey()})
}

// newLazyAuthServer starts a server holding no signing key at all, which is
// what a freshly seeded Zitadel looks like: it generates the key on the first
// token it issues, so `{"keys": []}` is a correct answer to a question asked
// too early (§1.4).
func newLazyAuthServer(t *testing.T) *authServer {
	t.Helper()
	return startAuthServer(t, map[string]*rsa.PrivateKey{})
}

func startAuthServer(t *testing.T, keys map[string]*rsa.PrivateKey) *authServer {
	t.Helper()

	a := &authServer{keys: keys}
	mux := http.NewServeMux()

	mux.HandleFunc(oidc.DiscoveryPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{
			"issuer":   a.issuer(),
			"jwks_uri": a.issuer() + "/oauth/v2/keys",
		})
	})

	mux.HandleFunc("/oauth/v2/keys", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.jwksFetches++
		keys := make([]map[string]string, 0, len(a.keys))
		for kid, key := range a.keys {
			keys = append(keys, jwkFor(kid, &key.PublicKey))
		}
		a.mu.Unlock()

		writeJSON(w, map[string]any{"keys": keys})
	})

	a.server = httptest.NewServer(mux)
	t.Cleanup(a.server.Close)
	return a
}

func (a *authServer) issuer() string { return a.server.URL }

func (a *authServer) jwksURI() string { return a.server.URL + "/oauth/v2/keys" }

// generateSigningKey is the lazy generation a freshly seeded Zitadel performs
// on the first token it issues.
func (a *authServer) generateSigningKey(kid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.keys[kid] = signingKey()
}

func (a *authServer) fetchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.jwksFetches
}

// claims returns a token body that passes every check, so each test can spoil
// exactly one thing and attribute the refusal to that one thing.
func (a *authServer) claims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            a.issuer(),
		"aud":            testAudience,
		"sub":            "user-subject-1",
		"exp":            time.Now().Add(10 * time.Minute).Unix(),
		"iat":            time.Now().Unix(),
		"jti":            "token-id-1",
		"scope":          "openid profile email findings:read",
		"email":          "someone@example.com",
		"email_verified": true,
	}
}

// mint signs a token with the server's key, under a key id of the caller's
// choosing so the unknown-kid path can be exercised.
func (a *authServer) mint(t *testing.T, kid string, claims jwt.MapClaims) string {
	t.Helper()
	return signWith(t, signingKey(), kid, claims)
}

func signWith(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing test token: %v", err)
	}
	return signed
}

func jwkFor(kid string, public *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
	}
}

// publicKeyPEM is the material the HS256 confusion attack uses as its HMAC
// secret: the authorization server's public key, which is public precisely so
// that anyone can have it.
func publicKeyPEM(t *testing.T, public *rsa.PublicKey) []byte {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatalf("marshalling the public key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// nowPlusTenMinutes and signClaims are small helpers for tests that build
// claim maps directly rather than starting from authServer.claims.
func nowPlusTenMinutes() int64 { return time.Now().Add(10 * time.Minute).Unix() }

func signClaims(t *testing.T, kid string, claims map[string]any) string {
	t.Helper()
	return signWith(t, signingKey(), kid, jwt.MapClaims(claims))
}
