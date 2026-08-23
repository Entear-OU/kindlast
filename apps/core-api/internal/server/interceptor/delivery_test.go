package interceptor_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	deliveryservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1/platformv1connect"
)

// DeliveryService's gate (ENT-256, part three), on the same chain as the
// sweep and tested the same way: the verifier is real, the outbox is a
// recorder, and what is asserted is that a human token cannot reach any of the
// three RPCs, that a service token can, and that the error codes the worker's
// retry policy keys on are the ones the handler actually returns.
//
// The recorder's Deliver calls the supplied send function, so the test also
// sees that the channel is the thing asked to send and that nothing but the
// recipient, subject and body reaches it.

type recordingOutbox struct {
	pending  []string
	lists    int
	delivers []string
	reclaims int
	// sent is what the channel was handed, via the handler's adapter.
	sent []delivery.Message
	// settled makes Deliver report "nothing pending by that id".
	settled bool
}

func (r *recordingOutbox) PendingMessageIDs(context.Context, int) ([]string, error) {
	r.lists++
	return r.pending, nil
}

func (r *recordingOutbox) DeliverMessage(
	ctx context.Context, id string, deliver postgres.Deliver,
) (postgres.Delivery, error) {
	r.delivers = append(r.delivers, id)
	if id == "not-a-uuid" {
		return postgres.Delivery{}, postgres.ErrBadMessageID
	}
	if r.settled {
		return postgres.Delivery{}, nil
	}
	msg := postgres.PendingMessage{
		ID: id, Kind: "invitation", RecipientEmail: "invitee@example.invalid",
		Subject: "You are invited", BodyText: "Accept at https://example.invalid/i/tok", Attempts: 1,
	}
	if err := deliver(ctx, msg); err != nil {
		return postgres.Delivery{Attempts: 2}, errors.Join(postgres.ErrNotDelivered, err)
	}
	return postgres.Delivery{Sent: true, Attempts: 2, SentAt: time.Unix(0, 0).UTC()}, nil
}

func (r *recordingOutbox) ReclaimOutbox(
	_ context.Context, _ time.Duration, _ int,
) (postgres.ReclaimResult, error) {
	r.reclaims++
	return postgres.ReclaimResult{Redacted: 3, Abandoned: 1}, nil
}

// recordingChannel is the mail server, minus the mail server.
type recordingChannel struct {
	outbox *recordingOutbox
	refuse error
}

func (c *recordingChannel) Name() string { return "recording" }
func (c *recordingChannel) Send(_ context.Context, msg delivery.Message) error {
	if c.refuse != nil {
		return c.refuse
	}
	c.outbox.sent = append(c.outbox.sent, msg)
	return nil
}

func buildDeliveryChain(t *testing.T, a *authServer, channel delivery.Channel) (
	platformv1connect.DeliveryServiceClient, *recordingOutbox,
) {
	t.Helper()

	live := requireStack(t, a.server.URL)
	scopes := realScopes(t)

	chain := connect.WithInterceptors(
		interceptor.Auth(a.verifier(t)),
		interceptor.JTI(live.revocations),
		scopes.Interceptor(),
	)

	outbox := &recordingOutbox{pending: []string{"11111111-1111-1111-1111-111111111111"}}
	if rc, ok := channel.(*recordingChannel); ok {
		rc.outbox = outbox
	}
	mux := http.NewServeMux()
	mux.Handle(platformv1connect.NewDeliveryServiceHandler(
		deliveryservice.New(outbox, channel), chain))

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return platformv1connect.NewDeliveryServiceClient(server.Client(), server.URL), outbox
}

const humanScopes = "openid profile email findings:read findings:act dashboard:read org:read org:manage notifications:read notifications:write"

func TestNoDeliveryRPCIsReachableWithAHumanToken(t *testing.T) {
	a := newAuthServer(t)
	client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
	human := sweepHeaders(t, a, humanScopes, "")

	_, err := client.ListUndelivered(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListUndeliveredRequest{}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("list: got %v, want permission_denied", got)
	}
	_, err = client.DeliverMessage(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("deliver: got %v, want permission_denied", got)
	}
	_, err = client.ReclaimMessages(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ReclaimMessagesRequest{}), human))
	if got := codeOf(t, err); got != connect.CodePermissionDenied {
		t.Fatalf("reclaim: got %v, want permission_denied", got)
	}

	if outbox.lists+len(outbox.delivers)+outbox.reclaims != 0 {
		t.Fatalf("the outbox was reached by a human token: %+v", outbox)
	}
}

func TestAServiceTokenListsDeliversAndReclaims(t *testing.T) {
	a := newAuthServer(t)
	client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
	service := sweepHeaders(t, a, "internal:ingest", "")

	listed, err := client.ListUndelivered(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ListUndeliveredRequest{}), service))
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if got := listed.Msg.GetMessageIds(); len(got) != 1 || got[0] != outbox.pending[0] {
		t.Fatalf("listed %v, want the one pending id", got)
	}

	delivered, err := client.DeliverMessage(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
	if err != nil {
		t.Fatalf("delivering: %v", err)
	}
	if delivered.Msg.GetOutcome() != platformv1.DeliverMessageResponse_OUTCOME_DELIVERED {
		t.Fatalf("outcome = %v, want delivered", delivered.Msg.GetOutcome())
	}
	if delivered.Msg.GetAttempts() != 2 || delivered.Msg.GetSentAt() == nil {
		t.Errorf("attempts and sent_at did not survive the round trip: %+v", delivered.Msg)
	}
	if len(outbox.sent) != 1 || outbox.sent[0].To != "invitee@example.invalid" {
		t.Fatalf("the channel was handed %+v, want the one message", outbox.sent)
	}

	reclaimed, err := client.ReclaimMessages(t.Context(), withHeaders(
		connect.NewRequest(&platformv1.ReclaimMessagesRequest{}), service))
	if err != nil {
		t.Fatalf("reclaiming: %v", err)
	}
	if reclaimed.Msg.GetRedacted() != 3 || reclaimed.Msg.GetAbandoned() != 1 {
		t.Errorf("counts did not survive the round trip: %+v", reclaimed.Msg)
	}
	if outbox.reclaims != 1 {
		t.Errorf("the reclaim ran %d times, want 1", outbox.reclaims)
	}
}

// The codes the worker's retry policy keys on, each one produced by the
// state that should produce it. A wrong code here is not a failing request,
// it is a message retried forever or given up on at once.
func TestDeliveryErrorCodesAreTheRetryContract(t *testing.T) {
	a := newAuthServer(t)
	service := sweepHeaders(t, a, "internal:ingest", "")

	t.Run("a refused send is unavailable, and was recorded", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a,
			&recordingChannel{refuse: errors.New("451 try again later")})
		_, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
		if got := codeOf(t, err); got != connect.CodeUnavailable {
			t.Fatalf("got %v, want unavailable", got)
		}
		if len(outbox.delivers) != 1 {
			t.Fatalf("the store was asked %d times, want 1: the attempt is recorded there", len(outbox.delivers))
		}
	})

	t.Run("no channel is failed_precondition, and the store is not asked", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, nil)
		_, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
		if got := codeOf(t, err); got != connect.CodeFailedPrecondition {
			t.Fatalf("got %v, want failed_precondition", got)
		}
		if len(outbox.delivers) != 0 {
			t.Fatal("a row was claimed with no channel to send it on")
		}
	})

	t.Run("a settled row is success, not an error", func(t *testing.T) {
		client, outbox := buildDeliveryChain(t, a, &recordingChannel{})
		outbox.settled = true
		res, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: outbox.pending[0]}), service))
		if err != nil {
			t.Fatalf("a settled row failed the call: %v", err)
		}
		if res.Msg.GetOutcome() != platformv1.DeliverMessageResponse_OUTCOME_ALREADY_SETTLED {
			t.Fatalf("outcome = %v, want already settled", res.Msg.GetOutcome())
		}
		if len(outbox.sent) != 0 {
			t.Fatal("a settled row was sent again")
		}
	})

	t.Run("a bad id is invalid_argument", func(t *testing.T) {
		client, _ := buildDeliveryChain(t, a, &recordingChannel{})
		_, err := client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{MessageId: "not-a-uuid"}), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Fatalf("got %v, want invalid_argument", got)
		}
		_, err = client.DeliverMessage(t.Context(), withHeaders(
			connect.NewRequest(&platformv1.DeliverMessageRequest{}), service))
		if got := codeOf(t, err); got != connect.CodeInvalidArgument {
			t.Fatalf("empty id: got %v, want invalid_argument", got)
		}
	})
}
