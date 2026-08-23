// Package modelroute answers one question for one organisation: where do its
// model calls go, and with what credential (ENT-236, ENT-256 part five).
//
// Extracted from the narrative service, where it first lived, because the
// answer now has two callers: NarrateFindings, which needs to know before it
// starts a batch that an organisation's provider can still be honoured, and
// CompletionService, which makes the call. One resolver, so the two cannot
// disagree about whose key an organisation's prompt is sent with.
package modelroute

import (
	"context"
	"errors"
	"fmt"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
)

// Choices reads which provider an organisation chose, and its sealed key.
type Choices interface {
	ActiveModelChoiceForOrg(ctx context.Context, orgID string) (postgres.Choice, postgres.Sealed, error)
}

// Route is where one organisation's completions go.
type Route struct {
	// Provider is the name the run record carries: `instance` for the
	// deployment's own model, otherwise the chosen provider.
	Provider string
	BaseURL  string
	// Model is the name on the wire. The deployment's own endpoint ignores
	// it; a hosted provider requires it.
	Model string
	// APIKey is the opened credential, or empty. It exists in memory for the
	// life of one call and is written nowhere; see CompletionService.
	APIKey string
}

// Instance reports whether this is the deployment's own model rather than an
// organisation's choice.
func (r Route) Instance() bool { return r.Provider == ProviderInstance }

// ProviderInstance names the deployment's own model in a run record.
const ProviderInstance = "instance"

// Resolver decides the route for an organisation.
type Resolver struct {
	// The deployment's own model, used by every organisation that has made
	// no choice. Empty means this deployment runs no model.
	instanceURL   string
	instanceModel string

	choices   Choices
	keys      *secrets.Keyring
	providers []modelchoice.Provider
	lookup    modelchoice.Lookup
}

// New builds a resolver for a deployment whose own model answers at
// `instanceURL` (empty for none).
func New(instanceURL, instanceModel string) *Resolver {
	return &Resolver{instanceURL: instanceURL, instanceModel: instanceModel}
}

// WithModelChoice makes the resolver honour an organisation's chosen provider.
//
// A nil store or a nil keyring is treated as absent, because honouring a choice
// means reading it and opening a sealed key, and doing either without the other
// is not a degraded version of this feature but a broken one.
//
// AN EMPTY PROVIDER LIST IS NOT ABSENT, AND THAT IS THE SUBTLE ONE. The obvious
// reading is that a deployment permitting nothing has no work for this to do,
// so it should not be wired. That is wrong in the one direction that matters:
// an operator can withdraw the last provider while an organisation still has a
// row, and an unwired resolver would then route that organisation to the
// deployment's own model, silently, with nothing saying its choice had stopped
// being honoured. Wired with an empty list, the same case fails loudly, because
// `Permitted` refuses every name.
func (r *Resolver) WithModelChoice(
	choices Choices,
	keys *secrets.Keyring,
	providers []modelchoice.Provider,
	lookup modelchoice.Lookup,
) *Resolver {
	if choices == nil || keys == nil {
		return r
	}
	if lookup == nil {
		lookup = modelchoice.SystemLookup
	}
	r.choices, r.keys, r.providers, r.lookup = choices, keys, providers, lookup
	return r
}

// ErrNoModel is returned when an organisation has made no choice and the
// deployment runs no model of its own.
var ErrNoModel = errors.New("modelroute: this deployment runs no model and the organisation has chosen no provider")

// Resolve returns where this organisation's completions go.
//
// # A REFUSAL HERE FAILS THE CALL RATHER THAN FALLING BACK
//
// Every error path below returns an error and none returns the instance route
// in its place. Falling back to the deployment's own model when an
// organisation's chosen provider cannot be honoured would mean processing that
// organisation's findings somewhere other than where its own record of
// processing says they are processed, quietly, with nothing in the product
// saying it happened. A call that stops and says why is recoverable; one that
// silently processes elsewhere is a disclosure nobody can date.
func (r *Resolver) Resolve(ctx context.Context, orgID string) (Route, error) {
	if r.choices != nil {
		choice, sealed, err := r.choices.ActiveModelChoiceForOrg(ctx, orgID)
		switch {
		case errors.Is(err, postgres.ErrNoModelChoice):
			// No choice: the instance route below.
		case err != nil:
			return Route{}, err
		default:
			return r.chosen(ctx, choice, sealed)
		}
	}
	if r.instanceURL == "" {
		return Route{}, ErrNoModel
	}
	return Route{Provider: ProviderInstance, BaseURL: r.instanceURL, Model: r.instanceModel}, nil
}

func (r *Resolver) chosen(ctx context.Context, choice postgres.Choice, sealed postgres.Sealed) (Route, error) {
	// RE-CHECKED, NOT TRUSTED BECAUSE IT WAS CHECKED WHEN IT WAS WRITTEN. The
	// allow-list is deployment configuration, so a provider an operator has
	// withdrawn has to stop being reachable for organisations that already
	// chose it, and an endpoint that has since started resolving inside the
	// deployment has to stop being dialled. 00025 makes the same argument about
	// a connection endpoint.
	provider, err := modelchoice.Permitted(r.providers, choice.Provider)
	if err != nil {
		return Route{}, fmt.Errorf("this organisation model provider is no longer permitted here: %w", err)
	}
	if err := modelchoice.ValidateEndpoint(ctx, choice.BaseURL, provider, r.lookup); err != nil {
		return Route{}, fmt.Errorf("this organisation model endpoint can no longer be reached safely: %w", err)
	}

	route := Route{Provider: choice.Provider, BaseURL: choice.BaseURL, Model: choice.Model}
	if len(sealed.Ciphertext) > 0 {
		key, err := r.keys.Open(sealed.Ciphertext, sealed.KeyID, choice.ID)
		if err != nil {
			// A key this deployment can no longer open. Refused rather than
			// dialled without one, because an unauthenticated request to a
			// hosted provider fails at the far end and takes the customer
			// signal with it on the way.
			return Route{}, fmt.Errorf("this organisation provider key cannot be opened: %w", err)
		}
		route.APIKey = key
	}
	return route, nil
}
