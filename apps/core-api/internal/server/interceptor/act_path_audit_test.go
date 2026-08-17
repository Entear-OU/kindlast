package interceptor_test

import (
	"testing"

	"connectrpc.com/connect"

	corev1 "github.com/Entear-OU/kindlast/gen/go/kindlast/core/v1"
)

// The audit assertions that used to live in db/tests/act-path-audit.test.ts
// (ENT-225).
//
// That file drove the act path by calling `approve_finding` and friends and
// asserted the rows they wrote. Those functions decided things, so they moved
// to Go and 00016 dropped them. The tests moved here rather than being
// rewritten to call a helper that writes the row it then asserts, which would
// have proved nothing.
//
// Asserted through the RPC layer with a real token, which is a stronger claim
// than the SQL version made: it exercises authentication, scope, tenancy and
// the handler as well as the write.
//
// What stayed in db/tests is what is still SQL: the Executor triggers
// (executor-action-type, dsar-clock) and the whole isolation suite, which this
// change does not touch.

func TestRejectingThroughTheAPIWritesOneAuditRowNamingTheAct(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "reject-audit", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	if _, err := feed.RejectFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.RejectFindingRequest{
			FindingId: finding, Reason: "Not us",
		}), ada.headers)); err != nil {
		t.Fatalf("rejecting: %v", err)
	}

	var rows int
	if err := conn.QueryRow(t.Context(),
		`select count(*)::int from audit_log where finding_id = $1`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rejecting wrote %d audit rows, want exactly 1", rows)
	}

	var action, role string
	if err := conn.QueryRow(t.Context(),
		`select action_type, actor_role from audit_log where finding_id = $1`,
		finding).Scan(&action, &role); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}
	if action != "reject_finding" {
		t.Errorf("action_type is %q, want reject_finding", action)
	}
	// The role is snapshotted at the time of the action, because roles change
	// and the trail must say what authority the actor held then.
	if role != "owner" {
		t.Errorf("actor_role is %q, want owner", role)
	}
}

func TestRejectingTwiceThroughTheAPIWritesNoSecondRow(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "reject-twice", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	for _, reason := range []string{"first", "second"} {
		if _, err := feed.RejectFinding(t.Context(), withHeaders(
			connect.NewRequest(&corev1.RejectFindingRequest{
				FindingId: finding, Reason: reason,
			}), ada.headers)); err != nil {
			t.Fatalf("rejecting (%s): %v", reason, err)
		}
	}

	var rows int
	if err := conn.QueryRow(t.Context(),
		`select count(*)::int from audit_log where finding_id = $1`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 1 {
		t.Fatalf("two rejections wrote %d audit rows, want exactly 1", rows)
	}
}

// Snooze is deliberately NOT idempotent, and this is the assertion that says so.
//
// Each deferral is a fresh decision with a new date, so each earns a row. A
// reader of the trail should be able to see that somebody pushed this finding
// twice, which is exactly the pattern worth noticing in a compliance record.
func TestEverySnoozeIsRecordedBecauseEachIsADecision(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "snooze-audit", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	for _, days := range []int32{7, 30} {
		if _, err := feed.SnoozeFinding(t.Context(), withHeaders(
			connect.NewRequest(&corev1.SnoozeFindingRequest{
				FindingId: finding, Days: days,
			}), ada.headers)); err != nil {
			t.Fatalf("snoozing %d days: %v", days, err)
		}
	}

	var rows int
	if err := conn.QueryRow(t.Context(), `
		select count(*)::int from audit_log
		 where finding_id = $1 and action_type = 'snooze_finding'
	`, finding).Scan(&rows); err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	if rows != 2 {
		t.Fatalf("two deferrals wrote %d audit rows, want 2: each is a decision", rows)
	}
}

// The row records what changed, so a reader can check the claim rather than
// trust it. `before` and `after` are the whole point of the table.
func TestTheAuditRowRecordsWhatChangedRatherThanThatSomethingDid(t *testing.T) {
	a := newAuthServer(t)
	sessions, feed, _ := buildFindingsChain(t, a, false)
	conn := seeder(t)

	ada := signInWith(t, a, sessions, "audit-diff", actScopes)
	finding := seedFinding(t, conn, ada.orgID)

	if _, err := feed.ApproveFinding(t.Context(), withHeaders(
		connect.NewRequest(&corev1.ApproveFindingRequest{
			FindingId: finding, Reviewed: true,
		}), ada.headers)); err != nil {
		t.Fatalf("approving: %v", err)
	}

	var beforeStatus, afterStatus string
	if err := conn.QueryRow(t.Context(), `
		select before ->> 'status', after ->> 'status'
		  from audit_log
		 where finding_id = $1 and action_type = 'approve_finding'
	`, finding).Scan(&beforeStatus, &afterStatus); err != nil {
		t.Fatalf("reading the audit row: %v", err)
	}

	if beforeStatus != "pending" {
		t.Errorf("before.status is %q, want pending", beforeStatus)
	}
	if afterStatus != "approved" {
		t.Errorf("after.status is %q, want approved", afterStatus)
	}
}
