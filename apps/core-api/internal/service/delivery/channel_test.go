package delivery

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// A row addressed to a channel this deployment has not configured (ENT-263).
//
// # WHY THIS IS ITS OWN CODE AND NOT `unavailable`
//
// The codes DeliverMessage returns are the contract with the worker's retry
// policy, so the difference between them is the difference between a message
// that drains when the mail server comes back and one that retries with growing
// intervals until somebody notices. A provider that is down is the first. A
// channel that is not configured is the second, and no amount of backing off
// makes it deliverable.
//
// It is reachable by exactly one route: an operator removing
// KINDLAST_TELEGRAM_BOT_TOKEN while rows addressed to Telegram are still
// queued. Linking a chat is refused when the channel is absent, so nothing can
// address a channel that never existed at all.
//
// FOUND BY DRIVING IT. `delivery.ErrChannelUnavailable` was declared with a
// comment promising exactly this distinction and nothing mapped it, so the
// first drive against the running stack returned `unavailable` for a row whose
// channel had been removed. This is the test that would have said so.

// channelStore is mintRecorder with DeliverMessage implemented: it calls the
// handler's own send function, which is what puts the router in the path, and
// reports the failure the way the real store does.
type channelStore struct {
	*mintRecorder
	row postgres.PendingMessage
}

func (c *channelStore) DeliverMessage(
	ctx context.Context, _ string, deliver postgres.Deliver,
) (postgres.Delivery, error) {
	if err := deliver(ctx, c.row); err != nil {
		// The same double wrap the store uses, which is what lets a caller
		// recover either sentinel.
		return postgres.Delivery{Attempts: 1},
			fmt.Errorf("%w: %w", postgres.ErrNotDelivered, err)
	}
	return postgres.Delivery{Sent: true, Attempts: 1, SentAt: time.Now()}, nil
}

func serviceRouting(row postgres.PendingMessage, channels *delivery.Router) *Service {
	return &Service{
		outbox:   &channelStore{mintRecorder: &mintRecorder{}, row: row},
		channels: channels,
		baseURL:  "http://localhost:3000",
		now:      time.Now,
	}
}

func deliverOne(t *testing.T, service *Service) error {
	t.Helper()
	ctx := interceptor.WithClaims(t.Context(), &oidc.Claims{Subject: "worker"})
	_, err := service.DeliverMessage(ctx, connect.NewRequest(&platformv1.DeliverMessageRequest{
		MessageId: "11111111-1111-1111-1111-111111111111",
	}))
	return err
}

func TestARowForAnUnconfiguredChannelIsFailedPreconditionAndNotUnavailable(t *testing.T) {
	t.Parallel()

	// Email is configured; the row names Telegram. A deployment that has just
	// had its bot token removed.
	channels := delivery.NewRouter()
	channels.Register(delivery.ChannelEmail, silentChannel{})

	err := deliverOne(t, serviceRouting(postgres.PendingMessage{
		ID:              "11111111-1111-1111-1111-111111111111",
		Kind:            "telegram_verification",
		Channel:         delivery.ChannelTelegram,
		RecipientChatID: "987654321",
		Subject:         "Kindlast verification code",
		BodyText:        "Your Kindlast verification code is 424242",
	}, channels))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v, want failed_precondition. `unavailable` would have the "+
			"worker retry with backoff forever over a channel no backoff can "+
			"configure.", connect.CodeOf(err))
	}
	if !errors.Is(err, delivery.ErrChannelUnavailable) {
		t.Errorf("err = %v, want the sentinel to survive the wrapping", err)
	}
}

// The neighbouring case, so the two are read together: a configured channel
// that refuses is still `unavailable`, which the retry policy rides out.
func TestARefusedSendIsStillUnavailable(t *testing.T) {
	t.Parallel()

	channels := delivery.NewRouter()
	channels.Register(delivery.ChannelEmail, refusingChannel{})

	err := deliverOne(t, serviceRouting(postgres.PendingMessage{
		ID:             "11111111-1111-1111-1111-111111111111",
		Kind:           "invitation",
		Channel:        delivery.ChannelEmail,
		RecipientEmail: "ada@example.test",
	}, channels))

	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("got %v, want unavailable", connect.CodeOf(err))
	}
}

// A row written before ENT-263 carries no channel at all, and must still be
// deliverable: an outbox whose whole job is outliving a restart cannot have
// rows become undeliverable at upgrade time.
func TestARowWithNoChannelIsStillEmail(t *testing.T) {
	t.Parallel()

	channels := delivery.NewRouter()
	channels.Register(delivery.ChannelEmail, silentChannel{})

	err := deliverOne(t, serviceRouting(postgres.PendingMessage{
		ID:             "11111111-1111-1111-1111-111111111111",
		Kind:           "invitation",
		RecipientEmail: "ada@example.test",
	}, channels))
	if err != nil {
		t.Fatalf("a row written before there was a second channel was refused: %v", err)
	}
}

type refusingChannel struct{}

func (refusingChannel) Name() string { return "test" }
func (refusingChannel) Send(context.Context, delivery.Message) error {
	return errors.New("451 try again later")
}
