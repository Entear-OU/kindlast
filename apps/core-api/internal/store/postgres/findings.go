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
	id, ok := parseFindingID(findingID)
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

// ApproveFinding approves, and reports what the Executor created.
//
// The acting user is never passed: approve_finding reads it from the GUC this
// transaction set, which is why the function lost its acting-user parameter in
// the ENT-192 rewrite. A caller cannot name someone else as the approver.
//
// The audit row is written by the database (00006). Writing one here too would
// duplicate it.
func (t *Tenant) ApproveFinding(ctx context.Context, findingID string, reviewed bool) (findings.Acted, error) {
	id, ok := parseFindingID(findingID)
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

	var target *string
	if err := t.tx.QueryRow(ctx,
		`select approve_finding($1, $2)::text`, id, reviewed,
	).Scan(&target); err != nil {
		return findings.Acted{}, fmt.Errorf("postgres: approving a finding: %w", err)
	}

	acted := findings.Acted{Applied: true}
	if target == nil {
		return acted, nil
	}
	acted.CreatedRecordID = *target
	table, err := t.createdRecordTable(ctx, id)
	if err != nil {
		return findings.Acted{}, err
	}
	acted.CreatedRecordTable = table
	return acted, nil
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
	id, ok := parseFindingID(findingID)
	if !ok {
		return findings.Acted{}, nil
	}

	var applied bool
	if err := t.tx.QueryRow(ctx,
		`select reject_finding($1, $2)`, id, nullIfEmpty(reason),
	).Scan(&applied); err != nil {
		return findings.Acted{}, fmt.Errorf("postgres: rejecting a finding: %w", err)
	}
	return findings.Acted{Applied: applied}, nil
}

// SnoozeFinding defers a finding.
//
// Unlike approve and reject this is not idempotent, deliberately: each deferral
// is a fresh decision with a new date and each writes its own audit row. See
// 00006's header.
func (t *Tenant) SnoozeFinding(ctx context.Context, findingID string, days int32) (findings.Acted, error) {
	id, ok := parseFindingID(findingID)
	if !ok {
		return findings.Acted{}, nil
	}

	var until *time.Time
	if err := t.tx.QueryRow(ctx,
		`select snooze_finding($1, $2)`, id, days,
	).Scan(&until); err != nil {
		return findings.Acted{}, fmt.Errorf("postgres: snoozing a finding: %w", err)
	}

	return findings.Acted{Applied: until != nil, SnoozedUntil: until}, nil
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

// createdRecordTable names the table the Executor wrote into.
//
// Selected by excluding rows whose target is the finding itself rather than by
// recency. The decision row and the creation row are both written inside one
// transaction and their order is trigger timing rather than a promise, so
// "newest row" would be coupling to something the schema does not guarantee.
func (t *Tenant) createdRecordTable(ctx context.Context, id uuid.UUID) (string, error) {
	var table string
	err := t.tx.QueryRow(ctx, `
		select target_table
		from audit_log
		where finding_id = $1
		  and target_id is not null
		  and target_table <> 'findings'
		order by occurred_at desc
		limit 1
	`, id).Scan(&table)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("postgres: reading the created record's table: %w", err)
	}
	return table, nil
}

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

// parseFindingID turns a caller-supplied id into a uuid.
//
// Returns a bool rather than an error on purpose. A malformed id is not a
// server fault and not something to report: it names no finding, which is the
// same answer as an id naming a finding in another organisation. Carrying it as
// an error would invite a caller to be told the difference, and would make
// every call site look like it was swallowing a failure.
func parseFindingID(findingID string) (uuid.UUID, bool) {
	id, err := uuid.Parse(findingID)
	return id, err == nil
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
