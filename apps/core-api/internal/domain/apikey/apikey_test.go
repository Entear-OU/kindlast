package apikey_test

import (
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
)

// Pure tests. Nothing here needs the stack, because nothing here touches it:
// the shape of a credential and the set a key may carry are decisions, and a
// decision that needs a database to exercise is in the wrong place.

func TestAMintedKeyRoundTrips(t *testing.T) {
	key, err := apikey.Generate()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	presented, err := apikey.Parse(key.Credential)
	if err != nil {
		t.Fatalf("parsing the credential we just minted: %v", err)
	}
	if presented.Handle != key.Handle {
		t.Errorf("handle %q survived parsing as %q", key.Handle, presented.Handle)
	}
	if !presented.Matches(key.SecretDigest) {
		t.Error("a freshly minted key did not match its own digest")
	}
}

// The credential must be recognisable on sight, so a scanner can find one in a
// commit and a person can find one in a config file.
func TestACredentialIsRecognisable(t *testing.T) {
	key, err := apikey.Generate()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	if !strings.HasPrefix(key.Credential, apikey.Prefix) {
		t.Errorf("credential %q does not carry the %q prefix", key.Credential, apikey.Prefix)
	}
	if len(key.SecretDigest) != sha256.Size {
		t.Errorf("digest is %d bytes, want %d", len(key.SecretDigest), sha256.Size)
	}
	// The credential must not contain the digest, which would make storing the
	// digest pointless.
	if strings.Contains(key.Credential, string(key.SecretDigest)) {
		t.Error("the credential contains its own digest")
	}
}

// Two mints must never collide, which is the whole basis for the handle being a
// unique index and for the digest being a plain SHA-256.
func TestTwoMintsDiffer(t *testing.T) {
	seen := make(map[string]struct{}, 256)
	for i := 0; i < 256; i++ {
		key, err := apikey.Generate()
		if err != nil {
			t.Fatalf("mint %d: %v", i, err)
		}
		if _, repeat := seen[key.Handle]; repeat {
			t.Fatalf("handle %q was minted twice in %d draws", key.Handle, i+1)
		}
		seen[key.Handle] = struct{}{}
	}
}

// THE SECURITY PROPERTY. A credential that is not the right one must not match,
// and every near miss below is a way somebody might try to get one that does.
func TestAWrongSecretDoesNotMatch(t *testing.T) {
	right, err := apikey.Generate()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	wrong, err := apikey.Generate()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	// A different key's credential, against this key's digest.
	presented, err := apikey.Parse(wrong.Credential)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if presented.Matches(right.SecretDigest) {
		t.Error("another key's credential matched this key's digest")
	}

	// The right handle with the wrong secret, which is the shape an attacker who
	// read a handle out of a console screenshot would try.
	spliced := apikey.Prefix + right.Handle + "_" +
		wrong.Credential[len(apikey.Prefix)+len(right.Handle)+1:]
	presented, err = apikey.Parse(spliced)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if presented.Handle != right.Handle {
		t.Fatalf("the splice did not produce the handle under test")
	}
	if presented.Matches(right.SecretDigest) {
		t.Error("the right handle with another key's secret matched")
	}

	// An empty or short digest must never match, so a row that somehow held one
	// cannot be authenticated against.
	if presented.Matches(nil) {
		t.Error("a nil digest matched")
	}
	if presented.Matches(right.SecretDigest[:16]) {
		t.Error("a truncated digest matched")
	}
}

// Every malformed shape gets the same answer, and none of them reaches a
// lookup.
func TestMalformedCredentialsAreRefusedIdentically(t *testing.T) {
	good, err := apikey.Generate()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	body := good.Credential[len(apikey.Prefix):]

	for name, credential := range map[string]string{
		"empty":           "",
		"no prefix":       body,
		"wrong prefix":    "sk_" + body,
		"truncated":       good.Credential[:len(good.Credential)-1],
		"overlong":        good.Credential + "a",
		"no separator":    strings.Replace(good.Credential, "_", "x", 2),
		"handle not hex":  apikey.Prefix + "zzzzzzzzzzzzzzzz_" + body[17:],
		"secret not b64":  good.Credential[:len(good.Credential)-1] + "!",
		"a bearer token":  "eyJhbGciOiJSUzI1NiIsImtpZCI6InRlc3Qta2V5In0.e30.x",
		"whitespace only": strings.Repeat(" ", len(good.Credential)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := apikey.Parse(credential); err == nil {
				t.Fatalf("%q parsed as a credential", credential)
			} else if err != apikey.ErrMalformed { //nolint:errorlint // identity is the assertion
				t.Errorf("got %v, want the single ErrMalformed answer", err)
			}
		})
	}
}

// THE INVARIANT THIS PACKAGE EXISTS FOR. A key may never be minted with a scope
// that reaches the platform surface.
//
// The database refuses it too (00043's api_keys_no_internal_scope), and that is
// the boundary. This is the readable message at the point somebody asked.
func TestAnInternalScopeIsNeverGrantable(t *testing.T) {
	for _, scope := range []string{
		"internal:act-on-behalf",
		"internal:ingest",
		"internal:intelligence",
		"internal:sweep",
	} {
		if apikey.Grantable(scope) {
			t.Errorf("%q is grantable to an API key, which would put a cross-tenant "+
				"verb on a tenant-scoped credential", scope)
		}
		mint := apikey.Mint{Name: "escalation", Scopes: []string{scope}}
		if _, err := mint.Validate(); err == nil {
			t.Errorf("a mint carrying %q was accepted", scope)
		}
	}
}

// The one exclusion that is not a nice-to-have: a key that could mint keys.
func TestAKeyCannotBeGrantedTheAbilityToMintKeys(t *testing.T) {
	if apikey.Grantable("org:manage") {
		t.Fatal("org:manage is grantable to an API key, so a key could mint " +
			"another key and extend its own access with no human in the loop")
	}
}

// A key must never reach a surface a signed-in person could not, because it acts
// under that person's membership. This is the test that keeps the two lists from
// drifting apart.
func TestEveryGrantableScopeIsAHumanScope(t *testing.T) {
	human := make(map[string]struct{}, len(interceptor.HumanScopes))
	for _, scope := range interceptor.HumanScopes {
		human[scope] = struct{}{}
	}

	for _, scope := range apikey.GrantableScopes {
		if _, ok := human[scope]; !ok {
			t.Errorf("%q may be minted onto a key but is not a scope a person holds, "+
				"so a key would carry authority its minter does not have", scope)
		}
	}
}

func TestValidateNormalises(t *testing.T) {
	got, err := (apikey.Mint{
		Name:   "  Reporting robot  ",
		Scopes: []string{"records:read", "audit:read", "records:read"},
	}).Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got.Name != "Reporting robot" {
		t.Errorf("name %q was not trimmed", got.Name)
	}
	if len(got.Scopes) != 2 {
		t.Fatalf("scopes %v were not deduplicated", got.Scopes)
	}
	if got.Scopes[0] != "audit:read" || got.Scopes[1] != "records:read" {
		t.Errorf("scopes %v were not sorted", got.Scopes)
	}
}

func TestValidateRefusesTheEmptyCases(t *testing.T) {
	if _, err := (apikey.Mint{Name: "", Scopes: []string{"records:read"}}).Validate(); err == nil {
		t.Error("a key with no name was accepted")
	}
	if _, err := (apikey.Mint{
		Name:   strings.Repeat("a", apikey.MaxNameLength+1),
		Scopes: []string{"records:read"},
	}).Validate(); err == nil {
		t.Error("a name longer than the column was accepted")
	}
	if _, err := (apikey.Mint{Name: "empty"}).Validate(); err == nil {
		t.Error("a key with no scopes was accepted, and it could do nothing")
	}

	tooMany := make([]string, apikey.MaxScopes+1)
	for i := range tooMany {
		tooMany[i] = "records:read"
	}
	if _, err := (apikey.Mint{Name: "many", Scopes: tooMany}).Validate(); err == nil {
		t.Error("more scopes than the ceiling were accepted")
	}
}

func TestPrincipalHoldsIsExact(t *testing.T) {
	principal := apikey.Principal{Scopes: []string{"records:read"}}

	if !principal.Holds("records:read") {
		t.Error("a held scope was not held")
	}
	// A prefix must not satisfy a longer requirement, matching HasScope.
	if principal.Holds("records:ropa:write") {
		t.Error("records:read satisfied records:ropa:write")
	}
	if principal.Holds("") {
		t.Error("the empty scope was held")
	}
}
