package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
)

// ErrBadCursor is returned when a page token is not one this store issued.
var ErrBadCursor = errors.New("postgres: the page token is not usable")

// DefaultPageSize is what a request that names no size gets.
const DefaultPageSize = 25

// MaxPageSize caps what a caller can ask for.
//
// A cap rather than an error above it, because a client asking for 500 wants a
// big page rather than a refusal, and the feed is rendered rather than
// exported. The number matters less than there being one: without a cap a
// single request can pin a connection reading a customer's entire history.
const MaxPageSize = 100

// findingColumns is the select list shared by the feed and the detail view, so
// the two cannot drift into showing different things about the same finding.
//
// The citation columns come from `obligations` rather than from the finding,
// with two deliberate exceptions. `f.regulatory_obligation` and `f.citation_url`
// are the label and link the Analyst assembled AT THE TIME, and they are what
// gets rendered. Reading the label live from the obligation would mean a
// finding's citation silently changing when an obligation is reworded, which is
// precisely the drift a compliance record must not have. The structured columns
// are joined live because they are identifiers rather than prose.
const findingColumns = `
	f.id::text,
	f.status,
	f.severity::text,
	f.detected,
	f.proposed_action,
	f.effort_estimate::text,
	f.action_type,
	coalesce(f.obligation_slug, ''),
	coalesce(o.title, ''),
	coalesce(o.citation_celex, ''),
	coalesce(o.citation_kind, ''),
	coalesce(o.citation_article, 0),
	coalesce(o.citation_recital, 0),
	coalesce(o.citation_annex, ''),
	coalesce(o.citation_paragraph, ''),
	coalesce(f.regulatory_obligation, ''),
	coalesce(f.citation_url, ''),
	f.created_at,
	f.snoozed_until,
	coalesce(f.approved_by::text, ''),
	coalesce(f.rejection_reason, '')
`

func scanFinding(row pgx.Row) (findings.Finding, error) {
	var f findings.Finding
	err := row.Scan(
		&f.ID, &f.Status, &f.Severity, &f.Detected, &f.ProposedAction,
		&f.EffortEstimate, &f.ActionType,
		&f.Citation.ObligationSlug, &f.Citation.Title, &f.Citation.CELEX,
		&f.Citation.Kind, &f.Citation.Article, &f.Citation.Recital,
		&f.Citation.Annex, &f.Citation.Paragraph,
		&f.Citation.Label, &f.Citation.URL,
		&f.CreatedAt, &f.SnoozedUntil, &f.ApprovedBy, &f.RejectionReason,
	)
	return f, err
}

// Findings is the feed: one page, newest first.
//
// Ordered by `(created_at desc, id desc)` over the `(org_id, created_at desc)`
// index 00002 built for this query. The id is a tie-break rather than
// decoration: two findings created in the same transaction share a timestamp,
// and without a second ordering column their relative order is whatever the
// planner chose, so a keyset cursor could skip one or repeat it forever.
//
// Keyset rather than offset because the feed is written to while it is read. An
// offset page 2 silently omits a row whenever one arrives between requests, and
// here that row is a compliance gap the customer never saw.
//
// No org predicate in the WHERE clause. RLS supplies it, and adding a second
// one here would create a place where the two could disagree.
func (t *Tenant) Findings(ctx context.Context, status, cursor string, pageSize int) (findings.Page, error) {
	limit := pageSize
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}

	where := []string{}
	args := []any{}

	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("f.status = $%d", len(args)))
	}

	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return findings.Page{}, err
		}
		args = append(args, at, id)
		where = append(where, fmt.Sprintf("(f.created_at, f.id) < ($%d, $%d)", len(args)-1, len(args)))
	}

	clause := ""
	if len(where) > 0 {
		clause = "where " + strings.Join(where, " and ")
	}

	// One more than asked for, so "is there another page" is answered by
	// reading rather than by a second count query that could disagree with it.
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		select %s
		from findings f
		left join obligations o on o.id = f.obligation_id
		%s
		order by f.created_at desc, f.id desc
		limit $%d
	`, findingColumns, clause, len(args))

	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return findings.Page{}, fmt.Errorf("postgres: listing findings: %w", err)
	}
	defer rows.Close()

	var page findings.Page
	for rows.Next() {
		f, err := scanFinding(rows)
		if err != nil {
			return findings.Page{}, fmt.Errorf("postgres: scanning a finding: %w", err)
		}
		page.Findings = append(page.Findings, f)
	}
	if err := rows.Err(); err != nil {
		return findings.Page{}, fmt.Errorf("postgres: reading findings: %w", err)
	}

	if len(page.Findings) > limit {
		last := page.Findings[limit-1]
		page.Findings = page.Findings[:limit]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return page, nil
}

// Finding reads one finding and the regulatory text behind it.
//
// Returns pgx.ErrNoRows for a finding that does not exist and for one in
// another organisation alike, because RLS makes them the same query result and
// the handler must not tell them apart.
func (t *Tenant) Finding(ctx context.Context, findingID string) (findings.Finding, []findings.SupportingChunk, error) {
	id, ok := parseID(findingID)
	if !ok {
		// Refused before it reaches SQL, so a malformed id reads as "no such
		// finding" rather than as a cast error from inside a policy.
		return findings.Finding{}, nil, pgx.ErrNoRows
	}

	query := fmt.Sprintf(`
		select %s
		from findings f
		left join obligations o on o.id = f.obligation_id
		where f.id = $1
	`, findingColumns)

	f, err := scanFinding(t.tx.QueryRow(ctx, query, id))
	if err != nil {
		return findings.Finding{}, nil, err
	}

	// finding_supporting_chunks is org-gated in the database as well, and
	// returns no rows rather than raising when the finding is not the caller's.
	// Coalesced because the function returns nulls: a corpus row can carry a
	// label and no body, and an article can have no anchor URL. A chunk with no
	// text is still a citation worth showing, so the null becomes an empty
	// string here rather than an error that loses the whole detail view.
	rows, err := t.tx.Query(ctx, `
		select ordinal,
		       coalesce(label, ''),
		       coalesce(quoted_text, ''),
		       coalesce(source_url, '')
		from finding_supporting_chunks($1)
		order by ordinal
	`, id)
	if err != nil {
		return findings.Finding{}, nil, fmt.Errorf("postgres: reading supporting chunks: %w", err)
	}
	defer rows.Close()

	var chunks []findings.SupportingChunk
	for rows.Next() {
		var c findings.SupportingChunk
		if err := rows.Scan(&c.Ordinal, &c.Label, &c.QuotedText, &c.SourceURL); err != nil {
			return findings.Finding{}, nil, fmt.Errorf("postgres: scanning a chunk: %w", err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return findings.Finding{}, nil, fmt.Errorf("postgres: reading chunks: %w", err)
	}

	return f, chunks, nil
}

// The act path, in Go (ENT-225 phase 1).
//
// # WHAT MOVED AND WHAT DID NOT
//
// `approve_finding`, `reject_finding` and `snooze_finding` were plpgsql until
// 00016. Each decided something: which status transition counts as a repeat,
// how many rejections of the same obligation make a product-review flag, how
// far a deferral may be pushed. Decisions are Go's (§14.5), so they are here.
//
// Three things deliberately did not move, and each would be a mistake to
// "finish" later:
//
//   - The acting user is still never passed in. It is read from
//     `app.current_user_id`, the GUC this transaction set, so a caller cannot
//     name somebody else as the approver. Adding a parameter would make the
//     handler the thing that refuses, when the session already does.
//   - Visibility is still RLS. Every statement below is org-scoped by policy,
//     not by a `where org_id = ?` this code could forget. The explicit org
//     predicates that remain are belt and braces, not the boundary.
//   - `record_audit_log` stays a database function and is called from here. It
//     snapshots the actor's role at the time of the action and writes an
//     append-only row, which is an invariant rather than a decision, and the
//     three Executor triggers call it too, from inside the UPDATE below. A Go
//     reimplementation would be a second writer of the same regulatory record.
//
// # THE ORDER OF THE TWO AUDIT ROWS
//
// Approving a finding whose action creates a record writes two rows: the
// Executor's creation row, from an `after update of status` trigger, and this
// decision row. The trigger fires during the UPDATE, so the creation row lands
// first and the decision row second, exactly as it did when the function did
// this. The tests assert set membership rather than order because nothing
// should depend on it, but the sequence here is chosen to keep the observed
// behaviour unchanged rather than to alter it silently.

// ApproveFinding approves, and reports what the Executor created.
func (t *Tenant) ApproveFinding(ctx context.Context, findingID string, reviewed bool) (findings.Acted, error) {
	id, ok := parseID(findingID)
	if !ok {
		return findings.Acted{}, nil
	}

	// The status is read BEFORE acting, and that ordering is the whole of the
	// idempotency contract. approve_finding returns null both when it changed
	// nothing and when it changed something that created no record, so its
	// return value cannot carry "applied". Reading the status afterwards is
	// worse still: a second approval leaves the finding approved, so an
	// after-the-fact check reports applied on every repeat call.
	before, visible, err := t.status(ctx, id)
	if err != nil {
		return findings.Acted{}, err
	}
	if !visible {
		// Unknown, or another organisation's. One answer for both.
		return findings.Acted{}, nil
	}

	if before == "approved" {
		// Already approved: nothing applied, and nothing written. The created
		// record is still reported, so a retry or a double submit navigates to
		// the same place the first call did rather than nowhere.
		return t.withCreatedRecord(ctx, id, findings.Acted{Applied: false})
	}

	snapshot, err := t.findingSnapshot(ctx, id)
	if err != nil {
		return findings.Acted{}, err
	}

	// `status <> 'approved'` and the read above are two guards, and measuring
	// which one the tests exercise was worth doing.
	//
	// Disabling either alone leaves `TestApprovingTwiceThroughTheAPIWritesNoSecondRow`
	// green; disabling both turns it red. So the test proves the behaviour and
	// not this line, and the redundancy is real rather than belt-and-braces
	// phrasing.
	//
	// They are kept because they cover different cases. The read decides what to
	// report, and it is what makes a second approval return the created record
	// rather than nothing. This decides what to write, and it is the only one
	// that survives two callers racing, where both pass the read and one must
	// still lose. Nothing tests that race, which is worth knowing rather than
	// implying otherwise.
	var updated *string
	if err := t.tx.QueryRow(ctx, `
		update findings
		   set status = 'approved',
		       approved_by = $2,
		       approval_reviewed = $3
		 where id = $1
		   and org_id = $4
		   and status <> 'approved'
		returning id::text
	`, id, t.userID, reviewed, t.orgID).Scan(&updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Lost the race, or already approved. Nothing written, and the
			// created record is still reported so a double submit navigates
			// where the first call did.
			return t.withCreatedRecord(ctx, id, findings.Acted{Applied: false})
		}
		return findings.Acted{}, fmt.Errorf("postgres: approving a finding: %w", err)
	}

	// Read after the UPDATE, so the Executor trigger's creation row is already
	// there. This is the same ordering the function had.
	acted := findings.Acted{Applied: true}
	acted, err = t.withCreatedRecord(ctx, id, acted)
	if err != nil {
		return findings.Acted{}, err
	}

	if err := t.recordAudit(ctx, auditEntry{
		FindingID:   &id,
		ActionType:  "approve_finding",
		TargetTable: "findings",
		TargetID:    &id,
		Before:      snapshot,
		After:       nil, // read below, after the write
	}); err != nil {
		return findings.Acted{}, err
	}
	return acted, nil
}

// findingSnapshot reads a finding as JSON, for the `before` half of an audit
// row.
//
// Org-scoped by policy. A finding in another organisation reads as nothing
// here, and the UPDATE that follows matches nothing either, so the two agree
// without this function needing to know why.
func (t *Tenant) findingSnapshot(ctx context.Context, id uuid.UUID) ([]byte, error) {
	var snapshot []byte
	err := t.tx.QueryRow(ctx,
		`select to_jsonb(f.*) from findings f where f.id = $1`, id).Scan(&snapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: reading a finding snapshot: %w", err)
	}
	return snapshot, nil
}

// auditEntry is one row for `record_audit_log`.
type auditEntry struct {
	FindingID   *uuid.UUID
	ActionType  string
	TargetTable string
	TargetID    *uuid.UUID
	Before      []byte
	After       []byte
}

// recordAudit writes the decision row.
//
// Through the database function rather than a direct insert, deliberately. It
// snapshots the actor's role at the time of the action, which the regulatory
// record needs because roles change, and the three Executor triggers call the
// same function from inside the UPDATE that precedes this. Reimplementing it in
// Go would give one audit trail two writers that could disagree about what a
// row means.
//
// `After` is read here rather than by the caller, so it is always the state as
// at the moment the row is written and cannot be a stale value the caller
// happened to fetch earlier.
func (t *Tenant) recordAudit(ctx context.Context, entry auditEntry) error {
	after := entry.After
	if after == nil && entry.TargetTable == "findings" && entry.TargetID != nil {
		var err error
		after, err = t.findingSnapshot(ctx, *entry.TargetID)
		if err != nil {
			return err
		}
	}

	_, err := t.tx.Exec(ctx, `
		select record_audit_log($1, $2, $3, $4, $5, $6, $7, $8, $2)
	`, t.orgID, t.userID, entry.FindingID, entry.ActionType,
		entry.TargetTable, entry.TargetID, entry.Before, after)
	if err != nil {
		return fmt.Errorf("postgres: recording the audit row: %w", err)
	}
	return nil
}

// withCreatedRecord fills in the Executor's record for a finding that was
// already acted on.
func (t *Tenant) withCreatedRecord(ctx context.Context, id uuid.UUID, acted findings.Acted) (findings.Acted, error) {
	var recordID, table string
	err := t.tx.QueryRow(ctx, `
		select target_id::text, target_table
		from audit_log
		where finding_id = $1
		  and target_id is not null
		  and target_table <> 'findings'
		order by occurred_at desc
		limit 1
	`, id).Scan(&recordID, &table)
	if errors.Is(err, pgx.ErrNoRows) {
		return acted, nil
	}
	if err != nil {
		return findings.Acted{}, fmt.Errorf("postgres: reading the created record: %w", err)
	}
	acted.CreatedRecordID = recordID
	acted.CreatedRecordTable = table
	return acted, nil
}

// RejectFinding rejects, optionally recording why.
func (t *Tenant) RejectFinding(ctx context.Context, findingID, reason string) (findings.Acted, error) {
	id, ok := parseID(findingID)
	if !ok {
		return findings.Acted{}, nil
	}

	before, err := t.findingSnapshot(ctx, id)
	if err != nil {
		return findings.Acted{}, err
	}

	var (
		updated string
		profile *uuid.UUID
		slug    *string
	)
	err = t.tx.QueryRow(ctx, `
		update findings
		   set status = 'rejected',
		       rejection_reason = nullif(btrim($2), ''),
		       snoozed_until = null
		 where id = $1
		   and org_id = $3
		   and status <> 'rejected'
		returning id::text, profile_id, obligation_slug
	`, id, reason, t.orgID).Scan(&updated, &profile, &slug)

	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown, another organisation's, or already rejected. One answer for
		// all three, as before.
		return findings.Acted{Applied: false}, nil
	}
	if err != nil {
		return findings.Acted{}, fmt.Errorf("postgres: rejecting a finding: %w", err)
	}

	if slug != nil && profile != nil {
		if err := t.flagForProductReview(ctx, *profile, *slug, id); err != nil {
			return findings.Acted{}, err
		}
	}

	if err := t.recordAudit(ctx, auditEntry{
		FindingID:   &id,
		ActionType:  "reject_finding",
		TargetTable: "findings",
		TargetID:    &id,
		Before:      before,
	}); err != nil {
		return findings.Acted{}, err
	}
	return findings.Acted{Applied: true}, nil
}

// RejectionsBeforeProductReview is how many times the same obligation has to be
// rejected by one organisation before the product should look at it.
//
// Three, carried over from the SQL unchanged. It is a product decision and it
// now reads as one: a threshold in Go that somebody can argue with, rather than
// a `c_threshold constant int := 3` inside a function body nobody opens.
//
// What it means is worth stating, because the number alone does not. A customer
// rejecting the same obligation three times is not a customer making mistakes.
// It is the product being wrong about them in a way it will keep being wrong
// about, and the flag exists so somebody reads the reasons rather than the
// count.
const RejectionsBeforeProductReview = 3

// flagForProductReview raises a flag when an obligation keeps being rejected.
//
// `on conflict do nothing` because `product_review_flags_no_update` forbids
// changing a flag once written: the row records what was true when it was
// raised, and a later rejection is not a correction to it.
func (t *Tenant) flagForProductReview(ctx context.Context, profile uuid.UUID, slug string, finding uuid.UUID) error {
	var count int
	if err := t.tx.QueryRow(ctx, `
		select count(*) from findings
		 where profile_id = $1 and obligation_slug = $2 and status = 'rejected'
	`, profile, slug).Scan(&count); err != nil {
		return fmt.Errorf("postgres: counting rejections: %w", err)
	}

	if count < RejectionsBeforeProductReview {
		return nil
	}

	_, err := t.tx.Exec(ctx, `
		insert into product_review_flags (
			org_id, created_by, profile_id, obligation_slug, finding_id,
			rejection_count, reasons
		)
		values ($1, $2, $3, $4, $5, $6, (
			select array_remove(array_agg(distinct rejection_reason), null)
			  from findings
			 where profile_id = $3 and obligation_slug = $4 and status = 'rejected'
		))
		on conflict (profile_id, obligation_slug) do nothing
	`, t.orgID, t.userID, profile, slug, finding, count)
	if err != nil {
		return fmt.Errorf("postgres: flagging an obligation for product review: %w", err)
	}
	return nil
}

// SnoozeFinding defers a finding.
//
// Unlike approve and reject this is not idempotent, deliberately: each deferral
// is a fresh decision with a new date and each writes its own audit row. See
// 00006's header.
func (t *Tenant) SnoozeFinding(ctx context.Context, findingID string, days int32) (findings.Acted, error) {
	id, ok := parseID(findingID)
	if !ok {
		return findings.Acted{}, nil
	}

	before, err := t.findingSnapshot(ctx, id)
	if err != nil {
		return findings.Acted{}, err
	}

	var until *time.Time
	err = t.tx.QueryRow(ctx, `
		update findings
		   set status = 'snoozed',
		       snoozed_until = now() + make_interval(days => $2)
		 where id = $1 and org_id = $3
		returning snoozed_until
	`, id, clampSnoozeDays(days), t.orgID).Scan(&until)

	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown, or another organisation's. Note there is no status guard
		// above: a finding already snoozed is snoozed again, on purpose.
		return findings.Acted{Applied: false}, nil
	}
	if err != nil {
		return findings.Acted{}, fmt.Errorf("postgres: snoozing a finding: %w", err)
	}

	if err := t.recordAudit(ctx, auditEntry{
		FindingID:   &id,
		ActionType:  "snooze_finding",
		TargetTable: "findings",
		TargetID:    &id,
		Before:      before,
	}); err != nil {
		return findings.Acted{}, err
	}

	return findings.Acted{Applied: until != nil, SnoozedUntil: until}, nil
}

// Snooze bounds, carried over from the SQL unchanged.
//
// A floor of one day because "snooze until now" is not a deferral, and a
// ceiling of a year because a compliance finding deferred indefinitely is a
// finding quietly deleted, which is the outcome the register exists to prevent.
const (
	MinSnoozeDays     = 1
	MaxSnoozeDays     = 365
	DefaultSnoozeDays = 7
)

// clampSnoozeDays bounds a requested deferral.
//
// Clamped rather than refused, which is the one place in this file where that
// is the right trade: the caller asked to defer, the exact number of days is
// not a regulatory fact, and refusing a slider that went to 400 would fail an
// action whose intent was unambiguous. Contrast `log_dsar`'s future receipt
// date, which is refused precisely because the date IS the regulatory fact.
func clampSnoozeDays(days int32) int32 {
	if days <= 0 {
		return DefaultSnoozeDays
	}
	if days < MinSnoozeDays {
		return MinSnoozeDays
	}
	if days > MaxSnoozeDays {
		return MaxSnoozeDays
	}
	return days
}

// status reads a finding's status, and reports whether the caller can see it
// at all.
//
// visible is false for a finding that does not exist and for one in another
// organisation alike, because RLS makes those the same query result and nothing
// above this may tell them apart.
func (t *Tenant) status(ctx context.Context, id uuid.UUID) (status string, visible bool, err error) {
	err = t.tx.QueryRow(ctx, `select status from findings where id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("postgres: reading a finding status: %w", err)
	}
	return status, true, nil
}

// createdRecordTable is gone (ENT-225).
//
// It read the Executor's created record's table, and `withCreatedRecord` reads
// the id and the table together from the same row. The two existed separately
// because the id came back from `approve_finding` and only the table needed a
// second query; with the approval in Go there is one lookup, so the "exclude
// rows whose target is the finding itself" rule now exists once instead of
// three times (here, in `withCreatedRecord`, and in the SQL function).
//
// That rule is the load-bearing part and it is worth restating where it now
// lives: the decision row and the creation row are written in the same
// transaction, so `occurred_at` is identical on both, because `now()` is the
// transaction timestamp. Ordering by it is not a tiebreak. The filter is what
// makes the lookup unambiguous.

// Dashboard reads the posture inputs, the open counts and the pipeline state.
//
// Deadlines are not their own table and never were: watcher_detect_deadlines
// emits them as signals, so they arrive here as open findings whose signal kind
// is `deadline`, carrying days_remaining in their signal metadata. The posture
// rule is the legacy one; only the source of its inputs changed.
func (t *Tenant) Dashboard(ctx context.Context) (findings.Dashboard, error) {
	rows, err := t.tx.Query(ctx, `
		select severity::text,
		       metadata ->> 'signal_kind',
		       (metadata -> 'signal_metadata' ->> 'days_remaining')::int
		from findings
		where status = 'pending'
	`)
	if err != nil {
		return findings.Dashboard{}, fmt.Errorf("postgres: reading posture inputs: %w", err)
	}
	defer rows.Close()

	var severities []string
	var deadlines []findings.Deadline
	for rows.Next() {
		var severity string
		var kind *string
		var daysRemaining *int
		if err := rows.Scan(&severity, &kind, &daysRemaining); err != nil {
			return findings.Dashboard{}, fmt.Errorf("postgres: scanning a posture input: %w", err)
		}
		severities = append(severities, severity)

		// A deadline signal with no days_remaining cannot be placed relative to
		// the window, so it counts as an open finding and not as a deadline.
		// Guessing a value would move a customer between bands on invented data.
		if kind != nil && *kind == "deadline" && daysRemaining != nil {
			deadlines = append(deadlines, findings.Deadline{
				Severity:      severity,
				DaysRemaining: *daysRemaining,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return findings.Dashboard{}, fmt.Errorf("postgres: reading posture inputs: %w", err)
	}

	pipeline, err := t.pipeline(ctx)
	if err != nil {
		return findings.Dashboard{}, err
	}

	posture := findings.ComputePosture(findings.PostureInputs{
		OpenSeverities: severities,
		Deadlines:      deadlines,
		Assessed:       pipeline.WatcherLastRunAt != nil,
	})

	return findings.Dashboard{
		Posture:  posture,
		Headline: findings.Headline(posture),
		Counts:   findings.CountSeverities(severities),
		Pipeline: pipeline,
	}, nil
}

// pipeline reports whether the agents have run, and whether there is anything
// for them to run against.
//
// max() over the organisation's profiles rather than one profile's column: an
// organisation can hold several, and "when did the Watcher last run here" is
// the most recent of them.
func (t *Tenant) pipeline(ctx context.Context) (findings.Pipeline, error) {
	var lastRun *time.Time
	var profiles int
	if err := t.tx.QueryRow(ctx, `
		select max(watcher_last_run_at), count(*)::int
		from compliance_profiles
	`).Scan(&lastRun, &profiles); err != nil {
		return findings.Pipeline{}, fmt.Errorf("postgres: reading pipeline status: %w", err)
	}
	return findings.Pipeline{WatcherLastRunAt: lastRun, ProfileExists: profiles > 0}, nil
}

// parseID turns a caller-supplied id into a uuid.
//
// Returns a bool rather than an error on purpose. A malformed id is not a
// server fault and not something to report: it names no row, which is the same
// answer as an id naming a row in another organisation. Carrying it as an error
// would invite a caller to be told the difference, and would make every call
// site look like it was swallowing a failure.
//
// Shared by findings and by the record registers, which is why it is not named
// after either.
func parseID(id string) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(id)
	return parsed, err == nil
}

func nullIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// The cursor is opaque to clients and deliberately not a format they can
// construct: it is the keyset position, and a client building one could ask for
// a page starting anywhere.
//
// Not encrypted, because it encodes nothing a caller cannot already see: the
// timestamp and id of a finding they were just shown.

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id),
	)
}

func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, uuid.UUID{}, ErrBadCursor
	}

	at, id, found := strings.Cut(string(raw), "|")
	if !found {
		return time.Time{}, uuid.UUID{}, ErrBadCursor
	}

	parsedAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, uuid.UUID{}, ErrBadCursor
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return time.Time{}, uuid.UUID{}, ErrBadCursor
	}
	return parsedAt, parsedID, nil
}
