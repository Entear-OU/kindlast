package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The bot token is the property these tests exist for. Everything else here is
// ordinary adapter behaviour; the token is the thing that must not leak, and
// the place it leaks from is the URL, because Telegram puts it in the path
// rather than in a header.

const testToken = "1234567:AAHtestTokenValueThatMustNeverAppearAnywhere"

func TestNewTelegramRefusesAnEmptyToken(t *testing.T) {
	t.Parallel()

	if _, err := NewTelegram("", "", nil); err == nil {
		t.Fatal("a Telegram channel with no bot token was built; " +
			"an unconfigured channel must be absent rather than present and broken")
	}
}

func TestTelegramSendsTheChatIDAndTheText(t *testing.T) {
	t.Parallel()

	var gotPath, gotChat, gotText string
	var gotParseMode any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("the request body is not JSON: %v", err)
		}
		gotChat, _ = payload["chat_id"].(string)
		gotText, _ = payload["text"].(string)
		gotParseMode = payload["parse_mode"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	channel, err := NewTelegram(testToken, server.URL, server.Client())
	if err != nil {
		t.Fatalf("building the channel: %v", err)
	}

	err = channel.Send(context.Background(), Message{
		Channel:  ChannelTelegram,
		To:       "987654321",
		Subject:  "A critical finding needs your attention",
		BodyText: "Open the finding: https://example.test/o/acme/feed/abc",
	})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	if want := "/bot" + testToken + "/sendMessage"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotChat != "987654321" {
		t.Errorf("chat_id = %q, want the recipient's chat id", gotChat)
	}
	// The subject is not a Telegram concept, so it leads the body rather than
	// being dropped: a notification whose first line is missing reads as a
	// truncated message.
	if !strings.Contains(gotText, "A critical finding needs your attention") {
		t.Errorf("text = %q, want it to carry the subject", gotText)
	}
	if !strings.Contains(gotText, "https://example.test/o/acme/feed/abc") {
		t.Errorf("text = %q, want it to carry the body", gotText)
	}
	// Not decoration. With a parse_mode set, Telegram interprets the text, and
	// the text carries an organisation name and a finding title that a customer
	// chose. Plain text is the whole mitigation.
	if gotParseMode != nil {
		t.Errorf("parse_mode = %v, want it absent so customer text is never interpreted", gotParseMode)
	}
}

func TestTelegramRefusesAMessageWithNoChat(t *testing.T) {
	t.Parallel()

	channel, err := NewTelegram(testToken, "https://api.example.test", http.DefaultClient)
	if err != nil {
		t.Fatalf("building the channel: %v", err)
	}
	if err := channel.Send(context.Background(), Message{Channel: ChannelTelegram}); err == nil {
		t.Fatal("a message with no chat id was accepted")
	}
}

// The three tests below are one property in three shapes: whatever goes wrong,
// the error a caller sees, logs, or writes into `last_error` must not contain
// the bot token. `last_error` is readable by anybody who can read the outbox
// row, so a token in it is a token in the domain schema.

func TestTelegramErrorsNeverCarryTheToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "an API error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
			},
		},
		{
			name: "an ok:false answer with a 200",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"chat not found"}`)
			},
		},
		{
			name: "an answer that is not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `<html>502 Bad Gateway</html>`)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			channel, err := NewTelegram(testToken, server.URL, server.Client())
			if err != nil {
				t.Fatalf("building the channel: %v", err)
			}
			err = channel.Send(context.Background(), Message{
				Channel: ChannelTelegram, To: "1", BodyText: "hello",
			})
			if err == nil {
				t.Fatal("the send reported success")
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("the error carries the bot token: %v", err)
			}
		})
	}
}

// A transport failure is the fourth shape, and the one most likely to leak:
// net/http wraps the whole URL into a *url.Error, and the whole URL is the
// token.
func TestTelegramTransportErrorsNeverCarryTheToken(t *testing.T) {
	t.Parallel()

	// A server that is closed before the call, so the dial fails.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	base := server.URL
	server.Close()

	channel, err := NewTelegram(testToken, base, client)
	if err != nil {
		t.Fatalf("building the channel: %v", err)
	}
	err = channel.Send(context.Background(), Message{
		Channel: ChannelTelegram, To: "1", BodyText: "hello",
	})
	if err == nil {
		t.Fatal("the send reported success against a closed server")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Errorf("the transport error carries the bot token: %v", err)
	}
	// And the wrapped chain, not only the top line, because a caller logging
	// with %+v or unwrapping gets the chain.
	var wrapped interface{ Unwrap() error }
	if errors.As(err, &wrapped) {
		if inner := wrapped.Unwrap(); inner != nil && strings.Contains(inner.Error(), testToken) {
			t.Errorf("an unwrapped error carries the bot token: %v", inner)
		}
	}
}

func TestTelegramNameIsStable(t *testing.T) {
	t.Parallel()

	channel, err := NewTelegram(testToken, "", http.DefaultClient)
	if err != nil {
		t.Fatalf("building the channel: %v", err)
	}
	if channel.Name() != ChannelTelegram {
		t.Errorf("Name() = %q, want %q", channel.Name(), ChannelTelegram)
	}
}
