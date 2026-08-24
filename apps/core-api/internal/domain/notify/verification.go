package notify

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Proving somebody holds the chat they claimed (ENT-263).
//
// # WHY A SHORT CODE AND NOT A LINK
//
// Every other bearer credential in this schema is 32 bytes of base64url in a
// URL, because every other one arrives in a mailbox and is clicked. This one
// arrives in a chat and is TYPED, into the console the person already has open,
// which changes the threat model in the direction that matters: the code never
// leaves the chat and the browser, so there is no redemption endpoint for
// somebody who intercepted it to hit, and no link for a person to be trained
// into clicking. A verification message carrying a link would be teaching
// people that a code in a chat comes with something to click, which is the
// shape of the attack this exists to prevent.
//
// The cost of six digits is that guessing is cheap, so three things pay for it
// and all three have to hold together: ten minutes of life, five attempts, and
// the code being stored hashed so the row cannot be read for it.

const (
	// VerificationCodeDigits is six, which is what a person will retype from a
	// phone without giving up.
	VerificationCodeDigits = 6

	// VerificationCodeLifetime is how long a code is good for.
	//
	// Ten minutes, and short deliberately. The person is looking at the
	// console and at their chat at the same time; there is no version of this
	// flow in which somebody comes back to it tomorrow, which is what makes a
	// short life free here and expensive for an invitation.
	VerificationCodeLifetime = 10 * time.Minute

	// MaxVerificationAttempts is what stops six digits being guessable.
	//
	// Five, counted on the row rather than in memory, so restarting the
	// process does not restore an attacker's budget. At five tries per code
	// and a ten minute life, guessing one code is a one in two hundred
	// thousand proposition per window.
	MaxVerificationAttempts = 5
)

// KindTelegramVerification names a `transactional_outbox` row carrying a code.
// It matches the check constraint 00044 widened, and adding a kind here
// without widening that constraint produces a row the database refuses.
const KindTelegramVerification = "telegram_verification"

// NewVerificationCode mints one code.
//
// crypto/rand rather than math/rand, and the error is returned rather than
// swallowed. A generator that silently fell back to a fixed value would make
// every code the same and every pending link verifiable by anybody who had
// ever seen one, and it would look exactly like working software.
func NewVerificationCode() (string, error) {
	limit := big.NewInt(1)
	for i := 0; i < VerificationCodeDigits; i++ {
		limit.Mul(limit, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("generating a verification code: %w", err)
	}
	// Zero-padded, so 42 is "000042" rather than a two digit code. Uniform
	// across the whole space either way; what this buys is that every code
	// looks the same length to the person typing it.
	return fmt.Sprintf("%0*d", VerificationCodeDigits, n), nil
}

// HashVerificationCode is what goes in the database.
//
// Plain SHA-256, matching HashInvitationToken and HashDelegationToken, and the
// reason it is not a password hash is worth stating rather than leaving to be
// questioned: the input is not a password. It is a uniformly random value from
// a space this process chose, used once, alive for ten minutes, and rate
// limited on the row. A slow KDF buys resistance to offline enumeration of a
// LOW-entropy secret, and the mitigation for a low-entropy secret here is the
// attempt ceiling, not the hash.
func HashVerificationCode(code string) string {
	digest := sha256.Sum256([]byte(code))
	return hex.EncodeToString(digest[:])
}

// VerificationCodeMatches compares a submitted code against a stored hash.
//
// Constant time, because the comparison is against a value an attacker
// supplies on a path they can call repeatedly. The hash makes a timing leak
// hard to exploit and does not make it absent, and subtle.ConstantTimeCompare
// is one line.
//
// An empty stored hash matches nothing. That is the state of a channel that is
// already verified (the check constraint forbids holding both), and a row in
// that state must not be re-verifiable by anybody who kept an old code.
func VerificationCodeMatches(code, storedHash string) bool {
	if code == "" || storedHash == "" {
		return false
	}
	got := HashVerificationCode(code)
	return subtle.ConstantTimeCompare([]byte(got), []byte(storedHash)) == 1
}

// TelegramVerification renders the message that carries a code to a chat.
//
// The copy has one job beyond delivering six digits: it has to be readable by
// somebody who did NOT ask for it. A chat id is a string one person types
// about a messenger account, so a code can land in the wrong chat through a
// typo or through somebody guessing at a colleague's id, and the person who
// receives it should be able to tell what is happening without knowing what
// Kindlast is.
//
// So it names the organisation, says what the code does, says the code is only
// useful to somebody already looking at the console, and carries no link at
// all. See the package comment for why the missing link is deliberate.
// The chat id is taken here rather than filled in by the caller so that the
// kind, the channel and the recipient field are written together in one place.
// Set apart, they are three lines a caller can get individually right and
// collectively wrong, and the wrong combination is a row the check constraint
// refuses at enqueue or, worse, one that names an address for a chat channel.
func TelegramVerification(code, orgName, chatID string) Message {
	org := strings.TrimSpace(orgName)
	if org == "" {
		org = "an organisation"
	}

	text := strings.Join([]string{
		fmt.Sprintf("Your Kindlast verification code is %s", code),
		"",
		fmt.Sprintf("Somebody is linking this chat to %s on Kindlast, a compliance", org),
		"workspace for GDPR and the EU AI Act, so that finding notifications",
		"arrive here instead of by email.",
		"",
		fmt.Sprintf("Type the code into the settings page to finish. It expires in %d minutes.",
			int(VerificationCodeLifetime.Minutes())),
		"",
		"If you did not start this, ignore this message. The code is no use to",
		"anybody who is not already signed in, and nothing is linked until it",
		"is typed in.",
	}, "\n")

	return Message{
		Kind:            KindTelegramVerification,
		Channel:         ChannelTelegram,
		RecipientChatID: chatID,
		Subject:         "Kindlast verification code",
		BodyText:        text,
	}
}
