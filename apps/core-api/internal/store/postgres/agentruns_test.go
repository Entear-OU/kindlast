package postgres

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/stackenv"
)

// agentStore opens the producer pool, or skips.
//
// Same convention as testStore: skips on a laptop, fails in CI when
// KINDLAST_REQUIRE_STACK is set. A self-skipping suite that reports green while
// testing nothing is how coverage disappears without anyone deciding it should.
func agentStore(t *testing.T) *AgentStore {
	t.Helper()

	dsn := stackenv.DSN("agent")

	store, err := NewAgent(t.Context(), dsn)
	if err != nil {
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", dsn, err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", dsn, err)
	}
	t.Cleanup(store.Close)
	return store
}

// seedOrg makes an organisation for a run to belong to, and removes it after.
//
// AS THE MIGRATOR, AND THE FIRST DRAFT DID IT AS THE AGENT AND FAILED.
//
// `permission denied for table organisations`, which is the role split working
// rather than a broken fixture: 00008 leaves `kindlast_agent` holding nothing
// on organisations, memberships or audit_log, so the role that can record a run
// cannot invent a tenant to record it against. Worth keeping the note, because
// the obvious fixture is the wrong one and the error arrives several minutes
// into writing the test rather than while designing it.
//
// Deleting the organisation takes the run with it, because agent_runs cascades
// from organisations. So the cleanup here is the erasure path, exercised in
// passing.
func seedOrg(t *testing.T) uuid.UUID {
	t.Helper()

	conn := migratorConn(t)
	id := uuid.New()
	_, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		id, "agent-runs-test-"+id.String()[:8], "Agent runs test")
	if err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, id)
	})
	return id
}

func aRun(org uuid.UUID) AgentRun {
	started := time.Now().Add(-2 * time.Second)
	return AgentRun{
		OrgID:         org,
		Skill:         "analyst.narrative",
		SkillVersion:  "1.0.0",
		Model:         "Qwen3.5-4B-Q4_K_M",
		ModelVersion:  "00fe7986ff5f6b463e62455821146049db6f9313603938a70800d1fb69ef11a4",
		RequestJSON:   `{"finding_id":"abc"}`,
		ToolCallsJSON: `[{"tool":"get_obligation","args":{"slug":"gdpr-art-30-ropa"}}]`,
		CitationsJSON: `{"resolved":["gdpr-art-30-ropa"],"rejected":[]}`,
		Outcome:       "succeeded",
		InputTokens:   44,
		OutputTokens:  34,
		QueuedAt:      started.Add(-time.Second),
		StartedAt:     started,
		FinishedAt:    time.Now(),
	}
}

// agentTxFor opens a producer transaction pointed at one organisation, for
// tests that read a row back after writing it.
//
// Until 00037 (ENT-272) these read backs went straight at `store.pool`, which
// worked because `agent_runs_agent` was `for all using (true)`: the producer
// could read any organisation's run records from a session that had never
// said which organisation it meant. That is the thing the migration removed,
// so a test that reads one has to name a tenant the same way the store's own
// methods do. Reaching past the policy with the migrator would have been the
// shorter fix and the wrong one, because then these tests would no longer be
// running against the role the code runs as.
func agentTxFor(t *testing.T, store *AgentStore, org uuid.UUID) pgx.Tx {
	t.Helper()
	tx, err := store.pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning a producer read: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if err := setLocal(t.Context(), tx, "app.current_org_id", org.String()); err != nil {
		t.Fatalf("pointing the read at %s: %v", org, err)
	}
	return tx
}

func TestRecordingARunStoresWhatItWasGiven(t *testing.T) {
	store := agentStore(t)
	org := seedOrg(t)

	id, err := store.RecordAgentRun(t.Context(), aRun(org))
	if err != nil {
		t.Fatalf("recording a run: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("no id came back")
	}

	var skill, model, outcome string
	var toolCalls, citations string
	var onBehalfOf *uuid.UUID
	err = agentTxFor(t, store, org).QueryRow(t.Context(), `
		select skill, model, outcome, tool_calls::text, citations::text,
		       on_behalf_of_user_id
		  from agent_runs where id = $1`, id).
		Scan(&skill, &model, &outcome, &toolCalls, &citations, &onBehalfOf)
	if err != nil {
		t.Fatalf("reading the run back: %v", err)
	}

	if skill != "analyst.narrative" || model != "Qwen3.5-4B-Q4_K_M" {
		t.Errorf("provenance not stored: skill=%q model=%q", skill, model)
	}
	if outcome != "succeeded" {
		t.Errorf("outcome = %q", outcome)
	}
	// A scheduled run names nobody, and storing a user id here would later read
	// as "this person asked for this".
	if onBehalfOf != nil {
		t.Errorf("on_behalf_of_user_id = %v, want null for a run with no person", onBehalfOf)
	}
	if toolCalls == "" || citations == "" {
		t.Error("the jsonb payloads came back empty")
	}
}

func TestARefusalIsARunAndIsRecorded(t *testing.T) {
	// §26.3 makes refusal what a working guardrail produces, not a failure. A
	// schema or a handler that could not store one would lose the distinction
	// that matters most for trust: "the harness stopped this" versus "the
	// harness broke".
	store := agentStore(t)
	org := seedOrg(t)

	run := aRun(org)
	run.Outcome = "refused"
	run.OutcomeDetail = "citation gdpr-art-99-invented does not resolve to a stored obligation"
	run.CitationsJSON = `{"resolved":[],"rejected":["gdpr-art-99-invented"]}`

	id, err := store.RecordAgentRun(t.Context(), run)
	if err != nil {
		t.Fatalf("recording a refusal: %v", err)
	}

	var outcome, detail, citations string
	err = agentTxFor(t, store, org).QueryRow(t.Context(),
		`select outcome, outcome_detail, citations::text from agent_runs where id = $1`, id).
		Scan(&outcome, &detail, &citations)
	if err != nil {
		t.Fatalf("reading the refusal back: %v", err)
	}
	if outcome != "refused" {
		t.Errorf("outcome = %q, want refused", outcome)
	}
	if detail == "" {
		t.Error("a refusal with no detail cannot be acted on")
	}
	// THE REJECTED HALF IS THE POINT. A validator that dropped a bad citation
	// silently would leave a record indistinguishable from a run where the
	// model never tried to cite anything.
	if !strings.Contains(citations, "gdpr-art-99-invented") {
		t.Errorf("the rejected citation was not kept: %s", citations)
	}
}

// TestACriticsRefusalKeepsTheTextAndTheRuleThatRejectedIt covers 00028
// (ENT-248).
//
// A narrative refused for stating the law wrongly is the case this column
// exists for, and the two halves it holds answer different questions. The
// pattern names are machine-readable so somebody can count how often a rule
// fires rather than parsing English out of `outcome_detail`, which is the
// mistake the records store made with `check_violation` messages. The text is
// here rather than in `outcome_detail` because that string is copied to
// `findings.narrative_refusal` and printed on the finding page, and a false
// statement of law must not appear under the heading explaining that it was
// refused.
func TestACriticsRefusalKeepsTheTextAndTheRuleThatRejectedIt(t *testing.T) {
	store := agentStore(t)
	org := seedOrg(t)

	// What the 2B tier wrote on the running stack, refused by the claim critic.
	const rejected = "This is a high severity issue because the obligation to keep " +
		"such records applies to every controller and processor, regardless of how " +
		"small the company is."

	run := aRun(org)
	run.Outcome = "refused"
	run.OutcomeDetail = "the text states the law rather than explaining applicability"
	run.RefusalJSON = `{"critic":"legal_claim",` +
		`"patterns":["a claim about who the law applies to"],` +
		`"text":` + strconv.Quote(rejected) + `}`

	id, err := store.RecordAgentRun(t.Context(), run)
	if err != nil {
		t.Fatalf("recording a critic's refusal: %v", err)
	}

	var refusal string
	err = agentTxFor(t, store, org).QueryRow(t.Context(),
		`select refusal::text from agent_runs where id = $1`, id).Scan(&refusal)
	if err != nil {
		t.Fatalf("reading the refusal back: %v", err)
	}

	if !strings.Contains(refusal, "legal_claim") {
		t.Errorf("the critic that refused was not kept: %s", refusal)
	}
	if !strings.Contains(refusal, "a claim about who the law applies to") {
		t.Errorf("the rule that fired was not kept: %s", refusal)
	}
	if !strings.Contains(refusal, "regardless of how") {
		t.Errorf("the rejected text was not kept: %s", refusal)
	}

	// And a run nothing refused says so, rather than leaving a reader unable to
	// tell "no critic objected" from "a critic objected and nobody recorded
	// what to".
	clean, err := store.RecordAgentRun(t.Context(), aRun(org))
	if err != nil {
		t.Fatalf("recording a clean run: %v", err)
	}
	if err := agentTxFor(t, store, org).QueryRow(t.Context(),
		`select refusal::text from agent_runs where id = $1`, clean).Scan(&refusal); err != nil {
		t.Fatalf("reading the clean run back: %v", err)
	}
	if refusal != "{}" {
		t.Errorf("a run nothing refused recorded a refusal: %s", refusal)
	}
}

func TestAnUnknownOutcomeIsRefusedByTheDatabase(t *testing.T) {
	// The check constraint, not the handler. A second writer appearing later
	// must not be able to invent an outcome the console cannot render.
	store := agentStore(t)
	org := seedOrg(t)

	run := aRun(org)
	run.Outcome = "probably-fine"

	if _, err := store.RecordAgentRun(t.Context(), run); err == nil {
		t.Fatal("an outcome outside the constraint was accepted")
	}
}

func TestMalformedJSONIsRefusedBeforeItIsStored(t *testing.T) {
	// Caught here rather than at render time, because the failure would
	// otherwise surface to a customer reading "how this was produced" rather
	// than to whoever sent it.
	store := agentStore(t)
	org := seedOrg(t)

	run := aRun(org)
	run.ToolCallsJSON = `[{"tool": unquoted}]`

	if _, err := store.RecordAgentRun(t.Context(), run); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
}

func TestTimestampsMustBeOrdered(t *testing.T) {
	// finished before started is not a slow run, it is a bug in whatever
	// reported it, and ENT-238 will measure latency from these columns.
	store := agentStore(t)
	org := seedOrg(t)

	run := aRun(org)
	run.FinishedAt = run.StartedAt.Add(-time.Minute)

	if _, err := store.RecordAgentRun(t.Context(), run); err == nil {
		t.Fatal("a run that finished before it started was accepted")
	}
}

func TestTheAgentCannotUpdateOrDeleteARun(t *testing.T) {
	// The append-only shape, as a grant rather than a convention.
	//
	// Proven against the database rather than by reading 00019, because the
	// grant is what actually holds and a comment claiming it is not the same
	// thing. ENT-243 exists because that difference had already gone unnoticed
	// once.
	store := agentStore(t)
	org := seedOrg(t)

	id, err := store.RecordAgentRun(t.Context(), aRun(org))
	if err != nil {
		t.Fatalf("recording a run: %v", err)
	}

	if _, err := store.pool.Exec(t.Context(),
		`update agent_runs set outcome = 'succeeded' where id = $1`, id); err == nil {
		t.Error("the agent role updated a run record; it holds no update grant and must not")
	}

	if _, err := store.pool.Exec(t.Context(),
		`delete from agent_runs where id = $1`, id); err == nil {
		t.Error("the agent role deleted a run record; it holds no delete grant and must not")
	}
}

func TestNotEvenTheMigratorCanUpdateARun(t *testing.T) {
	// THE TRIGGER, AND WHY IT IS NOT REDUNDANT WITH THE TEST ABOVE.
	//
	// Grants and policies constrain the application roles. They constrain the
	// migrator not at all: it bypasses RLS and holds every privilege, so
	// "kindlast_agent has no update grant" says nothing about what an operator
	// with a psql prompt can do.
	//
	// The claim this table makes to a customer is "how this was produced", and
	// that wants "nobody, including us" enforced rather than observed. Same
	// reasoning `audit_log` carries, and the reason the first draft of 00019
	// was wrong to leave the trigger out.
	store := agentStore(t)
	org := seedOrg(t)

	id, err := store.RecordAgentRun(t.Context(), aRun(org))
	if err != nil {
		t.Fatalf("recording a run: %v", err)
	}

	conn := migratorConn(t)
	if _, err := conn.Exec(t.Context(),
		`update agent_runs set outcome = 'failed' where id = $1`, id); err == nil {
		t.Error("the migrator rewrote a run record; the append-only trigger is not in force")
	}
}
