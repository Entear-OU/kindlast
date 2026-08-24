package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Telegram delivers through the Bot API (ENT-263).
//
// # THE TOKEN, WHICH IS THE WHOLE OF THE SECURITY STORY HERE
//
// A bot token is an operator secret of the same class as an SMTP password, and
// it is worse to leak than most, because Telegram puts it in the URL PATH
// rather than in a header: `https://api.telegram.org/bot<token>/sendMessage`.
// So every ordinary thing a Go program does with a failed HTTP call leaks it.
// `net/http` wraps the request URL into the *url.Error it returns. A logger
// given the request leaks it. An error written into `transactional_outbox.
// last_error` puts it in the domain schema, where every backup keeps it and
// anybody who can read the row can read it.
//
// Hence the two rules this type follows without exception, and the tests in
// telegram_test.go that hold it to them:
//
//  1. The URL is built at the moment of the call and never stored anywhere a
//     caller can reach.
//  2. Every error returned from Send is constructed here from a status code,
//     a description Telegram sent, and nothing else. Transport errors are
//     scrubbed rather than wrapped, because the thing being wrapped is the
//     token.
//
// The token is also not in the database, not in `web`, and not in
// Intelligence. It is read from core-api's configuration by the dispatcher and
// by nothing else, and a deployment that has not set it does not offer the
// channel at all rather than offering one that fails.
//
// # WHY NOTHING IS EVER PARSED AS MARKDOWN
//
// The Bot API will render Markdown or HTML if asked, and this never asks. The
// text carries an organisation name and a finding title, both of which a
// customer chose and neither of which this process should hand to a parser: at
// best a stray underscore mangles a message, at worst a crafted title
// constructs a link whose visible text and destination differ, which is a
// phishing primitive inside a message the recipient trusts because the product
// sent it. Plain text has no such reading.
//
// # THERE IS NO INBOUND HALF
//
// This adapter sends. It registers no webhook and polls for nothing, so
// nothing a person types into a chat enters the product through it. That is
// deliberate: a message from a chat is data and never instruction (OWASP
// LLM01), and the moment the product reads one it owes an answer about where
// that text may flow. Linking a chat needs no inbound path, because the person
// supplies their own chat id and proves they hold it by reading a code the bot
// sent there.
type Telegram struct {
	token   string
	baseURL string
	client  *http.Client
}

// DefaultTelegramAPI is where the Bot API answers.
//
// A constant rather than a setting, with the override existing for tests
// alone. An operator who could repoint this could point a deployment's bot
// token at a host they control, which is a credential exfiltration primitive
// dressed as a configuration option.
const DefaultTelegramAPI = "https://api.telegram.org"

// NewTelegram builds the channel.
//
// Nothing is dialled here, for the reason NewSMTP does not dial either: a
// provider that is unreachable at boot is an ordinary condition for a queue
// whose job is outliving one.
func NewTelegram(token, baseURL string, client *http.Client) (*Telegram, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		// Refused rather than defaulted, so an unconfigured channel is absent
		// from the router rather than present and failing every send.
		return nil, errors.New("delivery: a Telegram bot token is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultTelegramAPI
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Telegram{
		token:   token,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}, nil
}

func (t *Telegram) Name() string { return ChannelTelegram }

// sendMessageRequest is the Bot API's payload, as JSON rather than as a form,
// because a body carrying newlines in a form encoding is one more place for
// something to be quoted wrongly.
type sendMessageRequest struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
	// No parse_mode field at all, rather than an empty one. See the type
	// comment: this is the mitigation, and a field that could be set is a
	// field somebody sets.

	// Telegram unfurls the first link in a message into a preview card, which
	// for a link into a private console means Telegram's own servers fetching
	// a URL that carries a capability token in its path (§8). Disabled, so the
	// only thing that opens the link is the person the message went to.
	DisableWebPagePreview bool `json:"disable_web_page_preview"`
}

// sendMessageResponse is the envelope every Bot API method answers with. It is
// `ok: false` with a 200 often enough that reading only the status code would
// record deliveries that did not happen.
type sendMessageResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

func (t *Telegram) Send(ctx context.Context, msg Message) error {
	chatID := strings.TrimSpace(msg.To)
	if chatID == "" {
		// Not retryable, and returned as an error anyway for the reason SMTP
		// does the same: the row stays pending with the reason recorded, which
		// is visible, rather than being marked sent, which would be a lie.
		return errors.New("delivery: the message has no chat id")
	}

	body, err := json.Marshal(sendMessageRequest{
		ChatID:                chatID,
		Text:                  t.render(msg),
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("delivery: building the Telegram request: %w", err)
	}

	// Built here and held in no field. See the type comment.
	endpoint := t.baseURL + "/bot" + t.token + "/sendMessage"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		// url.Parse failing on an endpoint containing the token, so this one
		// is scrubbed like a transport error rather than wrapped.
		return t.scrub(err, "building the Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := t.client.Do(req)
	if err != nil {
		return t.scrub(err, "calling the Telegram API")
	}
	defer func() { _ = res.Body.Close() }()

	// Bounded, because this is a response from a host that may not be Telegram
	// at all (a captive portal, a proxy's error page) and an unbounded read of
	// one is a memory footgun on a path that runs per recipient.
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil {
		return t.scrub(err, "reading the Telegram response")
	}

	var answer sendMessageResponse
	if err := json.Unmarshal(raw, &answer); err != nil {
		// Deliberately does not quote the body. A proxy's error page can echo
		// the request URL, and the request URL is the token.
		return fmt.Errorf("delivery: the Telegram API answered HTTP %d with a body that is not JSON",
			res.StatusCode)
	}
	if !answer.OK {
		return fmt.Errorf("delivery: the Telegram API refused the message: HTTP %d, %s",
			res.StatusCode, describeTelegramError(answer))
	}
	return nil
}

// scrub turns an error that may contain the request URL into one that cannot.
//
// The chain is dropped rather than wrapped with %w, which is the deliberate
// part: `errors.Is` on a transport error buys nothing here (the retry policy
// treats every non-refusal the same), and wrapping would keep the *url.Error
// reachable through Unwrap for any caller that walked it, which is exactly the
// leak this function exists to close.
func (t *Telegram) scrub(err error, what string) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		// url.Error.Error() prints the URL; the wrapped cause does not.
		return fmt.Errorf("delivery: %s: %s", what, redactToken(urlErr.Err.Error(), t.token))
	}
	return fmt.Errorf("delivery: %s: %s", what, redactToken(err.Error(), t.token))
}

// redactToken is the belt to scrub's braces.
//
// Not the primary control, and it is here because the primary control is a
// claim about every error type net/http and encoding/json can produce, and
// that claim will be wrong once. The cost of being wrong is a bot token in a
// customer's database, so a second, dumb, unconditional check earns its place.
func redactToken(message, token string) string {
	if token == "" {
		return message
	}
	return strings.ReplaceAll(message, token, "[redacted bot token]")
}

// describeTelegramError renders what the API said, or says that it said
// nothing.
func describeTelegramError(answer sendMessageResponse) string {
	description := strings.TrimSpace(answer.Description)
	if description == "" {
		description = "no description"
	}
	if answer.ErrorCode != 0 {
		return fmt.Sprintf("error %d: %s", answer.ErrorCode, description)
	}
	return description
}

// render turns a Message into the text of one chat message.
//
// The subject leads rather than being dropped. It is the sentence that says
// what happened ("A critical finding needs your attention"), the body is the
// detail, and a chat message that opens with the detail reads as a fragment of
// something the reader missed the start of.
func (t *Telegram) render(msg Message) string {
	subject := strings.TrimSpace(msg.Subject)
	body := strings.TrimSpace(msg.BodyText)
	switch {
	case subject == "":
		return body
	case body == "":
		return subject
	default:
		return subject + "\n\n" + body
	}
}
