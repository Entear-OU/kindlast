package notify

import (
	"strings"
	"testing"
)

func TestNewVerificationCodeIsSixDigits(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewVerificationCode()
		if err != nil {
			t.Fatalf("generating: %v", err)
		}
		if len(code) != VerificationCodeDigits {
			t.Fatalf("code %q has %d digits, want %d", code, len(code), VerificationCodeDigits)
		}
		if strings.Trim(code, "0123456789") != "" {
			t.Fatalf("code %q is not all digits; a person types this into a console", code)
		}
		seen[code] = true
	}
	// Not a randomness test, a wiring test: a generator returning a constant
	// (an unseeded source, a swallowed error) would make every code the same
	// and every link verifiable by anybody who had ever seen one.
	if len(seen) < 100 {
		t.Errorf("200 codes produced only %d distinct values", len(seen))
	}
}

func TestHashVerificationCodeIsStableAndNotTheCode(t *testing.T) {
	t.Parallel()

	const code = "123456"
	first := HashVerificationCode(code)
	if first != HashVerificationCode(code) {
		t.Error("the same code hashed to two different values")
	}
	if strings.Contains(first, code) {
		t.Error("the hash contains the code")
	}
	if first == HashVerificationCode("123457") {
		t.Error("two different codes hashed the same")
	}
}

// A code is compared in constant time, because the comparison happens against
// an attacker-supplied value on a path they can call repeatedly.
func TestVerificationCodeMatches(t *testing.T) {
	t.Parallel()

	hash := HashVerificationCode("424242")
	if !VerificationCodeMatches("424242", hash) {
		t.Error("the right code did not match")
	}
	if VerificationCodeMatches("424243", hash) {
		t.Error("a wrong code matched")
	}
	if VerificationCodeMatches("", hash) {
		t.Error("an empty code matched")
	}
	if VerificationCodeMatches("424242", "") {
		t.Error("a code matched an empty hash; a row with no pending code must verify nothing")
	}
}

func TestTelegramVerificationMessage(t *testing.T) {
	t.Parallel()

	msg := TelegramVerification("424242", "Acme GmbH", "987654321")

	if msg.Kind != KindTelegramVerification {
		t.Errorf("Kind = %q, want %q", msg.Kind, KindTelegramVerification)
	}
	// The kind, the channel and the recipient field are set together or the
	// row is one the check constraint refuses at enqueue. Asserted here
	// because the alternative is finding out on a live stack.
	if msg.Channel != ChannelTelegram {
		t.Errorf("Channel = %q, want %q", msg.Channel, ChannelTelegram)
	}
	if msg.RecipientChatID != "987654321" {
		t.Errorf("RecipientChatID = %q, want the chat id", msg.RecipientChatID)
	}
	if msg.RecipientEmail != "" {
		t.Errorf("RecipientEmail = %q, want none: a chat message names no address",
			msg.RecipientEmail)
	}
	if !strings.Contains(msg.BodyText, "424242") {
		t.Errorf("body = %q, want it to carry the code", msg.BodyText)
	}
	if !strings.Contains(msg.BodyText, "Acme GmbH") {
		t.Errorf("body = %q, want it to name the organisation so somebody who did "+
			"not ask for this knows what is being linked", msg.BodyText)
	}
	// A code that arrives with no way to tell it apart from a phishing message
	// is one people either ignore or act on wrongly.
	if !strings.Contains(strings.ToLower(msg.BodyText), "did not") {
		t.Errorf("body = %q, want it to say what to do if this was not expected", msg.BodyText)
	}
	// No link. A verification message that carried one would be teaching
	// people to click links that arrive in a chat with a code, which is the
	// exact shape of the attack the code exists to prevent.
	if strings.Contains(msg.BodyText, "http") {
		t.Errorf("body = %q, want no link in a verification message", msg.BodyText)
	}
}
