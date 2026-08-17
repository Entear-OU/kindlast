package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	records "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The six manual record writes, in Go (ENT-225 phase 1).
//
// # WHAT THIS FILE USED TO BE, AND WHY IT IS NOT THAT ANY MORE
//
// Every write here was one line: `select public.create_processing_activity(...)`
// and so on, with the rules inside the function. The rules were decisions, so
// they are Go's now (§14.5), and one consequence is worth stating because it was
// the concrete cost of the old arrangement.
//
// This file used to contain a `classify` function that read English out of a
// Postgres exception. Two different business gates both raised
// `check_violation`, SQLSTATE 23514, so a Go caller could not tell a quota
// refusal from a review-required refusal by code alone, and recovered the
// difference by matching on substrings: "free tier limit", "reviewed approval",
// "no compliance profile", "not found or not owned". Its own header called that
// the fragile half of the arrangement, and an integration test existed purely to
// notice if somebody reworded a message.
//
// The sentinels below are unchanged, because they are the contract the service
// layer maps to Connect codes. What changed is that they are now returned by the
// code that made the decision, rather than reconstructed from the text of an
// exception it raised.
//
// # WHAT IS STILL THE DATABASE'S
//
// Everything that must hold no matter who writes. The org scoping on every
// statement is RLS, not the predicates: a row in another organisation is
// invisible to the SELECT and unmatched by the UPDATE without this file
// arranging it. `record_audit_log` still writes the audit row, because
// snapshotting the actor's role and appending immutably is an invariant, and
// the Executor triggers call the same function.

// The refusals a caller can act on. Unchanged from when the database raised
// them; only their origin moved.
var (
	// ErrQuotaExhausted means the plan's cap on manual records is reached. The
	// caller is entitled to the action and has run out of allowance, which is a
	// different thing from being denied it.
	ErrQuotaExhausted = errors.New("postgres: the plan's manual record limit is reached")

	// ErrReviewRequired means the change is one somebody has to confirm they
	// reviewed, because it asserts something a regulator will read.
	ErrReviewRequired = errors.New("postgres: the change requires a reviewed approval")

	// ErrNoProfile means the organisation has no compliance profile, so there is
	// nothing for a record to hang off yet.
	ErrNoProfile = errors.New("postgres: the organisation has no compliance profile")

	// ErrFutureReceipt means a DSAR was recorded as arriving in the future.
	ErrFutureReceipt = errors.New("postgres: the receipt date is in the future")
)

// AI Act risk classifications, as the register stores them.
//
// Only the two this file makes decisions about are named. `high` is the one
// that needs a reviewed approval, because calling a system High-Risk is an
// assertion about the customer's exposure under the AI Act. `unclassified` is
// what a system gets when nobody has said, which is deliberately not the same
// as `minimal`: "nobody has looked" and "somebody looked and it is fine" are
// different facts and a register that conflates them is worse than one with a
// gap in it.
const (
	ClassificationHigh         = "high"
	ClassificationUnclassified = "unclassified"
)

// FreeManualActivityLimit is how many Article 30 entries a free organisation may
// write by hand.
//
// Only manual ones. A record the Executor created on an approved finding is part
// of the compliance record and is never withheld behind a plan, which is why the
// count below carries `finding_id is null`. Withholding those would mean a
// customer's own regulatory record becoming unreadable when a card expires.
const FreeManualActivityLimit = 3

// manualActivityLimit is the cap for this organisation, and whether there is one.
//
// One implementation, used by both the write below and `ManualActivityQuota` on
// the read path. That is the property `server.go` used to preserve by NOT
// passing the billing flag to RecordsService: the console's cap and the cap a
// write meets have to be one answer, and two implementations are two answers
// waiting to disagree.
func (t *Tenant) manualActivityLimit(ctx context.Context) (limit int32, capped bool, err error) {
	// Billing off is the self-hosted default and means no cap at all. A
	// deployment that bills nobody must not gate anybody out of their own
	// compliance record (§18.1).
	if !t.billingEnabled {
		return 0, false, nil
	}

	plan, err := t.Plan(ctx)
	if err != nil {
		return 0, false, err
	}
	if plan == "pro" {
		return 0, false, nil
	}
	return FreeManualActivityLimit, true, nil
}

// currentProfile is the compliance profile a new record hangs off.
//
// The most recent one, matching what the SQL did. Returns ErrNoProfile rather
// than pgx.ErrNoRows, because "this organisation has not been set up yet" is a
// precondition the caller can explain to a person, and "no rows" is not.
func (t *Tenant) currentProfile(ctx context.Context) (string, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		select id::text from compliance_profiles
		 where org_id = $1
		 order by created_at desc
		 limit 1
	`, t.orgID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoProfile
	}
	if err != nil {
		return "", fmt.Errorf("postgres: reading the compliance profile: %w", err)
	}
	return id, nil
}

// CreateProcessingActivity adds an Article 30 entry a human wrote.
func (t *Tenant) CreateProcessingActivity(ctx context.Context, f records.ProcessingActivityFields) (records.ProcessingActivity, error) {
	profile, err := t.currentProfile(ctx)
	if err != nil {
		return records.ProcessingActivity{}, err
	}

	limit, capped, err := t.manualActivityLimit(ctx)
	if err != nil {
		return records.ProcessingActivity{}, err
	}
	if capped {
		var used int32
		if err := t.tx.QueryRow(ctx, `
			select count(*) from processing_activities where finding_id is null
		`).Scan(&used); err != nil {
			return records.ProcessingActivity{}, fmt.Errorf("postgres: counting manual activities: %w", err)
		}
		if used >= limit {
			return records.ProcessingActivity{}, fmt.Errorf(
				"%w: a manual ROPA is capped at %d activities", ErrQuotaExhausted, limit)
		}
	}

	var id string
	err = t.tx.QueryRow(ctx, `
		insert into processing_activities (
			profile_id, org_id, created_by, finding_id,
			name, purpose, legal_basis, data_categories, recipients, retention_period
		)
		values ($1, $2, $3, null, coalesce(nullif(btrim($4), ''), 'Untitled activity'),
		        $5, $6, coalesce($7::text[], '{}'), coalesce($8::text[], '{}'), $9)
		returning id::text
	`, profile, t.orgID, t.userID, f.Name, nullIfEmpty(f.Purpose), nullIfEmpty(f.LegalBasis),
		f.DataCategories, f.Recipients, nullIfEmpty(f.RetentionPeriod)).Scan(&id)
	if err != nil {
		return records.ProcessingActivity{}, fmt.Errorf("postgres: creating a processing activity: %w", err)
	}

	if err := t.auditRecord(ctx, "create_ropa_manual", "processing_activities", id, nil); err != nil {
		return records.ProcessingActivity{}, err
	}

	// Read back through the same query the read path uses, so a created record
	// and a listed one cannot describe the same row differently.
	return t.ProcessingActivity(ctx, id)
}

// UpdateProcessingActivity replaces an entry's fields.
func (t *Tenant) UpdateProcessingActivity(ctx context.Context, activityID string, f records.ProcessingActivityFields) (records.ProcessingActivity, error) {
	id, ok := parseID(activityID)
	if !ok {
		return records.ProcessingActivity{}, pgx.ErrNoRows
	}

	before, err := t.recordSnapshot(ctx, "processing_activities", id.String())
	if err != nil {
		return records.ProcessingActivity{}, err
	}

	if _, err := t.tx.Exec(ctx, `
		update processing_activities set
			name             = coalesce(nullif(btrim($2), ''), name),
			purpose          = $3,
			legal_basis      = $4,
			data_categories  = coalesce($5::text[], '{}'),
			recipients       = coalesce($6::text[], '{}'),
			retention_period = $7
		 where id = $1 and org_id = $8
	`, id, f.Name, nullIfEmpty(f.Purpose), nullIfEmpty(f.LegalBasis),
		f.DataCategories, f.Recipients, nullIfEmpty(f.RetentionPeriod), t.orgID); err != nil {
		return records.ProcessingActivity{}, fmt.Errorf("postgres: updating a processing activity: %w", err)
	}

	if err := t.auditIfChanged(ctx, "update_ropa", "processing_activities", id.String(), before); err != nil {
		return records.ProcessingActivity{}, err
	}

	return t.ProcessingActivity(ctx, id.String())
}

// CreateAiSystem registers a system nobody approved a finding about.
func (t *Tenant) CreateAiSystem(ctx context.Context, f records.AiSystemFields, reviewed bool) (records.AiSystem, error) {
	profile, err := t.currentProfile(ctx)
	if err != nil {
		return records.AiSystem{}, err
	}

	class := strings.TrimSpace(f.RiskClassification)
	if class == "" {
		class = ClassificationUnclassified
	}

	// Calling something High-Risk is an assertion about the customer's exposure
	// under the AI Act, so somebody has to say they looked. The gate is here
	// rather than in a check constraint because it depends on what the caller
	// confirmed, not on what the row contains.
	if class == ClassificationHigh && !reviewed {
		return records.AiSystem{}, fmt.Errorf(
			"%w: a High-Risk classification requires a reviewed approval", ErrReviewRequired)
	}

	var id string
	err = t.tx.QueryRow(ctx, `
		insert into ai_systems (
			profile_id, org_id, created_by, finding_id,
			name, vendor, purpose, risk_classification, documentation_status, last_reviewed_at
		)
		values ($1, $2, $3, null, coalesce(nullif(btrim($4), ''), 'Untitled system'),
		        $5, $6, $7, coalesce(nullif(btrim($8), ''), 'missing'), now())
		returning id::text
	`, profile, t.orgID, t.userID, f.Name, nullIfEmpty(f.Vendor), nullIfEmpty(f.Purpose),
		class, nullIfEmpty(f.DocumentationStatus)).Scan(&id)
	if err != nil {
		return records.AiSystem{}, fmt.Errorf("postgres: creating an AI system: %w", err)
	}

	if err := t.auditRecord(ctx, "create_ai_system_manual", "ai_systems", id, nil); err != nil {
		return records.AiSystem{}, err
	}

	return t.AiSystem(ctx, id)
}

// UpdateAiSystem replaces a system's fields.
func (t *Tenant) UpdateAiSystem(ctx context.Context, systemID string, f records.AiSystemFields, reviewed bool) (records.AiSystem, error) {
	id, ok := parseID(systemID)
	if !ok {
		return records.AiSystem{}, pgx.ErrNoRows
	}

	var oldClass string
	err := t.tx.QueryRow(ctx,
		`select risk_classification from ai_systems where id = $1 and org_id = $2`,
		id, t.orgID).Scan(&oldClass)
	if errors.Is(err, pgx.ErrNoRows) {
		// One answer for "no such system" and "not yours". The lookup is
		// org-scoped and cannot tell them apart either.
		return records.AiSystem{}, pgx.ErrNoRows
	}
	if err != nil {
		return records.AiSystem{}, fmt.Errorf("postgres: reading an AI system: %w", err)
	}

	// A blank classification on input means leave it alone, so a form that does
	// not render the field cannot silently reclassify.
	newClass := strings.TrimSpace(f.RiskClassification)
	if newClass == "" {
		newClass = oldClass
	}
	reclassified := newClass != oldClass

	if reclassified && !reviewed {
		return records.AiSystem{}, fmt.Errorf(
			"%w: a classification change requires a reviewed approval", ErrReviewRequired)
	}

	before, err := t.recordSnapshot(ctx, "ai_systems", id.String())
	if err != nil {
		return records.AiSystem{}, err
	}

	if _, err := t.tx.Exec(ctx, `
		update ai_systems set
			name                 = coalesce(nullif(btrim($2), ''), name),
			vendor               = $3,
			purpose              = $4,
			risk_classification  = $5,
			documentation_status = coalesce(nullif(btrim($6), ''), documentation_status),
			last_reviewed_at     = case when $7 then now() else last_reviewed_at end
		 where id = $1 and org_id = $8
	`, id, f.Name, nullIfEmpty(f.Vendor), nullIfEmpty(f.Purpose), newClass,
		nullIfEmpty(f.DocumentationStatus), reclassified, t.orgID); err != nil {
		return records.AiSystem{}, fmt.Errorf("postgres: updating an AI system: %w", err)
	}

	// The action names which kind of change it was, because a reclassification
	// is the one an auditor looks for.
	action := "update_ai_system"
	if reclassified {
		action = "reclassify_ai_system"
	}
	if err := t.auditIfChanged(ctx, action, "ai_systems", id.String(), before); err != nil {
		return records.AiSystem{}, err
	}

	return t.AiSystem(ctx, id.String())
}

// DsarResponseWindow is Article 12(3)'s deadline: one month from receipt.
//
// Thirty days rather than a calendar month, carried over unchanged. It is the
// figure the register has always used and changing it would move every existing
// deadline, so it is stated here as a constant to be argued with rather than
// buried in an interval literal.
const DsarResponseWindow = 30 * 24 * time.Hour

// LogDsar records a request that arrived.
func (t *Tenant) LogDsar(ctx context.Context, subjectName, requestType, handler string, receivedAt time.Time) (records.Dsar, error) {
	// Zero means today, which is the common case rather than a missing value.
	// Resolved here so there is one clock: a caller computing its own "today"
	// would be a second implementation that drifts by a timezone.
	var received *time.Time
	if !receivedAt.IsZero() {
		received = &receivedAt

		// Refused rather than clamped, and the asymmetry with the snooze clamp
		// is the point. A snooze length is a preference; a receipt date is the
		// regulatory fact the Article 12(3) deadline is computed from. Clamping
		// would accept a typo and record a deadline nobody chose, which is
		// exactly the kind of quietly-wrong date a compliance record must not
		// contain.
		//
		// Compared against the database's clock rather than this process's, so a
		// core-api container with a skewed clock cannot refuse a date the
		// database would accept, or accept one it would not.
		var future bool
		if err := t.tx.QueryRow(ctx,
			`select $1::timestamptz > now()`, receivedAt).Scan(&future); err != nil {
			return records.Dsar{}, fmt.Errorf("postgres: checking the receipt date: %w", err)
		}
		if future {
			return records.Dsar{}, fmt.Errorf(
				"%w: received_at %s is in the future; a request cannot have arrived yet",
				ErrFutureReceipt, receivedAt.Format(time.RFC3339))
		}
	}

	var id string
	err := t.tx.QueryRow(ctx, `
		insert into dsars (
			org_id, created_by, finding_id, subject_name, request_type, handler,
			status, received_at, response_due_at
		)
		select $1, $2, null, nullif(btrim($3), ''), nullif(btrim($4), ''),
		       nullif(btrim($5), ''), 'open', r.received, r.received + $7::interval
		  from (select coalesce($6::timestamptz, now()) as received) r
		returning id::text
	`, t.orgID, t.userID, subjectName, requestType, handler, received,
		fmt.Sprintf("%d seconds", int(DsarResponseWindow.Seconds()))).Scan(&id)
	if err != nil {
		return records.Dsar{}, fmt.Errorf("postgres: logging a DSAR: %w", err)
	}

	if err := t.auditRecord(ctx, "create_dsar_manual", "dsars", id, nil); err != nil {
		return records.Dsar{}, err
	}

	return t.Dsar(ctx, id)
}

// MarkDsarResponded stops the statutory clock.
//
// Reports whether this call was the one that changed anything, read BEFORE
// acting: reading afterwards would report applied on every repeat call, which
// is the exact bug the findings act path shipped with and had to fix.
func (t *Tenant) MarkDsarResponded(ctx context.Context, dsarID string, reviewed bool) (records.Dsar, bool, error) {
	id, ok := parseID(dsarID)
	if !ok {
		return records.Dsar{}, false, pgx.ErrNoRows
	}

	// The review gate is checked BEFORE the existence lookup, which preserves an
	// ordering the SQL had and which is easy to reverse by accident. An
	// unreviewed call about a DSAR that does not exist answers "requires a
	// reviewed approval", not "not found". That is the better order: it refuses
	// on the caller's own mistake without first confirming whether an id they
	// guessed is real.
	if !reviewed {
		return records.Dsar{}, false, fmt.Errorf(
			"%w: marking a DSAR responded requires a reviewed approval", ErrReviewRequired)
	}

	before, err := t.Dsar(ctx, dsarID)
	if err != nil {
		return records.Dsar{}, false, err
	}
	if before.Status == "responded" || before.Status == "closed" {
		// Already answered. Idempotent, and nothing written.
		return before, false, nil
	}

	snapshot, err := t.recordSnapshot(ctx, "dsars", id.String())
	if err != nil {
		return records.Dsar{}, false, err
	}

	if _, err := t.tx.Exec(ctx, `
		update dsars set status = 'responded', responded_at = now()
		 where id = $1 and org_id = $2
	`, id, t.orgID); err != nil {
		return records.Dsar{}, false, fmt.Errorf("postgres: marking a DSAR responded: %w", err)
	}

	if err := t.auditRecord(ctx, "mark_dsar_responded", "dsars", id.String(), snapshot); err != nil {
		return records.Dsar{}, false, err
	}

	after, err := t.Dsar(ctx, id.String())
	if err != nil {
		return records.Dsar{}, false, err
	}
	return after, true, nil
}

// recordSnapshot reads a row as JSON, for the `before` half of an audit row.
//
// The table name is interpolated and every caller passes a literal. Checked
// against a fixed set anyway, because a helper that formats SQL is one
// refactor away from being handed a variable, and the day that happens should
// be a compile-time-looking failure rather than an injection.
func (t *Tenant) recordSnapshot(ctx context.Context, table, id string) ([]byte, error) {
	switch table {
	case "processing_activities", "ai_systems", "dsars":
	default:
		return nil, fmt.Errorf("postgres: %q is not an auditable record table", table)
	}

	var snapshot []byte
	err := t.tx.QueryRow(ctx, fmt.Sprintf(
		`select to_jsonb(r.*) from %s r where r.id = $1 and r.org_id = $2`, table),
		id, t.orgID).Scan(&snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		// The record is not there, or not this organisation's. The caller
		// decides what that means; for an update it is ErrNoRows.
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: reading a record snapshot: %w", err)
	}
	return snapshot, nil
}

// auditRecord writes an audit row for a record write.
func (t *Tenant) auditRecord(ctx context.Context, action, table, id string, before []byte) error {
	after, err := t.recordSnapshot(ctx, table, id)
	if err != nil {
		return err
	}

	// `finding_id` links the row to the finding that caused it, when one did.
	// Read from the record rather than passed in, so a manual write and an
	// Executor-created one are described the same way.
	var findingID *string
	if err := t.tx.QueryRow(ctx, fmt.Sprintf(
		`select finding_id::text from %s where id = $1`, table), id).Scan(&findingID); err != nil {
		return fmt.Errorf("postgres: reading the originating finding: %w", err)
	}

	_, err = t.tx.Exec(ctx, `
		select record_audit_log($1, $2, $3, $4, $5, $6, $7, $8, $2)
	`, t.orgID, t.userID, findingID, action, table, id, before, after)
	if err != nil {
		return fmt.Errorf("postgres: recording the audit row: %w", err)
	}
	return nil
}

// auditIfChanged writes an audit row only when something actually changed.
//
// `updated_at` is excluded from the comparison because a trigger bumps it on
// every UPDATE, so including it would record an audit row for a save that
// changed nothing. An audit trail full of no-op entries is one nobody reads,
// which costs more than the entries save.
func (t *Tenant) auditIfChanged(ctx context.Context, action, table, id string, before []byte) error {
	var changed bool
	if err := t.tx.QueryRow(ctx, fmt.Sprintf(`
		select ($1::jsonb - 'updated_at') is distinct from (to_jsonb(r.*) - 'updated_at')
		  from %s r where r.id = $2
	`, table), before, id).Scan(&changed); err != nil {
		return fmt.Errorf("postgres: comparing a record: %w", err)
	}
	if !changed {
		return nil
	}
	return t.auditRecord(ctx, action, table, id, before)
}
