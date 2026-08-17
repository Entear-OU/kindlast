package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	billingservice "github.com/Entear-OU/kindlast/apps/core-api/internal/service/billing"
)

// Subscription reports what the caller's organisation is billed on.
//
// # PLAN AND STATUS TOGETHER, NEVER PLAN ALONE
//
// The plan returned here is the entitlement in force, which means a `pro` row
// whose status is `canceled` or `past_due` reads as `free`. That is the trap
// ENT-210 names and this codebase has now hit twice: `Tenant.Plan` had it until
// the feed's gating made it load-bearing, and `ropa_manual_activity_limit()`
// still had it when 00016 dropped it.
//
// The raw status is returned alongside rather than folded in, because the two
// carry different messages. A customer who cancelled and a customer whose card
// failed are both `free` in entitlement terms, and telling them the same thing
// is how a payment problem goes unnoticed until the renewal that never happens.
func (t *Tenant) Subscription(ctx context.Context) (billingservice.Subscription, error) {
	var (
		plan      string
		status    string
		periodEnd *time.Time
	)

	err := t.tx.QueryRow(ctx, `
		select plan, status, current_period_end
		  from subscriptions
		 where org_id = $1
	`, t.orgID).Scan(&plan, &status, &periodEnd)

	if errors.Is(err, pgx.ErrNoRows) {
		// A new organisation has no row, and that is `free` rather than an
		// error. Reporting it as missing would make "has not bought anything"
		// indistinguishable from a fault, and every caller would have to
		// default it themselves.
		return billingservice.Subscription{Plan: "free"}, nil
	}
	if err != nil {
		return billingservice.Subscription{}, fmt.Errorf("postgres: reading the subscription: %w", err)
	}

	entitled := plan
	if status != "active" {
		entitled = "free"
	}

	out := billingservice.Subscription{
		Plan:            entitled,
		Status:          status,
		HasSubscription: true,
	}
	if periodEnd != nil {
		out.PeriodEnd = timestamppb.New(*periodEnd)
	}
	return out, nil
}
