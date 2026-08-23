package postgres

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/libs/chassis/subject"
)

// Arriving leaves a record, the same way being invited does.
//
// # WHY THIS IS A SEPARATE FILE FROM members_audit_test.go
//
// Because the mechanism is different, and the difference is the whole reason
// this took two changes rather than one. The other four membership actions are
// written from Go by `recordAudit`, through `audit_log_insert_org`, by somebody
// who is already a member of the organisation they are acting in. A person
// redeeming an invitation is none of those things: at the moment the row has to
// be written they have no active organisation and no membership, so the policy
// refuses them. The row is written inside `accept_invitation` instead, which is
// SECURITY DEFINER and owned by a BYPASSRLS role, in the same statement block
// that creates the membership.
//
// So what these tests are really asserting is that the write happens at all,
// from a caller the ordinary insert path would have turned away, and that it
// happens inside the transaction that creates the membership rather than beside
// it.
//
// # WHY IT MATTERS THAT IT IS RECORDED
//
// The log already says who was invited and by whom. Without this it does not
// say whether they ever arrived. "Invited somebody to join" with nothing after
// it is the same row whether the invitation is still sitting unread in a
// mailbox or the person has been reading the compliance record for a month, and
// telling those two apart is the reason access is logged.
//
// Everything happens inside one transaction that is rolled back. `audit_log` is
// append-only by trigger, so a committed row here could not be cleaned up and
// would accumulate in the shared development database on every run.

// acceptedAuditRow reads back the one row an acceptance wrote.
//
// Read as the app role through `audit_log_select_org`, not as the migrator. A
// read that bypassed RLS would pass even if the row had been written into an
// organisation nobody in it can see, which is a row that exists and answers
// nobody.
func acceptedAuditRow(t *testing.T, tenant *Tenant, invitationID string) (
	actor, actorRole string, before, after map[string]string,
) {
	t.Helper()

	var rawBefore, rawAfter []byte
	err := tenant.Tx().QueryRow(t.Context(), `
		select user_id::text, coalesce(actor_role, ''), before, after
		from audit_log
		where action_type = $1 and target_table = 'invitations' and target_id = $2
	`, ActionAcceptInvitation, invitationID).Scan(&actor, &actorRole, &rawBefore, &rawAfter)
	if err != nil {
		t.Fatalf("reading the acceptance audit row: %v", err)
	}

	decode := func(raw []byte) map[string]string {
		if len(raw) == 0 {
			return nil
		}
		out := map[string]string{}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("decoding the acceptance audit row: %v", err)
		}
		return out
	}
	return actor, actorRole, decode(rawBefore), decode(rawAfter)
}

// invitationID reads the id of a seeded invitation, as the migrator.
//
// The invitation is seeded outside the caller's transaction and the caller
// cannot see it: nothing in the tenant policy surface shows a pending
// invitation to the person it names, which is 00003's point.
func invitationID(t *testing.T, token string) string {
	t.Helper()

	var id string
	err := migratorPool(t).QueryRow(t.Context(),
		`select id::text from invitations where token_hash = $1`,
		HashInvitationToken(token)).Scan(&id)
	if err != nil {
		t.Fatalf("reading the invitation id: %v", err)
	}
	return id
}

func TestAcceptingAnInvitationIsRecorded(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	claim := fmt.Sprintf("accept-audit-%d", time.Now().UnixNano())
	cleanup(t, claim)
	t.Cleanup(func() { cleanup(t, claim) })

	const address = "joins-and-is-logged@example.com"
	token := fmt.Sprintf("accept-audit-token-%d", time.Now().UnixNano())
	createInvitation(t, alphaOrg, address, "member", token, time.Hour)
	t.Cleanup(func() { deleteInvitation(t, token) })

	invitation := invitationID(t, token)

	// No organisation header, because there is nothing to put in one: this
	// caller belongs to no organisation yet. `app.current_org_id` is the
	// no-organisation sentinel for the whole of the accept call, which is
	// exactly why `audit_log_insert_org` cannot be the writer.
	tenant, err := store.BeginTenant(ctx, claim, "")
	if err != nil {
		t.Fatalf("the joiner's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	if tenant.OrgID() != noOrganisation {
		t.Fatalf("the joiner started in organisation %q, want none; "+
			"this test is no longer exercising the case it exists for", tenant.OrgID())
	}

	joined, err := tenant.AcceptInvitation(ctx, token, address)
	if err != nil {
		t.Fatalf("accepting: %v", err)
	}

	// What the next request would resolve to. `BeginTenant` would find the
	// membership that now exists and set this GUC to the joined organisation,
	// so advancing it here is that next request rather than a licence: the read
	// below still goes through `audit_log_select_org` as `kindlast_app`.
	if err := setLocal(ctx, tenant.Tx(), "app.current_org_id", joined.OrgID); err != nil {
		t.Fatalf("advancing the organisation GUC: %v", err)
	}

	actor, actorRole, before, after := acceptedAuditRow(t, tenant, invitation)

	joiner, err := subject.UUID(testIssuer, claim)
	if err != nil {
		t.Fatalf("deriving the joiner's id: %v", err)
	}
	if actor != joiner.String() {
		t.Fatalf("the row names %q as the actor, want the joiner %q", actor, joiner)
	}

	// The role snapshot is what proves the ordering. `record_audit_log` reads
	// the actor's role out of `memberships`, so a null here means the audit row
	// was written before the membership existed, and the log would say somebody
	// joined without saying what they became.
	if actorRole != "member" {
		t.Fatalf("actor_role = %q, want member; the audit row was written before the membership", actorRole)
	}

	// Nothing before. The absence is what says this is an arrival rather than a
	// change to an access that already existed.
	if before != nil {
		t.Fatalf("the acceptance recorded a `before` of %v, want none", before)
	}
	if after["role"] != "member" {
		t.Fatalf("after role = %q, want the role the invitation granted", after["role"])
	}
}

// Nothing happened, so nothing is recorded.
//
// The four ways an invitation is not usable are one answer to the caller on
// purpose (00003, 00033): expired, already accepted, never existed and
// addressed to somebody else are indistinguishable. They have to be one answer
// in the audit log too, and the only answer that works is silence. A row
// written on a refusal would be a record of an access grant that did not
// happen, and, worse, a log that a stranger holding a guessed token could write
// into.
func TestARefusedInvitationRecordsNothing(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	// The invitation is real and is addressed to somebody else. The most
	// dangerous of the four, because the caller holds a genuine token.
	const invited = "the-actual-recipient@example.com"
	token := fmt.Sprintf("accept-refused-token-%d", time.Now().UnixNano())
	createInvitation(t, alphaOrg, invited, "owner", token, time.Hour)
	t.Cleanup(func() { deleteInvitation(t, token) })

	invitation := invitationID(t, token)

	for _, refusal := range []struct {
		name  string
		token string
		email string
	}{
		{"addressed to somebody else", token, "mallory@example.com"},
		{"no such token", "no-such-token-at-all", invited},
		{"no address", token, ""},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			claim := fmt.Sprintf("accept-refused-%d", time.Now().UnixNano())
			cleanup(t, claim)
			t.Cleanup(func() { cleanup(t, claim) })

			tenant, err := store.BeginTenant(ctx, claim, "")
			if err != nil {
				t.Fatalf("the caller's transaction: %v", err)
			}
			defer tenant.Rollback(ctx)

			if _, err := tenant.AcceptInvitation(ctx, refusal.token, refusal.email); err == nil {
				t.Fatal("a refused invitation was accepted")
			}

			// Committed before counting, and that is what makes this test able
			// to fail rather than merely able to pass.
			//
			// The first draft rolled back and counted through a second
			// connection, which is green whatever the function does: an audit
			// row written on the refusal path would be rolled back too, so the
			// count was always zero and the assertion proved nothing. Verified
			// by pointing this case at the address the invitation names, which
			// makes the acceptance succeed and must therefore turn the count
			// red.
			//
			// Committing a refusal is safe by construction: on this path
			// nothing has been written, so the commit persists nothing. If that
			// stops being true, the count below is exactly where it shows up.
			if err := tenant.Commit(ctx); err != nil {
				t.Fatalf("committing the refused attempt: %v", err)
			}

			// Counted as the migrator, so the assertion is that the row does
			// not exist rather than that this caller cannot see it. Reading as
			// the refused caller would pass whether the row was absent or
			// merely invisible to them, which is the difference that matters.
			var rows int
			if err := migratorPool(t).QueryRow(ctx, `
				select count(*) from audit_log
				where action_type = $1 and target_id = $2
			`, ActionAcceptInvitation, invitation).Scan(&rows); err != nil {
				t.Fatalf("counting acceptance rows: %v", err)
			}
			if rows != 0 {
				t.Fatalf("a refused invitation wrote %d audit rows, want 0", rows)
			}
		})
	}
}
