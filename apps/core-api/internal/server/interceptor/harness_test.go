package interceptor_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/identity"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/session"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	"github.com/Entear-OU/kindlast/libs/chassis/denylist"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// These tests drive the real chain: real signed tokens against a real JWKS,
// a real Redis deny-list, and a real Postgres enforcing the policies. Nothing
// here is mocked, per §13.2 and §13.3, because the value of every assertion
// below depends on the thing under it being the real one.
//
// They need the compose stack, and they say so rather than passing vacuously:
//
//	docker compose -f deploy/compose.yaml up -d
//
// Skipping when the stack is absent mirrors the database suite's behaviour, so
// a green local run without the stack does not claim coverage it does not
// have. CI boots the stack and fails loudly if it cannot.

// The seeded fixtures (deploy/seed/seed.sql): two organisations, three humans.
// Ada and Bob are in different organisations, which is what makes them useful.
const (
	alphaOrg = "a0000000-0000-4000-8000-000000000001"
	adaUser  = "a0000000-0000-4000-8000-0000000000aa"
	betaOrg  = "b0000000-0000-4000-8000-000000000001"
	bobUser  = "b0000000-0000-4000-8000-0000000000ba"

	testAudience = "kindlast-core-api-test"
)

// unavailable skips locally and fails in CI. See requireStack.
func unavailable(t *testing.T, format string, args ...any) {
	t.Helper()

	if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
		t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: "+format, args...)
	}
	t.Skipf(format, args...)
}

func appDSN() string {
	if dsn := os.Getenv("PG_APP_URL"); dsn != "" {
		return dsn
	}
	return "postgres://kindlast_app:app-dev-password@127.0.0.1:5433/kindlast"
}

func redisAddr() string {
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:6379"
}

var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

// authServer is the authorization server double: discovery, a JWKS, and real
// RS256 signatures over tokens minted in the suite.
//
// It also serves userinfo, because the interesting case is a provider whose
// access tokens carry no profile claims. That is not a hypothetical shape: it
// is what the bundled Zitadel issues, and a suite that only ever mints tokens
// carrying `name` and `email` proves nothing about the deployment it ships to.
type authServer struct {
	server *httptest.Server

	mu       sync.Mutex
	userinfo map[string]any
	// userinfoDown makes the endpoint refuse, which is how the fail-soft path
	// is exercised: provisioning must survive an authorization server that is
	// unreachable at exactly the wrong moment.
	userinfoDown bool
	// userinfoFetches counts requests, because "this path makes no network
	// call" cannot be asserted from an outcome. Every failure here degrades
	// quietly by design, so a wasted call and no call at all produce the same
	// response, and only the server can tell them apart.
	userinfoFetches int
}

func newAuthServer(t *testing.T) *authServer {
	t.Helper()

	a := &authServer{}
	mux := http.NewServeMux()

	mux.HandleFunc(oidc.DiscoveryPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{
			"issuer":            a.server.URL,
			"jwks_uri":          a.server.URL + "/keys",
			"userinfo_endpoint": a.server.URL + "/userinfo",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.userinfoFetches++
		down, body := a.userinfoDown, a.userinfo
		a.mu.Unlock()

		if down {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, body)
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		public := &testKey().PublicKey
		writeJSON(w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test-key", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(public.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(public.E)).Bytes()),
		}}})
	})

	a.server = httptest.NewServer(mux)
	t.Cleanup(a.server.Close)
	return a
}

// serveUserInfo sets what the userinfo endpoint returns for the subject given.
func (a *authServer) serveUserInfo(subject string, profile map[string]any) {
	a.mu.Lock()
	defer a.mu.Unlock()

	body := map[string]any{"sub": subject}
	for key, value := range profile {
		body[key] = value
	}
	a.userinfo = body
}

// profiles builds the production adapter against this server's discovered
// userinfo endpoint, rather than a stub. Same reason the verifier is real: a
// stub here would make every assertion about naming a fact about the stub.
func (a *authServer) profiles(t *testing.T) *identity.UserInfo {
	t.Helper()

	provider, err := oidc.Discover(t.Context(), nil, a.server.URL)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	return identity.NewUserInfo(provider.UserInfoURI, nil)
}

// takeUserInfoDown makes every userinfo request fail.
func (a *authServer) takeUserInfoDown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.userinfoDown = true
}

func (a *authServer) userInfoFetchCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.userinfoFetches
}

// bringUserInfoUp reverses it, so one test can play out the sequence that
// produced the real defect: an organisation created while no profile was
// available, and the same person coming back later.
func (a *authServer) bringUserInfoUp() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.userinfoDown = false
}

// token mints a token for a subject carrying the scopes given.
func (a *authServer) token(t *testing.T, subject string, scopes string) string {
	t.Helper()
	return a.tokenWithClaims(t, jwt.MapClaims{
		"iss":   a.server.URL,
		"aud":   testAudience,
		"sub":   subject,
		"exp":   time.Now().Add(10 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   "jti-" + subject + "-" + scopes,
		"scope": scopes,
	})
}

func (a *authServer) tokenWithClaims(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key"

	signed, err := token.SignedString(testKey())
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return signed
}

func (a *authServer) verifier(t *testing.T) *oidc.Verifier {
	t.Helper()

	provider, err := oidc.Discover(t.Context(), nil, a.server.URL)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	keys := oidc.NewKeySet(provider.JWKSURI, nil)
	if err := keys.Warm(t.Context()); err != nil {
		t.Fatalf("warming: %v", err)
	}
	verifier, err := oidc.NewVerifier(keys, provider.Issuer, testAudience)
	if err != nil {
		t.Fatalf("verifier: %v", err)
	}
	return verifier
}

// stack is the running compose stack's Postgres and Redis.
type stack struct {
	store       *postgres.Store
	revocations *denylist.Redis
	redis       *redis.Client
}

// requireStack skips loudly rather than passing on an absent dependency.
//
// Except in CI, where KINDLAST_REQUIRE_STACK turns the skip into a failure.
// A suite that self-skips is right for a laptop and wrong for a pipeline: a
// green run that quietly tested nothing is how coverage disappears without
// anyone deciding to remove it. The compose and integration suites already
// work this way, so this keeps the property rather than inventing one.
func requireStack(t *testing.T, issuer string) *stack {
	t.Helper()

	store, err := postgres.New(t.Context(), appDSN(), issuer)
	if err != nil {
		unavailable(t, "compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", appDSN(), err)
	}
	t.Cleanup(store.Close)

	client := redis.NewClient(&redis.Options{Addr: redisAddr()})
	if err := client.Ping(t.Context()).Err(); err != nil {
		_ = client.Close()
		unavailable(t, "redis not reachable at %s (%v)", redisAddr(), err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return &stack{store: store, revocations: denylist.NewRedis(client, "test:denylist:"), redis: client}
}

// tenantOpener mirrors the adapter in main.go, so these tests exercise the
// same seam production does.
type tenantOpener struct{ store *postgres.Store }

func (o tenantOpener) BeginTenant(ctx context.Context, subject, orgID string) (interceptor.Tenant, error) {
	tenant, err := o.store.BeginTenant(ctx, subject, orgID)
	if err != nil {
		if errors.Is(err, postgres.ErrNotAMember) {
			return nil, interceptor.ErrNotAMember
		}
		return nil, err
	}
	return tenant, nil
}

// serve starts a Connect server behind the given interceptors and returns a
// client for it.
func serve(t *testing.T, a *authServer, interceptors ...connect.Interceptor) corev1connect.SessionServiceClient {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(corev1connect.NewSessionServiceHandler(session.New(a.profiles(t)),
		connect.WithInterceptors(interceptors...)))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return corev1connect.NewSessionServiceClient(server.Client(), server.URL)
}

// call makes the one RPC that exists, with whatever headers the test wants.
func call(t *testing.T, client corev1connect.SessionServiceClient, headers map[string]string) error {
	t.Helper()

	request := connect.NewRequest(&corev1.GetCurrentUserRequest{})
	for name, value := range headers {
		request.Header().Set(name, value)
	}

	_, err := client.GetCurrentUser(t.Context(), request)
	return err
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
