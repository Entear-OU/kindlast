package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The write path, and above all the error mapping.
//
// # WHY EVERY GATE HAS A TEST THAT PROVOKES THE REAL ERROR
//
// `classify` distinguishes the free-tier cap from the reviewed-approval
// requirement by a marker in the message, because the database raises
// `check_violation` for both. That is the fragile part of the design and these
// tests are the reason it is acceptable: a reworded message turns one of them
// red rather than silently remapping a customer's upgrade prompt into a confirm
// dialog, which is a failure nobody would notice from the outside.
//
// A test that asserted `classify` against a hand-built PgError would pass
// forever while the function's message drifted underneath it. So every one of
// these calls the real function and lets it raise.
//
// All of this runs inside a transaction that is rolled back, so it leaks
// nothing: the writes are real, the audit rows are real, and none of it
// survives.
//
// # A GATE THAT RAISES POISONS THE TRANSACTION, WHICH IS WHY `refused` EXISTS
//
// Every gate is a `raise exception`, and a raise aborts the whole Postgres
// transaction: every later command answers 25P02 until a rollback. That is
// invisible in production, where the tenancy interceptor gives each request its
// own transaction and each request performs one write, so a refusal is followed
// only by the rollback that was going to happen anyway.
//
// It is very visible here, where a test provokes a refusal and then continues.
// So each expected failure runs inside a savepoint that is rolled back to,
// leaving the surrounding transaction usable. Found by writing these tests and
// reading 25P02 rather than by reasoning about it.
//
// Worth knowing before anyone batches several writes into one request: the
// second write after a refused first will fail for a reason that has nothing to
// do with it.
func refused(t *testing.T, tenant *Tenant, ctx context.Context, call func() error) error {
	t.Helper()

	if _, err := tenant.Tx().Exec(ctx, "savepoint gate_probe"); err != nil {
		t.Fatalf("opening a savepoint: %v", err)
	}

	err := call()

	if _, rollbackErr := tenant.Tx().Exec(ctx, "rollback to savepoint gate_probe"); rollbackErr != nil {
		t.Fatalf("rolling back to the savepoint: %v", rollbackErr)
	}
	return err
}

func TestAManualActivityIsCreatedWithItsAuditRow(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedRegisters(t, tenant.Tx(), ctx, alphaOrg, adaUser)

	created, err := tenant.CreateProcessingActivity(ctx, records.ProcessingActivityFields{
		Name:            "Payroll",
		Purpose:         "Paying staff",
		LegalBasis:      "Article 6(1)(b)",
		DataCategories:  []string{"name", "bank details"},
		Recipients:      []string{"our accountant"},
		RetentionPeriod: "7 years",
	})
	if err != nil {
		t.Fatalf("creating an activity: %v", err)
	}

	if created.Name != "Payroll" || created.LegalBasis != "Article 6(1)(b)" {
		t.Fatalf("the created record came back wrong: %+v", created)
	}
	// Manually created, so no provenance. The read path uses this to decide
	// whether an entry needs review.
	if created.SourceFindingID != "" {
		t.Fatalf("a manual entry carries a source finding: %q", created.SourceFindingID)
	}

	// The audit row is written by the database in the same transaction. A
	// handler writing one too would produce a second row for one change.
	var audits int
	if err := tenant.Tx().QueryRow(ctx, `
		select count(*) from audit_log
		where target_table = 'processing_activities' and target_id = $1
		  and action_type = 'create_ropa_manual'
	`, created.ID).Scan(&audits); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if audits != 1 {
		t.Fatalf("got %d audit rows for the creation, want 1", audits)
	}
}

func TestAnActivityUpdateReplacesEveryFieldIncludingWithNothing(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedRegisters(t, tenant.Tx(), ctx, alphaOrg, adaUser)

	created, err := tenant.CreateProcessingActivity(ctx, records.ProcessingActivityFields{
		Name:       "Payroll",
		LegalBasis: "Article 6(1)(a), consent",
		Recipients: []string{"our accountant"},
	})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	// Clearing a field is a real edit somebody makes when they realise the
	// value recorded was wrong. A patch in which an omitted field means "leave
	// it alone" gives a client no way to say this, which is why the contract is
	// a full replacement.
	updated, err := tenant.UpdateProcessingActivity(ctx, created.ID, records.ProcessingActivityFields{
		Name: "Payroll",
	})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}

	if updated.LegalBasis != "" {
		t.Fatalf("legal basis survived a replacement that omitted it: %q", updated.LegalBasis)
	}
	if len(updated.Recipients) != 0 {
		t.Fatalf("recipients survived a replacement that omitted them: %v", updated.Recipients)
	}
}

// The gate that has an upgrade under it.
func TestTheFreeTierCapIsReportedAsAQuotaRatherThanADenial(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	// Bob's organisation, not Ada's, and that is the whole setup: Beta's fixture
	// subscription is `free` where Alpha's is `pro`, and
	// `ropa_manual_activity_limit()` returns null (uncapped) for pro.
	//
	// This started as a t.Skip when run against Alpha, which is worse than no
	// test: it reported green while never once exercising the branch that
	// produces a customer's upgrade prompt. Removing Alpha's subscription
	// instead is not an option and should not be, because `subscriptions`
	// carries a select policy and no delete policy: billing rows are written by
	// the webhook, never by the application.
	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedRegisters(t, tenant.Tx(), ctx, betaOrg, bobUser)

	quota, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("reading the quota: %v", err)
	}
	if quota.Limit == 0 {
		t.Fatal("the free plan reports an unlimited cap, so this test would pass " +
			"without ever exercising it; check Beta's fixture subscription")
	}

	// Fill it exactly, then go one over.
	for i := quota.Used; i < quota.Limit; i++ {
		if _, err := tenant.CreateProcessingActivity(ctx, records.ProcessingActivityFields{
			Name: "Filler",
		}); err != nil {
			t.Fatalf("filling the quota at %d: %v", i, err)
		}
	}

	_, err = tenant.CreateProcessingActivity(ctx, records.ProcessingActivityFields{Name: "One too many"})
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("over the cap: want ErrQuotaExhausted, got %v", err)
	}
	// Not confused with the other gate that raises the same SQLSTATE.
	if errors.Is(err, ErrReviewRequired) {
		t.Fatal("the quota error was also classified as needing review; the two gates are not distinguished")
	}
}

// The gate that has a confirm step under it.
func TestClassifyingASystemHighRequiresAReviewedApproval(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedRegisters(t, tenant.Tx(), ctx, alphaOrg, adaUser)

	fields := records.AiSystemFields{
		Name:               "CV ranking model",
		RiskClassification: "high",
	}

	err = refused(t, tenant, ctx, func() error {
		_, e := tenant.CreateAiSystem(ctx, fields, false)
		return e
	})
	if !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed high classification: want ErrReviewRequired, got %v", err)
	}
	// Not confused with the cap, which raises the same SQLSTATE.
	if errors.Is(err, ErrQuotaExhausted) {
		t.Fatal("the review gate was also classified as a quota; the two gates are not distinguished")
	}

	// The same call with the confirmation succeeds, which is what makes the
	// refusal a precondition rather than a permission.
	created, err := tenant.CreateAiSystem(ctx, fields, true)
	if err != nil {
		t.Fatalf("reviewed high classification: %v", err)
	}
	if created.RiskClassification != "high" {
		t.Fatalf("classification not recorded: %+v", created)
	}
}

// A reclassification of an existing system, which is the case the gate exists
// for: quietly moving a system out of `high` retires Articles 9 to 17 for it.
func TestChangingAnExistingClassificationRequiresAReviewedApproval(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	seedRegisters(t, tenant.Tx(), ctx, alphaOrg, adaUser)

	created, err := tenant.CreateAiSystem(ctx, records.AiSystemFields{
		Name:               "Support assistant",
		RiskClassification: "limited",
	}, false)
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	err = refused(t, tenant, ctx, func() error {
		_, e := tenant.UpdateAiSystem(ctx, created.ID, records.AiSystemFields{
			Name:               "Support assistant",
			RiskClassification: "minimal",
		}, false)
		return e
	})
	if !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed reclassification: want ErrReviewRequired, got %v", err)
	}

	// An edit that leaves the classification alone needs no confirmation, which
	// is the half that keeps the gate from being a nuisance on every save.
	updated, err := tenant.UpdateAiSystem(ctx, created.ID, records.AiSystemFields{
		Name:               "Support reply assistant",
		RiskClassification: "limited",
	}, false)
	if err != nil {
		t.Fatalf("unreviewed edit that keeps the classification: %v", err)
	}
	if updated.Name != "Support reply assistant" {
		t.Fatalf("the rename did not apply: %+v", updated)
	}
}

func TestMarkingADsarRespondedIsGatedAndIdempotent(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	logged, err := tenant.LogDsar(ctx, "M. Laurent", "erasure", "Privacy team")
	if err != nil {
		t.Fatalf("logging a dsar: %v", err)
	}
	if logged.ResponseDueAt.IsZero() {
		t.Fatal("the statutory deadline was not computed on logging")
	}
	if logged.Status != "open" {
		t.Fatalf("a newly logged request is %q, want open", logged.Status)
	}

	err = refused(t, tenant, ctx, func() error {
		_, _, e := tenant.MarkDsarResponded(ctx, logged.ID, false)
		return e
	})
	if !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed: want ErrReviewRequired, got %v", err)
	}

	answered, applied, err := tenant.MarkDsarResponded(ctx, logged.ID, true)
	if err != nil {
		t.Fatalf("marking responded: %v", err)
	}
	if !applied {
		t.Fatal("the first mark reported applied=false")
	}
	if answered.Status != "responded" || answered.RespondedAt.IsZero() {
		t.Fatalf("status or date not set: %+v", answered)
	}

	// The second call must report that it changed nothing, and must not
	// overwrite the date a response actually went out with the date somebody
	// clicked twice. Reading the status AFTER acting would report applied on
	// every repeat call, which is the bug the findings act path shipped with.
	again, applied, err := tenant.MarkDsarResponded(ctx, logged.ID, true)
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if applied {
		t.Fatal("the second mark reported applied=true; it is not idempotent")
	}
	if !again.RespondedAt.Equal(answered.RespondedAt) {
		t.Fatalf("the response date moved on a repeat call: %v then %v",
			answered.RespondedAt, again.RespondedAt)
	}
}

// A write against another organisation's record, and against a malformed id,
// both read as not-found rather than as anything a prober could learn from.
func TestWritingToARecordThatIsNotYoursIsNotFound(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// A well-formed uuid naming nothing Ada can see is indistinguishable from
	// one naming a row in Bob's organisation, because the function's lookup is
	// org-scoped and answers the same way for both.
	for _, id := range []string{"c0000000-0000-4000-8000-00000000000c", "not-a-uuid", ""} {
		err := refused(t, tenant, ctx, func() error {
			_, e := tenant.UpdateProcessingActivity(ctx, id,
				records.ProcessingActivityFields{Name: "x"})
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("activity %q: want ErrNoRows, got %v", id, err)
		}

		err = refused(t, tenant, ctx, func() error {
			_, e := tenant.UpdateAiSystem(ctx, id, records.AiSystemFields{Name: "x"}, true)
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("system %q: want ErrNoRows, got %v", id, err)
		}

		err = refused(t, tenant, ctx, func() error {
			_, _, e := tenant.MarkDsarResponded(ctx, id, true)
			return e
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("dsar %q: want ErrNoRows, got %v", id, err)
		}
	}
}

// An organisation that has not finished onboarding has no profile for a record
// to hang off. That is a precondition the customer can clear, not a fault.
func TestCreatingARecordWithNoComplianceProfileIsAPrecondition(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// Deliberately no seedRegisters: Beta has no compliance profile.
	var profiles int
	if err := tenant.Tx().QueryRow(ctx,
		"select count(*) from compliance_profiles").Scan(&profiles); err != nil {
		t.Fatalf("counting profiles: %v", err)
	}
	if profiles != 0 {
		t.Skipf("Beta has %d compliance profiles, so this precondition cannot be provoked", profiles)
	}

	_, err = tenant.CreateProcessingActivity(ctx, records.ProcessingActivityFields{Name: "Anything"})
	if !errors.Is(err, ErrNoProfile) {
		t.Fatalf("want ErrNoProfile, got %v", err)
	}
}
