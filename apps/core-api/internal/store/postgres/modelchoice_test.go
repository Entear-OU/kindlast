package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/modelchoice"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/stackenv"
)

// The model choice through the code path that will serve requests (ENT-236).
//
// The database suite already proves the invariants over the catalogue: the
// column-level grant, the partial unique index, the constraint that a revoked
// row holds no ciphertext, and isolation. What it cannot prove is that the
// store USES them correctly, and the failure modes there are quiet in the
// direction that matters.
//
// A `UseHostedModel` that inserted without revoking would be caught by the
// unique index and surface as an error. One that revoked without inserting
// would leave the organisation silently back on the deployment's own model
// while the console reported success, and every run after it would be
// processed somewhere other than the customer's record says.

func TestChoosingAProviderReplacesTheOneBeforeIt(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	// Rolled back rather than committed, so the fixture leaves nothing behind.
	// The whole test runs in one transaction, which is also how a request runs.
	defer tenant.Rollback(ctx)

	if _, err := tenant.ActiveModelChoice(ctx); !errors.Is(err, ErrNoModelChoice) {
		t.Fatalf("a fresh organisation reports %v, want ErrNoModelChoice", err)
	}

	first, entryID, err := tenant.UseHostedModel(ctx,
		"c0000000-0000-4000-8000-000000000001",
		"openai", "https://api.openai.com", "gpt-oss-120b", "1234",
		Sealed{Ciphertext: []byte("sealed-one"), KeyID: "2026-08"},
		modelchoice.ActionEnabled)
	if err != nil {
		t.Fatalf("choosing a provider: %v", err)
	}
	if entryID == "" {
		t.Fatal("no audit entry id came back, so nothing proves this was recorded")
	}
	if first.LastFour != "1234" {
		t.Fatalf("the stored hint is %q", first.LastFour)
	}

	// The switch. It has to revoke the first row and insert a second in one
	// transaction, or the partial unique index refuses it.
	second, secondEntry, err := tenant.UseHostedModel(ctx,
		"c0000000-0000-4000-8000-000000000002",
		"anthropic", "https://api.anthropic.com", "claude-x", "9999",
		Sealed{Ciphertext: []byte("sealed-two"), KeyID: "2026-08"},
		modelchoice.ActionChanged)
	if err != nil {
		t.Fatalf("switching provider: %v", err)
	}
	if secondEntry == entryID {
		t.Fatal("the switch reused the first decision's audit entry")
	}

	active, err := tenant.ActiveModelChoice(ctx)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if active.ID != second.ID || active.Provider != "anthropic" {
		t.Fatalf("the active choice is %+v", active)
	}

	// THE REVOKED ROW KEPT ITS PROVIDER AND LOST ITS KEY. Both halves matter:
	// the first is the history this table exists for, the second is the
	// constraint that stops a withdrawn credential sitting at rest.
	var provider string
	var ciphertext []byte
	err = tenant.Tx().QueryRow(ctx, `
		select provider, credential_ciphertext
		  from public.org_model_config
		 where id = $1`, first.ID).Scan(&provider, &ciphertext)
	if err != nil {
		t.Fatalf("reading the revoked row: %v", err)
	}
	if provider != "openai" {
		t.Fatalf("the revoked row now says %q, so the history is gone", provider)
	}
	if ciphertext != nil {
		t.Fatal("the revoked row still holds a sealed key")
	}
}

func TestTheAuditRowSaysWhatChangedAndCarriesNoCiphertext(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	if _, _, err := tenant.UseHostedModel(ctx,
		"c0000000-0000-4000-8000-000000000003",
		"openai", "https://api.openai.com", "gpt-oss-120b", "1234",
		Sealed{Ciphertext: []byte("sealed"), KeyID: "2026-08"},
		modelchoice.ActionEnabled); err != nil {
		t.Fatalf("choosing a provider: %v", err)
	}

	revertEntry, err := tenant.UseBundledModel(ctx)
	if err != nil {
		t.Fatalf("reverting: %v", err)
	}

	var actionType, before, after string
	err = tenant.Tx().QueryRow(ctx, `
		select action_type, coalesce(before::text, ''), coalesce(after::text, '')
		  from public.audit_log where id = $1`, revertEntry).Scan(&actionType, &before, &after)
	if err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}

	if actionType != modelchoice.ActionReverted {
		t.Fatalf("the reversal was recorded as %q", actionType)
	}
	if !strings.Contains(before, "openai") {
		t.Fatalf("the audit row does not say what was in place before: %s", before)
	}
	if after != "" {
		t.Fatalf("a reversal recorded an `after`: %s", after)
	}
	// `audit_log` is a table a customer exports and hands to somebody else, so
	// anything in it is something they have published.
	if strings.Contains(before, "sealed") || strings.Contains(before, "ciphertext") {
		t.Fatalf("the audit row carries the credential: %s", before)
	}

	if _, err := tenant.ActiveModelChoice(ctx); !errors.Is(err, ErrNoModelChoice) {
		t.Fatalf("after reverting the organisation reports %v", err)
	}

	// Reverting again is not an event and writes nothing.
	if _, err := tenant.UseBundledModel(ctx); !errors.Is(err, ErrNoModelChoice) {
		t.Fatalf("a second revert reported %v, want ErrNoModelChoice", err)
	}
}

// The producer pool's read, which is the one a run depends on.
//
// What it covers here is the two answers a narration job has to be able to tell
// apart: an organisation that made no choice, which is the default and not a
// failure, and an organisation id that is not one, which is a bad request
// rather than a cast error out of a policy.
//
// The grant itself is asserted in `db/tests/model-choice.test.ts`, over the
// catalogue and as the agent role, because a row this pool can read has to be
// committed and these tests deliberately commit nothing.
func TestTheProducerPoolResolvesTheEndpointAndTheSealedKey(t *testing.T) {
	ctx := t.Context()

	agent, err := NewAgent(ctx, stackenv.DSN("agent"))
	if err != nil {
		t.Skipf("the agent pool is not reachable (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", err)
	}
	t.Cleanup(agent.Close)

	if _, _, err := agent.ActiveModelChoiceForOrg(ctx, alphaOrg); !errors.Is(err, ErrNoModelChoice) {
		t.Fatalf("an organisation with no choice resolves to %v", err)
	}

	// A malformed organisation is a bad request rather than a cast error from
	// inside a policy, which reads as a server fault.
	if _, _, err := agent.ActiveModelChoiceForOrg(ctx, "not-a-uuid"); !errors.Is(err, ErrBadOrganisation) {
		t.Fatalf("a malformed org resolves to %v", err)
	}
}
