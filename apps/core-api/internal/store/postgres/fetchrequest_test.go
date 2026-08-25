package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// The agent's ask for a fetch, driven against the live stack (ENT-279).
//
// The properties here are the database's: the grant and write-capability live
// in `integration_tools`, the cooldown reads `integration_fetches`, and the
// producer role's policies are what keep an ask inside its organisation. The
// service tests prove the codes; these prove the rows.
//
// # WHAT IS DELIBERATELY NOT TESTED HERE
//
// That the ask causes a fetch. Nothing in this branch dials: a request row is
// served by the scheduled fetch relay (the other half of ENT-279, landing
// separately), and this suite proving a dial would mean this branch had grown
// one, which is exactly the second fetch path ENT-263 warned about.

func cutoffs() (attemptedSince, pendingSince time.Time) {
	now := time.Now().UTC()
	return now.Add(-time.Hour), now.Add(-time.Hour)
}

func TestAnAskForAGrantedReadOnlyToolQueuesARequest(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	attempted, pending := cutoffs()
	ask, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"search_tickets", "the profile is silent about access requests", attempted, pending)
	if err != nil {
		t.Fatalf("asking for a fetch: %v", err)
	}
	if ask.State != FetchAskQueued {
		t.Fatalf("the ask came back %q rather than queued", ask.State)
	}
	if ask.RequestID == "" {
		t.Fatal("queued with no request id, so nothing can be traced to this ask")
	}
	if ask.ConnectionName == "" {
		t.Fatal("the connection name came back empty")
	}

	// The row is really there, and it says why.
	conn := migratorConn(t)
	var reason string
	if err := conn.QueryRow(t.Context(),
		`select reason from fetch_requests where id = $1::uuid`,
		ask.RequestID).Scan(&reason); err != nil {
		t.Fatalf("reading the queued request back: %v", err)
	}
	if reason == "" {
		t.Fatal("the model's reason was not stored")
	}
}

func TestASecondIdenticalAskFindsTheFirstRatherThanQueueingTwice(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	attempted, pending := cutoffs()
	first, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"search_tickets", "", attempted, pending)
	if err != nil {
		t.Fatalf("the first ask: %v", err)
	}

	second, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"search_tickets", "", attempted, pending)
	if err != nil {
		t.Fatalf("the second ask: %v", err)
	}
	if second.State != FetchAskAlreadyQueued {
		t.Fatalf("the second ask came back %q rather than already_queued", second.State)
	}
	if second.RequestID != first.RequestID {
		t.Fatalf("the second ask points at %q rather than the waiting request %q",
			second.RequestID, first.RequestID)
	}

	conn := migratorConn(t)
	var count int
	if err := conn.QueryRow(t.Context(),
		`select count(*) from fetch_requests where integration_id = $1`,
		connection).Scan(&count); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if count != 1 {
		t.Fatalf("two identical asks left %d rows rather than 1", count)
	}
}

// A recent attempt answers the ask whatever its outcome. Attempts rather than
// successes, so a down endpoint is not redialled because a model asked twice.
func TestARecentFailedAttemptAnswersTheAskWithoutQueueing(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	conn := migratorConn(t)
	if _, err := conn.Exec(t.Context(),
		`insert into integration_fetches (org_id, integration_id, tool, outcome, detail, requested_at)
		 values ($1, $2, 'search_tickets', 'failed', 'the endpoint did not answer', now() - interval '10 minutes')`,
		org, connection); err != nil {
		t.Fatalf("seeding a failed fetch: %v", err)
	}

	attempted, pending := cutoffs()
	ask, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"search_tickets", "", attempted, pending)
	if err != nil {
		t.Fatalf("asking after a failure: %v", err)
	}
	if ask.State != FetchAskRecentlyFetched {
		t.Fatalf("the ask came back %q rather than recently_fetched", ask.State)
	}
	if ask.LastOutcome != "failed" {
		t.Fatalf("the last outcome came back %q", ask.LastOutcome)
	}

	var count int
	if err := conn.QueryRow(t.Context(),
		`select count(*) from fetch_requests where integration_id = $1`,
		connection).Scan(&count); err != nil {
		t.Fatalf("counting requests: %v", err)
	}
	if count != 0 {
		t.Fatalf("an ask inside the cooldown queued %d rows rather than none", count)
	}
}

func TestAnAskForAnUngrantedToolIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "close_ticket", false)

	attempted, pending := cutoffs()
	_, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"close_ticket", "", attempted, pending)
	if !errors.Is(err, ErrToolNotGranted) {
		t.Fatalf("an ungranted tool answered %v rather than ErrToolNotGranted", err)
	}
}

// A granted tool that can write is still refused. The SQL never checks this:
// it is Go's decision, so this test is what proves the decision exists.
func TestAWriteCapableToolIsRefusedEvenWhenGranted(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)

	conn := migratorConn(t)
	if _, err := conn.Exec(t.Context(),
		`insert into integration_tools (org_id, integration_id, name, description,
		                                write_capable, granted, granted_at)
		 values ($1, $2, 'create_ticket', '', true, true, now())`,
		org, connection); err != nil {
		t.Fatalf("seeding a write-capable tool: %v", err)
	}

	attempted, pending := cutoffs()
	_, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"create_ticket", "", attempted, pending)
	if !errors.Is(err, ErrToolWrites) {
		t.Fatalf("a write-capable tool answered %v rather than ErrToolWrites", err)
	}
}

func TestARevokedConnectionRefusesTheAsk(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	conn := migratorConn(t)
	if _, err := conn.Exec(t.Context(),
		`update integrations set status = 'revoked', revoked_at = now() where id = $1`,
		connection); err != nil {
		t.Fatalf("revoking the connection: %v", err)
	}

	attempted, pending := cutoffs()
	_, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"search_tickets", "", attempted, pending)
	if !errors.Is(err, ErrConnectionRevoked) {
		t.Fatalf("a revoked connection answered %v rather than ErrConnectionRevoked", err)
	}
}

// The tenancy assertion, and the one a fake could never carry: another
// organisation's connection is invisible to the ask, and indistinguishable
// from one that does not exist.
func TestAnAskCannotNameAnotherOrganisationsConnection(t *testing.T) {
	agent := agentStore(t)
	_, victim := seedConnection(t)
	attacker, _ := seedConnection(t)

	attempted, pending := cutoffs()
	_, err := agent.RequestFetch(t.Context(), attacker.String(), victim.String(),
		"search_tickets", "", attempted, pending)
	if !errors.Is(err, ErrNoConnection) {
		t.Fatalf("another organisation's connection answered %v rather than ErrNoConnection", err)
	}
}

// And the role split: the producer cannot update or delete an ask, so the
// record of why a customer's system was dialled cannot be rewritten by the
// role that asked. A 42501 is the grant surface working; anything else,
// including success, is the boundary gone.
func TestTheProducerCannotRewriteOrRemoveAnAsk(t *testing.T) {
	agent := agentStore(t)
	org, connection := seedConnection(t)
	seedGrantedTool(t, org, connection, "search_tickets", true)

	attempted, pending := cutoffs()
	ask, err := agent.RequestFetch(t.Context(), org.String(), connection.String(),
		"search_tickets", "before", attempted, pending)
	if err != nil {
		t.Fatalf("asking: %v", err)
	}

	permissionDenied := func(err error) bool {
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == "42501"
	}

	if _, err := agentTxFor(t, agent, org).Exec(t.Context(),
		`update fetch_requests set reason = 'after' where id = $1::uuid`,
		ask.RequestID); !permissionDenied(err) {
		t.Fatalf("the producer role updated an ask: %v", err)
	}
	if _, err := agentTxFor(t, agent, org).Exec(t.Context(),
		`delete from fetch_requests where id = $1::uuid`,
		ask.RequestID); !permissionDenied(err) {
		t.Fatalf("the producer role deleted an ask: %v", err)
	}
}
