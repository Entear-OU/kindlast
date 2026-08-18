// Package secrets seals a customer's third-party credentials before they reach
// a database column, and is the only thing in this system that holds the key
// (ENT-231, §25).
//
// # THE KEY MANAGEMENT DECISION, WRITTEN DOWN BECAUSE ENT-231 ASKS FOR IT
//
// A third-party credential is the customer's, it is useful to an attacker on
// its own, and it must survive a database backup being copied somewhere it
// should not have been. Four choices, and each is here rather than in a
// document nobody opens.
//
// WHERE THE KEY LIVES. In core-api's process memory, loaded at boot from an
// operator-supplied value or a mounted file, and nowhere else. Not in the
// database, because a key stored beside the ciphertext it protects is an
// encoding rather than an encryption. Not in the gateway, because the gateway
// is the process most exposed to a customer's system and should hold nothing
// at rest. Not in a KMS, because the self-hosted deployment is a first-class
// build and one that needed a cloud provider's key service would not be.
//
// WHAT THE ALGORITHM IS. AES-256-GCM, with a random 96-bit nonce per seal
// stored in front of the ciphertext, and the connection's own row id bound in
// as additional authenticated data. The last part is what stops a ciphertext
// being moved between rows: a credential lifted from one organisation's
// connection and pasted into another's fails to open rather than opening as
// somebody else's secret.
//
// HOW ROTATION WORKS. Every sealed value records the id of the key that sealed
// it, in a column beside it. The configuration takes a primary key and any
// number of retired ones, so an operator adds a new primary, restarts, and
// every row still opens; re-sealing rows onto the new key is then a background
// job that can take as long as it likes. A scheme with one key and no id is
// one where rotation means downtime, which is why rotation does not happen.
//
// AND WHAT HAPPENS WITH NO KEY CONFIGURED. Connections that need a credential
// are refused, with a message naming the setting. Not stored in plaintext, and
// not silently accepted: the failure mode of "we could not encrypt it so we
// kept it as it was" is the one that ends up in a breach notification.
//
// # EVIDENCE RETENTION, DECIDED HERE BECAUSE IT IS THE SAME QUESTION
//
// Fetched content becomes `org_evidence`, which is org-scoped, cascades on
// erasure, and holds no delete grant for anybody: a customer removes it by
// erasing their organisation, which is one gesture rather than a per-row
// button that would make "correct this" and "erase this" the same click.
// Retention is therefore "for the life of the organisation", deliberately, and
// the argument is that an observation is what a finding was derived from: a
// compliance record whose evidence expired on a timer is one where last year's
// finding can no longer be checked. What bounds the exposure instead is that
// the content is redacted before it is stored and that erasure is complete.
// A per-class expiry belongs with the retention policy work in §25 and wants a
// customer-visible setting rather than a constant in this package.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrNoKey is returned when this deployment has no sealing key.
//
// Distinguished from a failure to open, because the two want different
// reactions: one is an operator who has not finished configuring the stack,
// the other is a ciphertext that has been tampered with or a key that has been
// lost.
var ErrNoKey = errors.New("this deployment has no integration key, so it cannot store a credential")

// ErrCannotOpen is returned when a sealed value does not open.
//
// One error for every reason, and deliberately uninformative. Distinguishing
// "wrong key" from "tampered" from "wrong row" would tell somebody holding a
// stolen ciphertext which of their guesses was closest.
var ErrCannotOpen = errors.New("that stored credential cannot be opened")

// Keyring holds the key that seals, and the ones that only open.
type Keyring struct {
	primaryID string
	keys      map[string]cipher.AEAD
}

// NewKeyring builds a keyring from a primary key and any number of retired
// ones.
//
// Each entry is `id:base64key`, and the id is what a row records. Ids are
// opaque and an operator picks them; `2026-08` is as good as anything, and
// better than a counter, because a date says when a key started being used and
// a counter says nothing.
//
// An empty setting produces an empty keyring rather than an error, because a
// deployment that connects no integrations legitimately has no key, and
// refusing to start would be this feature deciding whether the rest of the
// product runs.
func NewKeyring(primary string, retired ...string) (*Keyring, error) {
	ring := &Keyring{keys: map[string]cipher.AEAD{}}

	if strings.TrimSpace(primary) == "" {
		return ring, nil
	}

	id, aead, err := parseKey(primary)
	if err != nil {
		return nil, fmt.Errorf("the primary integration key: %w", err)
	}
	ring.primaryID = id
	ring.keys[id] = aead

	for _, entry := range retired {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		retiredID, retiredAEAD, err := parseKey(entry)
		if err != nil {
			return nil, fmt.Errorf("a retired integration key: %w", err)
		}
		if retiredID == id {
			// Worth refusing rather than tolerating: two entries claiming one
			// id means a row's `key_id` no longer identifies which key sealed
			// it, which is the one thing the id is for.
			return nil, fmt.Errorf("the integration key %q is listed as both primary and retired", retiredID)
		}
		ring.keys[retiredID] = retiredAEAD
	}
	return ring, nil
}

// Configured reports whether this deployment can seal anything.
func (k *Keyring) Configured() bool { return k != nil && k.primaryID != "" }

// Seal encrypts a credential for one connection.
//
// `associated` is the connection's row id, bound into the authentication tag
// so a ciphertext cannot be moved between rows. Passing something that is not
// stable for the row's life would make every stored credential unopenable
// after the first change, which is why it is the id rather than the endpoint.
func (k *Keyring) Seal(plaintext, associated string) (sealed []byte, keyID string, err error) {
	if !k.Configured() {
		return nil, "", ErrNoKey
	}

	aead := k.keys[k.primaryID]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", fmt.Errorf("generating a nonce: %w", err)
	}

	// The nonce in front of the ciphertext, which is the ordinary layout and
	// is safe: a nonce is not a secret, only unique.
	out := aead.Seal(nonce, nonce, []byte(plaintext), []byte(associated))
	return out, k.primaryID, nil
}

// Open decrypts a credential sealed for one connection.
func (k *Keyring) Open(sealed []byte, keyID, associated string) (string, error) {
	if k == nil || len(k.keys) == 0 {
		return "", ErrNoKey
	}

	aead, known := k.keys[keyID]
	if !known {
		// A row sealed with a key this deployment no longer has. Reported as
		// unopenable rather than as a missing key, because from a caller's
		// side there is nothing to do differently, and because saying which
		// key ids exist would be free information for anybody who should not
		// have the ciphertext.
		return "", ErrCannotOpen
	}
	if len(sealed) < aead.NonceSize() {
		return "", ErrCannotOpen
	}

	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(associated))
	if err != nil {
		return "", ErrCannotOpen
	}
	return string(plaintext), nil
}

// parseKey reads an `id:base64key` entry.
func parseKey(entry string) (string, cipher.AEAD, error) {
	id, encoded, found := strings.Cut(strings.TrimSpace(entry), ":")
	if !found {
		return "", nil, errors.New("must be written `id:base64key`")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil, errors.New("has an empty id, and a row has to record which key sealed it")
	}

	key, err := decodeKey(strings.TrimSpace(encoded))
	if err != nil {
		return "", nil, err
	}
	if len(key) != 32 {
		// AES-256 only. Accepting 128 and 192 as well would mean a deployment
		// could be weakened by a configuration typo that still starts.
		return "", nil, fmt.Errorf("is %d bytes; it must be 32 (AES-256)", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, fmt.Errorf("building the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, fmt.Errorf("building GCM: %w", err)
	}
	return id, aead, nil
}

// decodeKey reads standard base64, falling back to raw (unpadded) base64.
//
// Both, because the two encodings are one `openssl rand -base64 32` away from
// each other and an operator should not have to know which one this expects.
func decodeKey(encoded string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return key, nil
	}
	key, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("is not base64")
	}
	return key, nil
}
