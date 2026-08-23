package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// What an agentic Watcher reads and writes (ENT-258), against the real
// database, because every property here is a property of the producer role's
// grants and policies: what it can see, what it cannot, and that its one write
// goes through the same deduplication the deterministic detectors use.
//
// PROVEN ABLE TO FAIL. Dropping the org GUC from `WatcherContextFor` turns
// "sees only its own organisation" red; replacing `emit_watcher_finding` with
// a plain insert turns "raising the same signal twice keeps one row" red.

func seedWatcherOrg(t *testing.T) (org uuid.UUID, profile string) {
	t.Helper()
	pool := migratorPool(t)
	org = uuid.New()
	owner := uuid.New()

	if _, err := pool.Exec(context.Background(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "watcher-"+org.String()[:8], "Watcher test"); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org, owner); err != nil {
		t.Fatalf("seeding an owner: %v", err)
	}
	session, profileID := uuid.New(), uuid.New()
	if _, err := pool.Exec(context.Background(),
		`insert into onboarding_sessions (id, org_id, created_by) values ($1, $2, $3)`,
		session, org, owner); err != nil {
		t.Fatalf("seeding a session: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into compliance_profiles
		  (id, org_id, created_by, session_id, industry, has_dpo, has_ropa, transfers_outside_eu)
		values ($1, $2, $3, $4, 'saas', 'no', 'no', 'no')
	`, profileID, org, owner, session); err != nil {
		t.Fatalf("seeding a profile: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into org_profile_facts (org_id, key, value, source, recorded_by)
		values ($1, 'staff_count', '{"value": 4}'::jsonb, 'onboarding', $2)
	`, org, owner); err != nil {
		t.Fatalf("seeding a fact: %v", err)
	}

	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		pool.Exec(context.WithoutCancel(context.Background()),
			`delete from organisations where id = $1`, org)
	})
	return org, profileID.String()
}

func TestTheWatcherContextIsAssembledFromWhatTheProducerMayAlreadyRead(t *testing.T) {
	agent := agentStore(t)
	org, profile := seedWatcherOrg(t)

	// A connection with two tools, one granted and one not: what the agent may
	// reach, and what it may only mention.
	pool := migratorPool(t)
	connection := uuid.New()
	if _, err := pool.Exec(context.Background(), `
		insert into integrations (id, org_id, kind, display_name, endpoint_url, status, created_by)
		select $1, $2, 'mcp', 'The helpdesk', 'https://mcp.example.invalid', 'active', m.user_id
		  from memberships m where m.org_id = $2 limit 1
	`, connection, org); err != nil {
		t.Fatalf("seeding a connection: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into integration_tools (integration_id, org_id, name, description, write_capable, granted, granted_at)
		values ($1, $2, 'search_tickets', 'Search the helpdesk', false, true, now()),
		       ($1, $2, 'close_ticket', 'Close a ticket', true, false, null)
	`, connection, org); err != nil {
		t.Fatalf("seeding tools: %v", err)
	}

	got, err := agent.WatcherContextFor(t.Context(), org.String())
	if err != nil {
		t.Fatalf("reading the context: %v", err)
	}
	if !got.HasProfile || got.ProfileID != profile {
		t.Fatalf("context = %+v, want the organisation's newest profile", got)
	}
	if len(got.Facts) != 1 || got.Facts[0].Key != "staff_count" || got.Facts[0].Source != "onboarding" {
		t.Fatalf("facts = %+v", got.Facts)
	}
	if len(got.Connections) != 1 || got.Connections[0].DisplayName != "The helpdesk" {
		t.Fatalf("connections = %+v", got.Connections)
	}
	tools := got.Connections[0].Tools
	if len(tools) != 2 {
		t.Fatalf("tools = %+v", tools)
	}
	var granted, writeCapable int
	for _, tool := range tools {
		if tool.Granted {
			granted++
		}
		if tool.WriteCapable {
			writeCapable++
		}
	}
	if granted != 1 || writeCapable != 1 {
		t.Fatalf("tools = %+v, want one granted and one write-capable", tools)
	}
	// Nothing has swept yet, and that is a state rather than a gap.
	if got.LastSweptAt != nil {
		t.Fatalf("last swept = %v on an organisation nothing has swept", got.LastSweptAt)
	}
}

// THE TEST THAT CAUGHT SOMETHING, TWICE. The producer's select policies on
// `org_profile_facts` (00023) and on `integrations` and `integration_tools`
// (00025) are `using (true)`, unlike its org-scoped policies on
// `compliance_profiles` and `watcher_findings`. So the scoping here is the
// query's `org_id` predicate rather than the policy's, and without it this
// context carried another organisation's connections and then its facts.
// Raised separately as a schema question; asserted here because this is the
// code that would leak.
func TestTheWatcherSeesOnlyItsOwnOrganisation(t *testing.T) {
	agent := agentStore(t)
	mine, _ := seedWatcherOrg(t)
	theirs, _ := seedWatcherOrg(t)

	pool := migratorPool(t)
	if _, err := pool.Exec(context.Background(), `
		insert into integrations (id, org_id, kind, display_name, endpoint_url, status, created_by)
		select gen_random_uuid(), $1, 'mcp', 'Their helpdesk', 'https://theirs.example.invalid', 'active', m.user_id
		  from memberships m where m.org_id = $1 limit 1
	`, theirs); err != nil {
		t.Fatalf("seeding their connection: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		insert into org_profile_facts (org_id, key, value, source, recorded_by)
		select $1, 'industry', '{"value": "theirs"}'::jsonb, 'onboarding', m.user_id
		  from memberships m where m.org_id = $1 limit 1
	`, theirs); err != nil {
		t.Fatalf("seeding their fact: %v", err)
	}

	got, err := agent.WatcherContextFor(t.Context(), mine.String())
	if err != nil {
		t.Fatalf("reading the context: %v", err)
	}
	for _, c := range got.Connections {
		if c.DisplayName == "Their helpdesk" {
			t.Fatal("another organisation's connection is in this context")
		}
	}
	for _, f := range got.Facts {
		if f.Key == "industry" {
			t.Fatal("another organisation's fact is in this context")
		}
	}
}

func TestAnOrganisationWithNoProfileHasNoContextAndTakesNoSignal(t *testing.T) {
	agent := agentStore(t)
	pool := migratorPool(t)
	org := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "watcher-bare-"+org.String()[:8], "Not onboarded"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		pool.Exec(context.WithoutCancel(context.Background()),
			`delete from organisations where id = $1`, org)
	})

	got, err := agent.WatcherContextFor(t.Context(), org.String())
	if err != nil {
		t.Fatalf("reading the context: %v", err)
	}
	if got.HasProfile {
		t.Fatal("an organisation with no profile reported one")
	}

	_, _, err = agent.RaiseSignal(t.Context(), org.String(), Signal{
		Kind: "profile_gap", DedupKey: "gap:x", Title: "Something", Severity: "medium",
	})
	if !errors.Is(err, ErrNoProfile) {
		t.Fatalf("err = %v, want ErrNoProfile: there is nothing to hang a signal on", err)
	}
}

func TestRaisingTheSameSignalTwiceKeepsOneOpenRow(t *testing.T) {
	agent := agentStore(t)
	org, profile := seedWatcherOrg(t)

	signal := Signal{
		Kind: "profile_gap", DedupKey: "gap:obligation:agent-test",
		Title: "No record of processing", Detail: "Nothing shows one",
		Severity: "critical", MetadataJSON: `{"missing":["ropa"]}`,
	}

	id, raised, err := agent.RaiseSignal(t.Context(), org.String(), signal)
	if err != nil {
		t.Fatalf("raising: %v", err)
	}
	if !raised || id == "" {
		t.Fatalf("first raise: id=%q raised=%v", id, raised)
	}

	// The same condition tomorrow. A daily sweep must not produce a row a
	// day, and the caller is told it did not raise anything new.
	signal.Title = "No record of processing (still)"
	again, raisedAgain, err := agent.RaiseSignal(t.Context(), org.String(), signal)
	if err != nil {
		t.Fatalf("raising again: %v", err)
	}
	if raisedAgain {
		t.Fatal("the second raise reported a new signal")
	}
	if again != id {
		t.Fatalf("the second raise made a new row: %s then %s", id, again)
	}

	var rows int
	var title, severity string
	if err := migratorPool(t).QueryRow(context.Background(), `
		select count(*) over (), title, severity::text
		  from watcher_findings
		 where profile_id = $1::uuid and dedup_key = $2
	`, profile, signal.DedupKey).Scan(&rows, &title, &severity); err != nil {
		t.Fatalf("reading the signal back: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d rows for one condition, want 1", rows)
	}
	// Updated in place, which is what makes the second call worth making: the
	// signal says what is true now.
	if title != "No record of processing (still)" || severity != "critical" {
		t.Fatalf("row = %q / %s", title, severity)
	}
}

// A signal citing an obligation the corpus does not have is the fabrication
// the citation validator refuses in a narrative, arriving by another door.
func TestASignalCitingAnUnknownObligationIsRefused(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedWatcherOrg(t)

	_, _, err := agent.RaiseSignal(t.Context(), org.String(), Signal{
		Kind: "profile_gap", DedupKey: "gap:invented", Title: "Invented",
		Severity: "high", ObligationSlug: "gdpr-art-99-does-not-exist",
	})
	if !errors.Is(err, ErrUnknownObligation) {
		t.Fatalf("err = %v, want ErrUnknownObligation", err)
	}

	var rows int
	if err := migratorPool(t).QueryRow(context.Background(),
		`select count(*) from watcher_findings where dedup_key = 'gap:invented'`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Fatal("a signal with an invented citation was written")
	}
}

// And a real one is accepted and kept, because the Analyst resolves it later.
func TestASignalCitingARealObligationKeepsTheCitation(t *testing.T) {
	agent := agentStore(t)
	org, _ := seedWatcherOrg(t)

	var slug string
	if err := migratorPool(t).QueryRow(context.Background(),
		`select slug from obligations limit 1`).Scan(&slug); err != nil {
		t.Skipf("no corpus loaded on this stack: %v", err)
	}

	if _, _, err := agent.RaiseSignal(t.Context(), org.String(), Signal{
		Kind: "regulatory_update", DedupKey: "update:" + slug, Title: "Something changed",
		Severity: "medium", ObligationSlug: slug,
	}); err != nil {
		t.Fatalf("raising: %v", err)
	}

	var stored string
	if err := migratorPool(t).QueryRow(context.Background(), `
		select obligation_slug from watcher_findings where dedup_key = $1
	`, "update:"+slug).Scan(&stored); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if stored != slug {
		t.Fatalf("stored %q, want %q", stored, slug)
	}
}

// THE OBLIGATIONS A RUN IS OFFERED ARE THE ONES THE SWEEP WOULD RAISE AGAINST
// (ENT-258, PR 2).
//
// The agent may cite only what it was shown, so what it is shown decides what
// it can say. Two properties are worth asserting rather than assuming.
//
// It is not the whole corpus. An organisation is offered the obligations whose
// applicability conditions hold for its own profile, so a signal citing
// something that does not apply to this organisation is impossible before any
// model is involved.
//
// And it is the SAME set the deterministic detectors work from, because both
// call `watcher_obligation_applies`. Two evaluators of "does this obligation
// bind this organisation" in one product is the arrangement ENT-246 was filed
// about, and an agent disagreeing with the sweep running beside it would be
// unexplainable to anybody reading the feed.
func TestTheObligationsOfferedAreTheOnesThatApplyToThisOrganisation(t *testing.T) {
	agent := agentStore(t)
	org, profile := seedWatcherOrg(t)

	context, err := agent.WatcherContextFor(t.Context(), org.String())
	if err != nil {
		t.Fatalf("assembling the context: %v", err)
	}
	if len(context.Obligations) == 0 {
		t.Fatal("no obligations offered, so this run could cite nothing at all")
	}

	// Every slug offered is one the corpus holds and one the shared evaluator
	// agrees applies to this profile. Asked of the database rather than
	// recomputed here, because a second implementation of the test is a second
	// thing that can be wrong in the same direction as the code.
	pool := migratorPool(t)
	for _, offered := range context.Obligations {
		var applies bool
		if err := pool.QueryRow(t.Context(), `
			select public.watcher_obligation_applies(o.applies_when, p)
			  from obligations o, compliance_profiles p
			 where o.slug = $1 and p.id = $2::uuid
		`, offered.Slug, profile).Scan(&applies); err != nil {
			t.Fatalf("checking %q: %v", offered.Slug, err)
		}
		if !applies {
			t.Errorf("%q was offered and does not apply to this organisation", offered.Slug)
		}
		if offered.Title == "" || offered.Summary == "" {
			t.Errorf("%q was offered with nothing for a model to read", offered.Slug)
		}
	}

	// And nothing that applies was withheld: an obligation the sweep will
	// raise a finding against, that the agent was never shown, is one it
	// cannot mention and would be refused for citing.
	var applicable int
	if err := pool.QueryRow(t.Context(), `
		select count(*)
		  from obligations o, compliance_profiles p
		 where p.id = $1::uuid and public.watcher_obligation_applies(o.applies_when, p)
	`, profile).Scan(&applicable); err != nil {
		t.Fatalf("counting what applies: %v", err)
	}
	if len(context.Obligations) != applicable {
		t.Fatalf("offered %d obligations and %d apply", len(context.Obligations), applicable)
	}
}
