package notifications

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The linking handlers, over a fake store (ENT-263).
//
// What these prove is the half that is this package's: that the bot token is
// never reachable from an RPC, that a code is never returned by the call that
// mints it, that the code rides the outbox rather than a second path, and that
// a deployment with no bot refuses a link before it writes anything. What they
// deliberately do not prove is tenancy, which is the policy's job and is
// asserted by `bun run test:db` against a live database.

// fakeStore is a tenant transaction that records rather than writes.
type fakeStore struct {
	prefs    notify.Preferences
	linked   []postgres.LinkedChannel
	enqueued []notify.Message

	// linkedChatID, linkedCodeHash and linkedExpiry are what LinkTelegramChat
	// was handed.
	linkedChatID   string
	linkedCodeHash string
	linkedExpiry   time.Time

	linkErr   error
	verifyErr error
	unlinked  bool
}

func (f *fakeStore) Preferences(context.Context) (notify.Preferences, error) {
	return f.prefs, nil
}

func (f *fakeStore) SavePreferences(_ context.Context, p notify.Preferences) error {
	f.prefs = p
	return nil
}

func (f *fakeStore) LinkedChannels(context.Context) ([]postgres.LinkedChannel, error) {
	return f.linked, nil
}

func (f *fakeStore) LinkTelegramChat(_ context.Context, chatID, codeHash string, expiresAt time.Time) error {
	if f.linkErr != nil {
		return f.linkErr
	}
	f.linkedChatID, f.linkedCodeHash, f.linkedExpiry = chatID, codeHash, expiresAt
	return nil
}

func (f *fakeStore) VerifyTelegramChat(context.Context, string) error { return f.verifyErr }

func (f *fakeStore) UnlinkTelegramChat(context.Context) (bool, error) {
	return f.unlinked, nil
}

func (f *fakeStore) EnqueueMessage(_ context.Context, msg notify.Message) error {
	f.enqueued = append(f.enqueued, msg)
	return nil
}

func (f *fakeStore) OrganisationName(context.Context) (string, error) { return "Acme GmbH", nil }

// The rest of interceptor.Tenant, which these handlers never call. Tenancy is
// the policy's, and §13.3 is why none of it is faked into meaning anything: a
// mocked membership check would assert nothing about the thing that actually
// enforces isolation.
func (f *fakeStore) OrgID() string                  { return "11111111-1111-1111-1111-111111111111" }
func (f *fakeStore) Role() string                   { return "owner" }
func (f *fakeStore) UserID() string                 { return "22222222-2222-2222-2222-222222222222" }
func (f *fakeStore) Commit(context.Context) error   { return nil }
func (f *fakeStore) Rollback(context.Context) error { return nil }

// contextFor builds what the interceptors would have put on the request.
func contextFor(t *testing.T, store *fakeStore) context.Context {
	t.Helper()
	ctx := interceptor.WithClaims(t.Context(), &oidc.Claims{Subject: "ada"})
	return interceptor.WithTenant(ctx, store)
}

func serviceWith(channels ...string) *Service {
	router := delivery.NewRouter()
	for _, name := range channels {
		router.Register(name, silentChannel{})
	}
	return New(router)
}

type silentChannel struct{}

func (silentChannel) Name() string                                 { return "test" }
func (silentChannel) Send(context.Context, delivery.Message) error { return nil }

// The acceptance criterion that the bot token is the dispatcher's alone. There
// is no field on any response in this service that could carry it, and the
// handler that most plausibly would (the one that mints a code and asks for it
// to be sent) is asked here to prove it does not.
func TestLinkingReturnsNeitherTheTokenNorTheCode(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	res, err := serviceWith(notify.ChannelTelegram).LinkTelegramChat(
		contextFor(t, store),
		connect.NewRequest(&corev1.LinkTelegramChatRequest{ChatId: "987654321"}))
	if err != nil {
		t.Fatalf("linking: %v", err)
	}

	if len(store.enqueued) != 1 {
		t.Fatalf("%d messages were queued, want exactly one", len(store.enqueued))
	}
	code := codeIn(t, store.enqueued[0].BodyText)

	// The response carries when the code expires and nothing else. A code
	// returned by the call that created it would prove nothing about who holds
	// the chat, which is the only thing it exists to prove.
	rendered := res.Msg.String()
	if strings.Contains(rendered, code) {
		t.Errorf("the response carries the verification code: %s", rendered)
	}
	if res.Msg.GetCodeExpiresAt() == nil {
		t.Error("no expiry was returned, so a console cannot say how long the code is good for")
	}

	// Hashed on the way to the store, in the clear only on the way to the chat.
	if store.linkedCodeHash == code {
		t.Error("the code was stored in the clear; a database dump must not yield a working code")
	}
	if store.linkedCodeHash != notify.HashVerificationCode(code) {
		t.Error("the hash the store was handed is not the hash of the code that was sent")
	}
}

// The code goes through the outbox, which is the whole of ENT-263's constraint
// in one assertion: no second delivery path, no second retry policy, no second
// answer to whether a message went out.
func TestTheVerificationCodeRidesTheOutbox(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	if _, err := serviceWith(notify.ChannelTelegram).LinkTelegramChat(
		contextFor(t, store),
		connect.NewRequest(&corev1.LinkTelegramChatRequest{ChatId: "987654321"})); err != nil {
		t.Fatalf("linking: %v", err)
	}

	msg := store.enqueued[0]
	if msg.Kind != notify.KindTelegramVerification {
		t.Errorf("Kind = %q, want %q", msg.Kind, notify.KindTelegramVerification)
	}
	if msg.Channel != notify.ChannelTelegram {
		t.Errorf("Channel = %q, want %q", msg.Channel, notify.ChannelTelegram)
	}
	if msg.RecipientChatID != "987654321" {
		t.Errorf("RecipientChatID = %q, want the claimed chat", msg.RecipientChatID)
	}
	if msg.RecipientEmail != "" {
		t.Errorf("RecipientEmail = %q, want none on a chat message", msg.RecipientEmail)
	}
	if !strings.Contains(msg.BodyText, "Acme GmbH") {
		t.Error("the message does not name the organisation; somebody who did not ask " +
			"for this cannot tell what is being linked")
	}
}

// A deployment with no bot token refuses before it writes anything. This is the
// Go half of what `bun run test:airgap` asserts from the outside: with no token
// there is no adapter, so nothing here constructs a message for one.
func TestLinkingIsRefusedWithNoBotConfigured(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	_, err := serviceWith(notify.ChannelEmail).LinkTelegramChat(
		contextFor(t, store),
		connect.NewRequest(&corev1.LinkTelegramChatRequest{ChatId: "987654321"}))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v, want failed_precondition", connect.CodeOf(err))
	}
	if store.linkedChatID != "" {
		t.Error("a claim was recorded on a deployment that cannot deliver a code to it")
	}
	if len(store.enqueued) != 0 {
		t.Error("a message was queued for a channel this deployment has no adapter for")
	}
	if !strings.Contains(err.Error(), "KINDLAST_TELEGRAM_BOT_TOKEN") {
		t.Errorf("err = %v, want it to name the setting an operator has to fill in", err)
	}
}

func TestAChatIDThatIsNotAChatIDIsRefused(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "  ", "@kindlast_findings", "12a34", "https://t.me/x", "-"} {
		store := &fakeStore{}
		_, err := serviceWith(notify.ChannelTelegram).LinkTelegramChat(
			contextFor(t, store),
			connect.NewRequest(&corev1.LinkTelegramChatRequest{ChatId: bad}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("chat_id %q: got %v, want invalid_argument", bad, connect.CodeOf(err))
		}
		if store.linkedChatID != "" {
			t.Errorf("chat_id %q was recorded", bad)
		}
	}
}

// Wrong, expired, spent and never issued are one answer; running out of
// attempts is the exception. Told apart, the first four make this an oracle for
// which chats have a code outstanding.
func TestVerifyingMapsTheStoresRefusals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want connect.Code
	}{
		{"no pending code", postgres.ErrNoPendingChannel, connect.CodePermissionDenied},
		{"budget spent", postgres.ErrTooManyVerificationAttempts, connect.CodeResourceExhausted},
		{"the database failed", errors.New("connection reset"), connect.CodeInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{verifyErr: c.err}
			_, err := serviceWith(notify.ChannelTelegram).VerifyTelegramChat(
				contextFor(t, store),
				connect.NewRequest(&corev1.VerifyTelegramChatRequest{Code: "424242"}))
			if connect.CodeOf(err) != c.want {
				t.Fatalf("got %v, want %v", connect.CodeOf(err), c.want)
			}
		})
	}
}

// Unlinking is allowed on a deployment whose operator has just removed the bot
// token. Somebody who linked a chat must always be able to undo it.
func TestUnlinkingDoesNotNeedTheChannelToBeConfigured(t *testing.T) {
	t.Parallel()

	store := &fakeStore{unlinked: true}
	res, err := serviceWith().UnlinkTelegramChat(
		contextFor(t, store),
		connect.NewRequest(&corev1.UnlinkTelegramChatRequest{}))
	if err != nil {
		t.Fatalf("unlinking: %v", err)
	}
	if !res.Msg.GetUnlinked() {
		t.Error("Unlinked = false, want true")
	}
}

// Capabilities read the same router the dispatcher sends through, so a settings
// page cannot offer a channel the deployment has no adapter for.
func TestCapabilitiesReadTheRouter(t *testing.T) {
	t.Parallel()

	ctx := contextFor(t, &fakeStore{})

	both, err := serviceWith(notify.ChannelEmail, notify.ChannelTelegram).
		GetNotificationCapabilities(ctx, connect.NewRequest(&corev1.GetNotificationCapabilitiesRequest{}))
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	for _, c := range both.Msg.GetChannels() {
		if !c.GetAvailable() {
			t.Errorf("%s is reported unavailable on a deployment that has it", c.GetId())
		}
	}

	neither, err := serviceWith().
		GetNotificationCapabilities(ctx, connect.NewRequest(&corev1.GetNotificationCapabilitiesRequest{}))
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	if len(neither.Msg.GetChannels()) != 2 {
		t.Fatalf("%d channels, want both reported with a reason rather than omitted",
			len(neither.Msg.GetChannels()))
	}
	for _, c := range neither.Msg.GetChannels() {
		if c.GetAvailable() {
			t.Errorf("%s is reported available on a deployment with no adapter for it", c.GetId())
		}
		if c.GetUnavailableReason() == "" {
			t.Errorf("%s is unavailable with no reason, so a console can only lack it silently",
				c.GetId())
		}
	}
}

// The channel names are written twice: once in the domain, which must not
// import the wire, and once in `internal/delivery`, which is the wire. This is
// the test the domain's comment promises, and the reason those two definitions
// cannot drift.
func TestChannelNamesAgreeWithTheDeliverySeam(t *testing.T) {
	t.Parallel()

	if notify.ChannelEmail != delivery.ChannelEmail {
		t.Errorf("notify.ChannelEmail = %q, delivery.ChannelEmail = %q",
			notify.ChannelEmail, delivery.ChannelEmail)
	}
	if notify.ChannelTelegram != delivery.ChannelTelegram {
		t.Errorf("notify.ChannelTelegram = %q, delivery.ChannelTelegram = %q",
			notify.ChannelTelegram, delivery.ChannelTelegram)
	}
}

// codeIn recovers the six digits out of a rendered verification message, so the
// assertions above can be about the code that was actually sent rather than one
// the test chose.
func codeIn(t *testing.T, body string) string {
	t.Helper()
	for _, field := range strings.Fields(body) {
		trimmed := strings.Trim(field, ".,")
		if len(trimmed) != notify.VerificationCodeDigits {
			continue
		}
		if strings.Trim(trimmed, "0123456789") == "" {
			return trimmed
		}
	}
	t.Fatalf("no verification code was found in the message: %q", body)
	return ""
}
