// Package apikey holds what an API key IS: its shape, its arithmetic, and the
// rules about what one may carry (ENT-262, §23, §1.7).
//
// A small package for the same reason `delegation` is one. The type that says
// "this request arrived on a partner's key" has to be nameable by the
// interceptor chain, by the Postgres store and by the handler that mints one,
// and putting it in any of those three would make the other two depend on
// something they have no business importing (§21.6).
//
// # THE RULES HERE ARE DECISIONS; THE INVARIANTS ARE IN 00043
//
// Which scopes a partner key may carry, what a key is called, and what a caller
// is told when a credential will not parse are all decisions, so they are Go's.
// That no key may ever hold an `internal:*` scope is an invariant that must hold
// no matter who writes, so it is a CHECK constraint, and the two are not
// duplicates: the constraint binds the migrator and a psql prompt, and
// GrantableScopes below binds the application, which is the only thing that
// should ever be minting.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Prefix marks a Kindlast API key wherever it turns up.
//
// Not decoration and not branding. A fixed, unusual prefix is what lets a
// secret scanner recognise one of these in a commit, a paste bin or a log line,
// which is the difference between a leaked key being revoked within the hour
// and being found by whoever else was looking. GitHub's `ghp_` and Stripe's
// `sk_live_` exist for this and it costs four characters.
const Prefix = "kl_"

// The two halves of a credential, in characters.
//
// The handle is eight bytes rendered as hex, which is the public lookup key.
// The secret is thirty two bytes rendered as unpadded base64url, which is 43
// characters. See the package comment in 00043 for why 256 bits of entropy is
// the thing that makes a plain SHA-256 the right digest.
const (
	handleBytes = 8
	secretBytes = 32

	handleLength     = handleBytes * 2                               // 16
	secretLength     = (secretBytes*8 + 5) / 6                       // 43
	credentialLength = len(Prefix) + handleLength + 1 + secretLength // 63
)

// ErrMalformed is the single answer for every credential that is not one.
//
// Wrong prefix, wrong length, bad hex, bad base64 and empty are one error on
// purpose, and the caller is told nothing more specific than that the credential
// is not usable. Somebody probing this boundary has proved nothing that entitles
// them to know which half they got wrong, and five distinguishable answers make
// this a shape oracle. The same decision `delegation.ErrUnusable` made.
var ErrMalformed = errors.New("apikey: no usable API key")

// Key is a freshly minted credential, as the caller who minted it sees it.
//
// Shown once, here, and never again: nothing stores Credential and nothing can
// recover it, because the table holds only the digest of its second half.
type Key struct {
	// Handle is the public half. It is stored in the clear, it is indexed, and
	// it is what a console shows next to the key's name so a person can tell
	// which row is which without ever seeing a secret.
	Handle string
	// Credential is the whole thing, prefix and both halves. This is the value
	// a partner puts in their configuration.
	Credential string
	// SecretDigest is what goes in the row.
	SecretDigest []byte
}

// Presented is a credential a caller sent, split but not yet verified.
//
// Split is not verified, and the type says so by carrying no method that
// decides anything. Parsing tells you the string is shaped like a key; only
// Matches, against a stored digest, tells you it is one.
type Presented struct {
	// Handle selects the row to check against. Public, and therefore safe to
	// log, unlike everything else in this file.
	Handle string
	// digest is the SHA-256 of the presented secret, computed at parse time so
	// the secret itself is never held on a struct that travels.
	digest []byte
}

// Generate produces a new credential.
//
// crypto/rand, and a failure from it is returned rather than swallowed or
// retried. A CSPRNG that cannot produce eight bytes is a machine that must not
// be minting credentials, and the one thing worse than refusing to mint is
// minting something predictable.
func Generate() (Key, error) {
	handleRaw := make([]byte, handleBytes)
	if _, err := rand.Read(handleRaw); err != nil {
		return Key{}, fmt.Errorf("apikey: generating the handle: %w", err)
	}

	secretRaw := make([]byte, secretBytes)
	if _, err := rand.Read(secretRaw); err != nil {
		return Key{}, fmt.Errorf("apikey: generating the secret: %w", err)
	}

	handle := hex.EncodeToString(handleRaw)
	secret := base64.RawURLEncoding.EncodeToString(secretRaw)
	digest := sha256.Sum256([]byte(secret))

	return Key{
		Handle:       handle,
		Credential:   Prefix + handle + "_" + secret,
		SecretDigest: digest[:],
	}, nil
}

// Parse splits a presented credential without deciding anything about it.
//
// Every check here is about SHAPE, and shape is not secret: the length, the
// prefix and the alphabet of a key are published in the documentation a partner
// reads. Refusing a malformed string before it reaches the database is what
// keeps a stream of garbage from becoming a stream of index lookups, and it
// leaks nothing, because an attacker can already read the format.
//
// What it deliberately does NOT do is tell the caller which check failed. See
// ErrMalformed.
func Parse(credential string) (Presented, error) {
	if len(credential) != credentialLength || !strings.HasPrefix(credential, Prefix) {
		return Presented{}, ErrMalformed
	}

	body := credential[len(Prefix):]
	handle, secret, found := strings.Cut(body, "_")
	if !found || len(handle) != handleLength || len(secret) != secretLength {
		return Presented{}, ErrMalformed
	}

	// The handle reaches a query, so it is validated as hex rather than trusted
	// to be. It is a bound parameter and cannot reach the parser as SQL, but a
	// handle that is not hex matches no row that could ever have been minted, so
	// refusing here saves a round trip and keeps the column's CHECK and this
	// function saying the same thing.
	if _, err := hex.DecodeString(handle); err != nil {
		return Presented{}, ErrMalformed
	}
	if _, err := base64.RawURLEncoding.DecodeString(secret); err != nil {
		return Presented{}, ErrMalformed
	}

	digest := sha256.Sum256([]byte(secret))
	return Presented{Handle: handle, digest: digest[:]}, nil
}

// Matches reports whether this credential is the one the stored digest was made
// from.
//
// CONSTANT TIME, ALWAYS, AND THAT IS NOT NEGOTIABLE EVEN THOUGH THE MATHS SAYS
// IT DOES NOT MATTER HERE.
//
// The honest analysis is that a variable-time compare over these two values
// leaks nothing exploitable: an attacker cannot choose the bytes of the digest,
// only the preimage, so there is no digest to walk one byte at a time. The
// reason it is subtle.ConstantTimeCompare anyway is that the analysis has to be
// redone every time somebody changes what is compared, and the next person to
// touch this will not do it. A rule that holds without argument is worth more
// than a correct argument nobody re-checks.
//
// The length guard is separate and deliberate: ConstantTimeCompare returns 0
// for unequal lengths, which is correct, and a stored digest of the wrong length
// means a row that 00043's octet_length CHECK should have refused.
func (p Presented) Matches(storedDigest []byte) bool {
	if len(storedDigest) != sha256.Size || len(p.digest) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(p.digest, storedDigest) == 1
}
