package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/audit"
)

// The audit log, read on the request's transaction so RLS scopes every row
// (ENT-223).
//
// # THIS FILE READS `audit_log` AND JOINS ONE TABLE FOR A NAME
//
// Nothing else. Not traces, not model calls, not anything an observability tool
// holds, which is the firewall §7.2 draws and the property the surface is
// selling: an auditor is buying a record a regulator can be shown, and a record
// assembled partly from a vendor's telemetry has completeness that depends on
// that vendor's retention settings.
//
// # NO ORG PREDICATE, AND NO WRITE
//
// RLS supplies the organisation from `app.current_org_id`. There is also no
// write path here at all, and that is structural rather than a decision this
// file makes: `audit_log` carries an append-only trigger, `kindlast_app` holds
// no update or delete grant on it, and the only insert is `record_audit_log`,
// bound by policy to the human in the GUC.
//
// # WHY THE ACTOR JOIN IS A LEFT JOIN
//
// An actor who has left the organisation has no `user_identities` row this
// caller can see, and their rows must still appear. An audit log that dropped
// entries when somebody was offboarded would be defeatable by offboarding
// somebody. The name goes; the user id and the act stay.

const auditColumns = `
	a.id::text,
	a.occurred_at,
	a.action_type,
	a.user_id::text,
	coalesce(i.display_name, ''),
	coalesce(i.email, ''),
	coalesce(a.actor_role, ''),
	coalesce(a.finding_id::text, ''),
	a.target_table,
	coalesce(a.target_id::text, ''),
	coalesce(a.before::text, ''),
	coalesce(a.after::text, '')
`

func scanAuditEntry(row interface {
	Scan(dest ...any) error
}) (audit.Entry, error) {
	var e audit.Entry
	err := row.Scan(
		&e.ID, &e.OccurredAt, &e.ActionType,
		&e.Actor.UserID, &e.Actor.DisplayName, &e.Actor.Email, &e.Actor.Role,
		&e.FindingID, &e.TargetTable, &e.TargetID,
		&e.BeforeJSON, &e.AfterJSON,
	)
	// Everything in this table today was written by a signed-in human, in the
	// same transaction as the act, by a policy that binds the row to the GUC
	// user. §26's agent runs will set this from `agent_runs` when ENT-218 lands;
	// stating it here rather than leaving the field zero means no reader has to
	// guess whether empty means human or means unknown.
	e.Actor.Kind = audit.ActorHuman
	return e, err
}

// auditPredicates turns a normalised filter into a where clause.
//
// The caller must have called Normalise first: an empty string left in
// `ActionTypes` reaches `= any` as a value that matches no row, which turns a
// filter into a silent no-op rather than an error.
func auditPredicates(filter audit.Filter, args []any) ([]string, []any) {
	var where []string

	if !filter.Since.IsZero() {
		args = append(args, filter.Since)
		where = append(where, fmt.Sprintf("a.occurred_at >= $%d", len(args)))
	}
	if !filter.Until.IsZero() {
		// Exclusive, so consecutive ranges tile without returning a boundary row
		// twice. A duplicated decision in an audit file is a question nobody
		// wants to have to answer.
		args = append(args, filter.Until)
		where = append(where, fmt.Sprintf("a.occurred_at < $%d", len(args)))
	}
	if len(filter.ActionTypes) > 0 {
		args = append(args, filter.ActionTypes)
		where = append(where, fmt.Sprintf("a.action_type = any($%d::text[])", len(args)))
	}
	if len(filter.ActorUserIDs) > 0 {
		args = append(args, filter.ActorUserIDs)
		where = append(where, fmt.Sprintf("a.user_id = any($%d::uuid[])", len(args)))
	}
	if filter.Query != "" {
		// Over the action type, the target table, and the actor's name and
		// email. NOT over `before` and `after`: those hold whatever the acted-on
		// row contained, which for a DSAR includes a data subject's name, and a
		// substring search across them would make the audit log a search engine
		// over the personal data it exists to account for.
		//
		// The pattern is bound as a parameter and the wildcards are added here,
		// so a user typing `%` searches for a per cent sign rather than matching
		// everything.
		args = append(args, "%"+escapeLike(filter.Query)+"%")
		where = append(where, fmt.Sprintf(
			`(a.action_type ilike $%d escape '\'
			  or a.target_table ilike $%d escape '\'
			  or coalesce(i.display_name, '') ilike $%d escape '\'
			  or coalesce(i.email, '') ilike $%d escape '\')`,
			len(args), len(args), len(args), len(args)))
	}

	return where, args
}

// escapeLike neutralises the wildcards in a user's search text.
//
// Without it, a query of `%` matches every row and a query of `_` matches any
// single character, so a person searching for a literal underscore (which
// every action type contains: `approve_finding`) gets results they cannot
// explain. Not a security boundary, since the pattern is already a bound
// parameter and cannot reach the parser as SQL; it is about the filter meaning
// what the person typed.
func escapeLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// AuditEntries returns one page of the log, newest first.
//
// Keyset over `(occurred_at desc, id desc)`, matching `audit_log_org_occurred_idx`.
// The id is a tie-break rather than decoration: an act writes two rows in one
// transaction (the decision, and the record it created) and they share
// `occurred_at` exactly, so without a second ordering column a cursor could skip
// one or repeat it forever.
func (t *Tenant) AuditEntries(
	ctx context.Context, filter audit.Filter, cursor string, pageSize int,
) ([]audit.Entry, string, error) {
	limit := audit.ClampPageSize(pageSize)

	where, args := auditPredicates(filter, nil)

	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		args = append(args, at, id)
		where = append(where, fmt.Sprintf("(a.occurred_at, a.id) < ($%d, $%d)", len(args)-1, len(args)))
	}

	clause := ""
	if len(where) > 0 {
		clause = "where " + strings.Join(where, " and ")
	}

	// One more than asked for, so "is there another page" is answered by reading
	// rather than by a count query that could disagree with it.
	args = append(args, limit+1)

	query := fmt.Sprintf(`
		select %s
		from audit_log a
		left join user_identities i on i.user_id = a.user_id
		%s
		order by a.occurred_at desc, a.id desc
		limit $%d
	`, auditColumns, clause, len(args))

	entries, err := t.scanAuditRows(ctx, query, args)
	if err != nil {
		return nil, "", err
	}

	var next string
	if len(entries) > limit {
		last := entries[limit-1]
		entries = entries[:limit]
		next = encodeCursor(last.OccurredAt, last.ID)
	}
	return entries, next, nil
}

// AuditEntriesForExport returns every matching row up to the cap, newest first.
//
// Deliberately not "the same as the list without a page size". The list is paged
// by construction and its caller can keep asking; an export answers "everything
// that matched" in one artefact that leaves the building, so the cap is read
// back to the caller rather than applied quietly. The bool is whether the cap
// was hit.
func (t *Tenant) AuditEntriesForExport(
	ctx context.Context, filter audit.Filter,
) ([]audit.Entry, bool, error) {
	where, args := auditPredicates(filter, nil)

	clause := ""
	if len(where) > 0 {
		clause = "where " + strings.Join(where, " and ")
	}

	// One past the cap, so truncation is detected by reading rather than by
	// assuming a full result means more exist. A set of exactly ExportRowCap
	// rows is complete and must not be reported as truncated.
	args = append(args, audit.ExportRowCap+1)

	query := fmt.Sprintf(`
		select %s
		from audit_log a
		left join user_identities i on i.user_id = a.user_id
		%s
		order by a.occurred_at desc, a.id desc
		limit $%d
	`, auditColumns, clause, len(args))

	entries, err := t.scanAuditRows(ctx, query, args)
	if err != nil {
		return nil, false, err
	}

	if len(entries) > audit.ExportRowCap {
		return entries[:audit.ExportRowCap], true, nil
	}
	return entries, false, nil
}

// AuditActionTypes lists every action type present in this organisation's log.
//
// Unfiltered on purpose: it populates a filter control, and a control offering
// values that are already excluded by the current filter would empty itself as
// soon as somebody used it. Distinct over the org's rows rather than over a
// constant list, so a console never offers a value that would return nothing.
func (t *Tenant) AuditActionTypes(ctx context.Context) ([]string, error) {
	rows, err := t.tx.Query(ctx, `
		select distinct action_type from audit_log order by action_type
	`)
	if err != nil {
		return nil, fmt.Errorf("postgres: listing audit action types: %w", err)
	}
	defer rows.Close()

	var types []string
	for rows.Next() {
		var actionType string
		if err := rows.Scan(&actionType); err != nil {
			return nil, fmt.Errorf("postgres: scanning an action type: %w", err)
		}
		types = append(types, actionType)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading audit action types: %w", err)
	}
	return types, nil
}

func (t *Tenant) scanAuditRows(ctx context.Context, query string, args []any) ([]audit.Entry, error) {
	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading the audit log: %w", err)
	}
	defer rows.Close()

	var entries []audit.Entry
	for rows.Next() {
		entry, err := scanAuditEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scanning an audit entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading audit entries: %w", err)
	}
	return entries, nil
}
