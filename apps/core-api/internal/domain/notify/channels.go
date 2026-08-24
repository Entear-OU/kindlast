package notify

import "fmt"

// Which channel a person's doorbell rings on, decided here rather than in SQL
// (ENT-263).
//
// # WHY THIS IS A GO FUNCTION AND NOT A `where` CLAUSE
//
// `notification_recipients` could have returned the chat id only when
// `verified_at` is set, and that one line would have been the product rule "an
// unverified chat is not delivered to" written in plpgsql: exercisable only
// through a live stack, and silently disagreeable by a later reader. §14.5 puts
// a rule that decides in Go. So the function returns the chat and whether it
// was proved, separately, and this refuses.
//
// The test that matters, TestRouteForRefusesAnUnverifiedChat, is therefore a
// table test rather than a database fixture, which is what lets it be broken
// deliberately and watched go red.

// The channels a person can choose between. These strings are also a check
// constraint on `notification_preferences.finding_channel`, a proto field, and
// `delivery.ChannelEmail` and `delivery.ChannelTelegram` one layer down.
//
// Defined twice rather than imported, because this is the domain and
// `internal/delivery` is the wire: a domain package that imported it would pull
// net/smtp and net/http in behind it, and the direction of that dependency is
// the thing §21 is careful about. The two definitions are held together by
// TestChannelNamesAgreeWithTheDeliverySeam in the service that uses both, so
// they cannot drift without something going red.
const (
	ChannelEmail    = "email"
	ChannelTelegram = "telegram"
)

// ValidChannel reports whether a value names a channel this build knows.
func ValidChannel(value string) bool {
	return value == ChannelEmail || value == ChannelTelegram
}

// Route is where one person's doorbell goes, and why it is not going where
// they asked when it is not.
type Route struct {
	// Channel and To are the answer, and are empty together when there is
	// none.
	Channel string
	To      string

	// Fallback says why this is not the channel the person chose. Empty when
	// it is theirs.
	//
	// Recorded rather than silent, because the failure it describes is
	// invisible from the outside: somebody who linked a chat, never finished
	// verifying, and quietly kept receiving email would conclude the link
	// worked and the product was ignoring it.
	Fallback string

	// Reason is why there is nowhere at all to send. Set only when Channel is
	// empty, and ends up on the outbox row for an operator asking why nothing
	// went out.
	Reason string
}

// Deliverable reports whether this route names somewhere to send.
func (r Route) Deliverable() bool { return r.Channel != "" && r.To != "" }

// RouteFor picks the channel for one recipient of one finding notification.
//
// # THE THREE RULES, IN THE ORDER THEY MATTER
//
//  1. An unverified chat is not delivered to. Ever, and not as a warning: the
//     whole point of the code somebody typed into the console is that until
//     they type it, the chat is a string one person asserted about another
//     person's messenger account.
//  2. A chat that is not there is not delivered to. Unlinking deletes the row,
//     so the dispatcher sees no chat at all while the preference still says
//     Telegram, and that must go to the remaining channel or nowhere. Never to
//     the chat, which no longer exists here to be sent to.
//  3. Nobody is second-guessed upward. Somebody who chose email and happens to
//     have a verified chat gets email. The link exists so a person CAN choose
//     Telegram, not so the product chooses it for them, and a compliance
//     product that starts pushing to a phone because it noticed it could is
//     one people turn off.
//
// An unrecognised channel reads as email, deliberately, and the settings write
// is where an unknown value is refused (Normalise). By the time a row is being
// delivered the choice was accepted hours ago, and dropping a compliance
// notification over a string this build does not recognise is the wrong trade
// in the same way an unloadable timezone is.
func RouteFor(chosen, email, telegramChatID string, telegramVerified bool) Route {
	if chosen == ChannelTelegram {
		switch {
		case telegramChatID != "" && telegramVerified:
			return Route{Channel: ChannelTelegram, To: telegramChatID}
		case telegramChatID != "":
			return withFallback(email,
				"the Telegram chat is linked but not verified, so it is not delivered to")
		default:
			return withFallback(email,
				"no Telegram chat is linked, so there is nothing to deliver to")
		}
	}
	if email != "" {
		return Route{Channel: ChannelEmail, To: email}
	}
	return Route{Reason: "this person has no address and no verified chat"}
}

// withFallback is the remaining channel, or nowhere.
func withFallback(email, why string) Route {
	if email == "" {
		return Route{Reason: why + ", and there is no address to fall back to"}
	}
	return Route{Channel: ChannelEmail, To: email, Fallback: why}
}

// normaliseChannel is Normalise's half of the channel rule, kept here beside
// the constants it validates against.
func normaliseChannel(value string) (string, error) {
	if value == "" {
		return ChannelEmail, nil
	}
	if !ValidChannel(value) {
		return "", fmt.Errorf("finding_channel must be %s or %s", ChannelEmail, ChannelTelegram)
	}
	return value, nil
}

// MaxChatIDLength bounds a claimed chat id.
//
// Telegram's ids are 64-bit integers, so nineteen digits and a sign is the
// whole space and this is generous against it. The bound exists because the
// value is attacker-supplied, is stored, and is later read back out to a
// console: an unbounded string here is a row somebody can make arbitrarily
// large for free.
const MaxChatIDLength = 32

// ValidChatID reports whether a value could be a Telegram chat id.
//
// # WHY THIS IS NARROWER THAN THE BOT API ACCEPTS
//
// `sendMessage` also takes `@channelusername`, and this refuses one
// deliberately. A personal notification channel is a private chat with a
// numeric id, an `@` name addresses a public channel, and the difference is
// somebody's compliance findings being posted somewhere anybody can read. The
// narrower rule is the product decision; refusing here means it is made once,
// at the moment somebody claims a chat, rather than at every send.
//
// The second thing it buys is that nothing shaped like a URL, a path segment or
// a header ever reaches the value the adapter puts in a Bot API call. That is
// not the control (the adapter sends a JSON body, so there is nothing to inject
// into), but a claimed identifier that can only be digits is one fewer thing to
// reason about the day somebody changes the adapter.
func ValidChatID(raw string) bool {
	if raw == "" || len(raw) > MaxChatIDLength {
		return false
	}
	digits := raw
	if digits[0] == '-' {
		// A group or supergroup id is negative, and somebody linking a group
		// they own is a reasonable thing to want.
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for i := range len(digits) {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}
