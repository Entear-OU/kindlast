package postgres

import "testing"

// The manual ROPA cap, in Go (ENT-225).
//
// These are the cases db/tests/self-hosted-ropa-cap.test.ts asserted against
// `ropa_manual_activity_limit()`. That function decided something and needed a
// third session GUC to do it, so 00016 dropped both and the decision moved
// here.
//
// The first case is the one that matters. 00013 exists because a self-hosted
// deployment, which bills nobody, was still capping manual Article 30 entries
// at three and refusing the fourth with a message about a plan it does not
// sell. That fix moved the billing fact into a GUC so a database function could
// read it: correct, in the wrong layer. This is the same behaviour with the
// layer straightened out, and the symptom if it regresses is identical, which
// is a self-hoster locked out of their own compliance record.
//
// # WHY THE READ PATH IS CHECKED BESIDE THE WRITE
//
// `ManualActivityQuota` is what the console renders; the limit is what a write
// meets. They came from one SQL function precisely so they could not disagree,
// and they now come from one Go method for the same reason. Each case below
// asserts both rather than trusting that.
//
// The refusal itself is covered by
// TestTheFreeTierCapIsReportedAsAQuotaRatherThanADenial, which fills the quota
// and goes one over.

func TestABillingDisabledDeploymentHasNoManualCap(t *testing.T) {
	// No WithBilling option at all, which is the default and the self-hosted
	// case.
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("opening a transaction: %v", err)
	}
	defer func() { _ = tenant.Rollback(ctx) }()

	limit, capped, err := tenant.manualActivityLimit(ctx)
	if err != nil {
		t.Fatalf("reading the limit: %v", err)
	}
	if capped {
		t.Fatalf("a deployment that bills nobody capped at %d", limit)
	}

	quota, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("reading the quota: %v", err)
	}
	if quota.Limit != 0 {
		t.Fatalf("the console would show a cap of %d on an unbilled deployment", quota.Limit)
	}
}

func TestAProOrganisationHasNoManualCapEvenWhenBillingIsOn(t *testing.T) {
	store := testStore(t, WithBilling(true))
	ctx := t.Context()

	// Alpha's fixture subscription is `pro`.
	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("opening a transaction: %v", err)
	}
	defer func() { _ = tenant.Rollback(ctx) }()

	limit, capped, err := tenant.manualActivityLimit(ctx)
	if err != nil {
		t.Fatalf("reading the limit: %v", err)
	}
	if capped {
		t.Fatalf("a pro organisation was capped at %d", limit)
	}

	quota, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("reading the quota: %v", err)
	}
	if quota.Limit != 0 {
		t.Fatalf("the console would show a cap of %d to a pro organisation", quota.Limit)
	}
}

func TestAFreeOrganisationIsCappedWhenBillingIsOn(t *testing.T) {
	store := testStore(t, WithBilling(true))
	ctx := t.Context()

	// Beta's fixture subscription is `free`.
	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("opening a transaction: %v", err)
	}
	defer func() { _ = tenant.Rollback(ctx) }()

	limit, capped, err := tenant.manualActivityLimit(ctx)
	if err != nil {
		t.Fatalf("reading the limit: %v", err)
	}
	if !capped {
		t.Fatal("a free organisation was not capped while billing is on; " +
			"check Beta's fixture subscription, because this test would then " +
			"pass without exercising anything")
	}
	if limit != FreeManualActivityLimit {
		t.Fatalf("limit is %d, want %d", limit, FreeManualActivityLimit)
	}

	quota, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("reading the quota: %v", err)
	}
	if quota.Limit != FreeManualActivityLimit {
		t.Fatalf("the console shows a cap of %d and the write enforces %d",
			quota.Limit, FreeManualActivityLimit)
	}
}
