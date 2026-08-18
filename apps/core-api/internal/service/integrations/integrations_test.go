package integrations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/gateway"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// The handler's own decisions, with a fake store and a fake gateway.
//
// The tests live inside the package because they set the unexported dialer
// seam and because a fake tenant has to satisfy an unexported interface. What
// they cover is the ordering the handler is responsible for, which no test
// further down can see: revoked before anything else, ungranted recorded
// rather than thrown, and a refusal stored as a row.

const connectionID = "11111111-1111-1111-1111-111111111111"

// --- the fakes -------------------------------------------------------------

// fakeTenant records what it was asked to write and answers from a fixture.
type fakeTenant struct {
	connection domain.Connection
	credential []byte
	keyID      string

	fetches  []domain.Fetch
	observed bool
	// dialed is set by the fake gateway, and the assertions that matter are
	// about it still being false.
	dialed *bool
}

func (f *fakeTenant) Connections(context.Context) ([]domain.Connection, error) {
	return []domain.Connection{f.connection}, nil
}

func (f *fakeTenant) Connection(_ context.Context, id string) (domain.Connection, error) {
	return f.connection, nil
}

func (f *fakeTenant) SealedCredential(context.Context, string) (string, []byte, string, error) {
	if f.connection.Status != domain.StatusActive {
		return "", nil, "", domain.ErrRevoked
	}
	return f.connection.EndpointURL, f.credential, f.keyID, nil
}

func (f *fakeTenant) CreateConnection(
	context.Context, string, string, string, string, []byte, string, []domain.Tool, string,
) (domain.Connection, error) {
	return f.connection, nil
}

func (f *fakeTenant) SetGrants(context.Context, string, []string, string) (domain.Connection, error) {
	return f.connection, nil
}

func (f *fakeTenant) RevokeConnection(context.Context, string, string) (domain.Connection, error) {
	f.connection.Status = domain.StatusRevoked
	revoked := time.Now().UTC()
	f.connection.RevokedAt = &revoked
	return f.connection, nil
}

func (f *fakeTenant) RecordObservation(
	_ context.Context, _, tool, _, _ string, redactions int32, _, requestedAt time.Time, _ string,
) (domain.Fetch, error) {
	f.observed = true
	fetch := domain.Fetch{
		ID: "fetch-ok", IntegrationID: connectionID, Tool: tool,
		Outcome: domain.OutcomeSucceeded, EvidenceID: "evidence-1",
		Redactions: redactions, RequestedAt: requestedAt,
	}
	f.fetches = append(f.fetches, fetch)
	return fetch, nil
}

func (f *fakeTenant) RecordRefusal(
	_ context.Context, _, tool, _, outcome, detail string, requestedAt time.Time, _ string,
) (domain.Fetch, error) {
	fetch := domain.Fetch{
		ID: "fetch-refused", IntegrationID: connectionID, Tool: tool,
		Outcome: outcome, Detail: detail, RequestedAt: requestedAt,
	}
	f.fetches = append(f.fetches, fetch)
	return fetch, nil
}

func (f *fakeTenant) Fetches(context.Context, string, int32, time.Time) ([]domain.Fetch, error) {
	return f.fetches, nil
}

func (f *fakeTenant) UserID() string { return "ada" }
func (f *fakeTenant) OrgID() string  { return "org-1" }
func (f *fakeTenant) Role() string   { return "owner" }

// The two that interceptor.Tenant wants and nothing here uses. A handler
// reaching either would be a handler managing a transaction it did not open,
// which is the interceptor's job.
func (f *fakeTenant) Commit(context.Context) error   { return nil }
func (f *fakeTenant) Rollback(context.Context) error { return nil }

// fakeGateway answers successfully and records that it was reached.
//
// Answering successfully matters: a fake that failed would make every "nothing
// was dialled" assertion below pass for the wrong reason.
type fakeGateway struct{ dialed *bool }

func (g fakeGateway) ListTools(context.Context, string, string, string) ([]gateway.Tool, error) {
	*g.dialed = true
	return []gateway.Tool{{Name: "search_tickets"}}, nil
}

func (g fakeGateway) CallTool(
	context.Context, string, string, string, string, string, string, bool, gateway.Policy,
) (gateway.Result, error) {
	*g.dialed = true
	return gateway.Result{
		ContentJSON: `{"tickets":3}`,
		Redactions:  2,
		FetchedAt:   time.Now().UTC(),
	}, nil
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

// tenantContext carries the verified identity and the transaction the handlers
// look for, exactly as the interceptor chain would.
func tenantContext(tenant *fakeTenant) context.Context {
	ctx := interceptor.WithClaims(context.Background(), &oidc.Claims{Subject: "ada"})
	return interceptor.WithTenant(ctx, tenant)
}

func activeConnection() domain.Connection {
	return domain.Connection{
		ID:          connectionID,
		Kind:        domain.KindMCP,
		DisplayName: "Helpdesk",
		EndpointURL: "https://tools.example.com/mcp",
		Status:      domain.StatusActive,
		CreatedAt:   time.Now().UTC(),
		Tools: []domain.Tool{
			{Name: "search_tickets", WriteCapable: false, Granted: true},
			{Name: "close_ticket", WriteCapable: true, Granted: false},
		},
	}
}

func serviceFor(t *testing.T, tenant *fakeTenant) (*Service, *bool) {
	t.Helper()
	dialed := false
	tenant.dialed = &dialed
	return New(fakeGateway{dialed: &dialed}, keyring(t)), &dialed
}

// --- the tests -------------------------------------------------------------

// Revoking a connection stops future fetches (ENT-231 acceptance criterion).
//
// Asserted by what did NOT happen: the gateway is never reached. An assertion
// only about the error would pass just as happily if the fetch went out and
// the response was discarded.
func TestARevokedConnectionIsNotFetchedFrom(t *testing.T) {
	tenant := &fakeTenant{connection: activeConnection()}
	service, dialed := serviceFor(t, tenant)

	// Revoke through the handler, so this exercises the path a person takes.
	if _, err := service.RevokeIntegration(tenantContext(tenant),
		connect.NewRequest(&corev1.RevokeIntegrationRequest{
			IntegrationId: connectionID,
		})); err != nil {
		t.Fatalf("RevokeIntegration: %v", err)
	}

	_, err := service.FetchNow(tenantContext(tenant),
		connect.NewRequest(fetchRequest(connectionID, "search_tickets")))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v (%v), want FailedPrecondition", err, connect.CodeOf(err))
	}
	if *dialed {
		t.Fatal("the gateway was reached for a revoked connection")
	}
}

// The guard is only worth having if it can fail. The same fetch on the same
// connection, before it is revoked, must reach the gateway and store what came
// back.
func TestTheRevocationCheckCanActuallyFail(t *testing.T) {
	tenant := &fakeTenant{connection: activeConnection()}
	service, dialed := serviceFor(t, tenant)

	response, err := service.FetchNow(tenantContext(tenant),
		connect.NewRequest(fetchRequest(connectionID, "search_tickets")))
	if err != nil {
		t.Fatalf("FetchNow on a live connection: %v", err)
	}
	if !*dialed {
		t.Fatal("the gateway was never reached, so the refusal above proves nothing")
	}
	if !tenant.observed {
		t.Error("nothing was stored as evidence")
	}
	if got := response.Msg.GetFetch().GetOutcome(); got != domain.OutcomeSucceeded {
		t.Errorf("outcome is %q", got)
	}
	if got := response.Msg.GetFetch().GetEvidenceId(); got == "" {
		t.Error("the fetch record points at no evidence")
	}
	// The redaction count crosses back from the gateway onto the record, so a
	// customer can see that something was removed before it was stored.
	if got := response.Msg.GetFetch().GetRedactions(); got != 2 {
		t.Errorf("redactions is %d, want 2", got)
	}
}

// A tool the connection has not granted is RECORDED as a refusal and returned
// as a successful RPC, so it appears in "what we fetched".
//
// This is the choice most likely to look wrong at a glance. A refusal returned
// as an error would show the customer a red box and leave the log empty, so
// they would see a product that failed rather than a product that declined.
func TestAnUngrantedToolIsRecordedAsARefusalRatherThanThrown(t *testing.T) {
	tenant := &fakeTenant{connection: activeConnection()}
	service, dialed := serviceFor(t, tenant)

	response, err := service.FetchNow(tenantContext(tenant),
		connect.NewRequest(fetchRequest(connectionID, "close_ticket")))
	if err != nil {
		t.Fatalf("FetchNow: %v", err)
	}
	if *dialed {
		t.Fatal("the gateway was reached for a tool that was not granted")
	}

	fetch := response.Msg.GetFetch()
	if fetch.GetOutcome() != domain.OutcomeRefused {
		t.Errorf("outcome is %q, want refused", fetch.GetOutcome())
	}
	if !strings.Contains(fetch.GetDetail(), "not granted") {
		t.Errorf("the refusal does not say why: %q", fetch.GetDetail())
	}
	if len(tenant.fetches) != 1 {
		t.Fatalf("%d rows written, want 1", len(tenant.fetches))
	}
	if tenant.observed {
		t.Error("a refusal stored evidence")
	}
}

// Arguments that are not JSON are refused before anything else happens, so a
// malformed request cannot reach a customer's system.
func TestMalformedArgumentsAreRefusedBeforeAnythingIsDialled(t *testing.T) {
	tenant := &fakeTenant{connection: activeConnection()}
	service, dialed := serviceFor(t, tenant)

	request := fetchRequest(connectionID, "search_tickets")
	request.ArgumentsJson = "{not json"

	_, err := service.FetchNow(tenantContext(tenant), connect.NewRequest(request))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v (%v), want InvalidArgument", err, connect.CodeOf(err))
	}
	if *dialed {
		t.Fatal("the gateway was reached with arguments that were not JSON")
	}
}

// Discovery never marks anything granted, because that list is the one a human
// is shown so they can decide.
func TestDiscoveryGrantsNothing(t *testing.T) {
	tenant := &fakeTenant{connection: activeConnection()}
	service, _ := serviceFor(t, tenant)

	response, err := service.DiscoverIntegration(tenantContext(tenant),
		connect.NewRequest(discoverRequest("https://tools.example.com/mcp")))
	if err != nil {
		t.Fatalf("DiscoverIntegration: %v", err)
	}
	if len(response.Msg.GetTools()) == 0 {
		t.Fatal("no tools came back")
	}
	for _, tool := range response.Msg.GetTools() {
		if tool.GetGranted() {
			t.Errorf("%s arrived pre-granted from discovery", tool.GetName())
		}
	}
}

// A credential cannot be stored by a deployment with no key, and is refused
// rather than written in plaintext.
func TestWithNoKeyACredentialIsRefusedRatherThanStored(t *testing.T) {
	tenant := &fakeTenant{connection: activeConnection()}
	dialed := false
	tenant.dialed = &dialed

	empty, err := secrets.NewKeyring("")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	service := New(fakeGateway{dialed: &dialed}, empty)

	_, err = service.ConnectIntegration(tenantContext(tenant),
		connect.NewRequest(connectRequest("Helpdesk", "https://tools.example.com/mcp", "sk_live_secret")))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("got %v (%v), want FailedPrecondition", err, connect.CodeOf(err))
	}
	if !strings.Contains(err.Error(), "KINDLAST_INTEGRATION_KEY") {
		t.Errorf("the error does not name the setting an operator has to set: %v", err)
	}

	// And the same connection with no credential is made, because an endpoint
	// on a customer's own network legitimately needs none.
	if _, err := service.ConnectIntegration(tenantContext(tenant),
		connect.NewRequest(connectRequest("Helpdesk", "https://tools.example.com/mcp", ""))); err != nil {
		t.Fatalf("a connection needing no credential was refused: %v", err)
	}
}

// --- request builders ------------------------------------------------------

func fetchRequest(connection, tool string) *corev1.FetchNowRequest {
	return &corev1.FetchNowRequest{
		IntegrationId: connection,
		Tool:          tool,
		ArgumentsJson: `{"status":"open"}`,
	}
}

func discoverRequest(endpoint string) *corev1.DiscoverIntegrationRequest {
	return &corev1.DiscoverIntegrationRequest{
		Kind:        corev1.IntegrationKind_INTEGRATION_KIND_MCP,
		EndpointUrl: endpoint,
	}
}

func connectRequest(name, endpoint, credential string) *corev1.ConnectIntegrationRequest {
	return &corev1.ConnectIntegrationRequest{
		Kind:        corev1.IntegrationKind_INTEGRATION_KIND_MCP,
		DisplayName: name,
		EndpointUrl: endpoint,
		Credential:  credential,
		OfferedTools: []*corev1.IntegrationTool{
			{Name: "search_tickets"},
		},
		GrantedTools: []string{"search_tickets"},
	}
}
