package oidc

import (
	"crypto"
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

func (k jwk) ecPublicKey() (*ecdsa.PublicKey, error) {
	var curve elliptic.Curve
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
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

	key := &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(x),
		Y:     new(big.Int).SetBytes(y),
	}
	if !curve.IsOnCurve(key.X, key.Y) {
		return nil, fmt.Errorf("oidc: ec point for %q is not on the curve", k.Crv)
	}
	return key, nil
}

// decodeSegment reads base64url without padding, which is what RFC 7515 §2
// specifies for every value in a JWK.
func decodeSegment(value string) ([]byte, error) {
	if value == "" {
		return nil, fmt.Errorf("oidc: empty value")
	}
	return base64.RawURLEncoding.DecodeString(value)
}
