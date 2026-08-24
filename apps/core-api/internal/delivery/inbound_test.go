package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The untrusted-input boundary for the Telegram channel, asserted rather than
// assumed (ENT-263).
//
// # THE PROPERTY
//
// Anything a person types into a chat is data and never instruction. This build
// holds that in the strongest form available, which is that it never reads one:
// the adapter sends, and there is no webhook, no long poll and no other Bot API
// method. A message somebody sends to the bot goes nowhere, so there is no path
// by which chat text could reach a prompt, a finding, a tool call or a row.
//
// # WHY THIS IS A TEST AND NOT A COMMENT
//
// Because the property is one edit away from being false, and the edit looks
// harmless. `getUpdates` is four lines next to the send that already works, and
// somebody adding "just to check the chat is reachable" would open an ingest
// path for attacker-controlled text without anything going red. A comment
// saying not to is the kind of control this repository does not count as one
// (the model may ask; only code refuses).
//
// So there are two halves here, and they fail differently on purpose. The first
// is behavioural: drive a real send and watch which endpoint it calls. The
// second is structural: read this package's own source and refuse any mention
// of a Bot API method that reads. The behavioural half cannot see a method
// nothing calls yet, and the structural half cannot see a method reached
// through a variable, so neither substitutes for the other.

// TestTheOnlyBotAPIMethodCalledIsSendMessage drives the adapter and records
// every path it asks for.
func TestTheOnlyBotAPIMethodCalledIsSendMessage(t *testing.T) {
	t.Parallel()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(server.Close)

	channel, err := NewTelegram(testToken, server.URL, server.Client())
	if err != nil {
		t.Fatalf("building the channel: %v", err)
	}
	if err := channel.Send(t.Context(), Message{
		Channel: ChannelTelegram, To: "987654321", Subject: "A finding", BodyText: "detail",
	}); err != nil {
		t.Fatalf("sending: %v", err)
	}

	if len(paths) != 1 {
		t.Fatalf("the adapter made %d calls, want exactly one: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "/sendMessage") {
		t.Errorf("the adapter called %q, want it to only ever send", paths[0])
	}
}

// botAPIReadMethods are the Bot API's ways of receiving what somebody typed.
//
// Named individually rather than matched by a pattern, because the list is what
// a reader needs to see: these are the four doors, and this package has none of
// them.
var botAPIReadMethods = []string{
	"getUpdates",
	"setWebhook",
	"getWebhookInfo",
	"deleteWebhook",
}

// TestTheTelegramAdapterHasNoInboundPath reads this package's source.
//
// A source-level assertion, which is unusual and is the point: the thing being
// asserted is the absence of code, and absence is not observable from the
// outside. A test that only drove the adapter would pass just as happily on the
// day somebody adds a poller behind a configuration flag nothing in the suite
// turns on.
func TestTheTelegramAdapterHasNoInboundPath(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		scanned++
		for _, method := range botAPIReadMethods {
			if strings.Contains(string(source), method) {
				t.Errorf("%s names %q. This package sends and never reads: anything typed "+
					"into a chat is data, never instruction, and the moment the product "+
					"ingests a chat message it owes an answer about where that text may "+
					"flow. The inbound half belongs with the Messenger (ENT-260), with "+
					"that answer written down.", name, method)
			}
		}
	}

	// A test that walks a list proves the members, not the list. If the glob
	// ever matched nothing, every assertion above would pass over no files.
	if scanned == 0 {
		t.Fatal("no source files were scanned, so this test proved nothing")
	}
}

// TestASubjectAndBodyAreSentAsPlainText is the second half of the same
// boundary, in the outbound direction.
//
// A finding title and an organisation name are text a customer chose, and this
// process hands them to Telegram to display. Asking for Markdown or HTML
// rendering would make that text into markup: at best a stray underscore
// mangles the message, at worst a crafted title builds a link whose visible
// text and destination differ, inside a message the reader trusts because the
// product sent it.
func TestASubjectAndBodyAreSentAsPlainText(t *testing.T) {
	t.Parallel()

	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(server.Close)

	channel, err := NewTelegram(testToken, server.URL, server.Client())
	if err != nil {
		t.Fatalf("building the channel: %v", err)
	}

	// A title somebody could choose, carrying the two constructs that would be
	// read as markup if anything asked for markup.
	const hostile = "[Click here](https://evil.example) _and_ *this*"
	if err := channel.Send(t.Context(), Message{
		Channel: ChannelTelegram, To: "1", Subject: hostile, BodyText: "detail",
	}); err != nil {
		t.Fatalf("sending: %v", err)
	}

	if _, ok := payload["parse_mode"]; ok {
		t.Error("parse_mode was sent. Customer-chosen text must never be handed to a " +
			"markup parser: it is the difference between a mangled message and a " +
			"link whose visible text and destination differ.")
	}
	if text, _ := payload["text"].(string); !strings.Contains(text, hostile) {
		t.Errorf("text = %q, want the title verbatim rather than escaped or stripped: "+
			"plain text has no reading that needs either", text)
	}
	if preview, _ := payload["disable_web_page_preview"].(bool); !preview {
		t.Error("link previews were left on. Telegram fetches the first link in a " +
			"message to build a preview card, so a notification carrying a §8 " +
			"capability link would be opened by Telegram's servers rather than " +
			"only by the person it went to.")
	}
}

// A compile-time reminder of what the Channel seam is. If somebody widens the
// interface with a Receive, this stops compiling and they have to come here and
// read why that is the wrong shape.
var _ interface {
	Name() string
	Send(context.Context, Message) error
} = (*Telegram)(nil)
