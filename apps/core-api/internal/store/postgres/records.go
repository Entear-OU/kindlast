package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The three record registers, read on the request's transaction so RLS scopes
// every row (§14.1).
//
// NO ORG PREDICATE IN ANY QUERY HERE, AND THAT IS THE POINT
//
// Same rule the findings store follows. RLS supplies the organisation from
// `app.current_org_id`, and adding `where org_id = $1` alongside it would create
// a second place the tenancy could be stated and therefore a place the two could
// disagree. The isolation suite asserts the policies; these queries rely on
// them.
//
// PAGINATION IS KEYSET, AND THE DSAR LOG SORTS THE OTHER WAY
//
// ROPA and AI systems page by `(created_at desc, id desc)`. The DSAR log pages
// by `(response_due_at asc, id asc)`, because the only question anyone asks of it
// is which request runs out first. That is not a cosmetic difference: the cursor
// comparison flips direction with it, so a token from one list is meaningless in
// another. `encodeCursor` is shared because the shape is the same; the predicate
// is not.
//
// The id is a tie-break rather than decoration. Two records created in the same
// transaction share a timestamp, and two requests can certainly share a due
// date, so without a second ordering column their relative order is whatever the
// planner chose and a cursor could skip a row or repeat one forever.

const processingActivityColumns = `
	p.id::text,
	p.name,
	coalesce(p.purpose, ''),
	coalesce(p.legal_basis, ''),
	p.data_categories,
	p.recipients,
	coalesce(p.retention_period, ''),
	coalesce(p.finding_id::text, ''),
	p.created_at,
	p.updated_at
`

func scanProcessingActivity(row pgx.Row) (records.ProcessingActivity, error) {
	var p records.ProcessingActivity
	err := row.Scan(
		&p.ID, &p.Name, &p.Purpose, &p.LegalBasis,
		&p.DataCategories, &p.Recipients, &p.RetentionPeriod,
		&p.SourceFindingID, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

const aiSystemColumns = `
	a.id::text,
	a.name,
	coalesce(a.vendor, ''),
	coalesce(a.purpose, ''),
	a.risk_classification,
	a.documentation_status,
	a.last_reviewed_at,
	coalesce(a.finding_id::text, ''),
	a.created_at,
	a.updated_at
`

func scanAiSystem(row pgx.Row) (records.AiSystem, error) {
	var a records.AiSystem
	// last_reviewed_at is nullable and means "never reviewed", which the domain
	// carries as the zero time rather than as a pointer: absent and never are
	// the same fact here, and a *time.Time in the domain would make every reader
	// handle two spellings of it. The pointer exists only long enough to cross
	// the scan.
	var lastReviewed *time.Time
	err := row.Scan(
		&a.ID, &a.Name, &a.Vendor, &a.Purpose,
		&a.RiskClassification, &a.DocumentationStatus, &lastReviewed,
		&a.SourceFindingID, &a.CreatedAt, &a.UpdatedAt,
	)
	if lastReviewed != nil {
		a.LastReviewedAt = *lastReviewed
	}
	return a, err
}

const dsarColumns = `
	d.id::text,
	coalesce(d.subject_name, ''),
	coalesce(d.request_type, ''),
	d.status,
	d.received_at,
	d.response_due_at,
	d.responded_at,
	coalesce(d.handler, ''),
	coalesce(d.finding_id::text, ''),
	d.created_at,
	d.updated_at
`

func scanDsar(row pgx.Row) (records.Dsar, error) {
	var d records.Dsar
	var responded *time.Time
	err := row.Scan(
		&d.ID, &d.SubjectName, &d.RequestType, &d.Status,
		&d.ReceivedAt, &d.ResponseDueAt, &responded,
		&d.Handler, &d.SourceFindingID, &d.CreatedAt, &d.UpdatedAt,
	)
	if responded != nil {
		d.RespondedAt = *responded
	}
	return d, err
}

// ProcessingActivities is one page of the Article 30 record, newest first.
func (t *Tenant) ProcessingActivities(ctx context.Context, cursor string, pageSize int) (records.Page[records.ProcessingActivity], error) {
	var page records.Page[records.ProcessingActivity]

	limit := clampPageSize(pageSize)
	args := []any{}
	clause := ""

	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return page, err
		}
		args = append(args, at, id)
		clause = "where (p.created_at, p.id) < ($1, $2)"
	}

	// One more than asked for, so "is there another page" is answered by
	// reading rather than by a count query that could disagree with it.
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		select %s
		from processing_activities p
		%s
		order by p.created_at desc, p.id desc
		limit $%d
	`, processingActivityColumns, clause, len(args))

	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("postgres: listing processing activities: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		p, err := scanProcessingActivity(rows)
		if err != nil {
			return records.Page[records.ProcessingActivity]{}, fmt.Errorf("postgres: scanning a processing activity: %w", err)
		}
		page.Items = append(page.Items, p)
	}
	if err := rows.Err(); err != nil {
		return records.Page[records.ProcessingActivity]{}, fmt.Errorf("postgres: reading processing activities: %w", err)
	}

	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return page, nil
}

// ProcessingActivity reads one entry.
//
// Returns pgx.ErrNoRows for an entry that does not exist and for one in another
// organisation alike, because RLS makes them the same query result and the
// handler must not be able to tell them apart.
func (t *Tenant) ProcessingActivity(ctx context.Context, activityID string) (records.ProcessingActivity, error) {
	id, ok := parseID(activityID)
	if !ok {
		return records.ProcessingActivity{}, pgx.ErrNoRows
	}

	query := fmt.Sprintf(`
		select %s
		from processing_activities p
		where p.id = $1
	`, processingActivityColumns)

	return scanProcessingActivity(t.tx.QueryRow(ctx, query, id))
}

// ManualActivityQuota is the plan cap on manually-created Article 30 entries,
// and how many of them exist.
//
// Both halves come from the database in one round trip, and the count applies
// the same `finding_id is null` predicate the limit is about. A record the
// Executor created on an approved finding is part of the compliance record and
// is never withheld behind a plan, so it does not count against the cap.
//
// `ropa_manual_activity_limit()` returns null for an unlimited plan, carried
// here as Limit 0 rather than as a pointer: unlimited and "no limit value" are
// the same fact, and a client that has to distinguish them will get it wrong.
func (t *Tenant) ManualActivityQuota(ctx context.Context) (records.Quota, error) {
	var q records.Quota
	var limit *int32

	err := t.tx.QueryRow(ctx, `
		select
			(select count(*) from processing_activities where finding_id is null),
			public.ropa_manual_activity_limit()
	`).Scan(&q.Used, &limit)
	if err != nil {
		return records.Quota{}, fmt.Errorf("postgres: reading the manual activity quota: %w", err)
	}

	if limit != nil {
		q.Limit = *limit
	}
	return q, nil
}

// AiSystems is one page of the AI Act register, newest first.
//
// Ordered over `(org_id, created_at desc)`, which 00011 added. Before that this
// table carried `(org_id)` alone and this query would have sorted the whole
// tenant on every page.
func (t *Tenant) AiSystems(ctx context.Context, cursor string, pageSize int) (records.Page[records.AiSystem], error) {
	var page records.Page[records.AiSystem]

	limit := clampPageSize(pageSize)
	args := []any{}
	clause := ""

	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return page, err
		}
		args = append(args, at, id)
		clause = "where (a.created_at, a.id) < ($1, $2)"
	}

	args = append(args, limit+1)

	query := fmt.Sprintf(`
		select %s
		from ai_systems a
		%s
		order by a.created_at desc, a.id desc
		limit $%d
	`, aiSystemColumns, clause, len(args))

	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("postgres: listing ai systems: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		a, err := scanAiSystem(rows)
		if err != nil {
			return records.Page[records.AiSystem]{}, fmt.Errorf("postgres: scanning an ai system: %w", err)
		}
		page.Items = append(page.Items, a)
	}
	if err := rows.Err(); err != nil {
		return records.Page[records.AiSystem]{}, fmt.Errorf("postgres: reading ai systems: %w", err)
	}

	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return page, nil
}

// AiSystem reads one register entry. Same not-found semantics as
// ProcessingActivity.
func (t *Tenant) AiSystem(ctx context.Context, systemID string) (records.AiSystem, error) {
	id, ok := parseID(systemID)
	if !ok {
		return records.AiSystem{}, pgx.ErrNoRows
	}

	query := fmt.Sprintf(`
		select %s
		from ai_systems a
		where a.id = $1
	`, aiSystemColumns)

	return scanAiSystem(t.tx.QueryRow(ctx, query, id))
}

// Dsars is one page of the DSAR log, soonest deadline first.
//
// The ordering and therefore the cursor comparison are the reverse of the other
// two registers. Ordered over `(org_id, response_due_at)`, which 00011 added:
// the pre-existing `dsars_due_idx` cannot serve this query because it does not
// lead with `org_id` and is partial on the two unfinished statuses, so it
// excludes exactly the answered requests an auditor asks to see.
func (t *Tenant) Dsars(ctx context.Context, status, cursor string, pageSize int) (records.Page[records.Dsar], error) {
	var page records.Page[records.Dsar]

	limit := clampPageSize(pageSize)
	where := []string{}
	args := []any{}

	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("d.status = $%d", len(args)))
	}

	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return page, err
		}
		args = append(args, at, id)
		// Greater-than, not less-than: this list ascends.
		where = append(where, fmt.Sprintf("(d.response_due_at, d.id) > ($%d, $%d)", len(args)-1, len(args)))
	}

	clause := ""
	if len(where) > 0 {
		clause = "where " + strings.Join(where, " and ")
	}

	args = append(args, limit+1)

	query := fmt.Sprintf(`
		select %s
		from dsars d
		%s
		order by d.response_due_at asc, d.id asc
		limit $%d
	`, dsarColumns, clause, len(args))

	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("postgres: listing dsars: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		d, err := scanDsar(rows)
		if err != nil {
			return records.Page[records.Dsar]{}, fmt.Errorf("postgres: scanning a dsar: %w", err)
		}
		page.Items = append(page.Items, d)
	}
	if err := rows.Err(); err != nil {
		return records.Page[records.Dsar]{}, fmt.Errorf("postgres: reading dsars: %w", err)
	}

	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		// Keyed on the due date, because that is what this list orders by.
		// Handing back a created_at cursor here would page through a different
		// sequence than the one being displayed.
		page.NextCursor = encodeCursor(last.ResponseDueAt, last.ID)
	}

	return page, nil
}

// Dsar reads one request. Same not-found semantics as ProcessingActivity.
func (t *Tenant) Dsar(ctx context.Context, dsarID string) (records.Dsar, error) {
	id, ok := parseID(dsarID)
	if !ok {
		return records.Dsar{}, pgx.ErrNoRows
	}

	query := fmt.Sprintf(`
		select %s
		from dsars d
		where d.id = $1
	`, dsarColumns)

	return scanDsar(t.tx.QueryRow(ctx, query, id))
}

func clampPageSize(pageSize int) int {
	switch {
	case pageSize <= 0:
		return DefaultPageSize
	case pageSize > MaxPageSize:
		return MaxPageSize
	default:
		return pageSize
	}
}
