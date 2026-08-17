package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Signature verification (ENT-210).
//
// This is the entire authentication of an unauthenticated endpoint that changes
// what a customer is entitled to. Anybody on the internet can POST to it, so
// every case below is somebody trying.

const secret = "whsec_test_secret"

func sign(t *testing.T, body []byte, at time.Time, using string) string {
	t.Helper()
	stamp := fmt.Sprintf("%d", at.Unix())
	mac := hmac.New(sha256.New, []byte(using))
	mac.Write([]byte(stamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return fmt.Sprintf("t=%s,v1=%s", stamp, hex.EncodeToString(mac.Sum(nil)))
}

func TestAValidSignatureIsAccepted(t *testing.T) {
	now := time.Now()
	body := []byte(`{"id":"evt_1"}`)

	if err := VerifySignature(sign(t, body, now, secret), body, secret, now); err != nil {
		t.Fatalf("a valid signature was refused: %v", err)
	}
}

func TestAForgedSignatureIsRefused(t *testing.T) {
	now := time.Now()
	body := []byte(`{"id":"evt_1"}`)

	// Signed with a different secret, which is what an attacker who has the
	// payload format but not the secret can produce.
	header := sign(t, body, now, "whsec_not_the_secret")

	if err := VerifySignature(header, body, secret, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a forged signature returned %v, want ErrBadSignature", err)
	}
}

// The property that makes signing worth doing at all.
func TestATamperedBodyIsRefused(t *testing.T) {
	now := time.Now()
	original := []byte(`{"id":"evt_1","plan":"free"}`)
	header := sign(t, original, now, secret)

	// The signature is real. The body is not the one that was signed, which is
	// the shape of an attacker replaying a captured delivery with the plan
	// changed.
	tampered := []byte(`{"id":"evt_1","plan":"pro"}`)

	if err := VerifySignature(header, tampered, secret, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a tampered body returned %v, want ErrBadSignature", err)
	}
}

func TestAMissingOrMalformedHeaderIsRefused(t *testing.T) {
	now := time.Now()
	body := []byte(`{}`)

	for _, header := range []string{
		"",
		"   ",
		"nonsense",
		"v1=abcdef",           // no timestamp
		"t=1234567890",        // no signature
		"t=notanumber,v1=abc", // unparseable timestamp
	} {
		if err := VerifySignature(header, body, secret, now); !errors.Is(err, ErrNoSignature) {
			t.Errorf("header %q returned %v, want ErrNoSignature", header, err)
		}
	}
}

func TestAnEmptySecretRefusesEverything(t *testing.T) {
	// A deployment that has not configured a signing secret must not accept
	// anything, and must especially not accept a request that also carries no
	// signature, which would otherwise be two empty strings comparing equal.
	now := time.Now()
	body := []byte(`{}`)

	if err := VerifySignature(sign(t, body, now, secret), body, "", now); !errors.Is(err, ErrNoSignature) {
		t.Fatalf("an unconfigured secret accepted a signature: %v", err)
	}
}

// The tolerance window bounds replay of a body that was never applied. The
// dedup ledger cannot help there, because it only knows about events it has
// seen.
func TestASignatureOutsideTheToleranceWindowIsRefused(t *testing.T) {
	now := time.Now()
	body := []byte(`{}`)

	old := sign(t, body, now.Add(-SignatureTolerance-time.Minute), secret)
	if err := VerifySignature(old, body, secret, now); !errors.Is(err, ErrSignatureExpired) {
		t.Errorf("a stale signature returned %v, want ErrSignatureExpired", err)
	}

	// Checked in both directions. A timestamp far in the future is as much a
	// forgery signal as one far in the past, and refusing only the past leaves
	// an attacker free to mint something that stays valid.
	future := sign(t, body, now.Add(SignatureTolerance+time.Minute), secret)
	if err := VerifySignature(future, body, secret, now); !errors.Is(err, ErrSignatureExpired) {
		t.Errorf("a future-dated signature returned %v, want ErrSignatureExpired", err)
	}

	// And the edges are inside.
	edge := sign(t, body, now.Add(-SignatureTolerance+time.Second), secret)
	if err := VerifySignature(edge, body, secret, now); err != nil {
		t.Errorf("a signature just inside the window was refused: %v", err)
	}
}

// A rotation should not be an outage: the provider sends both signatures for a
// period, and any one matching is enough.
func TestAnyOfSeveralSignaturesMatching(t *testing.T) {
	now := time.Now()
	body := []byte(`{}`)

	valid := sign(t, body, now, secret)
	stamp, good, _ := strings.Cut(valid, ",")

	// `t=...,v1=<old secret's signature>,v1=<current one>`. The stale value is
	// first, so this also proves the check does not stop at the first candidate.
	header := fmt.Sprintf("%s,v1=%s,%s", stamp, strings.Repeat("00", 32), good)
	if err := VerifySignature(header, body, secret, now); err != nil {
		t.Fatalf("a header carrying an old and a current signature was refused: %v", err)
	}

	// And a header carrying only stale candidates is still refused, so the
	// rotation tolerance is not "any header with several values passes".
	onlyStale := fmt.Sprintf("%s,v1=%s,v1=%s", stamp,
		strings.Repeat("00", 32), strings.Repeat("11", 32))
	if err := VerifySignature(onlyStale, body, secret, now); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("a header of only stale signatures returned %v, want ErrBadSignature", err)
	}
}

func TestValidateRefusesWhatThisSystemCannotAct(t *testing.T) {
	base := Event{ID: "evt_1", CustomerID: "cus_1", Plan: "pro", Status: "active"}
	if err := base.Validate(); err != nil {
		t.Fatalf("a valid event was refused: %v", err)
	}

	for _, tc := range []struct {
		name  string
		event Event
	}{
		{"no id, so it cannot be deduplicated", Event{CustomerID: "cus_1", Plan: "pro", Status: "active"}},
		{"no customer", Event{ID: "e", Plan: "pro", Status: "active"}},
		{"a plan this system does not sell", Event{ID: "e", CustomerID: "c", Plan: "enterprise", Status: "active"}},
		{"a status the column would refuse", Event{ID: "e", CustomerID: "c", Plan: "pro", Status: "trialing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.event.Validate(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
