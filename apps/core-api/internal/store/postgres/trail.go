package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	records "github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The DSAR trail: reads and the one append (ENT-226, 00024).
//
// # WHAT THIS IS FOR, AND WHY IT IS NOT THE AUDIT LOG
//
// `audit_log` records that somebody changed a row. The trail records how a
// response to a statutory request was assembled: which of the customer's stores
// was searched, when, what came back, and what went into the answer. The first
// is internal accountability, the second is the customer's own evidence, which
// they read, export and hand to a regulator.
//
// Writing one still writes the other. An access to a data subject's data in
// service of a DSAR is still an access, so `AddTrailEntry` appends the trail row
// and its audit row in the request's transaction, which is what stops the audit
// row being a second statement that can fail on its own.
//
// # NO ORG PREDICATE IN THE READS, AND A DELIBERATE ONE IN THE WRITE
//
// Reads rely on RLS, the same rule the rest of this store follows: a second
// statement of the tenancy is a second place it can be wrong. The insert names
// `org_id` because a row has to carry one, and the value comes from the
// transaction rather than from the caller. The database then checks it twice
// over: the RLS with-check refuses a row for another organisation, and the
// composite foreign key onto `dsars (id, org_id)` refuses an entry filed
// against another organisation's request even where the row's own org is right.

// ErrFutureOccurrence means a trail entry claims to have happened in the future.
//
// Refused rather than clamped, exactly as ErrFutureReceipt is and for the same
// reason: the point of `occurred_at` is to record when a search actually
// happened, and a value the database quietly rewrote is a worse record than a
// refusal the caller can fix.
var ErrFutureOccurrence = errors.New("postgres: the entry occurred in the future")

// ErrUnknownAgentRun means the run named as provenance is not one this
// organisation can see.
//
// Its own sentinel rather than pgx.ErrNoRows, because the caller has to be able
// to tell "no such request" from "no such run": one is a bad URL and the other
// is a bad field, and a console showing the wrong one sends somebody looking in
// the wrong place.
var ErrUnknownAgentRun = errors.New("postgres: no such agent run")

const trailEntryColumns = `
	e.id::text,
	e.dsar_id::text,
	e.source,
	e.action,
	coalesce(e.detail, ''),
	e.occurred_at,
	e.recorded_at,
	coalesce(e.created_by::text, ''),
	coalesce(e.agent_run_id::text, '')
`

func scanTrailEntry(row pgx.Row) (records.TrailEntry, error) {
	var e records.TrailEntry
	err := row.Scan(
		&e.ID, &e.DsarID, &e.Source, &e.Action, &e.Detail,
		&e.OccurredAt, &e.RecordedAt, &e.CreatedBy, &e.AgentRunID,
	)
	return e, err
}

// DsarTrail is one page of a request's trail, oldest first.
//
// Chronological, which is the opposite of every other list in this store. The
// registers answer "what is outstanding" and lead with the most pressing row; a
// trail answers "what did you do", and that reads forward. The cursor
// comparison ascends with it, so a token from here is meaningless elsewhere.
//
// Ordered by `occurred_at` rather than `recorded_at`, over the
// `(dsar_id, occurred_at, id)` index 00024 adds, because a reader wants the
// order the work happened in rather than the order it was typed up in.
//
// The DSAR is read first, on the same transaction and therefore under the same
// policies, so a trail is never served for a request the caller cannot see. RLS
// on the entries would return an empty page either way, and an empty page and
// "no such request" are different answers a console renders differently.
func (t *Tenant) DsarTrail(ctx context.Context, dsarID, cursor string, pageSize int) (records.Page[records.TrailEntry], error) {
	var page records.Page[records.TrailEntry]

	id, ok := parseID(dsarID)
	if !ok {
		return page, pgx.ErrNoRows
	}
	if _, err := t.Dsar(ctx, dsarID); err != nil {
		return page, err
	}

	limit := clampPageSize(pageSize)
	args := []any{id}
	clause := ""

	if cursor != "" {
		at, cursorID, err := decodeCursor(cursor)
		if err != nil {
			return page, err
		}
		args = append(args, at, cursorID)
		// Greater-than, not less-than: this list ascends.
		clause = fmt.Sprintf("and (e.occurred_at, e.id) > ($%d, $%d)", len(args)-1, len(args))
	}

	// One more than asked for, so "is there another page" is answered by
	// reading rather than by a count that could disagree with it.
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		select %s
		from dsar_trail_entries e
		where e.dsar_id = $1 %s
		order by e.occurred_at asc, e.id asc
		limit $%d
	`, trailEntryColumns, clause, len(args))

	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return page, fmt.Errorf("postgres: listing a DSAR trail: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		e, err := scanTrailEntry(rows)
		if err != nil {
			return records.Page[records.TrailEntry]{}, fmt.Errorf("postgres: scanning a trail entry: %w", err)
		}
		page.Items = append(page.Items, e)
	}
	if err := rows.Err(); err != nil {
		return records.Page[records.TrailEntry]{}, fmt.Errorf("postgres: reading a DSAR trail: %w", err)
	}

	if len(page.Items) > limit {
		last := page.Items[limit-1]
		page.Items = page.Items[:limit]
		// Keyed on `occurred_at`, because that is what this list orders by.
		page.NextCursor = encodeCursor(last.OccurredAt, last.ID)
	}

	return page, nil
}

// AddTrailEntry appends one step to a request's trail.
//
// The only write this table has. There is no update and no delete, in the
// contract or in the grants: an entry is evidence about how a response to a
// statutory request was built, and evidence a producer can revise afterwards is
// worth less than evidence it cannot. Correcting a mistake means adding an entry
// that says so.
func (t *Tenant) AddTrailEntry(
	ctx context.Context,
	dsarID string,
	entry records.TrailEntry,
) (records.TrailEntry, error) {
	id, ok := parseID(dsarID)
	if !ok {
		return records.TrailEntry{}, pgx.ErrNoRows
	}

	// Read the request first, so a caller naming one they cannot see gets
	// "not found" rather than a foreign key error written for a DBA. The
	// composite key still refuses the pair if this check is ever removed, which
	// is the arrangement: the good message is here and the guarantee is there.
	if _, err := t.Dsar(ctx, dsarID); err != nil {
		return records.TrailEntry{}, err
	}

	if !records.ValidTrailAction(entry.Action) {
		// A Go check in front of the same closed set the check constraint
		// holds. The constraint is what makes it true whoever writes; this is
		// what makes the refusal a sentence a person can act on.
		return records.TrailEntry{}, fmt.Errorf(
			"postgres: %q is not a trail action: %w", entry.Action, errBadTrailAction)
	}

	// Zero means now, which is the common case rather than a missing value.
	// Resolved against the database's clock for the same reason the receipt date
	// is: a core-api container with a skewed clock must not be able to refuse a
	// time the database would accept.
	var occurred *time.Time
	if !entry.OccurredAt.IsZero() {
		occurred = &entry.OccurredAt

		var future bool
		if err := t.tx.QueryRow(ctx,
			`select $1::timestamptz > now()`, entry.OccurredAt).Scan(&future); err != nil {
			return records.TrailEntry{}, fmt.Errorf("postgres: checking when the entry occurred: %w", err)
		}
		if future {
			return records.TrailEntry{}, fmt.Errorf(
				"%w: occurred_at %s is in the future; a search cannot have happened yet",
				ErrFutureOccurrence, entry.OccurredAt.Format(time.RFC3339))
		}
	}

	// Provenance a caller supplies is checked against what the caller can see,
	// on the tenant transaction and therefore under `agent_runs`'s own policy.
	// Without this a console could attribute an entry to a run in somebody
	// else's organisation: the foreign key alone would allow it, because
	// referential checks do not run under row security.
	var runID *string
	if entry.AgentRunID != "" {
		runUUID, ok := parseID(entry.AgentRunID)
		if !ok {
			return records.TrailEntry{}, ErrUnknownAgentRun
		}
		var found string
		err := t.tx.QueryRow(ctx,
			`select id::text from agent_runs where id = $1`, runUUID).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return records.TrailEntry{}, ErrUnknownAgentRun
		}
		if err != nil {
			return records.TrailEntry{}, fmt.Errorf("postgres: resolving the agent run: %w", err)
		}
		runID = &found
	}

	// `created_by` comes from the session and never from the request, the same
	// rule every other record write follows: a caller cannot name somebody else
	// as the person who did this.
	var entryID string
	err := t.tx.QueryRow(ctx, `
		insert into dsar_trail_entries
			(org_id, dsar_id, source, action, detail, occurred_at, created_by, agent_run_id)
		values ($1, $2, btrim($3), $4, nullif(btrim($5), ''),
		        coalesce($6::timestamptz, now()), $7, $8)
		returning id::text
	`, t.orgID, id, entry.Source, entry.Action, entry.Detail,
		occurred, t.userID, runID).Scan(&entryID)
	if err != nil {
		return records.TrailEntry{}, fmt.Errorf("postgres: appending a DSAR trail entry: %w", err)
	}

	// The access is auditable in the same transaction as the entry, because an
	// access to a data subject's data in service of a DSAR is still an access.
	// `finding_id` is the DSAR's own, so the audit row hangs off the same
	// finding the request does when one caused it.
	if err := t.auditTrailEntry(ctx, entryID, id.String()); err != nil {
		return records.TrailEntry{}, err
	}

	return scanTrailEntry(t.tx.QueryRow(ctx, fmt.Sprintf(`
		select %s from dsar_trail_entries e where e.id = $1
	`, trailEntryColumns), entryID))
}

// errBadTrailAction is unexported because no caller branches on it: the service
// layer validates the same vocabulary before it reaches here, and this exists so
// a store used directly cannot write a value the console would never send.
var errBadTrailAction = errors.New("postgres: not a trail action")

// auditTrailEntry appends the audit row for an entry.
//
// Its own helper rather than `auditRecord`, which reads `finding_id` off the
// record it is auditing. A trail entry has no finding of its own; the request it
// belongs to may have one, and that is the link worth recording, so the join is
// through `dsars` rather than through the entry.
func (t *Tenant) auditTrailEntry(ctx context.Context, entryID, dsarID string) error {
	var after []byte
	if err := t.tx.QueryRow(ctx,
		`select to_jsonb(e.*) from dsar_trail_entries e where e.id = $1`,
		entryID).Scan(&after); err != nil {
		return fmt.Errorf("postgres: reading a trail entry snapshot: %w", err)
	}

	var findingID *string
	if err := t.tx.QueryRow(ctx,
		`select finding_id::text from dsars where id = $1`, dsarID).Scan(&findingID); err != nil {
		return fmt.Errorf("postgres: reading the originating finding: %w", err)
	}

	// `before` is null and always will be: there is no version of this row that
	// preceded it. An append-only table has no before.
	if _, err := t.tx.Exec(ctx, `
		select record_audit_log($1, $2, $3, $4, $5, $6, null, $7, $2)
	`, t.orgID, t.userID, findingID, "add_dsar_trail_entry",
		"dsar_trail_entries", entryID, after); err != nil {
		return fmt.Errorf("postgres: recording the audit row: %w", err)
	}
	return nil
}
