package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// Writing to the three registers, always through the database functions in
// 00002 and never against a table.
//
// Each function resolves the acting human from the session GUC, enforces its own
// gate and writes the audit row in the same transaction as the change. Writing
// the table here would mean reimplementing all three, and the audit row would
// become a second statement that can fail on its own.
//
// TWO GATES SHARE ONE SQLSTATE, AND THEY NEED DIFFERENT ANSWERS
//
// `check_violation` (23514) is raised by both the free-tier cap and the
// reviewed-approval requirement. They are not the same situation: one is "you
// are entitled to this and cannot right now", which has an upgrade under it, and
// the other is "you may do this and must confirm first", which has a checkbox
// under it. A caller shown the wrong one is sent to the wrong place.
//
// The database does not distinguish them, so this file does, on a marker in the
// message. That is the fragile half of the arrangement and it is why every one
// of these paths has an integration test that provokes the real error from the
// real function: a reworded message turns a test red rather than silently
// remapping a customer's upgrade prompt into a confirm dialog.
//
// The alternative was a migration giving each gate its own SQLSTATE. It was not
// taken because it rewrites six shipped functions to change nothing a caller can
// observe except an error code, and the mapping below is verified rather than
// assumed. If a third gate ever shares the code, that trade flips.

// ErrQuotaExhausted is the free-tier cap on manually-created records.
var ErrQuotaExhausted = errors.New("postgres: the plan's limit on manual records is reached")

// ErrReviewRequired is a gate that a human must deliberately confirm.
var ErrReviewRequired = errors.New("postgres: this change requires a reviewed approval")

// ErrNoProfile means onboarding has not produced a compliance profile, so there
// is nothing for a record to hang off.
var ErrNoProfile = errors.New("postgres: the organisation has no compliance profile")

// ErrFutureReceipt is a data-subject request dated after now.
//
// A bad value rather than a rule the caller must satisfy first, which is why it
// is its own error and not folded into ErrReviewRequired despite sharing the
// SQLSTATE: the caller sends a different date, they do not confirm anything.
var ErrFutureReceipt = errors.New("postgres: the request cannot have arrived in the future")

// classify turns a raise from one of the write functions into the error the
// handler layer maps to a Connect code.
//
// Returns the original error untouched when it is not one of the known gates, so
// an unexpected database failure stays an unexpected database failure rather
// than being flattened into a business rule.
func classify(err error) error {
	if err == nil {
		return nil
	}

	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}

	message := strings.ToLower(pg.Message)

	switch {
	// Checked before the reviewed-approval marker, because both are
	// `check_violation` and this one's message is the more specific.
	case strings.Contains(message, "in the future"):
		return fmt.Errorf("%w: %s", ErrFutureReceipt, pg.Message)
	case strings.Contains(message, "free tier limit"):
		return fmt.Errorf("%w: %s", ErrQuotaExhausted, pg.Message)
	case strings.Contains(message, "reviewed approval"):
		return fmt.Errorf("%w: %s", ErrReviewRequired, pg.Message)
	case strings.Contains(message, "no compliance profile"):
		return fmt.Errorf("%w: %s", ErrNoProfile, pg.Message)
	case strings.Contains(message, "not found or not owned"):
		// One answer for "no such record" and "not yours", because the function
		// cannot tell them apart either: its lookup is org-scoped.
		return pgx.ErrNoRows
	}

	return err
}

// CreateProcessingActivity adds an Article 30 entry a human wrote.
func (t *Tenant) CreateProcessingActivity(ctx context.Context, f records.ProcessingActivityFields) (records.ProcessingActivity, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		select public.create_processing_activity($1, $2, $3, $4, $5, $6)::text
	`, f.Name, nullIfEmpty(f.Purpose), nullIfEmpty(f.LegalBasis),
		f.DataCategories, f.Recipients, nullIfEmpty(f.RetentionPeriod)).Scan(&id)
	if err != nil {
		return records.ProcessingActivity{}, classify(err)
	}

	// Read it back through the same query the read path uses, so a created
	// record and a listed one cannot describe the same row differently.
	return t.ProcessingActivity(ctx, id)
}

// UpdateProcessingActivity replaces an entry's fields.
func (t *Tenant) UpdateProcessingActivity(ctx context.Context, activityID string, f records.ProcessingActivityFields) (records.ProcessingActivity, error) {
	id, ok := parseID(activityID)
	if !ok {
		return records.ProcessingActivity{}, pgx.ErrNoRows
	}

	var updated string
	err := t.tx.QueryRow(ctx, `
		select public.update_processing_activity($1, $2, $3, $4, $5, $6, $7)::text
	`, id, f.Name, nullIfEmpty(f.Purpose), nullIfEmpty(f.LegalBasis),
		f.DataCategories, f.Recipients, nullIfEmpty(f.RetentionPeriod)).Scan(&updated)
	if err != nil {
		return records.ProcessingActivity{}, classify(err)
	}

	return t.ProcessingActivity(ctx, updated)
}

// CreateAiSystem registers a system nobody approved a finding about.
func (t *Tenant) CreateAiSystem(ctx context.Context, f records.AiSystemFields, reviewed bool) (records.AiSystem, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		select public.create_ai_system_manual($1, $2, $3, $4, $5, $6)::text
	`, f.Name, nullIfEmpty(f.Vendor), nullIfEmpty(f.Purpose),
		nullIfEmpty(f.RiskClassification), nullIfEmpty(f.DocumentationStatus), reviewed).Scan(&id)
	if err != nil {
		return records.AiSystem{}, classify(err)
	}

	return t.AiSystem(ctx, id)
}

// UpdateAiSystem replaces a system's fields.
//
// The classification gate lives in the function, not here. A check in this file
// would be a second implementation of it, and the one that drifts is always the
// copy furthest from the data.
func (t *Tenant) UpdateAiSystem(ctx context.Context, systemID string, f records.AiSystemFields, reviewed bool) (records.AiSystem, error) {
	id, ok := parseID(systemID)
	if !ok {
		return records.AiSystem{}, pgx.ErrNoRows
	}

	var updated string
	err := t.tx.QueryRow(ctx, `
		select public.update_ai_system($1, $2, $3, $4, $5, $6, $7)::text
	`, id, f.Name, nullIfEmpty(f.Vendor), nullIfEmpty(f.Purpose),
		nullIfEmpty(f.RiskClassification), nullIfEmpty(f.DocumentationStatus), reviewed).Scan(&updated)
	if err != nil {
		return records.AiSystem{}, classify(err)
	}

	return t.AiSystem(ctx, updated)
}

// LogDsar records a request that arrived.
//
// A zero `receivedAt` is passed as null, which the function reads as today
// (ENT-224). Sending `now()` from here instead would look equivalent and is
// not: the function is where the clock rule lives, and a caller that computes
// its own "today" is a second implementation of it that drifts by a timezone.
func (t *Tenant) LogDsar(ctx context.Context, subjectName, requestType, handler string, receivedAt time.Time) (records.Dsar, error) {
	var received *time.Time
	if !receivedAt.IsZero() {
		received = &receivedAt
	}

	var id string
	err := t.tx.QueryRow(ctx, `
		select public.log_dsar($1, $2, $3, $4)::text
	`, nullIfEmpty(subjectName), nullIfEmpty(requestType), nullIfEmpty(handler), received).Scan(&id)
	if err != nil {
		return records.Dsar{}, classify(err)
	}

	return t.Dsar(ctx, id)
}

// MarkDsarResponded stops the statutory clock.
//
// Reports whether this call was the one that changed anything. The function is
// idempotent and returns the id either way, so the status is read BEFORE acting:
// reading it afterwards would report `applied` on every repeat call, which is
// the exact bug the findings act path shipped with and had to fix.
func (t *Tenant) MarkDsarResponded(ctx context.Context, dsarID string, reviewed bool) (records.Dsar, bool, error) {
	id, ok := parseID(dsarID)
	if !ok {
		return records.Dsar{}, false, pgx.ErrNoRows
	}

	before, err := t.Dsar(ctx, dsarID)
	if err != nil {
		return records.Dsar{}, false, err
	}
	alreadyAnswered := before.Status == "responded" || before.Status == "closed"

	var updated string
	if err := t.tx.QueryRow(ctx, `
		select public.mark_dsar_responded($1, $2)::text
	`, id, reviewed).Scan(&updated); err != nil {
		return records.Dsar{}, false, classify(err)
	}

	after, err := t.Dsar(ctx, updated)
	if err != nil {
		return records.Dsar{}, false, err
	}

	return after, !alreadyAnswered, nil
}
