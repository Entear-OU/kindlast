package delivery

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/delivery"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelroute"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The Messenger's wiring into the doorbell path (ENT-280).
//
// Two halves. The plan tells the workflow how the Messenger would be asked,
// built from rows core-api already holds, and it never carries what the
// finding says, because MessageContext has no field that could (§17.1). The
// send accepts drafted words, checks them AGAIN beside the send, and renders
// them as the opening prose with every link still minted here.
//
// PROVEN ABLE TO FAIL: removing the AcceptableDraft check in NotifyRecipients
// turns the phishing-link test red on its own; making the workflow fail on a
// failed draft (workers side) turns the doorbell-still-rings test red there.

// draftStore is the Outbox surface for the plan and notify handlers, with
// recipients and counts scripted. Calls the tests do not reach panic, the way
// mintRecorder's do.
type draftStore struct {
	mintRecorder
	bell       postgres.Doorbell
	recipients []postgres.Recipient
	pending    int32
	total      int64
	sent       bool
}

func (d *draftStore) Begin(context.Context) (pgx.Tx, error) { return nil, nil }
func (d *draftStore) Doorbell(context.Context, string) (postgres.Doorbell, error) {
	return d.bell, nil
}
func (d *draftStore) LockDoorbell(context.Context, pgx.Tx, string) (postgres.Doorbell, error) {
	return d.bell, nil
}
func (d *draftStore) Recipients(context.Context, pgx.Tx, string) ([]postgres.Recipient, error) {
	return d.recipients, nil
}
func (d *draftStore) FindingCounts(context.Context, pgx.Tx, string, string) (int32, int64, error) {
	return d.pending, d.total, nil
}
func (d *draftStore) MintCapabilityToken(
	context.Context, pgx.Tx, string, string, string, string, string,
) error {
	return nil
}
func (d *draftStore) MarkDoorbellSent(context.Context, pgx.Tx, string) error {
	d.sent = true
	return nil
}

// recordingChannel keeps what left, so a test can read the words a recipient
// would.
type recordingChannel struct{ messages []delivery.Message }

func (r *recordingChannel) Name() string { return "recording" }
func (r *recordingChannel) Send(_ context.Context, m delivery.Message) error {
	r.messages = append(r.messages, m)
	return nil
}

func authed() context.Context {
	return interceptor.WithClaims(context.Background(), &oidc.Claims{})
}

func aRecipient() postgres.Recipient {
	return postgres.Recipient{
		UserID:          "u1",
		Email:           "cco@acme.example",
		EmailVerified:   true,
		MinSeverity:     "low",
		FindingSeverity: "high",
		OrgSlug:         "acme",
		OrgName:         "Acme Ltd",
		FindingChannel:  "email",
	}
}

func draftServiceFor(store Outbox, opts ...Option) (*Service, *recordingChannel) {
	channel := &recordingChannel{}
	channels := delivery.NewRouter()
	channels.Register(delivery.ChannelEmail, channel)
	return New(store, channels, "http://localhost:3000", opts...), channel
}

// --- The plan carries the drafting instruction ------------------------------

func TestThePlanTellsTheWorkflowHowToAskTheMessenger(t *testing.T) {
	t.Parallel()

	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}, pending: 4, total: 5}
	service, _ := draftServiceFor(store)

	res, err := service.PlanNotification(authed(), connect.NewRequest(
		&platformv1.PlanNotificationRequest{NotificationId: doorbell.ID}))
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}

	draft := res.Msg.GetDraft()
	if draft == nil {
		t.Fatal("the plan carries no drafting instruction")
	}
	if draft.GetOrgId() != doorbell.OrgID {
		t.Fatalf("the instruction names org %q, want %q", draft.GetOrgId(), doorbell.OrgID)
	}
	ctx := draft.GetContext()
	if ctx.GetOrgName() != "Acme Ltd" || ctx.GetSeverity() != "high" {
		t.Fatalf("the context misnames the organisation or severity: %+v", ctx)
	}
	if ctx.GetOpenFindings() != 4 {
		t.Fatalf("open findings %d, want 4", ctx.GetOpenFindings())
	}
	if ctx.GetFirstForOrg() {
		t.Fatal("five findings ever is not a first")
	}
	if !ctx.GetHasApproveLink() {
		t.Fatal("a verified recipient means the message will carry an approve link")
	}
	if got := ctx.GetChannels(); len(got) != 1 || got[0] != "email" {
		t.Fatalf("channels %v, want [email]", got)
	}
}

func TestTheFirstFindingEverIsSaidToBe(t *testing.T) {
	t.Parallel()

	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}, pending: 0, total: 1}
	service, _ := draftServiceFor(store)

	res, err := service.PlanNotification(authed(), connect.NewRequest(
		&platformv1.PlanNotificationRequest{NotificationId: doorbell.ID}))
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	if !res.Msg.GetDraft().GetContext().GetFirstForOrg() {
		t.Fatal("one finding ever is the first, and the context does not say so")
	}
}

func TestANotificationNobodyWantsCarriesNoDraftingInstruction(t *testing.T) {
	t.Parallel()

	// Everyone below their severity floor: the workflow will settle skipped,
	// and asking a model to write words nobody will read spends a run on
	// nothing.
	skip := aRecipient()
	skip.MinSeverity = "critical"
	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{skip}, pending: 4, total: 5}
	service, _ := draftServiceFor(store)

	res, err := service.PlanNotification(authed(), connect.NewRequest(
		&platformv1.PlanNotificationRequest{NotificationId: doorbell.ID}))
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	if res.Msg.GetDraft() != nil {
		t.Fatal("a notification nobody wants was given a drafting instruction")
	}
}

func TestTheContextCannotCarryTheFinding(t *testing.T) {
	t.Parallel()

	// §17.1 at the wire, the same pin the Python suite holds: no field of
	// MessageContext can carry the detected text, the proposed action or the
	// obligation summary, so the plan cannot leak them however the handler
	// changes. Widening the message is a red build here and in
	// test_service.py, with the argument written on the message itself.
	fields := platformv1.File_kindlast_platform_v1_intelligence_proto.
		Messages().ByName("MessageContext").Fields()
	var names []string
	for i := 0; i < fields.Len(); i++ {
		names = append(names, string(fields.Get(i).Name()))
	}
	want := "channels,first_for_org,has_approve_link,open_findings,org_name,severity"
	got := strings.Join(sorted(names), ",")
	if got != want {
		t.Fatalf("MessageContext fields changed: %s (want %s)", got, want)
	}
}

func TestAModelChoiceThatCannotBeHonouredDraftsNothing(t *testing.T) {
	t.Parallel()

	// The Messenger is the one caller of the router whose failure must not
	// fail the call: a doorbell nobody receives is worse than a doorbell in
	// the template's words, and nothing is processed anywhere when no draft
	// is asked for, so there is no quiet-fallback disclosure either.
	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}, pending: 1, total: 2}
	service, _ := draftServiceFor(store, WithModelRouter(failingRouter{}))

	res, err := service.PlanNotification(authed(), connect.NewRequest(
		&platformv1.PlanNotificationRequest{NotificationId: doorbell.ID}))
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	if res.Msg.GetDraft() != nil {
		t.Fatal("an unhonourable model choice still produced a drafting instruction")
	}
	if len(res.Msg.GetRecipients()) != 1 {
		t.Fatal("the plan lost its recipients with its draft")
	}
}

type failingRouter struct{}

func (failingRouter) Resolve(context.Context, string) (modelroute.Route, error) {
	return modelroute.Route{}, context.DeadlineExceeded
}

type namedRouter struct{}

func (namedRouter) Resolve(context.Context, string) (modelroute.Route, error) {
	return modelroute.Route{Provider: "anthropic", Model: "claude-haiku-x"}, nil
}

func TestTheInstructionNamesTheModelTheRunIsRecordedAgainst(t *testing.T) {
	t.Parallel()

	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}, pending: 1, total: 2}
	service, _ := draftServiceFor(store, WithModelRouter(namedRouter{}))

	res, err := service.PlanNotification(authed(), connect.NewRequest(
		&platformv1.PlanNotificationRequest{NotificationId: doorbell.ID}))
	if err != nil {
		t.Fatalf("planning failed: %v", err)
	}
	endpoint := res.Msg.GetDraft().GetModelEndpoint()
	if endpoint.GetProvider() != "anthropic" || endpoint.GetModel() != "claude-haiku-x" {
		t.Fatalf("the endpoint names %q/%q", endpoint.GetProvider(), endpoint.GetModel())
	}
	// The deprecated getter is the assertion: the field is deprecated
	// PRECISELY so nothing rides in it, and this is the test that a plan
	// never puts a credential where a workflow history would keep it.
	//nolint:staticcheck // asserting the deprecated field stays empty is the point
	if endpoint.GetApiKey() != "" || endpoint.GetBaseUrl() != "" {
		t.Fatal("names only: a credential or an endpoint must never ride here")
	}
}

// --- The send accepts drafted words, checked again --------------------------

func notifyRequest(subject, body string) *connect.Request[platformv1.NotifyRecipientsRequest] {
	return connect.NewRequest(&platformv1.NotifyRecipientsRequest{
		NotificationId: doorbell.ID,
		UserIds:        []string{"u1"},
		Subject:        subject,
		BodyText:       body,
	})
}

func TestDraftedWordsOpenTheMessageAndTheLinksSurvive(t *testing.T) {
	t.Parallel()

	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}}
	service, channel := draftServiceFor(store)

	_, err := service.NotifyRecipients(authed(), notifyRequest(
		"A serious finding is waiting on you in Acme Ltd",
		"Something in Acme Ltd's compliance record needs a decision from you.",
	))
	if err != nil {
		t.Fatalf("sending failed: %v", err)
	}
	if len(channel.messages) != 1 {
		t.Fatalf("%d messages left, want 1", len(channel.messages))
	}
	msg := channel.messages[0]
	if msg.Subject != "A serious finding is waiting on you in Acme Ltd" {
		t.Fatalf("the drafted subject was not used: %q", msg.Subject)
	}
	if !strings.HasPrefix(msg.BodyText, "Something in Acme Ltd's") {
		t.Fatalf("the drafted body does not open the message:\n%s", msg.BodyText)
	}
	// The whole reason drafted prose is safe to accept: everything actionable
	// is still minted here, after the draft.
	for _, want := range []string{"/o/acme/feed/", "/unsubscribe/", "/approve/", "You are receiving this because"} {
		if !strings.Contains(msg.BodyText, want) {
			t.Fatalf("a drafted message lost %q:\n%s", want, msg.BodyText)
		}
	}
}

func TestNoDraftIsTheTemplateExactlyAsBefore(t *testing.T) {
	t.Parallel()

	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}}
	service, channel := draftServiceFor(store)

	if _, err := service.NotifyRecipients(authed(), notifyRequest("", "")); err != nil {
		t.Fatalf("sending failed: %v", err)
	}
	if got := channel.messages[0].BodyText; !strings.Contains(got, "Kindlast has raised a high finding for Acme Ltd.") {
		t.Fatalf("the template regressed:\n%s", got)
	}
}

func TestHalfADraftIsRefusedAsACallerBug(t *testing.T) {
	t.Parallel()

	// The harness cannot produce the half-state (a refusal withholds both, a
	// success carries both), so one arriving alone means something between the
	// services changed. invalid_argument, which the worker marks non-retryable:
	// the row stays pending and the relay starts a fresh run with a fresh plan.
	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}}
	service, channel := draftServiceFor(store)

	for name, req := range map[string]*connect.Request[platformv1.NotifyRecipientsRequest]{
		"subject only": notifyRequest("A finding is waiting", ""),
		"body only":    notifyRequest("", "Something needs a decision."),
	} {
		_, err := service.NotifyRecipients(authed(), req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("%s: got %v, want invalid_argument", name, err)
		}
	}
	if len(channel.messages) != 0 {
		t.Fatal("a half-drafted message left anyway")
	}
}

func TestADraftCarryingALinkIsRefusedBesideTheSend(t *testing.T) {
	t.Parallel()

	// THE INVARIANT HALF OF THE LINK RULE. The harness's LinkCritic refused
	// this before the words left the Python service; this is the same rule
	// beside the send, because the words rode through a workflow history and
	// a second service on the way here. Under our From: header, a link the
	// model wrote is a phishing primitive, and the last code that can stop it
	// is this handler.
	//
	// PROVEN ABLE TO FAIL: deleting the AcceptableDraft call in
	// NotifyRecipients turns exactly this test red.
	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}}
	service, channel := draftServiceFor(store)

	_, err := service.NotifyRecipients(authed(), notifyRequest(
		"A finding is waiting",
		"Open https://phish.example/login and enter your password to decide.",
	))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v, want invalid_argument", err)
	}
	if !strings.Contains(err.Error(), "link") {
		t.Fatalf("the refusal does not say what was refused: %v", err)
	}
	if len(channel.messages) != 0 {
		t.Fatal("the message left anyway, carrying somebody's link")
	}
	if store.sent {
		t.Fatal("the row was marked sent for a message that must not leave")
	}
}

func TestADraftWithAForbiddenDashIsRefusedBesideTheSend(t *testing.T) {
	t.Parallel()

	store := &draftStore{bell: doorbell, recipients: []postgres.Recipient{aRecipient()}}
	service, channel := draftServiceFor(store)

	_, err := service.NotifyRecipients(authed(), notifyRequest(
		"A finding is waiting",
		"Something needs you \u2014 decide today.",
	))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v, want invalid_argument", err)
	}
	if len(channel.messages) != 0 {
		t.Fatal("the message left anyway")
	}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
