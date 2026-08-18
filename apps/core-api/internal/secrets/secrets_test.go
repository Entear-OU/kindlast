package secrets_test

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
)

// Sealing a customer's third-party credential (ENT-231, §25).
//
// The property worth asserting is not that AES-GCM works, which it does. It is
// the binding: a credential is sealed against the connection's row id, so a
// ciphertext lifted out of one row cannot be opened in another. Without that,
// somebody with write access to the database could move a credential between
// organisations and the product would happily use it.

func key(t *testing.T, id string) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	return id + ":" + base64.StdEncoding.EncodeToString(raw)
}

func TestACredentialOpensForItsOwnConnectionAndNoOther(t *testing.T) {
	ring, err := secrets.NewKeyring(key(t, "2026-08"))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	const connection = "11111111-1111-1111-1111-111111111111"
	sealed, keyID, err := ring.Seal("sk_live_helpdesk", connection)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if keyID != "2026-08" {
		t.Errorf("keyID is %q, want the primary key's id", keyID)
	}

	opened, err := ring.Open(sealed, keyID, connection)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened != "sk_live_helpdesk" {
		t.Errorf("opened %q", opened)
	}

	// THE ASSERTION THIS FILE EXISTS FOR. The same ciphertext, the same key,
	// a different connection: it must not open.
	const other = "22222222-2222-2222-2222-222222222222"
	if _, err := ring.Open(sealed, keyID, other); !errors.Is(err, secrets.ErrCannotOpen) {
		t.Fatalf("a credential opened for a connection it was not sealed against: %v", err)
	}
}

// The ciphertext does not contain the plaintext, which is the assertion that
// would catch somebody replacing the seal with an encoding.
func TestTheStoredValueIsNotThePlaintext(t *testing.T) {
	ring, err := secrets.NewKeyring(key(t, "2026-08"))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	sealed, _, err := ring.Seal("sk_live_helpdesk", "conn")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed, []byte("sk_live_helpdesk")) {
		t.Fatal("the sealed value contains the credential")
	}
}

// Two seals of the same credential differ, because the nonce is fresh each
// time. Identical ciphertexts would let somebody with the database tell which
// organisations use the same credential without opening anything.
func TestSealingTwiceProducesDifferentCiphertext(t *testing.T) {
	ring, err := secrets.NewKeyring(key(t, "2026-08"))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	first, _, err := ring.Seal("same-credential", "conn")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, _, err := ring.Seal("same-credential", "conn")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two seals of one credential produced identical ciphertext")
	}
}

// A tampered ciphertext does not open, which is what GCM's authentication tag
// is for and what a plain CTR mode would not give.
func TestATamperedCiphertextDoesNotOpen(t *testing.T) {
	ring, err := secrets.NewKeyring(key(t, "2026-08"))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}

	sealed, keyID, err := ring.Seal("sk_live_helpdesk", "conn")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	sealed[len(sealed)-1] ^= 0x01

	if _, err := ring.Open(sealed, keyID, "conn"); !errors.Is(err, secrets.ErrCannotOpen) {
		t.Fatalf("a tampered ciphertext opened: %v", err)
	}
}

// Rotation works without downtime: a value sealed with the old key still opens
// after a new primary is added, and new values are sealed with the new one.
//
// This is the whole reason a row records which key sealed it. A scheme with
// one key and no id is one where rotation means re-sealing everything inside a
// maintenance window, which is why rotation does not happen.
func TestARetiredKeyStillOpensWhatItSealed(t *testing.T) {
	old := key(t, "2025-01")

	before, err := secrets.NewKeyring(old)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	sealed, keyID, err := before.Seal("sk_live_old", "conn")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if keyID != "2025-01" {
		t.Fatalf("keyID is %q", keyID)
	}

	after, err := secrets.NewKeyring(key(t, "2026-08"), old)
	if err != nil {
		t.Fatalf("NewKeyring after rotation: %v", err)
	}

	opened, err := after.Open(sealed, keyID, "conn")
	if err != nil {
		t.Fatalf("a value sealed with the retired key did not open: %v", err)
	}
	if opened != "sk_live_old" {
		t.Errorf("opened %q", opened)
	}

	// And a new seal uses the new primary, so re-sealing moves rows forward.
	_, newKeyID, err := after.Seal("sk_live_new", "conn")
	if err != nil {
		t.Fatalf("Seal after rotation: %v", err)
	}
	if newKeyID != "2026-08" {
		t.Errorf("a new seal used %q, want the new primary", newKeyID)
	}
}

// A deployment with no key seals nothing, and says so with an error a caller
// can distinguish from a failure to open.
func TestWithNoKeyNothingIsSealed(t *testing.T) {
	ring, err := secrets.NewKeyring("")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if ring.Configured() {
		t.Fatal("an empty keyring reports itself configured")
	}
	if _, _, err := ring.Seal("secret", "conn"); !errors.Is(err, secrets.ErrNoKey) {
		t.Fatalf("got %v, want ErrNoKey", err)
	}
}

// Malformed keys are refused at construction, so a mistyped setting is a
// startup failure rather than a surprise the first time somebody connects.
func TestAMalformedKeyIsRefusedAtConstruction(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 16))

	for name, entry := range map[string]string{
		"no id":          base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"empty id":       ":" + base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"not base64":     "2026-08:not base64 at all!!",
		"AES-128 length": "2026-08:" + short,
	} {
		if _, err := secrets.NewKeyring(entry); err == nil {
			t.Errorf("%s: was accepted", name)
		}
	}
}

// Two entries claiming one id is refused, because a row's key id would then no
// longer identify which key sealed it, and identifying it is the only thing
// the id is for.
func TestTwoKeysCannotShareAnID(t *testing.T) {
	_, err := secrets.NewKeyring(key(t, "2026-08"), key(t, "2026-08"))
	if err == nil {
		t.Fatal("two keys with one id were accepted")
	}
	if !strings.Contains(err.Error(), "2026-08") {
		t.Errorf("the error does not name the id: %v", err)
	}
}
