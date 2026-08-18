package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedAgentRun records a run, so a narrative has a real row to point at.
//
// A real row rather than a random uuid, because `findings.agent_run_id` is a
// foreign key: pointing it at nothing would fail, which is the constraint doing
// its job and would look like a broken test.
func seedAgentRun(t *testing.T, store *AgentStore, org string) string {
	t.Helper()

	now := time.Now().UTC()
	id, err := store.RecordAgentRun(t.Context(), AgentRun{
		OrgID:        uuid.MustParse(org),
		Skill:        "analyst.narrative",
		SkillVersion: "1.0.0",
		Model:        "test",
		ModelVersion: "test",
		Outcome:      "succeeded",
		QueuedAt:     now,
		StartedAt:    now,
		FinishedAt:   now,
	})
	if err != nil {
		t.Fatalf("recording a run for the narrative to point at: %v", err)
	}
	return id.String()
}

// Findings waiting for a narrative, against the real database (ENT-245).
//
// The service tests next door prove the decisions with a fake store. These
// prove the queries, which is a different question and the one that fails
// quietly: a narrator whose "awaiting" query is wrong reports zero findings
// forever and looks exactly like a system with nothing to do.

// seedNarratableFinding creates a finding with no narrative and returns its id.
//
// AS THE MIGRATOR, because `kindlast_agent` holds nothing on
// `onboarding_sessions` and `compliance_profiles`. That is the role split
// working rather than a broken fixture: the role that narrates a finding
// cannot invent the profile a finding is about.
func seedNarratableFinding(t *testing.T, org string) uuid.UUID {
	t.Helper()

	migrator := migratorPool(t)
	ctx := t.Context()

	session := uuid.New()
	profile := uuid.New()
	watcher := uuid.New()
	finding := uuid.New()

	if _, err := migrator.Exec(ctx, `
		insert into public.onboarding_sessions (id, org_id, created_by, status)
		values ($1, $2, $3, 'completed')`, session, org, adaUser); err != nil {
		t.Skipf("cannot seed an onboarding session: %v", err)
	}

	if _, err := migrator.Exec(ctx, `
		insert into public.compliance_profiles
			(id, org_id, session_id, created_by, industry, has_dpo, has_ropa,
			 transfers_outside_eu)
		values ($1, $2, $3, $4, 'recruitment', 'no', 'no', 'no')`,
		profile, org, session, adaUser); err != nil {
		t.Fatalf("seeding a profile: %v", err)
	}

	if _, err := migrator.Exec(ctx, `
		insert into public.watcher_findings
			(id, org_id, profile_id, kind, obligation_slug, severity, title, dedup_key)
		values ($1, $2, $3, 'profile_gap', 'gdpr-art-30-ropa', 'high',
		        'No record of processing activities', $4)`,
		watcher, org, profile, watcher.String()); err != nil {
		t.Fatalf("seeding a watcher finding: %v", err)
	}

	if _, err := migrator.Exec(ctx, `
		insert into public.findings
			(id, org_id, profile_id, watcher_finding_id, obligation_id,
			 obligation_slug, detected, severity, proposed_action, action_type, status)
		select $1, $2, $3, $4, o.id, o.slug,
		       'No record of processing activities', 'high',
		       'Create a record of processing activities', 'create_ropa', 'pending'
		  from public.obligations o
		 where o.slug = 'gdpr-art-30-ropa'`,
		finding, org, profile, watcher); err != nil {
		t.Fatalf("seeding a finding: %v", err)
	}

	t.Cleanup(func() {
		// Ordered by dependency, and the session last because everything hangs
		// off it. Deleting the organisation instead would take the seeded
		// fixtures every other test in this package relies on.
		ctx := context.WithoutCancel(t.Context())
		_, _ = migrator.Exec(ctx, `delete from public.findings where id = $1`, finding)
		_, _ = migrator.Exec(ctx, `delete from public.watcher_findings where id = $1`, watcher)
		_, _ = migrator.Exec(ctx, `delete from public.compliance_profiles where id = $1`, profile)
		_, _ = migrator.Exec(ctx, `delete from public.onboarding_sessions where id = $1`, session)
	})

	return finding
}

func TestAFindingWithNoNarrativeIsOfferedWithItsObligation(t *testing.T) {
	store := agentStore(t)
	id := seedNarratableFinding(t, alphaOrg)

	pending, err := store.FindingsAwaitingNarrative(t.Context(), alphaOrg, 50)
	if err != nil {
		t.Fatalf("reading findings to narrate: %v", err)
	}

	found := false
	for _, p := range pending {
		if p.ID != id {
			continue
		}
		found = true
		// THE OBLIGATION IS THE OFFERED SET AND IT MUST NOT BE EMPTY.
		//
		// A run offered no obligations cannot cite anything, so every narrative
		// would be refused and the whole feature would report "working, refused
		// everything". The join is what makes this true, and a join that
		// silently dropped rows would show up here as an absent finding rather
		// than as an empty slug, which is why both are checked.
		if p.ObligationSlug == "" {
			t.Fatal("the finding was offered with no obligation to cite")
		}
		if p.ObligationSummary == "" {
			t.Fatal("the obligation was offered with no summary for the model to read")
		}
	}
	if !found {
		t.Fatal("a finding with no narrative was not offered for narration")
	}
}

func TestANarratedFindingIsNotOfferedAgain(t *testing.T) {
	store := agentStore(t)
	id := seedNarratableFinding(t, alphaOrg)
	ctx := t.Context()

	run := seedAgentRun(t, store, alphaOrg)
	if err := store.RecordNarrative(ctx, alphaOrg, id, "Because you hold candidate data.", run); err != nil {
		t.Fatalf("recording the narrative: %v", err)
	}

	pending, err := store.FindingsAwaitingNarrative(ctx, alphaOrg, 50)
	if err != nil {
		t.Fatalf("reading findings to narrate: %v", err)
	}
	for _, p := range pending {
		if p.ID == id {
			// Without this the narrator re-narrates the same finding on every
			// pass, which on a local model means it never reaches the second
			// one.
			t.Fatal("a narrated finding was offered again")
		}
	}
}

func TestARefusedFindingIsNotOfferedAgainEither(t *testing.T) {
	store := agentStore(t)
	id := seedNarratableFinding(t, alphaOrg)
	ctx := t.Context()

	if err := store.RecordNarrativeRefusal(
		ctx, alphaOrg, id, "citation gdpr-art-50 was not offered", ""); err != nil {
		t.Fatalf("recording the refusal: %v", err)
	}

	pending, err := store.FindingsAwaitingNarrative(ctx, alphaOrg, 50)
	if err != nil {
		t.Fatalf("reading findings to narrate: %v", err)
	}
	for _, p := range pending {
		if p.ID == id {
			// The reason a refusal is stored at all. A finding the model cannot
			// narrate correctly would otherwise be retried forever and burn the
			// whole budget in a loop.
			t.Fatal("a refused finding was offered again")
		}
	}
}

func TestNarratingDoesNotTouchWhatTheSweepWrote(t *testing.T) {
	store := agentStore(t)
	id := seedNarratableFinding(t, alphaOrg)
	ctx := t.Context()

	migrator := migratorPool(t)
	var beforeDetected, beforeAction, beforeStatus string
	if err := migrator.QueryRow(ctx, `
		select detected, proposed_action, status from public.findings where id = $1`,
		id).Scan(&beforeDetected, &beforeAction, &beforeStatus); err != nil {
		t.Fatalf("reading the finding: %v", err)
	}

	run := seedAgentRun(t, store, alphaOrg)
	if err := store.RecordNarrative(ctx, alphaOrg, id, "A paragraph of prose.", run); err != nil {
		t.Fatalf("recording the narrative: %v", err)
	}

	var afterDetected, afterAction, afterStatus, narrative string
	if err := migrator.QueryRow(ctx, `
		select detected, proposed_action, status, coalesce(narrative, '')
		  from public.findings where id = $1`,
		id).Scan(&afterDetected, &afterAction, &afterStatus, &narrative); err != nil {
		t.Fatalf("re-reading the finding: %v", err)
	}

	// ENT-164, AS AN ASSERTION RATHER THAN A FIX THAT COULD BE UNDONE.
	//
	// The narrative layer that existed before the console was removed wrote
	// model prose over `detected`, which the feed card renders as its HEADING,
	// so a short phrase became a paragraph. The bug reads as a rendering
	// problem and is a schema one: there was no slot for prose.
	//
	// If somebody ever "simplifies" this by writing the narrative into
	// `detected` again, this test is what stops them.
	if afterDetected != beforeDetected {
		t.Fatalf("narrating rewrote `detected`: %q became %q", beforeDetected, afterDetected)
	}
	if afterAction != beforeAction {
		t.Fatalf("narrating rewrote `proposed_action`: %q became %q", beforeAction, afterAction)
	}
	if afterStatus != beforeStatus {
		t.Fatalf("narrating changed the status: %q became %q", beforeStatus, afterStatus)
	}
	if narrative == "" {
		t.Fatal("the narrative was not stored")
	}
}

func TestNarratingAFindingInAnotherOrganisationChangesNothing(t *testing.T) {
	store := agentStore(t)
	id := seedNarratableFinding(t, alphaOrg)
	ctx := t.Context()

	// Named with beta's organisation, which the agent's own policies do not
	// stop it from setting: the role's policies on findings are unconditional
	// because it runs for organisations nobody is signed in to. What keeps that
	// honest is that the row it names is not in the organisation it set, so the
	// update matches nothing.
	err := store.RecordNarrative(ctx, betaOrg, id, "prose", "")
	if err == nil {
		t.Fatal("narrated a finding while pointed at another organisation")
	}

	migrator := migratorPool(t)
	var narrative string
	if qerr := migrator.QueryRow(ctx, `
		select coalesce(narrative, '') from public.findings where id = $1`,
		id).Scan(&narrative); qerr != nil {
		t.Fatalf("reading the finding: %v", qerr)
	}
	// The error above is only useful if nothing was written. A store that
	// reported a failure and wrote anyway would be worse than one that did
	// neither.
	if narrative != "" {
		t.Fatalf("it wrote %q into another organisation's finding", narrative)
	}
}

// The read side of ENT-162, which is the hop nothing covered.
//
// Every test above proves the narrative is written. None of them proves it can
// be read back through the query the feed actually runs, and that gap is
// exactly the shape of the bug: ENT-218 built the narrative layer, ENT-245 gave
// it a caller, and a finding still rendered as though no run had ever happened
// because the feed's select list did not mention the column.
//
// As the tenant rather than as the agent, deliberately. The agent writes the
// narrative and a signed-in human reads it, and those are different roles under
// different policies; asserting that the writing role can read its own write
// would prove nothing about the console.
func TestTheFeedReadsBackTheNarrativeTheAgentWrote(t *testing.T) {
	store := testStore(t)
	agent := agentStore(t)
	id := seedNarratableFinding(t, alphaOrg)
	ctx := t.Context()

	run := seedAgentRun(t, agent, alphaOrg)
	const prose = "You keep customer orders and support tickets, so Article 30 wants a written record of both."
	if err := agent.RecordNarrative(ctx, alphaOrg, id, prose, run); err != nil {
		t.Fatalf("recording the narrative: %v", err)
	}

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("beginning a tenant transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	finding, _, err := tenant.Finding(ctx, id.String())
	if err != nil {
		t.Fatalf("reading the finding back: %v", err)
	}

	if finding.Narrative != prose {
		t.Fatalf("the narrative did not reach the reader: %q", finding.Narrative)
	}
	if finding.AgentRunID != run {
		t.Fatalf("the run that produced it did not reach the reader: %q", finding.AgentRunID)
	}
	// ENT-164 from the reading end. The heading slot still holds the sweep's
	// short phrase, and a prose paragraph is not in it.
	if finding.Detected != "No record of processing activities" {
		t.Fatalf("the narrative displaced `detected`: %q", finding.Detected)
	}
	if finding.NarrativeRefusal != "" {
		t.Fatalf("a successful run recorded a refusal: %q", finding.NarrativeRefusal)
	}

	// And through the feed's own query, which is a different statement over the
	// same columns and could drift from the one above.
	page, err := tenant.Findings(ctx, "", "", 100)
	if err != nil {
		t.Fatalf("listing findings: %v", err)
	}
	for _, f := range page.Findings {
		if f.ID != id.String() {
			continue
		}
		if f.Narrative != prose {
			t.Fatalf("the feed list dropped the narrative: %q", f.Narrative)
		}
		return
	}
	t.Fatal("the narrated finding was not in the feed")
}
