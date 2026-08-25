package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The ingest door evidence comes in by when nobody is signed in (ENT-231).
//
// Driven against the live stack on the producer pool, because the properties
// worth asserting are the database's: the org-scoped insert policies, the
// constraint that a refusal carries no content, and the one that matters most,
// that a caller naming an organisation it has no business in writes nothing
// rather than something.

// seedConnection makes an organisation and a connection for it, as the
// migrator, and removes both afterwards.
//
// As the migrator rather than the agent, for the reason `seedOrg` next door
// gives at length: 00008 leaves `kindlast_agent` holding nothing on
// organisations, so the role that can record a fetch cannot invent a tenant to
// record it against. 00025 keeps that true, granting it column-level select on
// `integrations` and no insert at all.
func seedConnection(t *testing.T) (org uuid.UUID, connection uuid.UUID) {
	t.Helper()

	conn := migratorConn(t)
	org = uuid.New()
	connection = uuid.New()
	name := "Evidence test " + org.String()[:8]

	if _, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		org, "evidence-test-"+org.String()[:8], name); err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	t.Cleanup(func() {
		//nolint:errcheck // best effort cleanup on a test fixture
		conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, org)
	})

	if _, err := conn.Exec(t.Context(),
		`insert into integrations (id, org_id, kind, display_name, endpoint_url)
		 values ($1, $2, 'mcp', $3, 'https://tools.example.com/mcp')`,
		connection, org, name); err != nil {
		t.Fatalf("seeding a connection: %v", err)
	}
	return org, connection
}

func TestIngestEvidenceStoresAnObservationAndTheFetchThatProducedIt(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	migrator := migratorConn(t)

	observed := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
	deposit, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID:         org,
		ConnectionID:  connection,
		Tool:          "search_tickets",
		ArgumentsJSON: `{"status":"open"}`,
		ContentJSON:   `{"tickets":3}`,
		Outcome:       "succeeded",
		Redactions:    2,
		ObservedAt:    observed,
		RequestedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	if deposit.EvidenceID == "" {
		t.Fatal("a successful fetch stored no observation")
	}
	if deposit.FetchID == "" {
		t.Fatal("no fetch record was written")
	}

	// Source, connection and both timestamps. Every one of those is an
	// acceptance criterion rather than a nicety, so each is read back rather
	// than assumed.
	var source, connectionID, kind string
	var storedObserved, fetchedAt time.Time
	err = migrator.QueryRow(t.Context(), `
		select source, connection_id::text, kind, observed_at, fetched_at
		  from org_evidence where id = $1`, deposit.EvidenceID).
		Scan(&source, &connectionID, &kind, &storedObserved, &fetchedAt)
	if err != nil {
		t.Fatalf("reading the observation back: %v", err)
	}
	if source != "integration" {
		t.Errorf("source is %q", source)
	}
	if connectionID != connection.String() {
		t.Errorf("connection_id is %q, want %s", connectionID, connection)
	}
	if kind != "integration.search_tickets" {
		t.Errorf("kind is %q", kind)
	}
	if !storedObserved.UTC().Equal(observed) {
		t.Errorf("observed_at is %v, want %v", storedObserved.UTC(), observed)
	}
	// `fetched_at` is the database's own clock and is later than the
	// observation, which is the gap the console renders as "read by us N days
	// later". Two timestamps rather than one, because they are routinely far
	// apart and the distance is the interesting part.
	if !fetchedAt.After(storedObserved) {
		t.Errorf("fetched_at %v is not after observed_at %v", fetchedAt, storedObserved)
	}

	// And the fetch points at it, so "what we fetched" and "what we believe"
	// are one story rather than two lists.
	var linked string
	var redactions int32
	err = migrator.QueryRow(t.Context(), `
		select coalesce(evidence_id::text, ''), redactions
		  from integration_fetches where id = $1`, deposit.FetchID).
		Scan(&linked, &redactions)
	if err != nil {
		t.Fatalf("reading the fetch back: %v", err)
	}
	if linked != deposit.EvidenceID {
		t.Errorf("the fetch points at %q, want %s", linked, deposit.EvidenceID)
	}
	if redactions != 2 {
		t.Errorf("redactions is %d, want 2", redactions)
	}
}

// A refusal is recorded, with no observation, which is the half that makes the
// gateway legible to a customer.
func TestIngestEvidenceRecordsARefusalWithNothingStored(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	migrator := migratorConn(t)

	deposit, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID:        org,
		ConnectionID: connection,
		Tool:         "close_ticket",
		Outcome:      "refused",
		Detail:       "the tool is not granted on this connection",
	})
	if err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}
	if deposit.EvidenceID != "" {
		t.Errorf("a refusal stored an observation: %s", deposit.EvidenceID)
	}
	if deposit.FetchID == "" {
		t.Fatal("a refusal was not recorded, which is the whole point of recording refusals")
	}

	var outcome, detail string
	err = migrator.QueryRow(t.Context(),
		`select outcome, coalesce(detail, '') from integration_fetches where id = $1`,
		deposit.FetchID).Scan(&outcome, &detail)
	if err != nil {
		t.Fatalf("reading the fetch back: %v", err)
	}
	if outcome != "refused" {
		t.Errorf("outcome is %q", outcome)
	}
	if !strings.Contains(detail, "not granted") {
		t.Errorf("detail is %q", detail)
	}
}

// A CALLER NAMING AN ORGANISATION THAT DOES NOT OWN THE CONNECTION WRITES
// NOTHING, WHICH IS THE DIRECTION THIS HAS TO FAIL IN.
//
// The producer role's insert policies on these two tables read
// `app.current_org_id`, unlike `agent_runs` in 00019 whose policy is
// unconditional. So a caller that sets no organisation at all matches none and
// writes nothing, and one that names the wrong organisation is refused by the
// foreign key on a connection it does not own. The failure mode worth spending
// a test on is the other one: an insert that lands somewhere arbitrary.
func TestIngestEvidenceWritesNothingForAnOrganisationThatDoesNotOwnTheConnection(t *testing.T) {
	agent := agentStore(t)
	_, connection := seedConnection(t)
	migrator := migratorConn(t)

	stranger := uuid.New()
	_, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID:        stranger,
		ConnectionID: connection,
		Tool:         "search_tickets",
		ContentJSON:  `{"tickets":3}`,
		Outcome:      "succeeded",
	})
	if err == nil {
		t.Fatal("evidence was written for an organisation that does not own the connection")
	}

	// And nothing landed. An error with a row behind it would be the worst of
	// both.
	var rows int
	if err := migrator.QueryRow(t.Context(),
		`select count(*) from integration_fetches where org_id = $1`, stranger).
		Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Fatalf("%d fetch rows were written for an organisation that should have none", rows)
	}
}

// Malformed JSON is refused before it reaches a column, because a payload
// stored now is a page that cannot render later and a customer reading their
// own evidence is the wrong person to find that.
func TestIngestEvidenceRefusesContentThatIsNotJSON(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)

	_, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID:        org,
		ConnectionID: connection,
		Tool:         "search_tickets",
		ContentJSON:  "{not json",
		Outcome:      "succeeded",
	})
	if err == nil {
		t.Fatal("content that is not JSON was accepted")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("refused for an unexpected reason: %v", err)
	}
}

// An observation arriving with no `observed_at` is stamped with the moment it
// was fetched rather than with year one, which is what a zero time stores and
// what reads as data corruption to whoever finds it.
func TestIngestEvidenceStampsAnObservationThatCameWithNoTime(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	migrator := migratorConn(t)

	deposit, err := agent.IngestEvidence(t.Context(), FetchRecord{
		OrgID:        org,
		ConnectionID: connection,
		Tool:         "search_tickets",
		ContentJSON:  `{"tickets":3}`,
		Outcome:      "succeeded",
	})
	if err != nil {
		t.Fatalf("IngestEvidence: %v", err)
	}

	var observed time.Time
	if err := migrator.QueryRow(t.Context(),
		`select observed_at from org_evidence where id = $1`, deposit.EvidenceID).
		Scan(&observed); err != nil {
		t.Fatalf("reading the observation back: %v", err)
	}
	if observed.Year() < 2020 {
		t.Fatalf("observed_at is %v, which is a zero time stored as a date", observed)
	}
}
