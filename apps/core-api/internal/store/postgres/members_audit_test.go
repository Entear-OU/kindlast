package postgres

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Access changes leave a record.
//
// # WHY THESE ARE WORTH THE RUNTIME
//
// Every decision about a finding wrote an audit row and every change to who was
// allowed to make one wrote nothing. A reader of the log could see that Ada
// approved something and could not see how Ada came to be in the organisation,
// who let her in, at what role, or when somebody's authority was raised or
// taken away. For a product whose value is that a human can check the record,
// "who has access to this, granted by whom" is the first question and the log
// could not answer it.
//
// Each test asserts the row exists AND what it says, because a row that records
// the act without recording what changed is the shape this could regress into:
// `before` and `after` are the whole content of a role change, and an entry
// saying only "somebody's role changed" is not an audit trail.
//
// Written against the real database rather than a fake, because the thing being
// tested is partly the insert policy on `audit_log`: it binds the row to the
// two tenancy GUCs and to a membership, so a write that looked fine in Go can
// still be refused. A fake would prove nothing about that.
//
// Everything happens inside one transaction that is rolled back, following
// records_test.go: an earlier suite in this package leaked 42 fixture rows when
// its cleanup silently did nothing.

// auditRow reads back the one row an action wrote, with what it recorded.
func auditRow(
	t *testing.T, tenant *Tenant, action, targetTable, targetID string,
) (before, after map[string]string) {
	t.Helper()

	var rawBefore, rawAfter []byte
	err := tenant.Tx().QueryRow(t.Context(), `
		select before, after from audit_log
		where action_type = $1 and target_table = $2 and target_id = $3
	`, action, targetTable, targetID).Scan(&rawBefore, &rawAfter)
	if err != nil {
		t.Fatalf("reading the %s audit row: %v", action, err)
	}

	decode := func(raw []byte) map[string]string {
		if len(raw) == 0 {
			return nil
		}
		out := map[string]string{}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decoding %s: %v", action, err)
		}
		return out
	}
	return decode(rawBefore), decode(rawAfter)
}

func TestRenamingTheOrganisationIsRecorded(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	var was string
	if err := tenant.Tx().QueryRow(ctx,
		`select name from organisations where id = $1`, alphaOrg).Scan(&was); err != nil {
		t.Fatalf("reading the current name: %v", err)
	}

	if _, err := tenant.RenameOrganisation(ctx, "Renamed For The Test"); err != nil {
		t.Fatalf("renaming: %v", err)
	}

	before, after := auditRow(t, tenant, ActionRenameOrganisation, "organisations", alphaOrg)

	// Both halves. A log that recorded only the new name cannot answer "what
	// was this called when that finding was approved", which is the question a
	// rename makes somebody ask a year later.
	if before["name"] != was {
		t.Fatalf("before name = %q, want %q", before["name"], was)
	}
	if after["name"] != "Renamed For The Test" {
		t.Fatalf("after name = %q, want the new name", after["name"])
	}
}

// Saving the name without changing it is not an event.
//
// Found by driving the real console rather than by thinking about it: a form
// submitted twice produced "renamed Ada Furniture Group to Ada Furniture
// Group". A compliance log is read by somebody looking for the moment
// something changed, and rows recording that nothing changed make that harder
// for no gain.
func TestRenamingToTheSameNameRecordsNothing(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	var current string
	if err := tenant.Tx().QueryRow(ctx,
		`select name from organisations where id = $1`, alphaOrg).Scan(&current); err != nil {
		t.Fatalf("reading the current name: %v", err)
	}

	if _, err := tenant.RenameOrganisation(ctx, current); err != nil {
		t.Fatalf("renaming to the same name: %v", err)
	}

	var rows int
	if err := tenant.Tx().QueryRow(ctx, `
		select count(*) from audit_log
		where action_type = $1 and target_id = $2
	`, ActionRenameOrganisation, alphaOrg).Scan(&rows); err != nil {
		t.Fatalf("counting rename rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("a no-op rename wrote %d audit rows, want 0", rows)
	}
}

func TestInvitingSomebodyIsRecorded(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	token := fmt.Sprintf("audit-invite-%d", time.Now().UnixNano())
	invitation, err := tenant.CreateInvitation(ctx, "newcomer@example.com", "member", token)
	if err != nil {
		t.Fatalf("inviting: %v", err)
	}

	_, after := auditRow(t, tenant, ActionInviteMember, "invitations", invitation.ID)

	if after["email"] != "newcomer@example.com" {
		t.Fatalf("after email = %q, want the invited address", after["email"])
	}
	if after["role"] != "member" {
		t.Fatalf("after role = %q, want the offered role", after["role"])
	}
}

// The token never reaches the audit log.
//
// It is a capability: whoever holds it can join the organisation. The audit log
// is readable by every member and exportable to CSV, so a token recorded here
// would be an invitation quietly re-issued to everybody who can read the log.
func TestTheInvitationTokenIsNotWrittenToTheAuditLog(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	token := fmt.Sprintf("audit-secret-%d", time.Now().UnixNano())
	if _, err := tenant.CreateInvitation(ctx, "newcomer@example.com", "member", token); err != nil {
		t.Fatalf("inviting: %v", err)
	}

	var leaked int
	if err := tenant.Tx().QueryRow(ctx, `
		select count(*) from audit_log
		where before::text like '%' || $1 || '%'
		   or after::text  like '%' || $1 || '%'
	`, token).Scan(&leaked); err != nil {
		t.Fatalf("searching for the token: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("the raw invitation token appears in %d audit rows, want 0", leaked)
	}

	// The hash must not be there either: it is what redeeming compares against.
	var hashed int
	if err := tenant.Tx().QueryRow(ctx, `
		select count(*) from audit_log
		where before::text like '%' || $1 || '%'
		   or after::text  like '%' || $1 || '%'
	`, HashInvitationToken(token)).Scan(&hashed); err != nil {
		t.Fatalf("searching for the hash: %v", err)
	}
	if hashed != 0 {
		t.Fatalf("the invitation hash appears in %d audit rows, want 0", hashed)
	}
}

// Somebody removing themselves still records it.
//
// This is the case that makes the ordering in RemoveMember load-bearing, and it
// is not an edge case: leaving an organisation is a normal thing to do, and an
// owner who is not the last one is allowed to.
//
// `audit_log_insert_org` requires a membership for the acting user in the
// acting organisation. Write the row after the delete and that membership is
// precisely what has just stopped existing, so the insert is refused with a
// 42501 and takes the removal down with it: nobody can leave. The first draft
// did exactly that, and the interceptor suite caught it.
func TestLeavingRecordsTheRemovalRatherThanRefusingIt(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	// A second owner, so the one under test is not the last: the last-owner
	// rule is the handler's and would refuse for an unrelated reason.
	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	leaver := uuid.NewString()
	if _, err := tenant.Tx().Exec(ctx, `
		insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')
	`, alphaOrg, leaver); err != nil {
		t.Fatalf("seeding a second owner: %v", err)
	}

	// Ada removes Ada. The acting user and the removed user are the same.
	//
	// The assertion is that this returns at all. With the audit row written
	// after the delete it returned
	//
	//   new row violates row-level security policy for table "audit_log"
	//
	// and the removal was rolled back with it.
	//
	// What this test deliberately does NOT do is read the row back. Once the
	// membership is gone, `audit_log_select_org` stops this caller reading the
	// organisation's log at all, which is correct and is the same rule that
	// makes a former member unable to browse a record they have left. The row's
	// contents are asserted by
	// TestChangingAndRemovingAMembershipAreRecorded, where the actor is still a
	// member afterwards and can therefore see what was written.
	if err := tenant.RemoveMember(ctx, tenant.UserID()); err != nil {
		t.Fatalf("an owner could not remove themselves: %v", err)
	}
}

func TestChangingAndRemovingAMembershipAreRecorded(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// A second person to act on. Inserted through the same transaction, so it
	// disappears with the rollback.
	joiner := uuid.NewString()
	if _, err := tenant.Tx().Exec(ctx, `
		insert into memberships (org_id, user_id, role) values ($1, $2, 'viewer')
	`, alphaOrg, joiner); err != nil {
		t.Fatalf("seeding a member: %v", err)
	}

	if err := tenant.SetMemberRole(ctx, joiner, "owner"); err != nil {
		t.Fatalf("changing the role: %v", err)
	}

	before, after := auditRow(t, tenant, ActionChangeMemberRole, "memberships", joiner)

	// The escalation is the point. viewer to owner is the change an auditor
	// asks about, and a row that did not say what it was before could not show
	// that it happened.
	if before["role"] != "viewer" || after["role"] != "owner" {
		t.Fatalf("role change recorded as %q -> %q, want viewer -> owner",
			before["role"], after["role"])
	}

	if err := tenant.RemoveMember(ctx, joiner); err != nil {
		t.Fatalf("removing: %v", err)
	}

	removedBefore, removedAfter := auditRow(t, tenant, ActionRemoveMember, "memberships", joiner)

	if removedBefore["role"] != "owner" {
		t.Fatalf("removal recorded a previous role of %q, want owner",
			removedBefore["role"])
	}
	// Nothing after: the absence is what says the membership ended rather than
	// changed.
	if removedAfter != nil {
		t.Fatalf("removal recorded an `after` of %v, want none", removedAfter)
	}
}
