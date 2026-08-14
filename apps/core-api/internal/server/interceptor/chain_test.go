package interceptor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1/corev1connect"
	optionsv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/options/v1"
)

// The procedure the one real RPC is served at.
const getCurrentUser = "/kindlast.core.v1.SessionService/GetCurrentUser"

// buildChain assembles the production chain in the production order, over the
// real stack.
func buildChain(t *testing.T, a *authServer, scopes *interceptor.Scope) (corev1connect.SessionServiceClient, *stack) {
	t.Helper()

	live := requireStack(t, a.server.URL)

	return serve(t, a,
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
		interceptor.Tenancy(tenantOpener{live.store}),
	), live
}

// realScopes reads the table off the descriptors the binary actually serves,
// through the same registry the shipped server uses.
func realScopes(t *testing.T) *interceptor.Scope {
	t.Helper()

	scopes, err := interceptor.NewScope(server.Services())
	if err != nil {
		t.Fatalf("building the scope table: %v", err)
	}
	return scopes
}

// A token that passes every stage reaches the handler, which is unimplemented
// until ENT-196. `unimplemented` is therefore the success signal: it can only
// be reached by passing authentication, revocation, scope and tenancy.
func TestAValidTokenReachesTheHandler(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	err := call(t, client, map[string]string{
		"Authorization":       "Bearer " + a.token(t, adaUser, "openid profile"),
		interceptor.OrgHeader: alphaOrg,
	})

	assertOK(t, err, "a valid token did not reach the handler")
}

func TestTheChainRefusesWhatItShould(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	cases := []struct {
		name    string
		headers map[string]string
		want    connect.Code
		why     string
	}{
		{
			name:    "no Authorization header",
			headers: map[string]string{interceptor.OrgHeader: alphaOrg},
			want:    connect.CodeUnauthenticated,
			why:     "an unauthenticated request reached past the auth interceptor",
		},
		{
			name: "not a Bearer credential",
			headers: map[string]string{
				"Authorization":       "Basic " + a.token(t, adaUser, "openid"),
				interceptor.OrgHeader: alphaOrg,
			},
			want: connect.CodeUnauthenticated,
			why:  "a non-Bearer Authorization header was accepted",
		},
		{
			name: "expired token",
			headers: map[string]string{
				"Authorization": "Bearer " + a.tokenWithClaims(t, jwt.MapClaims{
					"iss": a.server.URL, "aud": testAudience, "sub": adaUser,
					"exp": time.Now().Add(-time.Hour).Unix(), "jti": "expired", "scope": "openid",
				}),
				interceptor.OrgHeader: alphaOrg,
			},
			want: connect.CodeUnauthenticated,
			why:  "an expired token was accepted",
		},
		{
			name: "token for another resource server",
			headers: map[string]string{
				"Authorization": "Bearer " + a.tokenWithClaims(t, jwt.MapClaims{
					"iss": a.server.URL, "aud": "kindlast-intelligence", "sub": adaUser,
					"exp": time.Now().Add(time.Hour).Unix(), "jti": "wrong-aud", "scope": "openid",
				}),
				interceptor.OrgHeader: alphaOrg,
			},
			want: connect.CodeUnauthenticated,
			why:  "a token minted for intelligence was replayed against core-api",
		},
		{
			name: "no jti, so it could never be revoked",
			headers: map[string]string{
				"Authorization": "Bearer " + a.tokenWithClaims(t, jwt.MapClaims{
					"iss": a.server.URL, "aud": testAudience, "sub": adaUser,
					"exp": time.Now().Add(time.Hour).Unix(), "scope": "openid",
				}),
				interceptor.OrgHeader: alphaOrg,
			},
			want: connect.CodeUnauthenticated,
			why:  "a token with no jti was accepted, opting that session out of the deny-list",
		},
		// "missing the declared scope" used to live here, asserting that a
		// token carrying only findings:read was refused by GetCurrentUser.
		// It cannot be tested against the real registry any more: both
		// shipped RPCs declare `openid`, and verification now asserts that,
		// because no authorization server issues a grant for it.
		//
		// The property itself is still covered, against fixtures declaring a
		// real permission rather than the bootstrap scope:
		// TestTheInterceptorEnforcesTheDeclaredValueNotAFixedOne and
		// TestAssertingOpenIDGrantsNothingElse.
		{
			name: "an organisation the caller does not belong to",
			headers: map[string]string{
				"Authorization":       "Bearer " + a.token(t, adaUser, "openid"),
				interceptor.OrgHeader: betaOrg,
			},
			want: connect.CodePermissionDenied,
			why:  "Ada acted inside Bob's organisation",
		},
		{
			name: "an organisation id that is not a uuid",
			headers: map[string]string{
				"Authorization":       "Bearer " + a.token(t, adaUser, "openid"),
				interceptor.OrgHeader: "not-a-uuid",
			},
			want: connect.CodePermissionDenied,
			why:  "a malformed organisation header was not refused cleanly",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			assertCode(t, call(t, client, testCase.headers), testCase.want, testCase.why)
		})
	}
}

// Half the scope guarantee: an RPC that declares no scope must not be
// reachable. NewScope refuses to build the table at all, so the binary does
// not start, which is the strongest form of failing closed available.
func TestAnUndeclaredRPCStopsTheServerStarting(t *testing.T) {
	stripped := serviceFixture(t, map[string]string{"GetCurrentUser": ""})

	_, err := interceptor.NewScope([]protoreflect.ServiceDescriptor{stripped})
	if !errors.Is(err, interceptor.ErrUndeclaredMethod) {
		t.Fatalf("error = %v, want ErrUndeclaredMethod; an unannotated RPC would have been served", err)
	}
}

// The other half, and the one a reflection test cannot give you.
//
// A reader hard-wired to return a single scope passes every "is a scope
// declared" check forever. So this declares a scope that is NOT the one the
// real proto carries, and asserts the interceptor enforces the declared value:
// a token holding `openid` (what the real proto declares) is refused, and one
// holding `findings:act` (what this fixture declares) is allowed through.
//
// Sabotage the reader to always return "openid" and this test goes red in both
// directions at once.
func TestTheInterceptorEnforcesTheDeclaredValueNotAFixedOne(t *testing.T) {
	a := newAuthServer(t)

	declared := serviceFixture(t, map[string]string{"GetCurrentUser": "findings:act"})
	scopes, err := interceptor.NewScope([]protoreflect.ServiceDescriptor{declared})
	if err != nil {
		t.Fatalf("building the scope table: %v", err)
	}

	if got, ok := scopes.RequiredScope(getCurrentUser); !ok || got != "findings:act" {
		t.Fatalf("required scope = %q (found=%v), want findings:act; the reader is not reading", got, ok)
	}

	client, _ := buildChain(t, a, scopes)

	t.Run("the scope the real proto declares is not enough here", func(t *testing.T) {
		err := call(t, client, map[string]string{
			"Authorization":       "Bearer " + a.token(t, adaUser, "openid profile"),
			interceptor.OrgHeader: alphaOrg,
		})
		assertCode(t, err, connect.CodePermissionDenied,
			"the interceptor accepted a scope other than the declared one")
	})

	t.Run("the declared scope is", func(t *testing.T) {
		err := call(t, client, map[string]string{
			"Authorization":       "Bearer " + a.token(t, adaUser, "findings:act"),
			interceptor.OrgHeader: alphaOrg,
		})
		assertOK(t, err, "the interceptor refused the scope the method actually declared")
	})
}

// A procedure absent from the table must be unreachable rather than open. This
// is the second line behind the boot-time refusal above.
func TestAProcedureMissingFromTheTableIsRefused(t *testing.T) {
	a := newAuthServer(t)

	other := serviceFixture(t, map[string]string{"SomeOtherMethod": "openid"})
	scopes, err := interceptor.NewScope([]protoreflect.ServiceDescriptor{other})
	if err != nil {
		t.Fatalf("building the scope table: %v", err)
	}

	client, _ := buildChain(t, a, scopes)

	err = call(t, client, map[string]string{
		"Authorization":       "Bearer " + a.token(t, adaUser, "openid profile findings:act"),
		interceptor.OrgHeader: alphaOrg,
	})
	assertCode(t, err, connect.CodePermissionDenied,
		"an RPC absent from the scope table was reachable with a valid token")
}

// Revocation, which is what makes local verification safe to choose (§1.4).
func TestARevokedTokenStopsWorkingBeforeItExpires(t *testing.T) {
	a := newAuthServer(t)
	client, live := buildChain(t, a, realScopes(t))

	token := a.tokenWithClaims(t, jwt.MapClaims{
		"iss": a.server.URL, "aud": testAudience, "sub": adaUser,
		"exp":   time.Now().Add(10 * time.Minute).Unix(),
		"jti":   "revocation-test-token",
		"scope": "openid",
	})
	headers := map[string]string{
		"Authorization":       "Bearer " + token,
		interceptor.OrgHeader: alphaOrg,
	}

	// Redis outlives the test, so the key is cleared on the way in as well as
	// on the way out. Without this, one interrupted run leaves a revocation
	// behind and every later run fails on the first assertion instead of the
	// one it is testing.
	const revokedID = "revocation-test-token"
	clearRevocation(t, live, revokedID)
	t.Cleanup(func() { clearRevocation(t, live, revokedID) })

	assertOK(t, call(t, client, headers),
		"the token did not work before being revoked, so revoking it proves nothing")

	expiry := time.Now().Add(10 * time.Minute)
	if err := live.revocations.Deny(t.Context(), revokedID, expiry); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	assertCode(t, call(t, client, headers), connect.CodeUnauthenticated,
		"a revoked token still worked")

	// The entry must expire with the token rather than accumulate forever
	// (§15.1), and the instance must not be free to evict it early (§15.3).
	ttl, err := live.revocations.TTL(t.Context(), revokedID)
	if err != nil {
		t.Fatalf("reading the ttl: %v", err)
	}
	if ttl <= 0 || ttl > 11*time.Minute {
		t.Fatalf("deny-list ttl = %s, want a positive value no longer than the token's own life", ttl)
	}

	policy, err := live.revocations.MaxMemoryPolicy(t.Context())
	if err != nil {
		t.Fatalf("reading maxmemory-policy: %v", err)
	}
	if policy != "noeviction" {
		t.Fatalf("redis maxmemory-policy = %q, want noeviction; "+
			"an evictable deny-list silently un-revokes tokens under memory pressure (§15.3)", policy)
	}
}

// clearRevocation removes a deny-list entry.
//
// context.Background rather than t.Context(): the test context is cancelled
// before cleanups run, so a cleanup using it issues a command that never
// reaches Redis and reports an error nobody reads. That exact mistake left a
// revocation behind and made the following run fail on an unrelated assertion.
func clearRevocation(t *testing.T, live *stack, tokenID string) {
	t.Helper()

	if err := live.redis.Del(context.Background(), "test:denylist:"+tokenID).Err(); err != nil {
		t.Fatalf("clearing the deny-list entry: %v", err)
	}
}

// assertOK is what "the whole chain passed" looks like now that ENT-196
// implemented the handler.
//
// Until then these assertions expected `unimplemented`, because reaching a
// stub was the only available proof that authentication, revocation, scope and
// tenancy had all passed. A real response is a strictly better proof, and the
// refusal cases below are unchanged, which is the point: replacing the handler
// did not weaken any of them.
func assertOK(t *testing.T, err error, why string) {
	t.Helper()

	if err != nil {
		t.Fatalf("call failed with %v (%v): %s", connect.CodeOf(err), err, why)
	}
}

func assertCode(t *testing.T, err error, want connect.Code, why string) {
	t.Helper()

	got := connect.CodeOf(err)
	if err == nil {
		t.Fatalf("call succeeded outright, want %v: %s", want, why)
	}
	if got != want {
		t.Fatalf("code = %v (%v), want %v: %s", got, err, want, why)
	}
}

// serviceFixture builds a descriptor standing in for the real SessionService,
// with whatever scopes the test needs declared on it.
//
// Built in code rather than by editing the real proto, so a test can express
// "this method declares findings:act" without a regeneration step, and so the
// enforced value can be made to differ from the one the shipped contract
// carries. An empty scope means the option is omitted entirely, which is what
// an unannotated RPC looks like.
func serviceFixture(t *testing.T, methods map[string]string) protoreflect.ServiceDescriptor {
	t.Helper()

	service := &descriptorpb.ServiceDescriptorProto{Name: proto.String("SessionService")}
	for name, declared := range methods {
		method := &descriptorpb.MethodDescriptorProto{
			Name:       proto.String(name),
			InputType:  proto.String(".kindlast.core.v1.GetCurrentUserRequest"),
			OutputType: proto.String(".kindlast.core.v1.GetCurrentUserResponse"),
		}
		if declared != "" {
			opts := &descriptorpb.MethodOptions{}
			proto.SetExtension(opts, optionsv1.E_RequiredScope, declared)
			method.Options = opts
		}
		service.Method = append(service.Method, method)
	}

	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    proto.String("kindlast/core/v1/fixture.proto"),
		Package: proto.String("kindlast.core.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("GetCurrentUserRequest")},
			{Name: proto.String("GetCurrentUserResponse")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{service},
	}, nil)
	if err != nil {
		t.Fatalf("building the fixture descriptor: %v", err)
	}
	return file.Services().Get(0)
}
