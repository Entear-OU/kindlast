package notify

import (
	"strings"
	"testing"
)

// The acceptance criterion this file exists for: an unverified chat is refused
// at the dispatch path. It is a Go table test rather than something that needs
// a live database precisely so that breaking it is a red build.

func TestRouteForSendsToAVerifiedTelegramChat(t *testing.T) {
	t.Parallel()

	route := RouteFor(ChannelTelegram, "ada@example.test", "987654321", true)

	if route.Channel != ChannelTelegram {
		t.Errorf("Channel = %q, want %q", route.Channel, ChannelTelegram)
	}
	if route.To != "987654321" {
		t.Errorf("To = %q, want the chat id", route.To)
	}
	if route.Fallback != "" {
		t.Errorf("Fallback = %q, want none: this is the channel the person chose", route.Fallback)
	}
}

func TestRouteForRefusesAnUnverifiedChat(t *testing.T) {
	t.Parallel()

	route := RouteFor(ChannelTelegram, "ada@example.test", "987654321", false)

	if route.Channel == ChannelTelegram {
		t.Fatal("a notification was routed to a chat nobody proved they hold")
	}
	if route.To == "987654321" {
		t.Fatal("the chat id reached the route despite the chat being unverified")
	}
	if route.Channel != ChannelEmail || route.To != "ada@example.test" {
		t.Errorf("route = %+v, want the remaining channel", route)
	}
	if !strings.Contains(route.Fallback, "verified") {
		t.Errorf("Fallback = %q, want it to say the chat is not verified", route.Fallback)
	}
}

// The unlink criterion: future messages go to the remaining channel or
// nowhere, never to the unlinked chat. Unlinking deletes the row, so the
// dispatcher sees no chat id at all while the preference still says telegram.
func TestRouteForFallsBackWhenTheChatIsUnlinked(t *testing.T) {
	t.Parallel()

	route := RouteFor(ChannelTelegram, "ada@example.test", "", false)

	if route.Channel != ChannelEmail {
		t.Errorf("Channel = %q, want the remaining channel", route.Channel)
	}
	if route.To != "ada@example.test" {
		t.Errorf("To = %q, want the address", route.To)
	}
	if route.Fallback == "" {
		t.Error("the fallback happened silently; an operator asking why a person " +
			"stopped getting Telegram messages deserves the reason on the row")
	}
}

func TestRouteForGoesNowhereWhenThereIsNowhereToGo(t *testing.T) {
	t.Parallel()

	route := RouteFor(ChannelTelegram, "", "", false)

	if route.Channel != "" {
		t.Errorf("Channel = %q, want nothing: an unverified chat and no address is nowhere", route.Channel)
	}
	if route.Deliverable() {
		t.Error("Deliverable() is true with no channel and no address")
	}
	if route.Reason == "" {
		t.Error("no reason was recorded for a recipient who cannot be reached")
	}
}

func TestRouteForDefaultsToEmail(t *testing.T) {
	t.Parallel()

	for _, chosen := range []string{"", ChannelEmail, "carrier pigeon"} {
		route := RouteFor(chosen, "ada@example.test", "987654321", true)
		if route.Channel != ChannelEmail {
			t.Errorf("chosen %q: Channel = %q, want email", chosen, route.Channel)
		}
		if route.To != "ada@example.test" {
			t.Errorf("chosen %q: To = %q, want the address", chosen, route.To)
		}
	}
}

// A person who chose email and happens to have a verified chat is not
// second-guessed. The link exists so that they CAN choose Telegram, not so
// that the product chooses it for them.
func TestRouteForDoesNotPreferTelegramUnasked(t *testing.T) {
	t.Parallel()

	route := RouteFor(ChannelEmail, "ada@example.test", "987654321", true)
	if route.Channel != ChannelEmail {
		t.Errorf("Channel = %q, want the channel the person chose", route.Channel)
	}
}

func TestValidChannel(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{ChannelEmail, ChannelTelegram} {
		if !ValidChannel(ok) {
			t.Errorf("ValidChannel(%q) = false", ok)
		}
	}
	for _, bad := range []string{"", "sms", "Email", "telegram "} {
		if ValidChannel(bad) {
			t.Errorf("ValidChannel(%q) = true", bad)
		}
	}
}

func TestNormaliseRejectsAnUnknownChannel(t *testing.T) {
	t.Parallel()

	_, err := Preferences{FindingChannel: "carrier pigeon"}.Normalise()
	if err == nil {
		t.Fatal("an unknown channel was accepted; a settings page that silently " +
			"reads it as email is one nobody can debug")
	}
	if !strings.Contains(err.Error(), "finding_channel") {
		t.Errorf("err = %v, want it to name the field", err)
	}
}

func TestNormaliseDefaultsTheChannelToEmail(t *testing.T) {
	t.Parallel()

	got, err := Preferences{}.Normalise()
	if err != nil {
		t.Fatalf("normalising: %v", err)
	}
	if got.FindingChannel != ChannelEmail {
		t.Errorf("FindingChannel = %q, want email", got.FindingChannel)
	}
	if Defaults().FindingChannel != ChannelEmail {
		t.Errorf("Defaults().FindingChannel = %q, want email", Defaults().FindingChannel)
	}
}
