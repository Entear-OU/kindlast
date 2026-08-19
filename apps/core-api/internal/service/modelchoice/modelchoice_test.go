package modelchoice_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"connectrpc.com/connect"

	domain "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/secrets"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/server/interceptor"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/service/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
	"github.com/Entear-OU/kindlast/libs/chassis/oidc"
)

// fakeStore stands in for the request's transaction.
//
// It records what it was asked to write, which is how the tests below assert
// that the key never reaches a column in the clear and that the action type
// tells "enabled" from "changed" from "rotated".
type fakeStore struct {
	role   string
	userID string

	active    postgres.Choice
	hasActive bool

	wroteProvider   string
	wroteBaseURL    string
	wroteModel      string
	wroteLastFour   string
	wroteSealed     postgres.Sealed
	wroteActionType string
	reverted        bool
}

func (f *fakeStore) OrgID() string  { return "org-1" }
func (f *fakeStore) Role() string   { return f.role }
func (f *fakeStore) UserID() string { return f.userID }

func (f *fakeStore) Commit(context.Context) error   { return nil }
func (f *fakeStore) Rollback(context.Context) error { return nil }

func (f *fakeStore) ActiveModelChoice(context.Context) (postgres.Choice, error) {
	if !f.hasActive {
		return postgres.Choice{}, postgres.ErrNoModelChoice
	}
	return f.active, nil
}

func (f *fakeStore) UseHostedModel(
	_ context.Context,
	id, provider, baseURL, model, lastFour string,
	sealed postgres.Sealed,
	actionType string,
) (postgres.Choice, string, error) {
	f.wroteProvider = provider
	f.wroteBaseURL = baseURL
	f.wroteModel = model
	f.wroteLastFour = lastFour
	f.wroteSealed = sealed
	f.wroteActionType = actionType
	f.active = postgres.Choice{
		ID: id, Provider: provider, BaseURL: baseURL, Model: model, LastFour: lastFour,
	}
	f.hasActive = true
	return f.active, "audit-entry-1", nil
}

func (f *fakeStore) UseBundledModel(context.Context) (string, error) {
	if !f.hasActive {
		return "", postgres.ErrNoModelChoice
	}
	f.reverted = true
	f.hasActive = false
	return "audit-entry-2", nil
}

func withTenant(store *fakeStore) context.Context {
	ctx := interceptor.WithClaims(context.Background(), &oidc.Claims{Subject: "ada"})
	return interceptor.WithTenant(ctx, store)
}

func publicLookup(_ context.Context, _ string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("104.18.7.192")}, nil
}

func keyring(t *testing.T) *secrets.Keyring {
	t.Helper()
	ring, err := secrets.NewKeyring("2026-08:MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatalf("building a keyring: %v", err)
	}
	return ring
}

func service(t *testing.T, spec string) *modelchoice.Service {
	t.Helper()
	providers, err := domain.ParseProviders(spec)
	if err != nil {
		t.Fatalf("parsing providers: %v", err)
	}
	return modelchoice.New(providers, keyring(t), publicLookup)
}

func hostRequest() *connect.Request[corev1.UseHostedModelRequest] {
	return connect.NewRequest(&corev1.UseHostedModelRequest{
		Provider:               "openai",
		BaseUrl:                "https://api.openai.com",
		Model:                  "gpt-oss-120b",
		ApiKey:                 "sk-proj-abcdefgh1234",
		AcknowledgeConsequence: true,
	})
}

func code(err error) connect.Code {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Code()
	}
	return connect.CodeUnknown
}

// THE DEFAULT IS THAT NOTHING LEAVES, AND IT IS NOT A UI DEFAULT.
//
// A deployment that has configured no providers refuses this whatever anybody
// asks for, including an owner. That is the "nobody here may point our
// compliance data at an external API" the operator has to be able to enforce.
func TestADeploymentThatPermitsNoProviderRefusesEveryone(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	svc := service(t, "")

	_, err := svc.UseHostedModel(withTenant(store), hostRequest())
	if err == nil {
		t.Fatal("an unconfigured deployment accepted a hosted model")
	}
	if code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code is %v, want failed_precondition: %v", code(err), err)
	}
	if store.wroteProvider != "" {
		t.Fatal("something was written")
	}
}

func TestOnlyAnOwnerMayChangeWhereTheDataGoes(t *testing.T) {
	for _, role := range []string{"admin", "member", "viewer"} {
		store := &fakeStore{role: role, userID: "u1"}
		svc := service(t, "openai=api.openai.com")

		_, err := svc.UseHostedModel(withTenant(store), hostRequest())
		if code(err) != connect.CodePermissionDenied {
			t.Errorf("%s got %v, want permission_denied", role, code(err))
		}

		_, err = svc.UseBundledModel(withTenant(store),
			connect.NewRequest(&corev1.UseBundledModelRequest{}))
		if code(err) != connect.CodePermissionDenied {
			t.Errorf("%s reverting got %v, want permission_denied", role, code(err))
		}
	}
}

// EVERY MEMBER MAY ASK WHERE THE DATA GOES. A product that answers that only
// for the person who can change it is one nobody else can check.
func TestAnyMemberMayReadTheSetting(t *testing.T) {
	store := &fakeStore{role: "viewer", userID: "u1"}
	svc := service(t, "openai=api.openai.com")

	response, err := svc.GetModelSetting(withTenant(store),
		connect.NewRequest(&corev1.GetModelSettingRequest{}))
	if err != nil {
		t.Fatalf("a viewer could not read the setting: %v", err)
	}
	if response.Msg.GetSetting().GetHosted() {
		t.Fatal("an organisation with no choice reads as hosted")
	}
	if response.Msg.GetConsequenceNotice() != domain.ConsequenceNotice {
		t.Fatal("the consequence notice is not the one the domain package holds")
	}
	if len(response.Msg.GetPermittedProviders()) != 1 {
		t.Fatalf("permitted providers: %v", response.Msg.GetPermittedProviders())
	}
}

// THE ACKNOWLEDGEMENT IS A CONTROL, NOT A PROMPT. Without it there is no way to
// show the person was told, which is the whole difference between a compliance
// event and a settings write.
func TestAnUnacknowledgedChangeIsRefusedWithTheConsequence(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	svc := service(t, "openai=api.openai.com")

	request := hostRequest()
	request.Msg.AcknowledgeConsequence = false

	_, err := svc.UseHostedModel(withTenant(store), request)
	if code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code is %v, want failed_precondition", code(err))
	}
	if !strings.Contains(err.Error(), "sub-processor") {
		t.Fatalf("the refusal does not state the consequence: %v", err)
	}
	if store.wroteProvider != "" {
		t.Fatal("something was written")
	}
}

func TestTheKeyIsSealedBeforeItReachesTheStore(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	svc := service(t, "openai=api.openai.com")

	response, err := svc.UseHostedModel(withTenant(store), hostRequest())
	if err != nil {
		t.Fatalf("a permitted provider was refused: %v", err)
	}

	if len(store.wroteSealed.Ciphertext) == 0 {
		t.Fatal("nothing was sealed")
	}
	if strings.Contains(string(store.wroteSealed.Ciphertext), "sk-proj") {
		t.Fatal("the key reached the store in the clear")
	}
	if store.wroteLastFour != "1234" {
		t.Fatalf("the hint is %q", store.wroteLastFour)
	}

	// AND IT NEVER COMES BACK. The response carries the hint and nothing else.
	setting := response.Msg.GetSetting()
	if setting.GetCredentialLastFour() != "1234" {
		t.Fatalf("the response hint is %q", setting.GetCredentialLastFour())
	}
	if strings.Contains(setting.String(), "sk-proj") {
		t.Fatalf("the response carries the key: %s", setting.String())
	}
	if response.Msg.GetAuditEntryId() == "" {
		t.Fatal("no audit entry was returned, so nothing proves this was recorded")
	}
}

// THREE DIFFERENT THINGS HAPPENED, AND THE RECORD HAS TO SAY WHICH.
func TestTheActionTypeSaysWhatActuallyChanged(t *testing.T) {
	svc := service(t, "openai=api.openai.com,anthropic=api.anthropic.com")

	fresh := &fakeStore{role: "owner", userID: "u1"}
	if _, err := svc.UseHostedModel(withTenant(fresh), hostRequest()); err != nil {
		t.Fatalf("enabling: %v", err)
	}
	if fresh.wroteActionType != "model_provider_enabled" {
		t.Fatalf("first change recorded as %q", fresh.wroteActionType)
	}

	// Same provider, new key.
	rotate := hostRequest()
	rotate.Msg.ApiKey = "sk-proj-zzzzzzzz9999"
	if _, err := svc.UseHostedModel(withTenant(fresh), rotate); err != nil {
		t.Fatalf("rotating: %v", err)
	}
	if fresh.wroteActionType != "model_provider_rotated" {
		t.Fatalf("a rotation recorded as %q", fresh.wroteActionType)
	}

	// Another provider entirely.
	moved := hostRequest()
	moved.Msg.Provider = "anthropic"
	moved.Msg.BaseUrl = "https://api.anthropic.com"
	if _, err := svc.UseHostedModel(withTenant(fresh), moved); err != nil {
		t.Fatalf("changing: %v", err)
	}
	if fresh.wroteActionType != "model_provider_changed" {
		t.Fatalf("a change of provider recorded as %q", fresh.wroteActionType)
	}
}

func TestAnEndpointThatIsNotTheProvidersIsRefused(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	svc := service(t, "openai=api.openai.com")

	request := hostRequest()
	request.Msg.BaseUrl = "https://evil.example.com"

	_, err := svc.UseHostedModel(withTenant(store), request)
	if code(err) != connect.CodeInvalidArgument {
		t.Fatalf("code is %v, want invalid_argument: %v", code(err), err)
	}
	if store.wroteProvider != "" {
		t.Fatal("something was written")
	}
}

func TestAnEndpointInsideTheDeploymentIsRefused(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	inside := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	providers, err := domain.ParseProviders("openai=api.openai.com")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	svc := modelchoice.New(providers, keyring(t), inside)

	_, err = svc.UseHostedModel(withTenant(store), hostRequest())
	if code(err) != connect.CodeInvalidArgument {
		t.Fatalf("code is %v, want invalid_argument: %v", code(err), err)
	}
	if store.wroteProvider != "" {
		t.Fatal("something was written")
	}
}

// A DEPLOYMENT WITH NO SEALING KEY REFUSES RATHER THAN STORING PLAINTEXT.
func TestWithoutASealingKeyAKeyedProviderIsRefused(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	providers, err := domain.ParseProviders("openai=api.openai.com")
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	empty, err := secrets.NewKeyring("")
	if err != nil {
		t.Fatalf("empty keyring: %v", err)
	}
	svc := modelchoice.New(providers, empty, publicLookup)

	_, err = svc.UseHostedModel(withTenant(store), hostRequest())
	if code(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code is %v, want failed_precondition: %v", code(err), err)
	}
	if store.wroteProvider != "" {
		t.Fatal("something was written")
	}
}

func TestRevertingAnOrganisationThatNeverLeftIsNotAnEvent(t *testing.T) {
	store := &fakeStore{role: "owner", userID: "u1"}
	svc := service(t, "openai=api.openai.com")

	response, err := svc.UseBundledModel(withTenant(store),
		connect.NewRequest(&corev1.UseBundledModelRequest{}))
	if err != nil {
		t.Fatalf("reverting an organisation on the bundled model failed: %v", err)
	}
	if response.Msg.GetAuditEntryId() != "" {
		t.Fatal("an audit entry was written for a decision nobody made")
	}
	if store.reverted {
		t.Fatal("something was revoked")
	}
}
