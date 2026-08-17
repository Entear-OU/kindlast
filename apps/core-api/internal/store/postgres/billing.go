package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/billing"
)

// BillingStore is the payment webhook's connection pool (ENT-210).
//
// Its own pool on its own role, `kindlast_billing`, which is NOSUPERUSER,
// NOBYPASSRLS, owns nothing, and holds grants on exactly two tables. See
// 00017's header for why this is a fifth role rather than the agent: granting
// the agent subscription writes would make it a role that can invent a finding
// AND grant itself a paid plan, which is a new capability rather than a wider
// read.
type BillingStore struct {
	pool *pgxpool.Pool
}

// NewBilling opens the webhook's pool.
//
// The DSN must name `kindlast_billing`. Naming the migrator or the superuser
// would work and would silently disable every policy this role is scoped by,
// which is the trap §14.1 spends a paragraph on and the one a rarely-read
// unauthenticated endpoint is least likely to surface.
func NewBilling(ctx context.Context, dsn string) (*BillingStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening the billing pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: pinging as the billing role: %w", err)
	}
	return &BillingStore{pool: pool}, nil
}

func (b *BillingStore) Close() { b.pool.Close() }

// Apply records a verified event and updates the subscription, in one
// transaction.
//
// # THE ORDER IS THE SECURITY PROPERTY
//
// A provider event says "customer cus_x changed". Which organisation that is is
// the answer rather than the question, so this cannot set `app.current_org_id`
// before it has looked. It therefore:
//
//  1. resolves the customer to an organisation, through the one unscoped select
//     policy the role holds,
//  2. sets the GUC to what it resolved,
//  3. writes under org-equality policies.
//
// Step 3 is bound by step 2, so a handler bug or a crafted payload cannot turn
// one event into an upgrade for a different tenant: the write matches zero rows
// rather than the wrong ones.
//
// # WHY THE DEDUP INSERT IS FIRST AND IN THE SAME TRANSACTION
//
// The event id is inserted before the subscription is touched, so a retried
// delivery hits the primary key and the whole transaction rolls back having
// applied nothing. Providers retry on any non-2xx and on timeouts, so a
// retried upgrade must not extend anything twice, and doing the dedup in a
// separate transaction would leave a window where the ledger says applied and
// the subscription says otherwise.
func (b *BillingStore) Apply(ctx context.Context, e billing.Event) error {
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: beginning a billing transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Claim the event id. A replay stops here having changed nothing.
	if _, err := tx.Exec(ctx,
		`insert into billing_webhook_events (event_id) values ($1)`, e.ID); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return billing.ErrAlreadyApplied
		}
		return fmt.Errorf("postgres: recording the billing event: %w", err)
	}

	// 2. Resolve the customer. The one unscoped read this role has, and the
	//    reason it has it: scoping is impossible before this answer.
	var orgID string
	err = tx.QueryRow(ctx, `
		select org_id::text from subscriptions where provider_customer_id = $1
	`, e.CustomerID).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Rolled back, so the event id is NOT recorded. That is deliberate: a
		// customer this deployment does not know about may be one whose
		// checkout has not landed yet, and burning the event id would make the
		// provider's retry a no-op forever.
		return billing.ErrUnknownCustomer
	}
	if err != nil {
		return fmt.Errorf("postgres: resolving the billing customer: %w", err)
	}

	// 3. Scope to what was resolved, then write.
	if err := setLocal(ctx, tx, "app.current_org_id", orgID); err != nil {
		return err
	}

	var periodEnd any
	if !e.PeriodEnd.IsZero() {
		periodEnd = e.PeriodEnd
	}

	tag, err := tx.Exec(ctx, `
		update subscriptions
		   set plan = $2, status = $3, current_period_end = $4
		 where org_id = $1::uuid
	`, orgID, e.Plan, e.Status, periodEnd)
	if err != nil {
		return fmt.Errorf("postgres: applying the subscription change: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Zero rows means the org-equality policy filtered the write, which
		// should be impossible given the id came from the resolve above. Treated
		// as a fault rather than swallowed: silently applying nothing is how a
		// customer pays and stays on free.
		return fmt.Errorf("postgres: the subscription write matched no rows for %s", orgID)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: committing the billing change: %w", err)
	}
	return nil
}
