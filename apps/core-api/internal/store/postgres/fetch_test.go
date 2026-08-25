package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/integrations"
)

// The two reads a scheduled fetch makes, and the dedup rule its deposits obey
// (ENT-279). Driven against the live stack, because everything worth asserting
// here is the database's: the definer functions' filters, the policies the
// plan runs under, and the content-hash comparison against real rows.

// seedFetchOrg makes an organisation with a member, as the migrator.
func seedFetchOrg(t *testing.T) (org uuid.UUID, member uuid.UUID) {
	t.Helper()
	conn := migratorConn(t)
	org, member = uuid.New(), uuid.New()

	if _, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "fetch-test-"+org.String()[:8], "Fetch test "+org.String()[:8]); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, org)
	})
	if _, err := conn.Exec(t.Context(),
		`insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
		org, member); err != nil {
		t.Fatalf("seeding a membership: %v", err)
	}
	return org, member
}

// seedFetchConnection makes a connection with one tool, as the migrator.
func seedFetchConnection(
	t *testing.T, org uuid.UUID, tool string, granted, writeCapable bool,
) uuid.UUID {
	t.Helper()
	conn := migratorConn(t)
	connection := uuid.New()

	if _, err := conn.Exec(t.Context(),
		`insert into integrations (id, org_id, kind, display_name, endpoint_url)
		 values ($1, $2, 'mcp', $3, 'https://tools.example.com/mcp')`,
		connection, org, "Fetch "+connection.String()[:8]); err != nil {
		t.Fatalf("seeding a connection: %v", err)
	}
	grantedAt := "null"
	if granted {
		grantedAt = "now()"
	}
	if _, err := conn.Exec(t.Context(),
		`insert into integration_tools (org_id, integration_id, name, write_capable, granted, granted_at)
		 values ($1, $2, $3, $4, $5, `+grantedAt+`)`,
		org, connection, tool, writeCapable, granted); err != nil {
		t.Fatalf("seeding a tool: %v", err)
	}
	return connection
}

func targetsOf(t *testing.T, agent *AgentStore) map[uuid.UUID]bool {
	t.Helper()
	targets, err := agent.FetchTargets(t.Context(), 24*time.Hour, time.Hour, 1000)
	if err != nil {
		t.Fatalf("FetchTargets: %v", err)
	}
	listed := map[uuid.UUID]bool{}
	for _, target := range targets {
		listed[uuid.MustParse(target.IntegrationID)] = true
	}
	return listed
}

// The four filters in `fetch_targets`, each of which is a customer decision a
// schedule must not widen. The database this runs against is shared, so the
// assertions are containment: our due connection is listed, and each excluded
// one is not.
//
// Proven able to fail: dropping `and t.granted` from the function body turns
// the ungranted case red, dropping `not t.write_capable` turns the
// write-capable case red, dropping the recency `not exists` turns the
// recent-failure case red, and dropping `i.status = 'active'` turns the
// revoked case red. Each was broken and restored through `create or replace`
// as the migrator while writing this file.
func TestFetchTargetsListsOnlyWhatACustomerActuallyOpened(t *testing.T) {
	agent := agentStore(t)
	migrator := migratorConn(t)
	org, _ := seedFetchOrg(t)

	due := seedFetchConnection(t, org, "list_records", true, false)
	ungranted := seedFetchConnection(t, org, "list_records", false, false)
	writeCapable := seedFetchConnection(t, org, "close_ticket", true, true)

	revoked := seedFetchConnection(t, org, "list_records", true, false)
	if _, err := migrator.Exec(t.Context(),
		`update integrations set status = 'revoked', revoked_at = now() where id = $1`,
		revoked); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	// A recent ATTEMPT suppresses the next tick, and deliberately including a
	// failed one: keyed on successes, a broken endpoint would be dialled on
	// every tick forever.
	recentlyFailed := seedFetchConnection(t, org, "list_records", true, false)
	if _, err := migrator.Exec(t.Context(),
		`insert into integration_fetches (org_id, integration_id, tool, outcome, detail)
		 values ($1, $2, 'list_records', 'failed', 'the endpoint did not answer usefully')`,
		org, recentlyFailed); err != nil {
		t.Fatalf("seeding a failed fetch: %v", err)
	}

	// An attempt older than the interval does not: staleness is the point.
	fetchedLongAgo := seedFetchConnection(t, org, "list_records", true, false)
	if _, err := migrator.Exec(t.Context(),
		`insert into integration_fetches
		     (org_id, integration_id, tool, outcome, requested_at, finished_at)
		 values ($1, $2, 'list_records', 'succeeded', now() - interval '2 days', now() - interval '2 days')`,
		org, fetchedLongAgo); err != nil {
		t.Fatalf("seeding an old fetch: %v", err)
	}

	listed := targetsOf(t, agent)
	if !listed[due] {
		t.Error("a granted read-only tool on an active connection with no recent fetch was not listed")
	}
	if !listed[fetchedLongAgo] {
		t.Error("a tool whose last fetch is older than the interval was not listed; nothing would ever be re-fetched")
	}
	if listed[ungranted] {
		t.Error("an ungranted tool was listed; granted is the customer's decision and the schedule widened it")
	}
	if listed[writeCapable] {
		t.Error("a write-capable tool was listed; a schedule may only read")
	}
	if listed[revoked] {
		t.Error("a revoked connection was listed; revoking is supposed to stop future fetches")
	}
	if listed[recentlyFailed] {
		t.Error("a connection with a recent FAILED attempt was listed; a broken endpoint would be dialled every tick forever")
	}
}

// The union arm (00050): a pending ask makes its pair due now, and nothing an
// ask says gets to skip a customer decision.
//
// The interesting cases are the refusals. An ask can change WHEN a permitted
// fetch happens; it cannot make an impermissible one permitted, and it cannot
// force a redial of a pair that has already been attempted since the ask.
func TestAnAskMakesItsPairDueAndSkipsNoFilter(t *testing.T) {
	agent := agentStore(t)
	migrator := migratorConn(t)
	org, _ := seedFetchOrg(t)

	ask := func(connection uuid.UUID, tool string, age string) {
		t.Helper()
		if _, err := migrator.Exec(t.Context(),
			`insert into fetch_requests (org_id, integration_id, tool, reason, created_at)
			 values ($1, $2, $3, 'the sweep judged the evidence stale', now() - $4::interval)`,
			org, connection, tool, age); err != nil {
			t.Fatalf("seeding an ask: %v", err)
		}
	}
	freshAttempt := func(connection uuid.UUID) {
		t.Helper()
		if _, err := migrator.Exec(t.Context(),
			`insert into integration_fetches (org_id, integration_id, tool, outcome, detail)
			 values ($1, $2, 'list_records', 'failed', 'did not answer')`,
			org, connection); err != nil {
			t.Fatalf("seeding an attempt: %v", err)
		}
	}

	// Fetched five minutes ago, so the schedule arm will not list it for a
	// day. A pending ask is what makes it due anyway, which is the entire
	// point of the RPC.
	asked := seedFetchConnection(t, org, "list_records", true, false)
	freshAttempt(asked)
	// The attempt above predates nothing; the ask must come after it.
	ask(asked, "list_records", "0 seconds")

	// Same shape, but an attempt landed AFTER the ask: the ask is served,
	// failed included, and does not force a redial. This is 00048's
	// attempts-suppress rule surviving the union arm.
	served := seedFetchConnection(t, org, "list_records", true, false)
	ask(served, "list_records", "10 minutes")
	freshAttempt(served)

	// An ask older than the window stops counting, so a relay outage does not
	// leave a stack of stale asks dialling customers when it comes back.
	expired := seedFetchConnection(t, org, "list_records", true, false)
	freshAttempt(expired)
	ask(expired, "list_records", "2 hours")

	// And the customer decisions hold against an ask: a grant withdrawn after
	// the ask stops the fetch, and an ask for a revoked connection fetches
	// nothing. The Go ask-path refuses these up front; this proves fetch time
	// refuses them AGAIN, because the withdrawal can happen in between.
	ungrantedAsk := seedFetchConnection(t, org, "list_records", false, false)
	ask(ungrantedAsk, "list_records", "0 seconds")

	revokedAsk := seedFetchConnection(t, org, "list_records", true, false)
	ask(revokedAsk, "list_records", "0 seconds")
	if _, err := migrator.Exec(t.Context(),
		`update integrations set status = 'revoked', revoked_at = now() where id = $1`,
		revokedAsk); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	listed := targetsOf(t, agent)
	if !listed[asked] {
		t.Error("a pending ask did not make its pair due; RequestFetch acknowledges an ask the relay would never serve")
	}
	if listed[served] {
		t.Error("an ask already served by a later attempt was listed; an ask could force a redial of a down endpoint")
	}
	if listed[expired] {
		t.Error("an ask older than the request window was listed; old asks should stop counting")
	}
	if listed[ungrantedAsk] {
		t.Error("an ask for an ungranted tool was listed; an ask must not widen the customer's grant")
	}
	if listed[revokedAsk] {
		t.Error("an ask for a revoked connection was listed; revoking is supposed to stop future fetches")
	}
}

// What the listing answers with: ids and a tool name, and nothing that would
// let the producer role dial anybody. The endpoint and the credential stay
// behind the application role's policies, which is the boundary this whole
// issue is careful not to move.
func TestFetchTargetsCarriesNoEndpointAndNoCredential(t *testing.T) {
	migrator := migratorConn(t)

	var signature string
	if err := migrator.QueryRow(t.Context(),
		`select pg_get_function_result(oid) from pg_proc where proname = 'fetch_targets'`).
		Scan(&signature); err != nil {
		t.Fatalf("reading the function's result shape: %v", err)
	}
	want := "TABLE(org_id uuid, integration_id uuid, tool text)"
	if signature != want {
		t.Fatalf("fetch_targets returns %q, want %q: anything more is the producer role "+
			"learning how to dial rather than what is due", signature, want)
	}
}

// FetchPlan runs as the person who consented, and that is the property under
// test: the most recent consent names whose authority, a connection nobody
// consented to falls back to its creator, and a consent whose person has left
// the organisation stops the fetch because the policy hides the row.
func TestFetchPlanRunsOnTheMostRecentConsent(t *testing.T) {
	store := testStore(t)
	migrator := migratorConn(t)
	org, ada := seedFetchOrg(t)
	connection := seedFetchConnection(t, org, "list_records", true, false)

	// Two consents: an old one by a departed colleague, then ada's. The plan
	// must run as ada, and the way to see whose authority it used is to make
	// the difference matter: the older person is NOT a member any more, so a
	// plan running as them would find nothing.
	departed := uuid.New()
	if _, err := migrator.Exec(t.Context(),
		`insert into integration_consents
		     (org_id, integration_id, consented_by, endpoint_url, granted_tools, consented_at)
		 values ($1, $2, $3, 'https://tools.example.com/mcp', array['list_records'], now() - interval '1 hour')`,
		org, connection, departed); err != nil {
		t.Fatalf("seeding the older consent: %v", err)
	}
	if _, err := migrator.Exec(t.Context(),
		`insert into integration_consents
		     (org_id, integration_id, consented_by, endpoint_url, granted_tools)
		 values ($1, $2, $3, 'https://tools.example.com/mcp', array['list_records'])`,
		org, connection, ada); err != nil {
		t.Fatalf("seeding the newer consent: %v", err)
	}
	if _, err := migrator.Exec(t.Context(),
		`update integrations
		    set credential_ciphertext = '\xdeadbeef'::bytea, credential_key_id = 'k1'
		  where id = $1`, connection); err != nil {
		t.Fatalf("sealing a credential: %v", err)
	}

	plan, err := store.FetchPlan(t.Context(), connection.String(), "list_records")
	if err != nil {
		t.Fatalf("FetchPlan: %v", err)
	}
	if plan.OrgID != org.String() {
		t.Fatalf("plan is for %q, want %s", plan.OrgID, org)
	}
	if plan.EndpointURL != "https://tools.example.com/mcp" {
		t.Fatalf("endpoint = %q", plan.EndpointURL)
	}
	if len(plan.Sealed) == 0 || plan.KeyID != "k1" {
		t.Fatalf("the sealed credential did not travel: %v %q", plan.Sealed, plan.KeyID)
	}
	if !plan.Tool.Granted || plan.Tool.WriteCapable {
		t.Fatalf("tool = %+v", plan.Tool)
	}
	if len(plan.Granted) != 1 || plan.Granted[0] != "list_records" {
		t.Fatalf("policy = %+v", plan.Granted)
	}
}

func TestFetchPlanFallsBackToTheCreatorWhenNobodyConsented(t *testing.T) {
	store := testStore(t)
	migrator := migratorConn(t)
	org, ada := seedFetchOrg(t)
	connection := seedFetchConnection(t, org, "list_records", true, false)
	if _, err := migrator.Exec(t.Context(),
		`update integrations set created_by = $1 where id = $2`, ada, connection); err != nil {
		t.Fatalf("stamping the creator: %v", err)
	}

	plan, err := store.FetchPlan(t.Context(), connection.String(), "list_records")
	if err != nil {
		t.Fatalf("FetchPlan: %v", err)
	}
	if plan.OrgID != org.String() {
		t.Fatalf("plan is for %q", plan.OrgID)
	}
}

// The property the whole split exists for: when the consenting person's
// membership is gone, the two-GUC policy hides the connection from the
// transaction acting as them, and the scheduled fetch STOPS. The organisation
// still comes back, so the caller can record the refusal.
func TestFetchPlanStopsWhenTheConsentingPersonHasLeft(t *testing.T) {
	store := testStore(t)
	migrator := migratorConn(t)
	org, ada := seedFetchOrg(t)
	connection := seedFetchConnection(t, org, "list_records", true, false)
	if _, err := migrator.Exec(t.Context(),
		`insert into integration_consents
		     (org_id, integration_id, consented_by, endpoint_url, granted_tools)
		 values ($1, $2, $3, 'https://tools.example.com/mcp', array['list_records'])`,
		org, connection, ada); err != nil {
		t.Fatalf("seeding the consent: %v", err)
	}

	// While ada is a member, the plan works: this is what makes the red half
	// below mean something.
	if _, err := store.FetchPlan(t.Context(), connection.String(), "list_records"); err != nil {
		t.Fatalf("FetchPlan with a live membership: %v", err)
	}

	if _, err := migrator.Exec(t.Context(),
		`delete from memberships where org_id = $1 and user_id = $2`, org, ada); err != nil {
		t.Fatalf("removing the membership: %v", err)
	}

	plan, err := store.FetchPlan(t.Context(), connection.String(), "list_records")
	if !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection: consent that outlives everyone who gave it kept working", err)
	}
	if plan.OrgID != org.String() {
		t.Fatalf("the refusal has nowhere to be recorded: org = %q", plan.OrgID)
	}
}

func TestFetchPlanRefusesARevokedConnectionAndAnUngrantedTool(t *testing.T) {
	store := testStore(t)
	migrator := migratorConn(t)
	org, ada := seedFetchOrg(t)

	revoked := seedFetchConnection(t, org, "list_records", true, false)
	for _, connection := range []uuid.UUID{revoked} {
		if _, err := migrator.Exec(t.Context(),
			`insert into integration_consents
			     (org_id, integration_id, consented_by, endpoint_url, granted_tools)
			 values ($1, $2, $3, 'https://tools.example.com/mcp', array['list_records'])`,
			org, connection, ada); err != nil {
			t.Fatalf("seeding the consent: %v", err)
		}
	}
	if _, err := migrator.Exec(t.Context(),
		`update integrations set status = 'revoked', revoked_at = now() where id = $1`,
		revoked); err != nil {
		t.Fatalf("revoking: %v", err)
	}
	if _, err := store.FetchPlan(t.Context(), revoked.String(), "list_records"); !errors.Is(err, integrations.ErrRevoked) {
		t.Fatalf("a revoked connection answered %v, want ErrRevoked", err)
	}

	ungranted := seedFetchConnection(t, org, "list_records", false, false)
	if _, err := migrator.Exec(t.Context(),
		`insert into integration_consents
		     (org_id, integration_id, consented_by, endpoint_url, granted_tools)
		 values ($1, $2, $3, 'https://tools.example.com/mcp', array[]::text[])`,
		org, ungranted, ada); err != nil {
		t.Fatalf("seeding the consent: %v", err)
	}
	if _, err := store.FetchPlan(t.Context(), ungranted.String(), "list_records"); !errors.Is(err, integrations.ErrNotGranted) {
		t.Fatalf("an ungranted tool answered %v, want ErrNotGranted", err)
	}

	if _, err := store.FetchPlan(t.Context(), uuid.NewString(), "list_records"); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("an unknown connection answered %v, want ErrNoConnection", err)
	}
}

// --- the content-hash rule -------------------------------------------------

// The same bytes twice make one observation and two fetch records. 00020 made
// `content_hash` an index rather than a unique constraint so that dedup would
// be Go's explicit call; this is the call, and a schedule fetching an
// unchanged system daily for a year is why it exists.
func TestTheSameBytesTwiceMakeOneObservationAndTwoFetchRecords(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	migrator := migratorConn(t)

	record := FetchRecord{
		OrgID: org, ConnectionID: connection, Tool: "list_records",
		ContentJSON: `{"records":3}`, Outcome: "succeeded",
	}
	first, err := agent.IngestEvidence(t.Context(), record)
	if err != nil {
		t.Fatalf("first IngestEvidence: %v", err)
	}
	if !first.EvidenceIsNew || first.EvidenceID == "" {
		t.Fatalf("the first observation was not new: %+v", first)
	}

	second, err := agent.IngestEvidence(t.Context(), record)
	if err != nil {
		t.Fatalf("second IngestEvidence: %v", err)
	}
	if second.EvidenceIsNew {
		t.Fatal("the same bytes were stored as a second observation")
	}
	if second.EvidenceID != first.EvidenceID {
		t.Fatalf("the second fetch links %q, want the existing observation %q",
			second.EvidenceID, first.EvidenceID)
	}
	if second.FetchID == "" || second.FetchID == first.FetchID {
		t.Fatal("the confirming fetch was not recorded on its own row")
	}

	var observations, fetches int
	if err := migrator.QueryRow(t.Context(),
		`select count(*) from org_evidence where org_id = $1`, org).Scan(&observations); err != nil {
		t.Fatalf("counting observations: %v", err)
	}
	if err := migrator.QueryRow(t.Context(),
		`select count(*) from integration_fetches where org_id = $1`, org).Scan(&fetches); err != nil {
		t.Fatalf("counting fetches: %v", err)
	}
	if observations != 1 || fetches != 2 {
		t.Fatalf("%d observations and %d fetches, want 1 and 2: "+
			"the fact is stored once and the confirmations are the fetch log's", observations, fetches)
	}

	// The confirming fetch row points at the observation, so "we checked on
	// the 24th and it still said this" is readable from the log.
	var linked string
	if err := migrator.QueryRow(t.Context(),
		`select evidence_id::text from integration_fetches where id = $1`,
		second.FetchID).Scan(&linked); err != nil {
		t.Fatalf("reading the link: %v", err)
	}
	if linked != first.EvidenceID {
		t.Fatalf("the confirming fetch links %q", linked)
	}
}

// Changed content is a new observation, and a reversion is too: only the
// NEWEST observation is compared, because A then B then A again is three
// facts (the change and the reversion both happened), not a duplicate.
func TestChangedContentAndAReversionAreBothNewObservations(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	migrator := migratorConn(t)

	for i, content := range []string{`{"records":3}`, `{"records":4}`, `{"records":3}`} {
		deposit, err := agent.IngestEvidence(t.Context(), FetchRecord{
			OrgID: org, ConnectionID: connection, Tool: "list_records",
			ContentJSON: content, Outcome: "succeeded",
		})
		if err != nil {
			t.Fatalf("IngestEvidence %d: %v", i, err)
		}
		if !deposit.EvidenceIsNew {
			t.Fatalf("observation %d was deduplicated; a change or a reversion is a fact", i)
		}
	}

	var observations int
	if err := migrator.QueryRow(t.Context(),
		`select count(*) from org_evidence where org_id = $1`, org).Scan(&observations); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if observations != 3 {
		t.Fatalf("%d observations, want 3", observations)
	}
}

// Two tools returning the same bytes are two observations: the hash is
// compared within one connection and one kind, because "the helpdesk says X"
// and "the CRM says X" are different facts that happen to agree.
func TestTheSameBytesFromADifferentToolAreADifferentObservation(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)

	first, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID: org, ConnectionID: connection, Tool: "list_records",
		ContentJSON: `{"count":3}`, Outcome: "succeeded",
	})
	if err != nil {
		t.Fatalf("first IngestEvidence: %v", err)
	}
	second, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID: org, ConnectionID: connection, Tool: "search_tickets",
		ContentJSON: `{"count":3}`, Outcome: "succeeded",
	})
	if err != nil {
		t.Fatalf("second IngestEvidence: %v", err)
	}
	if !second.EvidenceIsNew || second.EvidenceID == first.EvidenceID {
		t.Fatalf("two tools' identical answers were collapsed: %+v then %+v", first, second)
	}
}
