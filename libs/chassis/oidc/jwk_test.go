package oidc

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
)

// Tests for the JWKS parser (ENT-216).
//
// WHY THIS FILE EXISTS
//
// The parser had no tests at all, which matters more here than the line count
// suggests. `ecPublicKey` is what stands between a JWKS endpoint and the key
// used to verify every token, so its rejections are a security boundary rather
// than input hygiene. ENT-216 replaces the on-curve check with `crypto/ecdh`,
// and swapping an untested security check for a different untested security
// check is not a migration, it is a coin toss.
//
// The off-curve case is the one that has to be able to fail. Per AGENTS.md it
// was verified by deleting the check and watching it go red, before the
// replacement was written, not after.

// encodeCoord writes a big.Int as base64url without padding, left-padded to the
// curve's byte length, which is what RFC 7518 §6.2.1.2 requires.
func encodeCoord(v *big.Int, byteLen int) string {
	b := make([]byte, byteLen)
	v.FillBytes(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ecJWK builds a JWK document for one EC public key.
func ecJWK(t *testing.T, kid, crv string, x, y *big.Int, byteLen int) []byte {
	t.Helper()
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "EC",
			"kid": kid,
			"crv": crv,
			"x":   encodeCoord(x, byteLen),
			"y":   encodeCoord(y, byteLen),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}
	return body
}

func TestECKeyOnTheCurveIsAccepted(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	keys, err := parseJWKS(ecJWK(t, "good", "P-256", key.X, key.Y, 32))
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}

	got, ok := keys["good"].(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("key %q is %T, want *ecdsa.PublicKey", "good", keys["good"])
	}
	if got.X.Cmp(key.X) != 0 || got.Y.Cmp(key.Y) != 0 {
		t.Fatal("the parsed point is not the one that was encoded")
	}
	if got.Curve != elliptic.P256() {
		t.Fatalf("curve is %v, want P-256", got.Curve.Params().Name)
	}
}

// The load-bearing test. A point that is not on the curve must never become a
// verification key.
//
// Y+1 is off the curve for any valid (X, Y): the curve equation admits at most
// two Y values for a given X, and they are Y and -Y, which differ by more than
// one for any real key.
func TestECKeyOffTheCurveIsRejected(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	bad := new(big.Int).Add(key.Y, big.NewInt(1))

	keys, err := parseJWKS(ecJWK(t, "offcurve", "P-256", key.X, bad, 32))
	if err != nil {
		t.Fatalf("parseJWKS returned an error for the document: %v", err)
	}
	if _, present := keys["offcurve"]; present {
		t.Fatal("an off-curve point was accepted as a verification key")
	}
}

// The point at infinity encodes as (0, 0) and is not a usable public key.
// crypto/ecdh rejects it explicitly; elliptic.IsOnCurve did too, and losing
// that in the migration would be silent.
func TestECPointAtInfinityIsRejected(t *testing.T) {
	keys, err := parseJWKS(ecJWK(t, "zero", "P-256", big.NewInt(0), big.NewInt(0), 32))
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if _, present := keys["zero"]; present {
		t.Fatal("the point at infinity was accepted as a verification key")
	}
}

// RFC 7518 requires the full coordinate length with leading zeros preserved,
// and issuers exist that strip them anyway.
//
// This guards a real regression risk in ENT-216 rather than a hypothetical one:
// the old code read coordinates with big.Int.SetBytes, which does not care how
// many bytes it is given, and crypto/ecdh's NewPublicKey requires exactly the
// curve length and rejects anything shorter. A migration that forgets to
// left-pad turns a working issuer into a total verification outage, and the
// failure only appears for the ~1 key in 256 whose X starts with a zero byte,
// which is to say: not in anybody's staging environment.
func TestECCoordinatesWithStrippedLeadingZerosAreAccepted(t *testing.T) {
	var key *ecdsa.PublicKey
	for range 20000 {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("generating key: %v", err)
		}
		// A coordinate needing fewer than the full 32 bytes is exactly the case
		// a stripping issuer would send short.
		if len(k.X.Bytes()) < 32 {
			key = &k.PublicKey
			break
		}
	}
	if key == nil {
		t.Fatal("no key with a leading zero byte in X after 20000 attempts")
	}

	// Encoded at their natural length, so X is short: what a stripping issuer
	// actually puts on the wire.
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "EC",
			"kid": "short",
			"crv": "P-256",
			"x":   base64.RawURLEncoding.EncodeToString(key.X.Bytes()),
			"y":   base64.RawURLEncoding.EncodeToString(key.Y.Bytes()),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	got, ok := keys["short"].(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("a key whose X had its leading zero stripped was rejected (%d bytes)", len(key.X.Bytes()))
	}
	if got.X.Cmp(key.X) != 0 || got.Y.Cmp(key.Y) != 0 {
		t.Fatal("the parsed point is not the one that was encoded")
	}
}

// A coordinate longer than the curve is malformed, not a big number.
func TestECCoordinateLongerThanTheCurveIsRejected(t *testing.T) {
	oversized := make([]byte, 33)
	for i := range oversized {
		oversized[i] = 0xff
	}
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "EC",
			"kid": "long",
			"crv": "P-256",
			"x":   base64.RawURLEncoding.EncodeToString(oversized),
			"y":   base64.RawURLEncoding.EncodeToString(oversized),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if _, present := keys["long"]; present {
		t.Fatal("an oversized coordinate was accepted")
	}
}

func TestUnsupportedCurveIsRejected(t *testing.T) {
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "EC",
			"kid": "p224",
			"crv": "P-224",
			"x":   base64.RawURLEncoding.EncodeToString(make([]byte, 28)),
			"y":   base64.RawURLEncoding.EncodeToString(make([]byte, 28)),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if _, present := keys["p224"]; present {
		t.Fatal("an unsupported curve was accepted")
	}
}

// The algorithm-confusion case the parser's own doc comment calls out: an `oct`
// key is an HMAC secret, and accepting one lets an attacker who can read the
// JWKS sign tokens the verifier will trust.
func TestSymmetricKeyIsRejected(t *testing.T) {
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "oct",
			"kid": "hmac",
			"k":   base64.RawURLEncoding.EncodeToString([]byte("a shared secret")),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if _, present := keys["hmac"]; present {
		t.Fatal("a symmetric key was accepted as a verification key")
	}
}

// One bad key must not blind the verifier to a good one beside it, which is the
// realistic shape of a JWKS mid key-rotation.
func TestOneMalformedKeyDoesNotDropTheOthers(t *testing.T) {
	good, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	bad := new(big.Int).Add(good.Y, big.NewInt(1))

	doc := map[string]any{
		"keys": []map[string]string{
			{
				"kty": "EC", "kid": "broken", "crv": "P-256",
				"x": encodeCoord(good.X, 32), "y": encodeCoord(bad, 32),
			},
			{
				"kty": "EC", "kid": "usable", "crv": "P-256",
				"x": encodeCoord(good.X, 32), "y": encodeCoord(good.Y, 32),
			},
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if _, present := keys["broken"]; present {
		t.Fatal("the off-curve key was accepted")
	}
	if _, present := keys["usable"]; !present {
		t.Fatal("the usable key was dropped alongside the broken one")
	}
}

// `use: enc` marks a key for encryption. Verifying a signature with it is a
// category error even when the key type is right.
func TestEncryptionKeyIsSkipped(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "EC", "kid": "enconly", "use": "enc", "crv": "P-256",
			"x": encodeCoord(key.X, 32), "y": encodeCoord(key.Y, 32),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if _, present := keys["enconly"]; present {
		t.Fatal("a key marked use=enc was accepted for verification")
	}
}

// P-384 and P-521 are supported curves, and P-521 is the one whose byte length
// (66, from 521 bits) a padding bug rounds wrong.
func TestOtherSupportedCurvesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		crv     string
		curve   elliptic.Curve
		byteLen int
	}{
		{"P-384", elliptic.P384(), 48},
		{"P-521", elliptic.P521(), 66},
	} {
		t.Run(tc.crv, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(tc.curve, rand.Reader)
			if err != nil {
				t.Fatalf("generating key: %v", err)
			}
			keys, err := parseJWKS(ecJWK(t, "k", tc.crv, key.X, key.Y, tc.byteLen))
			if err != nil {
				t.Fatalf("parseJWKS: %v", err)
			}
			got, ok := keys["k"].(*ecdsa.PublicKey)
			if !ok {
				t.Fatalf("%s key was rejected", tc.crv)
			}
			if got.X.Cmp(key.X) != 0 || got.Y.Cmp(key.Y) != 0 {
				t.Fatal("the parsed point is not the one that was encoded")
			}
		})
	}
}

// RSA is the other key type anything issues, and Zitadel issues it, so a
// regression here is a total outage rather than an edge case.
func TestRSAKeyRoundTrips(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	doc := map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": "rsa",
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling jwks: %v", err)
	}

	keys, err := parseJWKS(body)
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	got, ok := keys["rsa"].(*rsa.PublicKey)
	if !ok {
		t.Fatalf("rsa key is %T, want *rsa.PublicKey", keys["rsa"])
	}
	if got.N.Cmp(key.N) != 0 || got.E != key.E {
		t.Fatal("the parsed RSA key is not the one that was encoded")
	}
}

// An empty document is a correct answer to a question asked too early, not an
// error: a freshly seeded Zitadel generates its signing key lazily (§1.4).
func TestEmptyJWKSIsNotAnError(t *testing.T) {
	keys, err := parseJWKS([]byte(`{"keys": []}`))
	if err != nil {
		t.Fatalf("parseJWKS: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("got %d keys, want 0", len(keys))
	}
}
