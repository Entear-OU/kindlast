package oidc_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// `openid` is asserted by verification, not carried as a grant.
//
// It is the one scope in the vocabulary that is not a permission. Every other
// value answers "may this client touch this kind of resource"; `openid`
// answers "did this caller arrive through an OIDC login", and a token that
// passes signature, issuer, audience and expiry is exactly the proof of that.
//
// Measured, not assumed: a real authorization-code token from the seeded
// Zitadel carries seven project roles and no `openid`, because no
// authorization server issues a grant for it. Requiring it as a claim made
// GetCurrentUser unreachable by every valid token, which is the endpoint where
// a new user's organisation is created, so a caller could never reach the call
// that would grant them anything.
//
// The rule this creates, and it matters more than the code: **never declare
// `openid` on an endpoint that grants authority.** It means signed in, not
// permitted.
func TestVerificationAssertsOpenID(t *testing.T) {
	t.Run("a token carrying no scopes at all still holds openid", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		delete(claims, "scope")

		verified, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		if err != nil {
			t.Fatalf("verifying: %v", err)
		}
		if !verified.HasScope("openid") {
			t.Fatalf("scopes = %v, want openid among them; verification is the proof of it", verified.Scopes)
		}
	})

	t.Run("the shape a real Zitadel token has", func(t *testing.T) {
		// Roles in a vendor claim, no `scope`, no `scp`. This is the exact
		// shape measured on the running stack.
		const rolesClaim = "urn:zitadel:iam:org:project:roles"

		a := newAuthServer(t)
		provider, err := oidc.Discover(t.Context(), nil, a.issuer())
		if err != nil {
			t.Fatalf("discovery: %v", err)
		}
		keys := oidc.NewKeySet(provider.JWKSURI, nil)
		if err := keys.Warm(t.Context()); err != nil {
			t.Fatalf("warming: %v", err)
		}
		verifier, err := oidc.NewVerifier(keys, provider.Issuer, testAudience,
			oidc.WithScopeClaims(rolesClaim))
		if err != nil {
			t.Fatalf("verifier: %v", err)
		}

		claims := a.claims()
		delete(claims, "scope")
		claims[rolesClaim] = map[string]any{
			"findings:read": map[string]string{"org-1": "Kindlast"},
			"org:manage":    map[string]string{"org-1": "Kindlast"},
		}

		verified, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		if err != nil {
			t.Fatalf("verifying: %v", err)
		}

		for _, want := range []string{"openid", "findings:read", "org:manage"} {
			if !verified.HasScope(want) {
				t.Errorf("scopes = %v, want %q among them", verified.Scopes, want)
			}
		}
	})

	t.Run("openid is not duplicated when the token does carry it", func(t *testing.T) {
		// Keycloak and Entra echo requested scopes back, so `openid` arrives
		// on its own. Asserting it a second time must not produce it twice.
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		claims["scope"] = "openid profile"

		verified, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		if err != nil {
			t.Fatalf("verifying: %v", err)
		}

		count := 0
		for _, scope := range verified.Scopes {
			if scope == "openid" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("scopes = %v, want exactly one openid, got %d", verified.Scopes, count)
		}
	})

	t.Run("a token that fails verification asserts nothing", func(t *testing.T) {
		// The assertion is the *result* of verification, so a refused token
		// must not be handed scopes on the way out.
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		claims["aud"] = "kindlast-intelligence"

		verified, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		if err == nil {
			t.Fatal("a token for another audience was accepted")
		}
		if verified != nil {
			t.Fatalf("claims returned for a refused token: %v", verified)
		}
	})
}
