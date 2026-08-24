package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Router is how a second channel arrives without a second dispatch path
// (ENT-263).
//
// # WHAT IT IS FOR
//
// The dispatcher holds one delivery.Channel and has held one since ENT-219,
// and the package comment says why: the dispatcher must not know what SMTP is.
// A second provider could have been added by giving the dispatcher a second
// field and an `if`, and that `if` is the beginning of the second mechanism
// this whole seam exists to prevent. It multiplies: the doorbell path grows
// one, the transactional path grows another, and within two changes there are
// two answers to "which channel did this go out on".
//
// So the dispatcher still holds one Channel. It happens to be this one, which
// reads the channel the message already names and hands it on. Every retry,
// every failure record and every notion of "sent" stays exactly where it was.
//
// # WHY A CHANNEL IS REGISTERED RATHER THAN DISCOVERED FROM Name()
//
// Name() is the provider (`smtp`), not the channel (`email`), because SMTP is
// one way of serving mail and a hosted API would be another. Keying the map on
// Name() would tie the channel a person chose in their settings to whichever
// provider a deployment happens to use, and a deployment that switched
// providers would silently stop delivering to everybody who had chosen the
// old one.
type Router struct {
	channels map[string]Channel
}

// ErrChannelUnavailable is what a message addressed to a channel this
// deployment has not configured comes back as.
//
// A sentinel rather than a string, because the caller has to tell it from a
// provider that is merely down: the first is a `failed_precondition` naming
// the setting an operator has to fill in, and the second is an `unavailable`
// the retry policy rides out. Told apart by the string, they would be the same
// thing to the workflow, and a message for an unconfigured channel would retry
// with backoff forever.
var ErrChannelUnavailable = errors.New("delivery: this deployment has no such channel")

// NewRouter builds an empty router. Channels are registered onto it.
func NewRouter() *Router {
	return &Router{channels: map[string]Channel{}}
}

// Register attaches a provider to a channel, and does nothing when the
// provider is nil.
//
// The nil case is the ordinary one rather than a guard against a bug: both
// mailChannel and telegramChannel return nil on a deployment that has not
// configured them, and a deployment configuring neither is supported (the rows
// queue, nothing is lost, see mailChannel's comment). Accepting the nil here
// keeps that decision in one place instead of at every call site.
func (r *Router) Register(channel string, provider Channel) {
	if r == nil || provider == nil || channel == "" {
		return
	}
	r.channels[channel] = provider
}

// Has reports whether this deployment can deliver on a channel.
//
// Nil-safe, because a deployment with no channel at all has no router, and
// GetNotificationCapabilities asks this question before anything has checked
// whether one was built.
func (r *Router) Has(channel string) bool {
	if r == nil {
		return false
	}
	_, ok := r.channels[channel]
	return ok
}

// Empty reports whether nothing at all is configured, which is what the
// dispatcher checks in place of the nil channel it used to check for.
func (r *Router) Empty() bool { return r == nil || len(r.channels) == 0 }

// Configured lists the channels this deployment can deliver on, sorted so the
// answer is stable between calls.
func (r *Router) Configured() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.channels))
	for name := range r.channels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Name identifies the router in logs. A provider's own Name is what appears in
// the error it returns, so nothing is lost by this being the same every time.
func (r *Router) Name() string { return "router" }

// Send hands the message to the channel it names.
func (r *Router) Send(ctx context.Context, msg Message) error {
	name := msg.Channel
	if name == "" {
		// Every message written before ENT-263 has no channel and is email.
		// Defaulting rather than refusing, because the alternative is that a
		// row already sitting in the outbox at upgrade time becomes
		// undeliverable, which is the one thing an outbox is supposed to make
		// impossible.
		name = ChannelEmail
	}
	channel, ok := r.channels[name]
	if !ok {
		return fmt.Errorf("%w: %q is not configured", ErrChannelUnavailable, name)
	}
	return channel.Send(ctx, msg)
}
