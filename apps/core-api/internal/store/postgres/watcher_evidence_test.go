package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// What a Watcher run may look at, driven against the live stack (ENT-274).
//
// The properties here are the database's and cannot be asserted anywhere else.
// The grant lives in `integration_tools`, the tenancy in the producer role's
// policies, and the shape of what a fetch deposited in `org_evidence.kind`.
// A fake would let all three agree with whatever this file believed.
//
// # THE ONE THAT MATTERS MOST IS THE LAST ONE
//
// A Watcher decides what to look at, so the question "can a run read an
// organisation it was not run for" is not academic here the way it is on a
// console surface where a session already answered it.

func seedGrantedTool(t *testing.T, org, connection uuid.UUID, name string, granted bool) {
	t.Helper()

	conn := migratorConn(t)
	if _, err := conn.Exec(t.Context(),
		// `granted_at` alongside `granted`, because the schema constrains the
		// two together: a grant with no moment it was given is refused.
		`insert into integration_tools (org_id, integration_id, name, description,
		                                write_capable, granted, granted_at)
		 values ($1, $2, $3, '', false, $4, case when $4 then now() end)`,
		org, connection, name, granted); err != nil {
		t.Fatalf("seeding a tool: %v", err)
	}
}

func depositObservation(t *testing.T, org, connection uuid.UUID, tool, body string, observed time.Time) string {
	t.Helper()

	conn := migratorConn(t)
	var id string
	if err := conn.QueryRow(t.Context(),
		`insert into org_evidence (org_id, source, connection_id, observed_at, kind, body)
		 values ($1, 'integration', $2, $3, $4, $5::jsonb)
		 returning id::text`,
		org, connection, observed, "integration."+tool, body).Scan(&id); err != nil {
		t.Fatalf("depositing an observation: %v", err)
	}
	return id
}

func TestAWatcherReadsWhatAFetchDeposited(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	older := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	newer := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)
	depositObservation(t, org, connection, "search_tickets", `{"open":1}`, older)
	latest := depositObservation(t, org, connection, "search_tickets", `{"open":4}`, newer)

	name, observations, err := agent.EvidenceFor(
		t.Context(), org.String(), connection.String(), "search_tickets", 0)
	if err != nil {
		t.Fatalf("reading stored evidence: %v", err)
	}
	if name == "" {
		t.Fatal("the connection name came back empty")
	}
	if len(observations) != 2 {
		t.Fatalf("expected two observations, got %d", len(observations))
	}
	// Newest first: a Watcher asks what a system says now, so the ordering is
	// part of the answer rather than a convenience.
	if observations[0].EvidenceID != latest {
		t.Fatalf("the newest observation is not first: got %q, want %q",
			observations[0].EvidenceID, latest)
	}
	if observations[0].BodyJSON != `{"open": 4}` && observations[0].BodyJSON != `{"open":4}` {
		t.Fatalf("the body came back as %q", observations[0].BodyJSON)
	}
}

// A tool the customer has not granted reads nothing, and the refusal is a
// typed error rather than an empty list.
//
// The difference is what a run is told. "Nothing has been fetched through that
// tool" and "you may not look at that tool" lead a model to opposite next
// steps, and only one of them is true.
func TestAToolThatIsNotGrantedIsRefusedEvenWhenRowsExist(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "close_ticket", false)
	depositObservation(t, org, connection, "close_ticket", `{"closed":1}`,
		time.Now().UTC().Truncate(time.Second))

	_, _, err := agent.EvidenceFor(
		t.Context(), org.String(), connection.String(), "close_ticket", 0)
	if !errors.Is(err, ErrToolNotGranted) {
		t.Fatalf("an ungranted tool answered %v rather than ErrToolNotGranted", err)
	}
}

// A tool the connection never offered reads the same as one it has not
// granted, deliberately. Telling the two apart is only useful to somebody
// probing for tool names.
func TestAToolTheConnectionNeverOfferedIsRefusedTheSameWay(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	_, _, err := agent.EvidenceFor(
		t.Context(), org.String(), connection.String(), "invented_tool", 0)
	if !errors.Is(err, ErrToolNotGranted) {
		t.Fatalf("an unknown tool answered %v rather than ErrToolNotGranted", err)
	}
}

// ONE GRANTED TOOL DOES NOT CARRY BACK WHAT ANOTHER DEPOSITED.
//
// The grant is per tool, so the query matches the whole stored kind rather
// than a prefix. Without that a tool named `search` would answer with what
// `search_all` reported, and a withdrawn grant would have no effect on
// anything already in the table.
func TestOneToolsReadDoesNotReturnAnothersObservations(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search", true)
	seedGrantedTool(t, org, connection, "search_all", true)

	now := time.Now().UTC().Truncate(time.Second)
	depositObservation(t, org, connection, "search", `{"from":"search"}`, now)
	depositObservation(t, org, connection, "search_all", `{"from":"search_all"}`, now)

	_, observations, err := agent.EvidenceFor(
		t.Context(), org.String(), connection.String(), "search", 0)
	if err != nil {
		t.Fatalf("reading stored evidence: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("expected one observation, got %d", len(observations))
	}
	if observations[0].BodyJSON != `{"from": "search"}` &&
		observations[0].BodyJSON != `{"from":"search"}` {
		t.Fatalf("a read of `search` returned %q", observations[0].BodyJSON)
	}
}

// A superseded observation is what a system USED to say, which is a real
// question and not the one a Watcher is asking.
func TestASupersededObservationIsNotWhatTheSystemSaysNow(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	now := time.Now().UTC().Truncate(time.Second)
	old := depositObservation(t, org, connection, "search_tickets", `{"open":1}`, now.Add(-time.Hour))
	current := depositObservation(t, org, connection, "search_tickets", `{"open":4}`, now)

	conn := migratorConn(t)
	if _, err := conn.Exec(t.Context(),
		`update org_evidence set superseded_by = $1::uuid where id = $2::uuid`,
		current, old); err != nil {
		t.Fatalf("superseding an observation: %v", err)
	}

	_, observations, err := agent.EvidenceFor(
		t.Context(), org.String(), connection.String(), "search_tickets", 0)
	if err != nil {
		t.Fatalf("reading stored evidence: %v", err)
	}
	if len(observations) != 1 || observations[0].EvidenceID != current {
		t.Fatalf("expected only the current observation, got %d", len(observations))
	}
}

// THE ONE THAT IS A SECURITY BOUNDARY AND NOT A FILTER.
//
// A run is given an organisation by core-api and a connection id by a model.
// If a connection belonging to somebody else could be read by naming its id,
// the whole surface would be a tenancy hole with a language model holding the
// keyboard. What stops it is the producer role's policies and the GUC this
// store sets, not the query remembering to filter.
func TestAConnectionInAnotherOrganisationIsUnreadable(t *testing.T) {
	agent := agentStore(t)
	mine, _ := seedConnection(t)
	theirs, theirConnection := seedConnection(t)
	seedGrantedTool(t, theirs, theirConnection, "search_tickets", true)
	depositObservation(t, theirs, theirConnection, "search_tickets", `{"secret":true}`,
		time.Now().UTC().Truncate(time.Second))

	_, observations, err := agent.EvidenceFor(
		t.Context(), mine.String(), theirConnection.String(), "search_tickets", 0)
	if !errors.Is(err, ErrNoConnection) {
		t.Fatalf("reading another organisation's connection answered %v (%d rows) "+
			"rather than ErrNoConnection", err, len(observations))
	}

	// AND THE SAME CALL WITH THE RIGHT ORGANISATION WORKS, which is what stops
	// the assertion above from passing for the wrong reason. Without it a
	// broken seed, a missing grant or a typo in the tool name would produce
	// the same refusal and the test would report isolation it never measured.
	// The two calls differ in one argument, so the organisation is what
	// decided.
	if _, theirRows, err := agent.EvidenceFor(
		t.Context(), theirs.String(), theirConnection.String(), "search_tickets", 0,
	); err != nil || len(theirRows) != 1 {
		t.Fatalf("the same read for the owning organisation must succeed, and "+
			"answered %v with %d rows", err, len(theirRows))
	}
}

// The row cap, which is a property of the query rather than of a run. The
// harness has its own bounds on what one run may look at; this one stops a
// single call returning a customer's whole history.
func TestAReadIsCappedHoweverManyRowsAreAskedFor(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	now := time.Now().UTC().Truncate(time.Second)
	for i := range maxObservations + 5 {
		depositObservation(t, org, connection, "search_tickets", `{"open":1}`,
			now.Add(-time.Duration(i)*time.Minute))
	}

	_, observations, err := agent.EvidenceFor(
		t.Context(), org.String(), connection.String(), "search_tickets", 1_000)
	if err != nil {
		t.Fatalf("reading stored evidence: %v", err)
	}
	if len(observations) != maxObservations {
		t.Fatalf("a read of 1000 returned %d rows rather than the cap of %d",
			len(observations), maxObservations)
	}
}
