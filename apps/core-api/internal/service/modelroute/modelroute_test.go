package modelroute

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
)

// Where an organisation's completions go (ENT-236, ENT-256 part five). The
// one property every test here circles is that a choice which cannot be
// honoured is an error and never the instance route: a silent fallback would
// process a customer's findings somewhere other than where their own record of
// processing says, and nothing in the product would say it happened.

type fakeChoices struct {
	choice postgres.Choice
	sealed postgres.Sealed
	err    error
}

func (c *fakeChoices) ActiveModelChoiceForOrg(context.Context, string) (postgres.Choice, postgres.Sealed, error) {
	return c.choice, c.sealed, c.err
}

func testKeyring(t *testing.T) *secrets.Keyring {
	t.Helper()
	ring, err := secrets.NewKeyring("2026-08:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("building a keyring: %v", err)
	}
	return ring
}

func publicLookup(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("104.18.7.192")}, nil
}

const rowID = "c0000000-0000-4000-8000-000000000001"

func openai(t *testing.T) []modelchoice.Provider {
	t.Helper()
	providers, err := modelchoice.ParseProviders("openai=api.openai.com")
	if err != nil {
		t.Fatalf("parsing providers: %v", err)
	}
	return providers
}

func TestAChosenProviderResolvesWithItsKeyOpened(t *testing.T) {
	keys := testKeyring(t)
	sealed, keyID, err := keys.Seal("sk-proj-abcdefgh1234", rowID)
	if err != nil {
		t.Fatalf("sealing: %v", err)
	}
	r := New("http://model:8080", "local").WithModelChoice(&fakeChoices{
		choice: postgres.Choice{ID: rowID, Provider: "openai", BaseURL: "https://api.openai.com", Model: "gpt-oss-120b"},
		sealed: postgres.Sealed{Ciphertext: sealed, KeyID: keyID},
	}, keys, openai(t), publicLookup)

	route, err := r.Resolve(t.Context(), "org")
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if route.Provider != "openai" || route.BaseURL != "https://api.openai.com" || route.Model != "gpt-oss-120b" {
		t.Fatalf("route = %+v", route)
	}
	if route.APIKey != "sk-proj-abcdefgh1234" {
		t.Fatal("the sealed key did not open, so the call would reach the provider unauthenticated")
	}
	if route.Instance() {
		t.Fatal("a chosen provider reads as the instance model")
	}
}

func TestNoChoiceIsTheInstanceModel(t *testing.T) {
	r := New("http://model:8080", "local").WithModelChoice(
		&fakeChoices{err: postgres.ErrNoModelChoice}, testKeyring(t), openai(t), publicLookup)
	route, err := r.Resolve(t.Context(), "org")
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if !route.Instance() || route.BaseURL != "http://model:8080" || route.Model != "local" || route.APIKey != "" {
		t.Fatalf("route = %+v, want the instance model with no key", route)
	}
}

func TestNoChoiceAndNoInstanceModelIsARefusal(t *testing.T) {
	_, err := New("", "").Resolve(t.Context(), "org")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("err = %v, want ErrNoModel", err)
	}
}

// THE ONE THAT MUST NOT BE A SILENT FALLBACK.
func TestAWithdrawnProviderIsAnErrorAndNeverTheInstanceModel(t *testing.T) {
	r := New("http://model:8080", "local").WithModelChoice(&fakeChoices{
		choice: postgres.Choice{ID: rowID, Provider: "openai", BaseURL: "https://api.openai.com", Model: "m"},
	}, testKeyring(t), nil /* everything withdrawn */, publicLookup)
	if _, err := r.Resolve(t.Context(), "org"); err == nil {
		t.Fatal("a withdrawn provider resolved anyway")
	}
}

func TestAnEndpointResolvingInsideTheDeploymentIsAnError(t *testing.T) {
	inside := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	r := New("http://model:8080", "local").WithModelChoice(&fakeChoices{
		choice: postgres.Choice{ID: rowID, Provider: "openai", BaseURL: "https://api.openai.com", Model: "m"},
	}, testKeyring(t), openai(t), inside)
	if _, err := r.Resolve(t.Context(), "org"); err == nil {
		t.Fatal("an endpoint resolving inside the deployment was routed to")
	}
}

func TestAKeyThatWillNotOpenIsAnErrorRatherThanAnUnauthenticatedCall(t *testing.T) {
	r := New("http://model:8080", "local").WithModelChoice(&fakeChoices{
		choice: postgres.Choice{ID: rowID, Provider: "openai", BaseURL: "https://api.openai.com", Model: "m"},
		sealed: postgres.Sealed{Ciphertext: []byte("not a ciphertext"), KeyID: "2026-08"},
	}, testKeyring(t), openai(t), publicLookup)
	if _, err := r.Resolve(t.Context(), "org"); err == nil {
		t.Fatal("a key that will not open resolved to a route")
	}
}
