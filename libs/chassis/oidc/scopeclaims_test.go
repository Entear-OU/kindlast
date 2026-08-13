package oidc_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
	"github.com/golang-jwt/jwt/v5"
)

// Scopes have to be readable from wherever the authorization server actually
// puts them, and the bundled one does not put them where RFC 9068 says.
//
// Measured, not assumed: a client-credentials token from the seeded Zitadel
// v2.71 carries exactly aud, client_id, exp, iat, iss, jti, nbf and sub. No
// `scope`, no `scp`, and no roles claim whatever reserved scope is requested.
// So the reader has to cope with a vendor claim without this package growing a
// table of vendor names, which is what WithScopeClaims is for (§18.2).
func TestScopesAreReadFromWhicheverClaimCarriesThem(t *testing.T) {
	const zitadelRoles = "urn:zitadel:iam:org:project:386089669365858307:roles"

	cases := []struct {
		name   string
		claim  string
		value  any
		expect string
	}{
		{
			name:   "standard space delimited scope",
			claim:  "scope",
			value:  "openid findings:read",
			expect: "findings:read",
		},
		{
			name:   "scp as an array",
			claim:  "scp",
			value:  []string{"openid", "findings:read"},
			expect: "findings:read",
		},
		{
			// Zitadel's shape: an object whose keys are the granted roles and
			// whose values map organisation ids to names.
			name:  "vendor claim as an object keyed by role",
			claim: zitadelRoles,
			value: map[string]any{
				"findings:read": map[string]string{"386089611182538755": "Kindlast"},
				"org:read":      map[string]string{"386089611182538755": "Kindlast"},
			},
			expect: "findings:read",
		},
		{
			name:   "vendor claim as a plain array",
			claim:  "realm_access_roles",
			value:  []string{"findings:read"},
			expect: "findings:read",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
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
				oidc.WithScopeClaims(zitadelRoles, "realm_access_roles"))
			if err != nil {
				t.Fatalf("building the verifier: %v", err)
			}

			claims := a.claims()
			delete(claims, "scope")
			claims[testCase.claim] = testCase.value

			verified, err := verifier.Verify(t.Context(), a.mint(t, "key-1", jwt.MapClaims(claims)))
			if err != nil {
				t.Fatalf("verifying: %v", err)
			}
			if !verified.HasScope(testCase.expect) {
				t.Fatalf("scopes = %v, want %q among them", verified.Scopes, testCase.expect)
			}
		})
	}
}

// A configured vendor claim must not become a way to hold a scope nobody
// granted. The reader is a reader: it reports what the signed token says, and
// the signature is what makes that trustworthy.
func TestAnUnconfiguredVendorClaimIsIgnored(t *testing.T) {
	a := newAuthServer(t)
	verifier := newVerifier(t, a)

	claims := a.claims()
	delete(claims, "scope")
	claims["some:other:claim"] = []string{"billing:manage"}

	verified, err := verifier.Verify(t.Context(), a.mint(t, "key-1", claims))
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if verified.HasScope("billing:manage") {
		t.Fatalf("scopes = %v; a claim the deployment never configured granted authority", verified.Scopes)
	}
}

// Exact match, never a prefix. records:read must not satisfy
// records:ropa:write, and a prefix comparison would let it, which is the kind
// of bug that only shows up once the scope vocabulary splits per resource
// (§23.3, already applied in the seeded role set).
func TestScopeMatchingIsExact(t *testing.T) {
	claims := &oidc.Claims{Scopes: []string{"records:read", "findings:act"}}

	for _, held := range []string{"records:read", "findings:act"} {
		if !claims.HasScope(held) {
			t.Errorf("HasScope(%q) = false, want true", held)
		}
	}
	for _, notHeld := range []string{"records", "records:ropa:write", "findings", "findings:act:all"} {
		if claims.HasScope(notHeld) {
			t.Errorf("HasScope(%q) = true; matching is not exact", notHeld)
		}
	}
}
