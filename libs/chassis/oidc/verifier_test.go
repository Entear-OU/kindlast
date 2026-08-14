package oidc_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The audience this resource server accepts, and the only one. §1.4: core-api
// accepts `aud: kindlast-core-api`, intelligence accepts
// `aud: kindlast-intelligence`, and neither accepts the other's.
const testAudience = "kindlast-core-api"

// newVerifier wires a verifier the way main.go does: discover the issuer, take
// the JWKS URI from the discovery document rather than assuming a path, warm
// the cache once at boot.
func newVerifier(t *testing.T, a *authServer) *oidc.Verifier {
	t.Helper()

	provider, err := oidc.Discover(t.Context(), nil, a.issuer())
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if provider.JWKSURI != a.jwksURI() {
		t.Fatalf("discovery returned jwks_uri %q, want %q", provider.JWKSURI, a.jwksURI())
	}

	keys := oidc.NewKeySet(provider.JWKSURI, nil)
	if err := keys.Warm(t.Context()); err != nil {
		t.Fatalf("warming the key set: %v", err)
	}

	verifier, err := oidc.NewVerifier(keys, provider.Issuer, testAudience)
	if err != nil {
		t.Fatalf("building the verifier: %v", err)
	}
	return verifier
}

// The token battery of §13.2. Each case spoils exactly one property of a token
// that would otherwise be accepted, and asserts both that it was refused and
// which check refused it. Asserting only "denied" would let a broken audience
// check hide behind a working expiry check.
func TestTokenBattery(t *testing.T) {
	t.Run("valid token allowed", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims, err := verifier.Verify(t.Context(), a.mint(t, "key-1", a.claims()))
		if err != nil {
			t.Fatalf("a valid token was refused: %v", err)
		}

		if claims.Subject != "user-subject-1" {
			t.Errorf("subject = %q, want user-subject-1", claims.Subject)
		}
		if claims.TokenID != "token-id-1" {
			t.Errorf("jti = %q, want token-id-1", claims.TokenID)
		}
		if !claims.EmailVerified {
			t.Error("email_verified came back false; it gates finding approval (§1.7)")
		}
		if !claims.HasScope("findings:read") {
			t.Errorf("scopes = %v, want findings:read among them", claims.Scopes)
		}
	})

	t.Run("wrong aud denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		claims["aud"] = "kindlast-intelligence"

		_, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		assertDenied(t, err, oidc.ErrAudienceMismatch,
			"a token minted for the other resource server replayed successfully (§1.4)")
	})

	t.Run("wrong iss denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		claims["iss"] = "https://issuer.example.invalid"

		_, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		assertDenied(t, err, oidc.ErrIssuerMismatch, "a token from another issuer was accepted")
	})

	t.Run("alg none denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		_, err := verifier.Verify(t.Context(), unsignedToken(t, "key-1", a.claims()))
		assertDenied(t, err, oidc.ErrTokenInvalid, "a token with alg:none and no signature was accepted")
	})

	t.Run("HS256 signed with the public key denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		// The classic algorithm-confusion attack. The signing key is the
		// server's public key, which the attacker fetches from the JWKS the
		// same way this verifier does.
		forged := jwt.NewWithClaims(jwt.SigningMethodHS256, a.claims())
		forged.Header["kid"] = "key-1"

		signed, err := forged.SignedString(publicKeyPEM(t, &signingKey().PublicKey))
		if err != nil {
			t.Fatalf("forging the HS256 token: %v", err)
		}

		_, err = verifier.Verify(t.Context(), signed)
		assertDenied(t, err, oidc.ErrTokenInvalid,
			"a token signed with the public key as an HMAC secret was accepted; anyone holding the JWKS can mint tokens")
	})

	t.Run("expired denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		claims["exp"] = time.Now().Add(-time.Hour).Unix()

		_, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		assertDenied(t, err, oidc.ErrTokenExpired, "an expired token was accepted")
	})

	t.Run("token with no expiry denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		claims := a.claims()
		delete(claims, "exp")

		// A token that never expires defeats the bound that makes local
		// verification safe in the first place (§1.4): revocation latency is
		// only acceptable because it is capped by the token's own lifetime.
		_, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
		assertDenied(t, err, oidc.ErrTokenInvalid, "a token with no exp claim was accepted")
	})

	t.Run("signed by a key the server does not serve denied", func(t *testing.T) {
		a := newAuthServer(t)
		verifier := newVerifier(t, a)

		// Right kid, wrong key: the signature is checked against the key the
		// JWKS actually publishes, not against whatever signed the token.
		forged := signWith(t, strangerKey(), "key-1", a.claims())

		_, err := verifier.Verify(t.Context(), forged)
		assertDenied(t, err, oidc.ErrTokenInvalid, "a token signed by an unknown key was accepted")
	})
}

// "Unknown kid triggers exactly one JWKS refetch, not one per request."
//
// The counter lives on the authorization server double rather than on the
// cache, because the criterion is about outbound traffic. A cache that
// answered correctly while hammering the IdP would pass any assertion made
// against its own internals.
func TestUnknownKidRefetchesExactlyOnce(t *testing.T) {
	a := newAuthServer(t)
	verifier := newVerifier(t, a)

	afterWarm := a.fetchCount()
	if afterWarm != 1 {
		t.Fatalf("boot fetched the JWKS %d times, want 1", afterWarm)
	}

	token := a.mint(t, "key-that-does-not-exist", a.claims())

	const requests = 5
	for i := range requests {
		if _, err := verifier.Verify(t.Context(), token); err == nil {
			t.Fatalf("request %d: a token naming an unknown kid was accepted", i+1)
		}
	}

	refetches := a.fetchCount() - afterWarm
	if refetches != 1 {
		t.Fatalf("%d requests naming an unknown kid caused %d refetches, want exactly 1; "+
			"an unknown kid must not be a request amplifier against the authorization server",
			requests, refetches)
	}
}

// The cooldown must suppress a stampede without becoming a lockout. If it
// never lapsed, a genuine key rotation would be an outage lasting until the
// process restarted, which is a worse failure than the one it prevents.
func TestTheRefetchCooldownLapses(t *testing.T) {
	a := newAuthServer(t)

	provider, err := oidc.Discover(t.Context(), nil, a.issuer())
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	keys := oidc.NewKeySet(provider.JWKSURI, nil)
	keys.SetRefetchCooldown(20 * time.Millisecond)
	if err := keys.Warm(t.Context()); err != nil {
		t.Fatalf("warming: %v", err)
	}
	verifier, err := oidc.NewVerifier(keys, provider.Issuer, testAudience)
	if err != nil {
		t.Fatalf("building the verifier: %v", err)
	}

	afterWarm := a.fetchCount()
	token := a.mint(t, "rotated-key", a.claims())

	if _, err := verifier.Verify(t.Context(), token); err == nil {
		t.Fatal("an unknown kid was accepted")
	}
	if got := a.fetchCount() - afterWarm; got != 1 {
		t.Fatalf("first unknown kid caused %d refetches, want 1", got)
	}

	time.Sleep(40 * time.Millisecond)

	// The server has now rotated: the key the token names exists.
	a.generateSigningKey("rotated-key")

	if _, err := verifier.Verify(t.Context(), token); err != nil {
		t.Fatalf("after the cooldown lapsed and the key was published, verification still failed: %v", err)
	}
}

// The trap that motivated this whole issue, and the one that would otherwise
// ship silently.
//
// A freshly seeded Zitadel serves `{"keys": []}` because it generates its
// signing key lazily, on the first token it issues. A cache populated once at
// boot therefore holds nothing and rejects every token that follows, and the
// error reads as a signature problem rather than as an empty cache.
//
// This test fails if Warm is ever counted as the refetch, which is the exact
// mistake the design note in §1.4 warns about: the boot fetch must never be
// the last fetch.
func TestAnEmptyJWKSAtStartupDoesNotBreakVerificationForever(t *testing.T) {
	a := newLazyAuthServer(t)
	verifier := newVerifier(t, a)

	if a.fetchCount() != 1 {
		t.Fatalf("boot fetched the JWKS %d times, want 1", a.fetchCount())
	}

	// Zitadel mints its first token, generating the signing key as it does so.
	a.generateSigningKey("key-1")

	claims, err := verifier.Verify(t.Context(), a.mint(t, "key-1", a.claims()))
	if err != nil {
		t.Fatalf("verification never recovered from an empty JWKS at boot: %v\n"+
			"the boot fetch cached {\"keys\": []} and was treated as the last word (§1.4)", err)
	}
	if claims.Subject != "user-subject-1" {
		t.Fatalf("subject = %q, want user-subject-1", claims.Subject)
	}
}

// Discovery is how §18.2 stays true: a self-hoster points at their own IdP and
// nothing in this codebase assumes a Zitadel-shaped path. The issuer check is
// the part that makes trusting the document safe.
func TestDiscoveryRejectsADocumentNamingAnotherIssuer(t *testing.T) {
	a := newAuthServer(t)

	_, err := oidc.Discover(t.Context(), nil, a.issuer()+"/tenant-b")
	if err == nil {
		t.Fatal("a discovery document naming a different issuer was accepted; " +
			"an attacker who can steer the issuer URL could then supply the keys too")
	}
}

func assertDenied(t *testing.T, err error, want error, why string) {
	t.Helper()

	if err == nil {
		t.Fatalf("token accepted: %s", why)
	}
	if !errors.Is(err, want) {
		t.Fatalf("denied for the wrong reason: got %v, want %v\n(%s)", err, want, why)
	}
}

// unsignedToken builds the `alg: none` token by hand, because no signing
// library will produce one for you, which is itself the point.
func unsignedToken(t *testing.T, kid string, claims jwt.MapClaims) string {
	t.Helper()

	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT", "kid": kid})
	if err != nil {
		t.Fatalf("marshalling the header: %v", err)
	}
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshalling the claims: %v", err)
	}

	return strings.Join([]string{
		base64.RawURLEncoding.EncodeToString(header),
		base64.RawURLEncoding.EncodeToString(body),
		"",
	}, ".")
}
