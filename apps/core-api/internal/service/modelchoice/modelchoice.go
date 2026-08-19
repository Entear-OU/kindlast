// Package modelchoice serves ModelService: where an organisation's model runs
// (ENT-236, §26.6).
//
// # THE HANDLER IS WHERE THIS STOPS BEING A SETTINGS WRITE
//
// The schema makes the choice a record that keeps its history. What makes it a
// compliance EVENT rather than a row is here, and it is four checks in a fixed
// order, each of which refuses on its own:
//
//  1. The deployment permits this provider at all. An operator's list, and an
//     empty list refuses everybody including an owner.
//  2. The caller is an owner. A role threshold is a decision, so it is Go's
//     rather than a policy's (db/README.md).
//  3. The consequence has been acknowledged. Refused with the sentence itself
//     as the message, so the only way to make the change is to have been handed
//     the description of it.
//  4. The endpoint is really the provider's, and really outside. Resolved, not
//     parsed: see domain/modelchoice.
//
// Only then is the key sealed and the row written, and the audit entry's id
// comes back so a console can show the record rather than assert one exists.
//
// # NOTHING IN THIS PACKAGE EVER LOGS A KEY
//
// The key arrives in one field, is sealed, and is not held. There is no logger
// in this file, no error that wraps the request message, and the only thing
// derived from it that leaves is four characters.
package modelchoice

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// RevertNotice is what somebody is told when they go back to the bundled model.
//
// It says what reverting cannot do. A product that let an owner believe an off
// switch was a recall would be helping them tell a regulator something untrue.
const RevertNotice = "Turning this off stops anything further leaving this deployment and " +
	"destroys the stored key. It cannot reach content the provider has already processed, " +
	"which stays subject to your agreement with them. Your audit log and the provider " +
	"recorded against each agent run are what bound the period this applies to."

// store is what these handlers need of the request's transaction, declared
// where it is used (§21.6).
type store interface {
	Role() string
	UserID() string
	ActiveModelChoice(ctx context.Context) (postgres.Choice, error)
	UseHostedModel(ctx context.Context, id, provider, baseURL, model, lastFour string,
		sealed postgres.Sealed, actionType string) (postgres.Choice, string, error)
	UseBundledModel(ctx context.Context) (string, error)
}

// Service implements corev1connect.ModelServiceHandler.
type Service struct {
	providers []domain.Provider
	keys      *secrets.Keyring
	lookup    domain.Lookup
}

// New builds the service.
//
// `providers` is the operator's allow-list and an empty one is the default: a
// deployment that has said nothing about hosted models permits none, which is
// what keeps "this stack can run with no outbound internet" a property rather
// than a setting somebody has to remember.
//
// `lookup` is injected so the SSRF check is exercised without a network and so
// the resolver in play is the caller's choice rather than a package's.
func New(providers []domain.Provider, keys *secrets.Keyring, lookup domain.Lookup) *Service {
	if lookup == nil {
		lookup = domain.SystemLookup
	}
	return &Service{providers: providers, keys: keys, lookup: lookup}
}

func (s *Service) GetModelSetting(
	ctx context.Context,
	_ *connect.Request[corev1.GetModelSettingRequest],
) (*connect.Response[corev1.GetModelSettingResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	setting, err := s.currentSetting(ctx, tenant)
	if err != nil {
		return nil, err
	}

	response := &corev1.GetModelSettingResponse{
		Setting:           setting,
		ConsequenceNotice: domain.ConsequenceNotice,
		RevertNotice:      RevertNotice,
	}
	for _, provider := range s.providers {
		response.PermittedProviders = append(response.PermittedProviders,
			&corev1.PermittedProvider{Name: provider.Name, Host: provider.Host})
	}
	return connect.NewResponse(response), nil
}

func (s *Service) UseHostedModel(
	ctx context.Context,
	request *connect.Request[corev1.UseHostedModelRequest],
) (*connect.Response[corev1.UseHostedModelResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}

	// THE OPERATOR'S DECISION IS CHECKED FIRST, before the role and before the
	// acknowledgement. A deployment that permits nothing should say so rather
	// than telling a member they are not an owner, which would imply that an
	// owner could.
	provider, err := domain.Permitted(s.providers, request.Msg.GetProvider())
	if err != nil {
		if len(s.providers) == 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(
				"this deployment permits no hosted model providers, so every organisation "+
					"here uses the model it runs itself; an operator sets KINDLAST_BYOK_PROVIDERS"))
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}

	if err := requireOwner(tenant); err != nil {
		return nil, err
	}
	if !request.Msg.GetAcknowledgeConsequence() {
		// The sentence itself, not a pointer to it. A caller that has to fetch
		// the notice separately is one that can skip reading it and still send
		// the flag; a caller that is handed it in the refusal cannot.
		// The full stop is trimmed because Go error strings do not carry one and
		// the linter says so. The sentence is the notice, unchanged otherwise.
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New(strings.TrimSuffix(domain.ConsequenceNotice, ".")))
	}

	if request.Msg.GetModel() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(
			"a hosted provider needs the name of a model to ask for"))
	}
	if err := domain.ValidateEndpoint(ctx, request.Msg.GetBaseUrl(), provider, s.lookup); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	previous, err := tenant.ActiveModelChoice(ctx)
	switch {
	case err == nil, errors.Is(err, postgres.ErrNoModelChoice):
	default:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	actionType := actionFor(previous, err == nil, provider.Name)

	// The row id is minted here because the seal binds it in as additional
	// authenticated data, so a ciphertext lifted out of one organisation's row
	// fails to open in another's rather than opening as somebody else's key.
	id := uuid.NewString()

	var sealed postgres.Sealed
	if key := request.Msg.GetApiKey(); key != "" {
		ciphertext, keyID, sealErr := s.keys.Seal(key, id)
		if sealErr != nil {
			if errors.Is(sealErr, secrets.ErrNoKey) {
				// REFUSED, NOT STORED IN THE CLEAR. The failure mode of "we
				// could not encrypt it so we kept it as it was" is the one that
				// ends up in a breach notification.
				return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New(
					"this deployment has no KINDLAST_INTEGRATION_KEY, so it cannot store a "+
						"provider key; an operator sets one before this can be turned on"))
			}
			return nil, connect.NewError(connect.CodeInternal, sealErr)
		}
		sealed = postgres.Sealed{Ciphertext: ciphertext, KeyID: keyID}
	}

	choice, entryID, err := tenant.UseHostedModel(ctx, id,
		provider.Name, request.Msg.GetBaseUrl(), request.Msg.GetModel(),
		domain.LastFour(request.Msg.GetApiKey()), sealed, actionType)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.UseHostedModelResponse{
		Setting:      toProto(choice, true),
		AuditEntryId: entryID,
	}), nil
}

func (s *Service) UseBundledModel(
	ctx context.Context,
	_ *connect.Request[corev1.UseBundledModelRequest],
) (*connect.Response[corev1.UseBundledModelResponse], error) {
	tenant, err := tenantFrom(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireOwner(tenant); err != nil {
		return nil, err
	}

	// NO ACKNOWLEDGEMENT ON THE WAY BACK. Stopping data leaving is not a
	// decision anybody needs protecting from, and a confirmation step on the
	// safe direction is friction that makes the unsafe one look equivalent.
	entryID, err := tenant.UseBundledModel(ctx)
	if errors.Is(err, postgres.ErrNoModelChoice) {
		// Already on the bundled model. Not an error, and deliberately not an
		// audit row either: writing one would record a decision nobody made.
		return connect.NewResponse(&corev1.UseBundledModelResponse{
			Setting: &corev1.ModelSetting{Hosted: false},
		}), nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&corev1.UseBundledModelResponse{
		Setting:      &corev1.ModelSetting{Hosted: false},
		AuditEntryId: entryID,
	}), nil
}

func (s *Service) currentSetting(ctx context.Context, tenant store) (*corev1.ModelSetting, error) {
	choice, err := tenant.ActiveModelChoice(ctx)
	if errors.Is(err, postgres.ErrNoModelChoice) {
		return &corev1.ModelSetting{Hosted: false}, nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return toProto(choice, true), nil
}

// actionFor names what happened, from what was there before.
func actionFor(previous postgres.Choice, had bool, provider string) string {
	switch {
	case !had:
		return domain.ActionEnabled
	case previous.Provider == provider:
		return domain.ActionRotated
	default:
		return domain.ActionChanged
	}
}

func toProto(choice postgres.Choice, hosted bool) *corev1.ModelSetting {
	setting := &corev1.ModelSetting{
		Hosted:             hosted,
		Provider:           choice.Provider,
		BaseUrl:            choice.BaseURL,
		Model:              choice.Model,
		CredentialLastFour: choice.LastFour,
		ChangedByUserId:    choice.ChangedBy,
	}
	if !choice.ChangedAt.IsZero() {
		setting.ChangedAt = timestamppb.New(choice.ChangedAt)
	}
	return setting
}

// requireOwner is the role threshold, in Go because it is a decision.
//
// `permission_denied` rather than `not_found`, unlike a slug the caller does
// not belong to. The caller is a member of this organisation and is entitled to
// know the setting exists; what they are not entitled to do is change it, and
// saying so sends them to an owner who can.
func requireOwner(tenant store) error {
	if tenant.Role() != "owner" {
		return connect.NewError(connect.CodePermissionDenied, errors.New(
			"only an owner can change where this organisation's compliance data is processed"))
	}
	return nil
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
			fmt.Errorf("the tenant transaction cannot reach the model choice"))
	}
	return typed, nil
}
