// Package apikeys serves ApiKeyService: the credentials a partner's software
// calls with (ENT-262, §23).
//
// # THE HANDLER IS WHERE THIS STOPS BEING A ROW AND BECOMES ACCESS
//
// The schema makes a key a record with a digest and a revocation column. What
// makes minting one a decision rather than an insert is here, and it is three
// checks in a fixed order, each of which refuses on its own:
//
//  1. The scope interceptor has already required `org:manage`, which bounds
//     what the CLIENT may do.
//  2. The caller is an owner. A role threshold is a decision, so it is Go's
//     rather than a policy's (db/README.md), and it bounds which PERSON may.
//  3. The scopes asked for are ones a key may carry. `internal:*` is refused
//     here for a readable message and refused again by a CHECK constraint,
//     which is what actually binds.
//
// Only then is the key minted, and the credential comes back once.
//
// # NOTHING IN THIS PACKAGE EVER LOGS A CREDENTIAL
//
// The credential exists in one local variable, goes into one response field,
// and is not held. There is no logger in this file and no error that wraps the
// value.
package apikeys

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/apikey"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/org"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// store is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type store interface {
	Role() string
	APIKeys(ctx context.Context) ([]postgres.APIKey, error)
	MintAPIKey(ctx context.Context, mint apikey.Mint) (postgres.APIKey, string, error)
	RevokeAPIKey(ctx context.Context, id string) (postgres.APIKey, error)
}

// Service implements corev1connect.ApiKeyServiceHandler.
//
// No fields. Everything it needs arrives on the request's transaction, which is
// what having no configuration to get wrong looks like.
type Service struct{}

// New builds the service.
func New() *Service { return &Service{} }

func (s *Service) ListApiKeys(
	ctx context.Context,
	_ *connect.Request[corev1.ListApiKeysRequest],
) (*connect.Response[corev1.ListApiKeysResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	keys, err := tenant.APIKeys(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	response := &corev1.ListApiKeysResponse{Keys: make([]*corev1.ApiKey, 0, len(keys))}
	for _, key := range keys {
		response.Keys = append(response.Keys, asProto(key))
	}
	return connect.NewResponse(response), nil
}

func (s *Service) CreateApiKey(
	ctx context.Context,
	request *connect.Request[corev1.CreateApiKeyRequest],
) (*connect.Response[corev1.CreateApiKeyResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(tenant); err != nil {
		return nil, err
	}

	// A KEY MAY NOT MINT A KEY, AND THIS IS THE SECOND PLACE THAT IS TRUE.
	//
	// The first is `apikey.GrantableScopes`, which contains no `org:manage`, so
	// a key can never hold the scope this RPC requires and the scope
	// interceptor would already have refused. This check exists because
	// "unreachable because a list does not contain an entry" is exactly the
	// kind of guarantee that stops being true when somebody edits the list, and
	// what it guards is a credential extending its own access with no human in
	// the loop. Two independent refusals for the one property worth two.
	if _, isKey := interceptor.APIKeyFrom(ctx); isKey {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("an API key cannot mint another API key"))
	}

	mint := apikey.Mint{
		Name:   request.Msg.GetName(),
		Scopes: request.Msg.GetScopes(),
	}
	if _, err := mint.Validate(); err != nil {
		// InvalidArgument rather than PermissionDenied. The caller is entitled
		// to mint keys; they asked for one that cannot exist, and the message
		// says which part.
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	key, credential, err := tenant.MintAPIKey(ctx, mint)
	if err != nil {
		if errors.Is(err, apikey.ErrNoScopes) || errors.Is(err, apikey.ErrBadName) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.CreateApiKeyResponse{
		Key:        asProto(key),
		Credential: credential,
	}), nil
}

func (s *Service) RevokeApiKey(
	ctx context.Context,
	request *connect.Request[corev1.RevokeApiKeyRequest],
) (*connect.Response[corev1.RevokeApiKeyResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(tenant); err != nil {
		return nil, err
	}

	key, err := tenant.RevokeAPIKey(ctx, request.Msg.GetKeyId())
	if err != nil {
		if errors.Is(err, postgres.ErrNoSuchAPIKey) {
			// NotFound covers "no such key", "already revoked" and "somebody
			// else's key", because the store cannot tell them apart and should
			// not: distinguishing them would make this a way to ask which key
			// ids are real.
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.RevokeApiKeyResponse{Key: asProto(key)}), nil
}

// requireOwner is the role threshold, and it is the second of three checks.
//
// The scope interceptor has already asked whether this client may touch keys at
// all. RLS will refuse any row outside the caller's organisation whatever
// happens here. This is the middle question, which neither of the other two can
// express: whether this PERSON, in this organisation, may hand out a credential
// that reaches their colleagues' compliance record.
//
// Owner rather than member, and revoke is owner too. Revoking is destructive to
// whatever integration was using the key, so a viewer who spotted a problem
// should raise it rather than break a partner's nightly export.
func requireOwner(tenant store) error {
	if tenant.Role() != org.RoleOwner {
		return connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("managing API keys is for owners, and you are %q", tenant.Role()))
	}
	return nil
}

func asProto(key postgres.APIKey) *corev1.ApiKey {
	out := &corev1.ApiKey{
		Id:        key.ID,
		Handle:    key.Handle,
		Name:      key.Name,
		Scopes:    key.Scopes,
		CreatedBy: key.CreatedBy,
		CreatedAt: timestamppb.New(key.CreatedAt),
		RevokedBy: key.RevokedBy,
	}
	if key.LastUsedAt != nil {
		out.LastUsedAt = timestamppb.New(*key.LastUsedAt)
	}
	if key.RevokedAt != nil {
		out.RevokedAt = timestamppb.New(*key.RevokedAt)
	}
	return out
}

func tenantFrom(ctx context.Context) (store, error) {
	// Either a verified identity or an authenticated key. Unlike every other
	// service on this surface, this one is reachable on a key: ListApiKeys is
	// `org:read`, which a key may carry, so a partner can check whether the
	// credential they hold is still live and which scopes it has.
	_, human := interceptor.ClaimsFrom(ctx)
	_, key := interceptor.APIKeyFrom(ctx)
	if !human && !key {
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
			errors.New("the tenant transaction cannot reach the API keys"))
	}
	return typed, nil
}
