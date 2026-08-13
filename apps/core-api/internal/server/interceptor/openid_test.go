package interceptor_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The end of the problem measured in ENT-195, through the whole chain.
//
// A real Zitadel access token carries no `scope` claim and no `scp` claim at
// all. Before `openid` was asserted by verification, this request was refused
// at the scope stage with permission_denied, which made GetCurrentUser
// unreachable by every valid token, and GetCurrentUser is the endpoint where a
// new user's organisation is created. A caller could never reach the call that
// would grant them anything.
func TestATokenWithNoScopeClaimStillReachesTheBootstrapCall(t *testing.T) {
	a := newAuthServer(t)
	client, _ := buildChain(t, a, realScopes(t))

	claim := fmt.Sprintf("chain-noscope-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	// Exactly the shape Zitadel issues: identity, audience, expiry, jti, and
	// not a scope in sight.
	token := a.tokenWithClaims(t, jwt.MapClaims{
		"iss":   a.server.URL,
		"aud":   testAudience,
		"sub":   claim,
		"exp":   time.Now().Add(10 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   "no-scope-" + claim,
		"email": "noscope@example.com",
	})

	me, err := meCall(t, client, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		t.Fatalf("a token with no scope claim was refused: %v\n"+
			"no authorization server issues a grant for openid; verification is the proof of it", err)
	}
	if len(me.GetMemberships()) != 1 {
		t.Fatalf("memberships = %d, want 1; the bootstrap call did not provision", len(me.GetMemberships()))
	}
}

// The rule that comes with the assertion, guarded rather than only written
// down: `openid` means signed in, not permitted, so a token carrying no grants
// must still be refused anything that asks for a real one.
func TestAssertingOpenIDGrantsNothingElse(t *testing.T) {
	a := newAuthServer(t)

	// A fixture declaring a real permission rather than the bootstrap scope.
	declared := serviceFixture(t, map[string]string{"GetCurrentUser": "findings:act"})
	scopes, err := interceptor.NewScope([]protoreflect.ServiceDescriptor{declared})
	if err != nil {
		t.Fatalf("building the scope table: %v", err)
	}
	client, _ := buildChain(t, a, scopes)

	claim := fmt.Sprintf("chain-nogrant-%d", time.Now().UnixNano())
	forget(t, a.server.URL, claim)
	t.Cleanup(func() { forget(t, a.server.URL, claim) })

	token := a.tokenWithClaims(t, jwt.MapClaims{
		"iss": a.server.URL, "aud": testAudience, "sub": claim,
		"exp": time.Now().Add(10 * time.Minute).Unix(),
		"jti": "nogrant-" + claim,
	})

	_, err = meCall(t, client, map[string]string{"Authorization": "Bearer " + token})
	if err == nil {
		t.Fatal("a token carrying no grants reached an endpoint requiring findings:act; " +
			"asserting openid must not imply anything else")
	}
}
