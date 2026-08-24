package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recordingChannel struct {
	name string
	got  []Message
	err  error
}

func (c *recordingChannel) Name() string { return c.name }

func (c *recordingChannel) Send(_ context.Context, msg Message) error {
	c.got = append(c.got, msg)
	return c.err
}

func routerWith(email, telegram Channel) *Router {
	r := NewRouter()
	r.Register(ChannelEmail, email)
	r.Register(ChannelTelegram, telegram)
	return r
}

func TestRouterSendsToTheNamedChannel(t *testing.T) {
	t.Parallel()

	email := &recordingChannel{name: "smtp"}
	telegram := &recordingChannel{name: "telegram"}
	router := routerWith(email, telegram)

	if err := router.Send(context.Background(), Message{
		Channel: ChannelTelegram, To: "42", BodyText: "hello",
	}); err != nil {
		t.Fatalf("sending: %v", err)
	}

	if len(telegram.got) != 1 {
		t.Fatalf("the Telegram channel received %d messages, want 1", len(telegram.got))
	}
	if len(email.got) != 0 {
		t.Fatalf("the email channel received %d messages, want 0; "+
			"a message addressed to one channel must never reach another", len(email.got))
	}
}

// An empty channel is email, so every row written before there was a second
// channel is still deliverable after the upgrade.
func TestRouterTreatsAnUnnamedChannelAsEmail(t *testing.T) {
	t.Parallel()

	email := &recordingChannel{name: "smtp"}
	router := routerWith(email, nil)

	if err := router.Send(context.Background(), Message{To: "a@example.test"}); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if len(email.got) != 1 {
		t.Fatalf("the email channel received %d messages, want 1", len(email.got))
	}
}

func TestRouterRefusesAChannelThisDeploymentDoesNotHave(t *testing.T) {
	t.Parallel()

	router := routerWith(&recordingChannel{name: "smtp"}, nil)

	err := router.Send(context.Background(), Message{Channel: ChannelTelegram, To: "42"})
	if err == nil {
		t.Fatal("a message for an unconfigured channel reported success; " +
			"it must stay queued with a reason instead")
	}
	if !errors.Is(err, ErrChannelUnavailable) {
		t.Errorf("err = %v, want it to be ErrChannelUnavailable so the caller can tell "+
			"an unconfigured channel from a failing one", err)
	}
	if !strings.Contains(err.Error(), ChannelTelegram) {
		t.Errorf("err = %v, want it to name the channel an operator has to configure", err)
	}
}

// Register takes the nil a deployment that configured nothing produces, rather
// than every call site having to guard it.
func TestRouterIgnoresNilChannels(t *testing.T) {
	t.Parallel()

	router := routerWith(&recordingChannel{name: "smtp"}, nil)

	if err := router.Send(context.Background(), Message{Channel: ChannelEmail, To: "a@b.test"}); err != nil {
		t.Fatalf("sending: %v", err)
	}
	if router.Has(ChannelTelegram) {
		t.Error("Has reports a channel that was never configured")
	}
	if !router.Has(ChannelEmail) {
		t.Error("Has does not report a channel that was configured")
	}
	if router.Empty() {
		t.Error("Empty reports nothing configured when email is")
	}
	if got := router.Configured(); len(got) != 1 || got[0] != ChannelEmail {
		t.Errorf("Configured() = %v, want just email", got)
	}
}

func TestRouterIsNilSafe(t *testing.T) {
	t.Parallel()

	// A deployment with neither SMTP nor a bot token has no router at all, and
	// GetNotificationCapabilities asks it what is available before anything
	// checks whether it exists.
	var router *Router
	if router.Has(ChannelEmail) {
		t.Error("a nil router reports a channel")
	}
	if !router.Empty() {
		t.Error("a nil router does not report itself empty")
	}
	if got := router.Configured(); got != nil {
		t.Errorf("Configured() = %v on a nil router, want nil", got)
	}
}

// An empty router is what a deployment with no SMTP address and no bot token
// has, and the dispatcher refuses on it rather than dereferencing it.
func TestEmptyRouterRefusesEverything(t *testing.T) {
	t.Parallel()

	router := NewRouter()
	if !router.Empty() {
		t.Fatal("a router with nothing registered does not report itself empty")
	}
	if err := router.Send(context.Background(), Message{To: "a@b.test"}); !errors.Is(err, ErrChannelUnavailable) {
		t.Errorf("err = %v, want ErrChannelUnavailable", err)
	}
}
