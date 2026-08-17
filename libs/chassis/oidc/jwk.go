package oidc

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
)

// jwkSet and jwk are the wire shapes from RFC 7517.
//
// Parsed by hand rather than pulled from a JWK library, for two reasons. It is
// sixty lines for the two key types anything issues (RSA and EC), and the
// parser is where an algorithm-confusion bug would hide, so it is worth being
// able to read the whole thing during a security review. Note what is absent:
// no symmetric key type. A JWKS that offers an `oct` key is offering an HMAC
// secret, and accepting one is precisely the confusion §13.2 tests against.
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`

	// RSA
	N string `json:"n"`
	E string `json:"e"`

	// EC
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// parseJWKS turns a JWKS document into public keys by key id.
//
// An empty document is not an error. A freshly seeded Zitadel serves
// `{"keys": []}` because it generates its signing key lazily, on the first
// token it issues, so "no keys yet" is a correct answer to a question asked
// too early rather than a broken authorization server (§1.4). Treating it as
// an error here would turn a boot-order detail into a crash loop; KeySet
// handles it by refetching when a key is actually needed.
func parseJWKS(body []byte) (map[string]crypto.PublicKey, error) {
	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("oidc: parsing jwks: %w", err)
	}

	keys := make(map[string]crypto.PublicKey, len(set.Keys))
	for _, key := range set.Keys {
		// `use: enc` marks a key for encryption, not signatures. Verifying a
		// signature with it is a category error even when it is the right kty.
		if key.Use == "enc" {
			continue
		}
		public, err := key.publicKey()
		if err != nil {
			// One malformed or unsupported key must not blind the verifier to
			// the others alongside it, which is the realistic shape of a JWKS
			// during a key-type migration.
			continue
		}
		keys[key.Kid] = public
	}
	return keys, nil
}

func (k jwk) publicKey() (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		return k.rsaPublicKey()
	case "EC":
		return k.ecPublicKey()
	default:
		return nil, fmt.Errorf("oidc: unsupported key type %q", k.Kty)
	}
}

func (k jwk) rsaPublicKey() (*rsa.PublicKey, error) {
	modulus, err := decodeSegment(k.N)
	if err != nil {
		return nil, fmt.Errorf("oidc: rsa modulus: %w", err)
	}
	exponent, err := decodeSegment(k.E)
	if err != nil {
		return nil, fmt.Errorf("oidc: rsa exponent: %w", err)
	}
	if len(exponent) == 0 || len(exponent) > 8 {
		return nil, fmt.Errorf("oidc: rsa exponent is %d bytes", len(exponent))
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: int(new(big.Int).SetBytes(exponent).Int64()),
	}, nil
}

// ecPublicKey validates an EC key from a JWKS and returns it for signature
// verification.
//
// The on-curve check is a security boundary rather than input hygiene: it is
// what stands between a JWKS endpoint and the key that verifies every token, so
// a point that is not on the curve must never become a verification key.
//
// It is done by handing the uncompressed point to crypto/ecdh, which validates
// on construction, rather than by elliptic.Curve.IsOnCurve, which is deprecated
// as a low-level unsafe API (ENT-216). The same check, on a type whose whole
// purpose is that an invalid point cannot be built in the first place.
func (k jwk) ecPublicKey() (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	var validating ecdh.Curve
	switch k.Crv {
	case "P-256":
		curve, validating = elliptic.P256(), ecdh.P256()
	case "P-384":
		curve, validating = elliptic.P384(), ecdh.P384()
	case "P-521":
		curve, validating = elliptic.P521(), ecdh.P521()
	default:
		return nil, fmt.Errorf("oidc: unsupported curve %q", k.Crv)
	}

	x, err := decodeSegment(k.X)
	if err != nil {
		return nil, fmt.Errorf("oidc: ec x: %w", err)
	}
	y, err := decodeSegment(k.Y)
	if err != nil {
		return nil, fmt.Errorf("oidc: ec y: %w", err)
	}

	// Derived rather than tabulated so it cannot drift from the curve. P-521 is
	// 66 bytes, which is the one a hand-written table gets wrong.
	size := (curve.Params().BitSize + 7) / 8
	if len(x) > size || len(y) > size {
		return nil, fmt.Errorf("oidc: ec coordinate for %q is longer than the curve", k.Crv)
	}

	// A SEC 1 uncompressed point, 0x04 || X || Y, each coordinate left-padded to
	// the curve length.
	//
	// The padding is load-bearing. RFC 7518 §6.2.1.2 requires the full length
	// with leading zeros preserved, and issuers exist that strip them anyway.
	// The previous implementation read coordinates straight into big.Int, which
	// does not care how many bytes it is given, whereas NewPublicKey requires
	// exactly this length. Without the padding such an issuer would stop
	// verifying entirely, and only for the roughly one key in 256 whose X starts
	// with a zero byte: a failure nobody reproduces before a customer does.
	point := make([]byte, 1+2*size)
	point[0] = 4
	copy(point[1+size-len(x):1+size], x)
	copy(point[1+2*size-len(y):], y)

	// The validation, and the only reason this conversion exists. Rejects a
	// point off the curve and the point at infinity. The resulting ecdh key is
	// deliberately discarded: what verifies a token is ecdsa.
	if _, err := validating.NewPublicKey(point); err != nil {
		return nil, fmt.Errorf("oidc: ec point for %q is not a valid public key: %w", k.Crv, err)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}, nil
}

// decodeSegment reads base64url without padding, which is what RFC 7515 §2
// specifies for every value in a JWK.
func decodeSegment(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("oidc: empty value")
	}
	return base64.RawURLEncoding.DecodeString(value)
}
