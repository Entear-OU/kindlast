package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/stackenv"
)

// migratorConn opens a connection with enough privilege to stage a billing row.
//
// `subscriptions` carries a select policy and no update policy for
// `kindlast_app`, which is correct: billing rows are the webhook's to write,
// never the application's. So a test that needs a cancelled subscription cannot
// stage one through the store under test and needs its own connection, the same
// way db/tests do.
//
// Fatal rather than Skip when it cannot connect. A skip here would report a
// green suite while never once exercising the entitlement rule, which is the
// failure mode this whole file exists to prevent.
func migratorConn(t *testing.T) *pgx.Conn {
	t.Helper()

	dsn := stackenv.DSN("migrator")

	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		// Same convention as agentStore and testStore: skips on a laptop,
		// fails in CI when KINDLAST_REQUIRE_STACK is set. Every caller used to
		// reach this only after another helper had already skipped, so the
		// hard Fatalf here looked safe; the first test to open with a
		// migrator read (fetch_test.go) failed the stackless `go` job instead
		// of skipping, which is this line's job to prevent.
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", dsn, err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", dsn, err)
	}
	t.Cleanup(func() { _ = conn.Close(t.Context()) })
	return conn
}

// Entitlement reads consult status, never the plan column alone (ENT-210).
//
// # A LATENT BUG THAT ENT-225 CLOSED WITHOUT MEANING TO
//
// `ropa_manual_activity_limit()`, dropped by 00016, decided the cap like this:
//
//	when exists (
//	  select 1 from public.subscriptions
//	  where org_id = public.app_current_org_id() and plan = 'pro'
//	) then null::integer
//
// No `status`. So an organisation whose pro subscription had been cancelled,
// or had gone `past_due` after a failed payment, kept reading as pro and stayed
// uncapped indefinitely. Nothing surfaced it: the customer simply kept a paid
// entitlement they had stopped paying for, which is the direction of failure
// nobody reports.
//
// ENT-210 names this trap explicitly, having already been caught by it once:
// `Tenant.Plan` had the same shape until the feed's plan gating made it
// load-bearing. `Plan` was fixed then and filters `status = 'active'`; the SQL
// function was not, because nothing had made it matter yet.
//
// Moving the cap into Go made it call `Plan`, which fixed the SQL version's bug
// as a side effect. A fix nobody asked for is exactly the kind that gets
// reverted by a later refactor, so it is pinned here rather than left implicit.

func TestACancelledProSubscriptionDoesNotKeepTheCapOff(t *testing.T) {
	store := testStore(t, WithBilling(true))
	ctx := t.Context()
	migrator := migratorConn(t)

	// Alpha is the pro fixture, so it starts uncapped. Asserted first, because
	// if that ever stopped being true this test would pass while proving
	// nothing at all.
	before, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("opening a transaction: %v", err)
	}
	if _, capped, err := before.manualActivityLimit(ctx); err != nil {
		t.Fatalf("reading the limit: %v", err)
	} else if capped {
		t.Fatal("the pro fixture is already capped, so this test proves nothing")
	}
	_ = before.Rollback(ctx)

	// Cancel it, and put it back afterwards: the fixture is shared with every
	// other test in this package.
	var restore string
	if err := migrator.QueryRow(ctx,
		`select status from subscriptions where org_id = $1`, alphaOrg).Scan(&restore); err != nil {
		t.Fatalf("reading the fixture subscription: %v", err)
	}
	if _, err := migrator.Exec(ctx,
		`update subscriptions set status = 'canceled' where org_id = $1`, alphaOrg); err != nil {
		t.Fatalf("cancelling the subscription: %v", err)
	}
	t.Cleanup(func() {
		if _, err := migrator.Exec(context.WithoutCancel(ctx),
			`update subscriptions set status = $2 where org_id = $1`, alphaOrg, restore); err != nil {
			t.Errorf("restoring the fixture subscription to %q: %v", restore, err)
		}
	})

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("opening a transaction: %v", err)
	}
	defer func() { _ = tenant.Rollback(ctx) }()

	limit, capped, err := tenant.manualActivityLimit(ctx)
	if err != nil {
		t.Fatalf("reading the limit after cancellation: %v", err)
	}
	if !capped {
		t.Fatal("a cancelled pro subscription still reads as uncapped; " +
			"an entitlement read consulted plan without status")
	}
	if limit != FreeManualActivityLimit {
		t.Fatalf("limit after cancellation is %d, want the free cap %d",
			limit, FreeManualActivityLimit)
	}

	// And the console agrees, because both halves read one method.
	quota, err := tenant.ManualActivityQuota(ctx)
	if err != nil {
		t.Fatalf("reading the quota: %v", err)
	}
	if quota.Limit != FreeManualActivityLimit {
		t.Fatalf("the console shows a cap of %d after cancellation, want %d",
			quota.Limit, FreeManualActivityLimit)
	}
}
