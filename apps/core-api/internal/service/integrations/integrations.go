// Package integrations serves IntegrationsService: the console's control over
// which of a customer's systems Kindlast may reach (ENT-231, §26.4).
//
// # NOTHING HERE OPENS A CONNECTION TO A CUSTOMER
//
// Every method that needs to reach an endpoint goes through the gateway
// client, which talks to a service on the internal network. core-api has no
// HTTP client pointed at a customer-supplied address, and that is the property
// this package exists to preserve rather than a detail of how it is written.
//
// # A REFUSAL IS RECORDED, NOT ONLY RETURNED
//
// When the gateway refuses a fetch, this writes a `refused` row and returns
// it as a successful RPC. That is deliberate and it is the choice most likely
// to look wrong at first glance.
//
// A refusal is what a working control produces. If it came back as an error,
// the console would show a red box and the "what we fetched" view would have
// nothing in it, so the customer would see a product that failed rather than a
// product that declined. The record is the feature: "we did not call
// close_ticket because this connection has not granted write access" is a
// sentence that builds trust, and it cannot be written by an error path that
// stores nothing.
//
// A transport failure is recorded the same way, as `failed`. The distinction
// between the two lives in the row rather than in whether the call threw.
package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/gateway"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// store is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type store interface {
	Connections(ctx context.Context) ([]domain.Connection, error)
	Connection(ctx context.Context, id string) (domain.Connection, error)
	SealedCredential(ctx context.Context, id string) (endpoint string, sealed []byte, keyID string, err error)
	CreateConnection(ctx context.Context, id, kind, displayName, endpoint string,
		sealed []byte, keyID string, tools []domain.Tool, createdBy string) (domain.Connection, error)
	SetGrants(ctx context.Context, id string, granted []string, by string) (domain.Connection, error)
	RevokeConnection(ctx context.Context, id, by string) (domain.Connection, error)
	RecordObservation(ctx context.Context, connectionID, tool, argumentsJSON, contentJSON string,
		redactions int32, observedAt, requestedAt time.Time, requestedBy string) (domain.Fetch, error)
	RecordRefusal(ctx context.Context, connectionID, tool, argumentsJSON, outcome, detail string,
		requestedAt time.Time, requestedBy string) (domain.Fetch, error)
	Fetches(ctx context.Context, connectionID string, pageSize int32, before time.Time) ([]domain.Fetch, error)
	UserID() string
}

// Service implements corev1connect.IntegrationsServiceHandler.
type Service struct {
	gateway *gateway.Client
	keys    *secrets.Keyring
}

func New(client *gateway.Client, keys *secrets.Keyring) *Service {
	return &Service{gateway: client, keys: keys}
}

func (s *Service) ListIntegrations(
	ctx context.Context,
	_ *connect.Request[corev1.ListIntegrationsRequest],
) (*connect.Response[corev1.ListIntegrationsResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	connections, err := tenant.Connections(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListIntegrationsResponse{}
	for _, connection := range connections {
		response.Integrations = append(response.Integrations, toProto(connection))
	}
	return connect.NewResponse(response), nil
}

func (s *Service) DiscoverIntegration(
	ctx context.Context,
	request *connect.Request[corev1.DiscoverIntegrationRequest],
) (*connect.Response[corev1.DiscoverIntegrationResponse], error) {
	// Discovery writes nothing, so it needs no store. The tenant check still
	// happens, because it is what proves the caller is a member of the
	// organisation whose rate limit and log line this call is about to spend.
	if _, err := tenantFrom(ctx); err != nil {
		return nil, err
	}
	if err := requireKind(request.Msg.GetKind()); err != nil {
		return nil, err
	}
	if err := domain.ValidateEndpoint(request.Msg.GetEndpointUrl()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	orgID, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	tools, err := s.gateway.ListTools(ctx, orgID,
		request.Msg.GetEndpointUrl(), request.Msg.GetCredential())
	if err != nil {
		return nil, discoveryError(err)
	}

	response := &corev1.DiscoverIntegrationResponse{}
	for _, tool := range tools {
		// GRANTED IS FALSE ON EVERY TOOL HERE, WITHOUT EXCEPTION. This is the
		// list a human is shown so they can decide, and pre-ticking anything
		// would make the screen a formality.
		response.Tools = append(response.Tools, &corev1.IntegrationTool{
			Name:         tool.Name,
			Description:  tool.Description,
			WriteCapable: tool.WriteCapable,
			Granted:      false,
		})
	}
	return connect.NewResponse(response), nil
}

func (s *Service) ConnectIntegration(
	ctx context.Context,
	request *connect.Request[corev1.ConnectIntegrationRequest],
) (*connect.Response[corev1.ConnectIntegrationResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireKind(request.Msg.GetKind()); err != nil {
		return nil, err
	}
	if err := domain.ValidateDisplayName(request.Msg.GetDisplayName()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := domain.ValidateEndpoint(request.Msg.GetEndpointUrl()); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	offered := make([]domain.Tool, 0, len(request.Msg.GetOfferedTools()))
	for _, tool := range request.Msg.GetOfferedTools() {
		if strings.TrimSpace(tool.GetName()) == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("a tool with no name cannot be recorded"))
		}
		offered = append(offered, domain.Tool{
			Name:         tool.GetName(),
			Description:  tool.GetDescription(),
			WriteCapable: tool.GetWriteCapable(),
		})
	}
	if len(offered) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("that endpoint offered no tools, so there is nothing to connect"))
	}

	// Grants resolved from the request's list only. A tool arriving with
	// `granted` already set does not become granted, which closes the obvious
	// way to grant one without ticking it.
	tools, err := domain.ResolveGrants(offered, request.Msg.GetGrantedTools())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// THE ID IS MINTED HERE RATHER THAN BY THE DATABASE, AND THAT IS THE
	// SEALING FLOW RATHER THAN A PREFERENCE.
	//
	// A credential is sealed with the connection's id as additional
	// authenticated data, so a ciphertext lifted from one row cannot be opened
	// in another. That binding needs the id before the insert. The
	// alternatives are both worse: insert, then seal, then update, which is a
	// window in which a plaintext-shaped row exists and a second statement
	// that can fail; or bind to something else, such as the endpoint, which
	// then cannot ever change and is not unique anyway.
	id := uuid.NewString()

	sealed, keyID, err := s.seal(request.Msg.GetCredential(), id)
	if err != nil {
		return nil, err
	}

	connection, err := tenant.CreateConnection(ctx, id,
		domain.KindMCP, strings.TrimSpace(request.Msg.GetDisplayName()),
		strings.TrimSpace(request.Msg.GetEndpointUrl()),
		sealed, keyID, tools, tenant.UserID())
	if err != nil {
		return nil, writeError(err)
	}

	return connect.NewResponse(&corev1.ConnectIntegrationResponse{
		Integration: toProto(connection),
	}), nil
}

func (s *Service) UpdateToolGrants(
	ctx context.Context,
	request *connect.Request[corev1.UpdateToolGrantsRequest],
) (*connect.Response[corev1.UpdateToolGrantsResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	connection, err := tenant.SetGrants(ctx,
		request.Msg.GetIntegrationId(), request.Msg.GetGrantedTools(), tenant.UserID())
	if err != nil {
		return nil, writeError(err)
	}
	return connect.NewResponse(&corev1.UpdateToolGrantsResponse{
		Integration: toProto(connection),
	}), nil
}

func (s *Service) RevokeIntegration(
	ctx context.Context,
	request *connect.Request[corev1.RevokeIntegrationRequest],
) (*connect.Response[corev1.RevokeIntegrationResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	connection, err := tenant.RevokeConnection(ctx, request.Msg.GetIntegrationId(), tenant.UserID())
	if err != nil {
		return nil, writeError(err)
	}
	return connect.NewResponse(&corev1.RevokeIntegrationResponse{
		Integration: toProto(connection),
	}), nil
}

func (s *Service) ListFetches(
	ctx context.Context,
	request *connect.Request[corev1.ListFetchesRequest],
) (*connect.Response[corev1.ListFetchesResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	var before time.Time
	if token := request.Msg.GetPageToken(); token != "" {
		parsed, err := time.Parse(time.RFC3339Nano, token)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("that page token is not one we issued"))
		}
		before = parsed
	}

	fetches, err := tenant.Fetches(ctx,
		request.Msg.GetIntegrationId(), request.Msg.GetPageSize(), before)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListFetchesResponse{}
	for _, fetch := range fetches {
		response.Fetches = append(response.Fetches, fetchToProto(fetch))
	}
	// A next token only when the page was full. Issuing one on a short page
	// sends a console round again for nothing, and "there might be more" is a
	// worse answer than "there is not".
	if size := postgres.EffectiveFetchPageSize(request.Msg.GetPageSize()); int32(len(fetches)) == size && size > 0 {
		response.NextPageToken = fetches[len(fetches)-1].RequestedAt.Format(time.RFC3339Nano)
	}
	return connect.NewResponse(response), nil
}

// FetchNow is the live request from the rail.
//
// # THE ORDER HERE IS THE PRODUCT BEHAVIOUR
//
// Read the connection, refuse if revoked, check the tool is granted, unseal
// the credential, call the gateway, store what came back, record the fetch.
// The revocation check is first because "revoking a connection stops future
// fetches" is an acceptance criterion, and a check placed after the call would
// satisfy the letter of it while the fetch had already happened.
func (s *Service) FetchNow(
	ctx context.Context,
	request *connect.Request[corev1.FetchNowRequest],
) (*connect.Response[corev1.FetchNowResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := orgOf(ctx)
	if err != nil {
		return nil, err
	}

	connectionID := request.Msg.GetIntegrationId()
	toolName := strings.TrimSpace(request.Msg.GetTool())
	if toolName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("no tool was named"))
	}

	arguments := strings.TrimSpace(request.Msg.GetArgumentsJson())
	if arguments == "" {
		arguments = "{}"
	}
	if !json.Valid([]byte(arguments)) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("the arguments are not JSON"))
	}

	connection, err := tenant.Connection(ctx, connectionID)
	if err != nil {
		return nil, writeError(err)
	}
	requestedAt := time.Now().UTC()

	// REVOKED FIRST. Refused as an error rather than recorded as a refusal,
	// and the difference is deliberate: a fetch on a revoked connection is a
	// caller bug rather than a policy decision about a live connection, and
	// filling the log with rows for a connection nobody may use any more would
	// bury the refusals that mean something.
	if connection.Status != domain.StatusActive {
		return nil, connect.NewError(connect.CodeFailedPrecondition, domain.ErrRevoked)
	}

	tool, offered := domain.Find(connection.Tools, toolName)
	if !offered || !tool.Granted {
		// RECORDED, because this one is a policy decision about a live
		// connection and the customer should see it in "what we fetched".
		fetch, recordErr := tenant.RecordRefusal(ctx, connectionID, toolName, arguments,
			domain.OutcomeRefused,
			"the tool is not granted on this connection",
			requestedAt, tenant.UserID())
		if recordErr != nil {
			return nil, connect.NewError(connect.CodeInternal, recordErr)
		}
		return connect.NewResponse(&corev1.FetchNowResponse{Fetch: fetchToProto(fetch)}), nil
	}

	endpoint, sealed, keyID, err := tenant.SealedCredential(ctx, connectionID)
	if err != nil {
		return nil, writeError(err)
	}

	credential := ""
	if len(sealed) > 0 {
		credential, err = s.keys.Open(sealed, keyID, connectionID)
		if err != nil {
			// Recorded as a failure rather than a refusal: nothing decided
			// against this call, something is wrong with this deployment's
			// keys, and an operator needs to see it in the log rather than in
			// a browser console.
			fetch, recordErr := tenant.RecordRefusal(ctx, connectionID, toolName, arguments,
				domain.OutcomeFailed,
				"this deployment could not open the stored credential for that connection",
				requestedAt, tenant.UserID())
			if recordErr != nil {
				return nil, connect.NewError(connect.CodeInternal, recordErr)
			}
			return connect.NewResponse(&corev1.FetchNowResponse{Fetch: fetchToProto(fetch)}), nil
		}
	}

	result, err := s.gateway.CallTool(ctx, orgID, connectionID, endpoint, credential,
		toolName, arguments, tool.WriteCapable,
		gateway.Policy{
			Granted:      domain.GrantedNames(connection.Tools),
			WriteGranted: domain.WriteGrants(connection.Tools),
		})
	if err != nil {
		outcome := domain.OutcomeFailed
		if errors.Is(err, gateway.ErrRefused) {
			outcome = domain.OutcomeRefused
		}
		fetch, recordErr := tenant.RecordRefusal(ctx, connectionID, toolName, arguments,
			outcome, reason(err), requestedAt, tenant.UserID())
		if recordErr != nil {
			return nil, connect.NewError(connect.CodeInternal, recordErr)
		}
		return connect.NewResponse(&corev1.FetchNowResponse{Fetch: fetchToProto(fetch)}), nil
	}

	fetch, err := tenant.RecordObservation(ctx, connectionID, toolName, arguments,
		result.ContentJSON, result.Redactions, result.FetchedAt, requestedAt, tenant.UserID())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&corev1.FetchNowResponse{Fetch: fetchToProto(fetch)}), nil
}

// seal encrypts a credential, or refuses when this deployment has no key.
//
// NOT STORED IN PLAINTEXT WHEN THERE IS NO KEY. The failure mode of "we could
// not encrypt it, so we kept it as it was" is the one that ends up in a breach
// notification, so an unconfigured deployment refuses the connection and names
// the setting.
//
// An empty credential is not an absent key: an MCP endpoint on a customer's
// own network legitimately needs none, and that connection is made whether or
// not this deployment has a key at all.
func (s *Service) seal(credential, connectionID string) ([]byte, string, error) {
	if strings.TrimSpace(credential) == "" {
		return nil, "", nil
	}
	if !s.keys.Configured() {
		return nil, "", connect.NewError(connect.CodeFailedPrecondition,
			errors.New("this deployment has no integration key, so it cannot store a credential; "+
				"set KINDLAST_INTEGRATION_KEY, or connect an endpoint that needs no credential"))
	}

	sealed, keyID, err := s.keys.Seal(credential, connectionID)
	if err != nil {
		return nil, "", connect.NewError(connect.CodeInternal, err)
	}
	return sealed, keyID, nil
}

func requireKind(kind corev1.IntegrationKind) error {
	if kind != corev1.IntegrationKind_INTEGRATION_KIND_MCP {
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("the only kind of connection this product makes today is an MCP endpoint"))
	}
	return nil
}

// discoveryError maps a gateway refusal onto something a console can render.
//
// PermissionDenied rather than Internal, because the overwhelmingly common
// cause is a customer typing a host their operator has not permitted, and that
// is their problem to fix rather than ours to page somebody about.
func discoveryError(err error) error {
	if errors.Is(err, gateway.ErrRefused) {
		return connect.NewError(connect.CodePermissionDenied, errors.New(reason(err)))
	}
	return connect.NewError(connect.CodeUnavailable, errors.New(reason(err)))
}

func writeError(err error) error {
	switch {
	case errors.Is(err, postgres.ErrNoConnection):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, domain.ErrRevoked):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrInvalid):
		return connect.NewError(connect.CodeInvalidArgument, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

// reason strips Connect's code prefix off an error message.
//
// The message ends up in a fetch record a customer reads, and
// "permission_denied: the tool close_ticket is not granted on this connection"
// reads as a stack trace where the second half reads as an explanation.
func reason(err error) string {
	message := err.Error()
	if _, after, found := strings.Cut(message, ": "); found {
		return after
	}
	return message
}

func toProto(connection domain.Connection) *corev1.Integration {
	out := &corev1.Integration{
		Id:          connection.ID,
		Kind:        corev1.IntegrationKind_INTEGRATION_KIND_MCP,
		DisplayName: connection.DisplayName,
		EndpointUrl: connection.EndpointURL,
		Status:      statusToProto(connection.Status),
		CreatedAt:   connection.CreatedAt.Format(time.RFC3339Nano),
		ConsentedBy: connection.ConsentedBy,
	}
	if connection.RevokedAt != nil {
		out.RevokedAt = connection.RevokedAt.Format(time.RFC3339Nano)
	}
	if connection.ConsentedAt != nil {
		out.ConsentedAt = connection.ConsentedAt.Format(time.RFC3339Nano)
	}
	for _, tool := range connection.Tools {
		out.Tools = append(out.Tools, &corev1.IntegrationTool{
			Name:         tool.Name,
			Description:  tool.Description,
			WriteCapable: tool.WriteCapable,
			Granted:      tool.Granted,
		})
	}
	return out
}

func statusToProto(status string) corev1.IntegrationStatus {
	if status == domain.StatusRevoked {
		return corev1.IntegrationStatus_INTEGRATION_STATUS_REVOKED
	}
	return corev1.IntegrationStatus_INTEGRATION_STATUS_ACTIVE
}

func fetchToProto(fetch domain.Fetch) *corev1.Fetch {
	return &corev1.Fetch{
		Id:              fetch.ID,
		IntegrationId:   fetch.IntegrationID,
		IntegrationName: fetch.IntegrationName,
		Tool:            fetch.Tool,
		Outcome:         fetch.Outcome,
		Detail:          fetch.Detail,
		RequestedAt:     fetch.RequestedAt.Format(time.RFC3339Nano),
		FinishedAt:      fetch.FinishedAt.Format(time.RFC3339Nano),
		EvidenceId:      fetch.EvidenceID,
		Redactions:      fetch.Redactions,
		RequestedBy:     fetch.RequestedBy,
	}
}

func tenantFrom(ctx context.Context) (store, error) {
	if _, ok := interceptor.ClaimsFrom(ctx); !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no verified identity"))
	}
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	typed, ok := tenant.(store)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction cannot reach integrations"))
	}
	return typed, nil
}

func orgOf(ctx context.Context) (string, error) {
	tenant, ok := interceptor.TenantFrom(ctx)
	if !ok {
		return "", connect.NewError(connect.CodeInternal,
			errors.New("handler reached with no tenant transaction"))
	}
	typed, ok := tenant.(interface{ OrgID() string })
	if !ok {
		return "", connect.NewError(connect.CodeInternal,
			errors.New("the tenant transaction does not name an organisation"))
	}
	return typed.OrgID(), nil
}
