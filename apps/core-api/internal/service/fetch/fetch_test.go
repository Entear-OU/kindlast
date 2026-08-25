package fetch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/gateway"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The handler's own decisions, with a fake plan, a fake recorder and a fake
// gateway (ENT-279).
//
// The property most of these defend is a negative one: a scheduled fetch that
// should not happen writes a row saying why and DIALS NOTHING. So the fakes
// are arranged the way the integrations tests arrange theirs: the gateway
// records that it was reached, it answers successfully so that "nothing was
// dialled" can never pass because the dial failed, and the assertions are
// about what did not happen.

const (
	orgID        = "11111111-1111-1111-1111-111111111111"
	connectionID = "22222222-2222-2222-2222-222222222222"
)

// --- the fakes -------------------------------------------------------------

type fakeTargets struct {
	staleAfter    time.Duration
	requestWindow time.Duration
	limit         int
	targets       []postgres.FetchTarget
}

func (f *fakeTargets) FetchTargets(
	_ context.Context, staleAfter, requestWindow time.Duration, limit int,
) ([]postgres.FetchTarget, error) {
	f.staleAfter = staleAfter
	f.requestWindow = requestWindow
	f.limit = limit
	return f.targets, nil
}

type fakePlans struct {
	plan postgres.FetchPlan
	err  error
}

func (f *fakePlans) FetchPlan(context.Context, string, string) (postgres.FetchPlan, error) {
	return f.plan, f.err
}

// fakeEvidence records the one FetchRecord the handler deposits.
type fakeEvidence struct {
	records []postgres.FetchRecord
	deposit postgres.Deposit
}

func (f *fakeEvidence) IngestEvidence(
	_ context.Context, record postgres.FetchRecord,
) (postgres.Deposit, error) {
	f.records = append(f.records, record)
	deposit := f.deposit
	if deposit.FetchID == "" {
		deposit.FetchID = "fetch-1"
	}
	return deposit, nil
}

type fakeGateway struct {
	dialed bool
	sent   struct {
		endpoint, credential, tool, arguments string
		writeCapable                          bool
		policy                                gateway.Policy
	}
	result gateway.Result
	err    error
}

func (g *fakeGateway) CallTool(
	_ context.Context, _, _, endpoint, credential, tool, argumentsJSON string,
	writeCapable bool, policy gateway.Policy,
) (gateway.Result, error) {
	g.dialed = true
	g.sent.endpoint, g.sent.credential = endpoint, credential
	g.sent.tool, g.sent.arguments = tool, argumentsJSON
	g.sent.writeCapable, g.sent.policy = writeCapable, policy
	if g.err != nil {
		return gateway.Result{}, g.err
	}
	if g.result.ContentJSON == "" {
		return gateway.Result{
			ContentJSON: `{"records":3}`, Redactions: 1, FetchedAt: time.Now().UTC(),
		}, nil
	}
	return g.result, nil
}

// --- the harness -----------------------------------------------------------

func keyring(t *testing.T) *secrets.Keyring {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	ring, err := secrets.NewKeyring("test:" + base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return ring
}

func internalContext() context.Context {
	return interceptor.WithClaims(context.Background(), &oidc.Claims{Subject: "core-api-client"})
}

func grantedPlan() postgres.FetchPlan {
	return postgres.FetchPlan{
		OrgID:       orgID,
		EndpointURL: "https://tools.example.com/mcp",
		Tool:        domain.Tool{Name: "list_records", Granted: true},
		Granted:     []string{"list_records"},
	}
}

func run(
	t *testing.T, plans *fakePlans, dial *fakeGateway, keys *secrets.Keyring,
) (*platformv1.RunScheduledFetchResponse, *fakeEvidence, error) {
	t.Helper()
	evidence := &fakeEvidence{}
	if keys == nil {
		keys = keyring(t)
	}
	service := New(&fakeTargets{}, plans, evidence, dial, keys)
	res, err := service.RunScheduledFetch(internalContext(),
		connect.NewRequest(&platformv1.RunScheduledFetchRequest{
			IntegrationId: connectionID,
			Tool:          "list_records",
		}))
	if err != nil {
		return nil, evidence, err
	}
	return res.Msg, evidence, nil
}

// --- what is recorded, and what is never dialled ---------------------------

func TestARevokedConnectionRecordsARefusalAndDialsNothing(t *testing.T) {
	plans := &fakePlans{plan: postgres.FetchPlan{OrgID: orgID}, err: domain.ErrRevoked}
	dial := &fakeGateway{}

	res, evidence, err := run(t, plans, dial, nil)
	if err != nil {
		t.Fatalf("RunScheduledFetch: %v", err)
	}
	if dial.dialed {
		t.Fatal("a revoked connection was dialled")
	}
	if res.GetOutcome() != domain.OutcomeRefused || !strings.Contains(res.GetDetail(), "revoked") {
		t.Fatalf("answer = %q %q, want a refusal naming the revocation", res.GetOutcome(), res.GetDetail())
	}
	if len(evidence.records) != 1 || evidence.records[0].Outcome != domain.OutcomeRefused {
		t.Fatalf("recorded %+v, want one refusal row", evidence.records)
	}
	if evidence.records[0].ContentJSON != "" {
		t.Fatal("a refusal carried content")
	}
}

func TestAnUngrantedToolRecordsARefusalAndDialsNothing(t *testing.T) {
	plans := &fakePlans{plan: postgres.FetchPlan{OrgID: orgID}, err: domain.ErrNotGranted}
	dial := &fakeGateway{}

	res, evidence, err := run(t, plans, dial, nil)
	if err != nil {
		t.Fatalf("RunScheduledFetch: %v", err)
	}
	if dial.dialed {
		t.Fatal("an ungranted tool was dialled; granted is the customer's decision and a schedule widened it")
	}
	if res.GetOutcome() != domain.OutcomeRefused || !strings.Contains(res.GetDetail(), "not granted") {
		t.Fatalf("answer = %q %q", res.GetOutcome(), res.GetDetail())
	}
	if len(evidence.records) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(evidence.records))
	}
}

// The consenting person leaving the organisation stops the fetch: the plan
// runs under their authority, the membership check hides the connection, and
// what the customer sees is a refusal saying whose consent expired.
func TestAConsentWhosePersonHasLeftRecordsARefusalAndDialsNothing(t *testing.T) {
	plans := &fakePlans{plan: postgres.FetchPlan{OrgID: orgID}, err: postgres.ErrNoConnection}
	dial := &fakeGateway{}

	res, evidence, err := run(t, plans, dial, nil)
	if err != nil {
		t.Fatalf("RunScheduledFetch: %v", err)
	}
	if dial.dialed {
		t.Fatal("a connection nobody stands behind was dialled")
	}
	if res.GetOutcome() != domain.OutcomeRefused || !strings.Contains(res.GetDetail(), "no longer a member") {
		t.Fatalf("answer = %q %q", res.GetOutcome(), res.GetDetail())
	}
	if len(evidence.records) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(evidence.records))
	}
}

// A connection that does not exist at all is the one shape of "no" with no
// organisation to record it against, so it is the one that is an error.
func TestAnUnknownConnectionIsNotFoundAndNothingIsRecorded(t *testing.T) {
	plans := &fakePlans{plan: postgres.FetchPlan{}, err: postgres.ErrNoConnection}
	dial := &fakeGateway{}

	_, evidence, err := run(t, plans, dial, nil)
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("err = %v, want not_found", err)
	}
	if dial.dialed {
		t.Fatal("an unknown connection was dialled")
	}
	if len(evidence.records) != 0 {
		t.Fatalf("recorded %d rows for a connection that does not exist", len(evidence.records))
	}
}

// A write-capable tool is refused HERE, not only filtered out of the listing.
// `fetch_targets` never returns one, but the RPC names a tool directly, and a
// control that only holds when the caller went through the list is not a
// control.
func TestAWriteCapableToolIsRefusedEvenWhenGranted(t *testing.T) {
	plan := grantedPlan()
	plan.Tool = domain.Tool{Name: "list_records", Granted: true, WriteCapable: true}
	plans := &fakePlans{plan: plan}
	dial := &fakeGateway{}

	res, evidence, err := run(t, plans, dial, nil)
	if err != nil {
		t.Fatalf("RunScheduledFetch: %v", err)
	}
	if dial.dialed {
		t.Fatal("a write-capable tool was dialled by a schedule; nobody was watching")
	}
	if res.GetOutcome() != domain.OutcomeRefused || !strings.Contains(res.GetDetail(), "only reads") {
		t.Fatalf("answer = %q %q", res.GetOutcome(), res.GetDetail())
	}
	if len(evidence.records) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(evidence.records))
	}
}

// A credential this deployment cannot open is a FAILURE (something is wrong
// with the keys), recorded without repeating the error, because the error is
// about key material and the row is read by a customer.
func TestAnUnopenableCredentialRecordsAFailureWithoutTheKeyError(t *testing.T) {
	plan := grantedPlan()
	plan.Sealed = []byte("not something the keyring sealed")
	plan.KeyID = "k1"
	plans := &fakePlans{plan: plan}
	dial := &fakeGateway{}

	res, evidence, err := run(t, plans, dial, nil)
	if err != nil {
		t.Fatalf("RunScheduledFetch: %v", err)
	}
	if dial.dialed {
		t.Fatal("the endpoint was dialled with no credential to send")
	}
	if res.GetOutcome() != domain.OutcomeFailed {
		t.Fatalf("outcome = %q, want failed: nothing decided against this call", res.GetOutcome())
	}
	if !strings.Contains(res.GetDetail(), "could not open the stored credential") {
		t.Fatalf("detail = %q", res.GetDetail())
	}
	if len(evidence.records) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(evidence.records))
	}
}

// The gateway declining is a refusal; the endpoint breaking is a failure. The
// distinction lives in the row because it answers different questions: one
// says a control worked, the other says something broke.
func TestAGatewayRefusalAndATransportFailureAreRecordedApart(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		outcome string
	}{
		{
			name: "egress refusal",
			err: errors.Join(gateway.ErrRefused, connect.NewError(connect.CodePermissionDenied,
				errors.New(`"tools.example.com" is not on this deployment's egress allow-list`))),
			outcome: domain.OutcomeRefused,
		},
		{
			name:    "endpoint down",
			err:     connect.NewError(connect.CodeUnavailable, errors.New("the endpoint did not answer usefully")),
			outcome: domain.OutcomeFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plans := &fakePlans{plan: grantedPlan()}
			dial := &fakeGateway{err: tc.err}

			res, evidence, err := run(t, plans, dial, nil)
			if err != nil {
				t.Fatalf("RunScheduledFetch: %v", err)
			}
			if res.GetOutcome() != tc.outcome {
				t.Fatalf("outcome = %q, want %q", res.GetOutcome(), tc.outcome)
			}
			if res.GetDetail() == "" {
				t.Fatal("a fetch that did not succeed said nothing about why")
			}
			if len(evidence.records) != 1 || evidence.records[0].Outcome != tc.outcome {
				t.Fatalf("recorded %+v", evidence.records)
			}
			if evidence.records[0].ContentJSON != "" {
				t.Fatal("a fetch that did not succeed carried content")
			}
		})
	}
}

// The success path: the credential is opened and sent, the arguments are `{}`
// and nothing else, and what came back is deposited with the gateway's own
// clock as observed_at.
func TestASuccessfulFetchDepositsWhatTheEndpointSaid(t *testing.T) {
	keys := keyring(t)
	sealed, keyID, err := keys.Seal("token-123", connectionID)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	plan := grantedPlan()
	plan.Sealed, plan.KeyID = sealed, keyID
	plans := &fakePlans{plan: plan}
	observed := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	dial := &fakeGateway{result: gateway.Result{
		ContentJSON: `{"records":3}`, Redactions: 2, FetchedAt: observed,
	}}

	res, evidence, err := run(t, plans, dial, keys)
	if err != nil {
		t.Fatalf("RunScheduledFetch: %v", err)
	}
	if !dial.dialed {
		t.Fatal("nothing was dialled")
	}
	if dial.sent.credential != "token-123" {
		t.Fatalf("the credential sent was %q", dial.sent.credential)
	}
	if dial.sent.arguments != "{}" {
		t.Fatalf("arguments = %q; nothing can validate arguments, so a schedule sends none", dial.sent.arguments)
	}
	if dial.sent.writeCapable {
		t.Fatal("the call claimed the tool writes")
	}
	if res.GetOutcome() != domain.OutcomeSucceeded {
		t.Fatalf("outcome = %q", res.GetOutcome())
	}
	if len(evidence.records) != 1 {
		t.Fatalf("recorded %d rows, want 1", len(evidence.records))
	}
	record := evidence.records[0]
	if record.ContentJSON != `{"records":3}` || record.Redactions != 2 {
		t.Fatalf("deposited %+v", record)
	}
	if !record.ObservedAt.Equal(observed) {
		t.Fatalf("observed_at = %v, want the gateway's clock %v", record.ObservedAt, observed)
	}
	if record.OrgID != uuid.MustParse(orgID) || record.ConnectionID != uuid.MustParse(connectionID) {
		t.Fatalf("deposited for %s/%s", record.OrgID, record.ConnectionID)
	}
}

// --- the staleness bound is the server's, not the caller's ------------------

// How stale is stale is a constant here, and the listing request carries no
// way to change it. A caller able to say "everything is stale" would be a
// caller able to dial every customer's systems at once, so the first half of
// this pins the message shape against the field being added back, and the
// second half pins that the constant is what actually reaches the store.
func TestTheStalenessIntervalIsNotACallerDecision(t *testing.T) {
	fields := (&platformv1.ListFetchTargetsRequest{}).ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		name := string(fields.Get(i).Name())
		if name != "limit" {
			t.Fatalf("ListFetchTargetsRequest carries %q; it may carry a limit and nothing else, "+
				"because anything else becomes a caller deciding how often customers are dialled", name)
		}
	}

	targets := &fakeTargets{targets: []postgres.FetchTarget{
		{OrgID: orgID, IntegrationID: connectionID, Tool: "list_records"},
	}}
	service := New(targets, &fakePlans{}, &fakeEvidence{}, &fakeGateway{}, keyring(t))
	res, err := service.ListFetchTargets(internalContext(),
		connect.NewRequest(&platformv1.ListFetchTargetsRequest{}))
	if err != nil {
		t.Fatalf("ListFetchTargets: %v", err)
	}
	if targets.staleAfter != EvidenceStaleAfter {
		t.Fatalf("the store was asked with %v, want the constant %v", targets.staleAfter, EvidenceStaleAfter)
	}
	if len(res.Msg.GetTargets()) != 1 || res.Msg.GetTargets()[0].GetTool() != "list_records" {
		t.Fatalf("targets = %+v", res.Msg.GetTargets())
	}
}

func TestTheListLimitIsClamped(t *testing.T) {
	targets := &fakeTargets{}
	service := New(targets, &fakePlans{}, &fakeEvidence{}, &fakeGateway{}, keyring(t))

	if _, err := service.ListFetchTargets(internalContext(),
		connect.NewRequest(&platformv1.ListFetchTargetsRequest{Limit: 1_000_000})); err != nil {
		t.Fatalf("ListFetchTargets: %v", err)
	}
	if targets.limit != MaxListLimit {
		t.Fatalf("limit = %d, want the cap %d", targets.limit, MaxListLimit)
	}
	if _, err := service.ListFetchTargets(internalContext(),
		connect.NewRequest(&platformv1.ListFetchTargetsRequest{})); err != nil {
		t.Fatalf("ListFetchTargets: %v", err)
	}
	if targets.limit != DefaultListLimit {
		t.Fatalf("limit = %d, want the default %d", targets.limit, DefaultListLimit)
	}
}
