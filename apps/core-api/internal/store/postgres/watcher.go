package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// What an agentic Watcher reads and writes (ENT-258), on the producer pool.
//
// # WHY THE AGENT POOL AND NOT A TENANT TRANSACTION
//
// A Watcher run has no person behind it: it is started by a schedule, for an
// organisation, and there is no membership to resolve. That is the same shape
// the deterministic sweep already has, and `kindlast_agent` is the role built
// for it: one GUC, `app.current_org_id`, and policies that are org equality
// alone (00008).
//
// What it can reach is worth stating, because it is narrower than it looks and
// none of it is new. 00008 gave the agent `select` on `compliance_profiles` and
// insert/select/update on `watcher_findings`; 00023 gave it `select` on
// `org_profile_facts` so `watcher_obligation_applies` could read them; ENT-231
// gave it a COLUMN-LIMITED `select` on `integrations` that deliberately omits
// `credential_ciphertext`, and `select` on `integration_tools`. So the agent
// can already learn what an organisation is believed to be and what it has
// connected, and cannot learn any credential. This file adds no grant: it
// assembles what the agent may already read, and adds nothing to what it may
// write.

// WatcherContext is everything one Watcher run reasons over.
type WatcherContext struct {
	HasProfile  bool
	ProfileID   string
	Facts       []WatchedFact
	Connections []WatchedConnection
	OpenSignals []OpenSignal
	LastSweptAt *time.Time
	// Obligations is what a signal from this context may cite, and the only
	// thing it may cite. See WatchedObligation.
	Obligations []WatchedObligation
}

// WatchedObligation is one obligation the organisation may be cited against.
type WatchedObligation struct {
	Slug    string
	Title   string
	Summary string
}

// WatchedFact is one open belief about the organisation.
type WatchedFact struct {
	Key       string
	ValueJSON string
	Source    string
	ValidFrom time.Time
}

// WatchedConnection is one integration and what may be done there.
type WatchedConnection struct {
	ID          string
	Kind        string
	DisplayName string
	Status      string
	Revoked     bool
	Tools       []WatchedTool
}

// WatchedTool is one tool a connection offers.
type WatchedTool struct {
	Name         string
	Description  string
	WriteCapable bool
	Granted      bool
	ConnectionID string
}

// OpenSignal is something the Watcher has already said.
type OpenSignal struct {
	ID       string
	Kind     string
	DedupKey string
	Title    string
	Severity string
	// Source is `detector` or `agent` (00039). Carried out to the model so it
	// can tell which of these keys are a rule's and not its to write. See the
	// field's comment in watcher.proto for why that is worth a round trip.
	Source    string
	UpdatedAt time.Time
}

// WatcherContextFor assembles one organisation's context.
//
// One transaction with one GUC, so every read below is scoped by the agent's
// policies rather than by the queries remembering to filter. The profile is
// the newest one, which is the same profile `run_watcher()` sweeps: an
// organisation that has onboarded twice is watched against what it said most
// recently.
func (a *AgentStore) WatcherContextFor(ctx context.Context, orgID string) (WatcherContext, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return WatcherContext{}, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return WatcherContext{}, fmt.Errorf("postgres: beginning the watcher context: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return WatcherContext{}, err
	}

	var context WatcherContext
	err = tx.QueryRow(ctx, `
		select id::text, watcher_last_run_at
		  from compliance_profiles
		 order by created_at desc
		 limit 1
	`).Scan(&context.ProfileID, &context.LastSweptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// No profile: an organisation that has not finished onboarding. There
		// is nothing to watch and nowhere to hang a signal, and saying so is
		// more useful than an empty context the caller has to interpret.
		return WatcherContext{}, nil
	}
	if err != nil {
		return WatcherContext{}, fmt.Errorf("postgres: reading the profile to watch: %w", err)
	}
	context.HasProfile = true

	if context.Facts, err = watchedFacts(ctx, tx, org.String()); err != nil {
		return WatcherContext{}, err
	}
	if context.Connections, err = watchedConnections(ctx, tx, org.String()); err != nil {
		return WatcherContext{}, err
	}
	if context.OpenSignals, err = openSignals(ctx, tx, context.ProfileID); err != nil {
		return WatcherContext{}, err
	}
	if context.Obligations, err = watchedObligations(ctx, tx, context.ProfileID); err != nil {
		return WatcherContext{}, err
	}
	return context, nil
}

// watchedObligations reads what this organisation may be cited against.
//
// # THE SAME EVALUATOR THE DETERMINISTIC DETECTORS USE, DELIBERATELY
//
// `watcher_obligation_applies` is 00023's function, and calling it here rather
// than reimplementing the test is the whole point. Two evaluators of "does this
// obligation bind this organisation" in one product is the arrangement ENT-246
// was filed about, and an agent offered a differently computed set would
// disagree with the sweep running beside it in ways nobody could explain.
//
// When ENT-225 moves that function into Go, this query loses its function call
// and gains a caller-side filter over the same declaration. It should keep
// returning the same set, and the test above it is what says so.
//
// # WHY THE PROFILE ROW AND NOT THE ORGANISATION
//
// The function takes a `compliance_profiles` row because applicability is
// evaluated against what the organisation SAID, which is the profile. Passing
// the newest profile is what `run_watcher()` does, so an organisation that
// onboarded twice is offered obligations against the same answers it is swept
// against.
//
// The corpus is not tenant data: `obligations` has no `org_id` and no RLS
// predicate to satisfy, which is why this needs no organisation in its
// predicate while every other read in this file does.
func watchedObligations(ctx context.Context, tx pgx.Tx, profileID string) ([]WatchedObligation, error) {
	rows, err := tx.Query(ctx, `
		select o.slug, o.title, o.summary
		  from obligations o, compliance_profiles p
		 where p.id = $1::uuid
		   and public.watcher_obligation_applies(o.applies_when, p)
		 order by o.slug
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading the obligations to offer: %w", err)
	}
	defer rows.Close()

	var offered []WatchedObligation
	for rows.Next() {
		var o WatchedObligation
		if err := rows.Scan(&o.Slug, &o.Title, &o.Summary); err != nil {
			return nil, fmt.Errorf("postgres: scanning an offered obligation: %w", err)
		}
		offered = append(offered, o)
	}
	return offered, rows.Err()
}

// watchedFacts reads what the organisation is currently believed to be.
//
// Open values only, which is what `MemoryService.ListProfileFacts` returns to
// a person for the same reason: history is a different question, and carrying
// every superseded value would put the answer to "what did we think in March"
// into a prompt that is asking about today.
func watchedFacts(ctx context.Context, tx pgx.Tx, orgID string) ([]WatchedFact, error) {
	// `org_id` in the predicate for the reason watchedConnections gives at
	// length: the producer's select policy on this table is `using (true)`
	// too (00023), so the scoping here is the query's and not the policy's.
	rows, err := tx.Query(ctx, `
		select key, value::text, source, valid_from
		  from org_profile_facts
		 where org_id = $1::uuid and valid_to is null
		 order by key
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading profile facts for the watcher: %w", err)
	}
	defer rows.Close()

	var facts []WatchedFact
	for rows.Next() {
		var f WatchedFact
		if err := rows.Scan(&f.Key, &f.ValueJSON, &f.Source, &f.ValidFrom); err != nil {
			return nil, fmt.Errorf("postgres: reading a profile fact: %w", err)
		}
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// watchedConnections reads what the organisation has connected.
//
// Revoked connections are included and marked, because "we used to be able to
// read this" is part of deciding what has changed since the last look, and
// because the console shows them for the same reason (ENT-231).
//
// The endpoint URL is deliberately NOT read. The agent decides what to look
// at, not where to dial: a fetch goes through the gateway, which holds the
// egress allow-list and re-checks the policy it is sent. Handing a model an
// address is handing it the one part of the request the allow-list exists to
// decide.
//
// # THE ORGANISATION IS IN THE QUERY, AND IT IS LOAD BEARING, NOT BELT AND BRACES
//
// `compliance_profiles` and `watcher_findings` give the producer an
// org-equality policy, so a query against those may leave the scoping to the
// GUC. Five tables do not: `org_profile_facts` (00023) and `integrations`,
// `integration_tools`, `integration_fetches` and `audit_evidence` (00025) are
// `for select to kindlast_agent using (true)`, where the insert policies
// beside them are org equality and say why. So a select here with no `org_id`
// predicate reads EVERY organisation's connections, which is what
// `TestTheWatcherSeesOnlyItsOwnOrganisation` caught the first time it ran.
//
// Measured rather than assumed, and raised as its own change: tightening five
// shipped policies wants a migration and an isolation test of its own, not a
// paragraph inside a feature. Until then every producer-pool read of those
// tables names its organisation, here and anywhere else.
func watchedConnections(ctx context.Context, tx pgx.Tx, orgID string) ([]WatchedConnection, error) {
	rows, err := tx.Query(ctx, `
		select id::text, kind, display_name, status, revoked_at is not null
		  from integrations
		 where org_id = $1::uuid
		 order by created_at desc
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading connections for the watcher: %w", err)
	}
	defer rows.Close()

	var connections []WatchedConnection
	for rows.Next() {
		var c WatchedConnection
		if err := rows.Scan(&c.ID, &c.Kind, &c.DisplayName, &c.Status, &c.Revoked); err != nil {
			return nil, fmt.Errorf("postgres: reading a connection: %w", err)
		}
		connections = append(connections, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading connections for the watcher: %w", err)
	}
	if len(connections) == 0 {
		return nil, nil
	}

	tools, err := watchedTools(ctx, tx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range connections {
		connections[i].Tools = tools[connections[i].ID]
	}
	return connections, nil
}

// watchedTools reads every connection's tools in one query, keyed by
// connection: a join would repeat each connection once per tool and the
// assembly afterwards is the same work with more chances to get it wrong.
func watchedTools(ctx context.Context, tx pgx.Tx, orgID string) (map[string][]WatchedTool, error) {
	rows, err := tx.Query(ctx, `
		select integration_id::text, name, coalesce(description, ''), write_capable, granted
		  from integration_tools
		 where org_id = $1::uuid
		 order by name
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading connection tools for the watcher: %w", err)
	}
	defer rows.Close()

	tools := map[string][]WatchedTool{}
	for rows.Next() {
		var t WatchedTool
		if err := rows.Scan(&t.ConnectionID, &t.Name, &t.Description, &t.WriteCapable, &t.Granted); err != nil {
			return nil, fmt.Errorf("postgres: reading a connection tool: %w", err)
		}
		tools[t.ConnectionID] = append(tools[t.ConnectionID], t)
	}
	return tools, rows.Err()
}

// openSignals reads what the Watcher has already said about this profile.
func openSignals(ctx context.Context, tx pgx.Tx, profileID string) ([]OpenSignal, error) {
	rows, err := tx.Query(ctx, `
		select id::text, kind, dedup_key, title, severity::text, source, updated_at
		  from watcher_findings
		 where profile_id = $1::uuid and status = 'open'
		 order by updated_at desc
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading open signals: %w", err)
	}
	defer rows.Close()

	var signals []OpenSignal
	for rows.Next() {
		var s OpenSignal
		if err := rows.Scan(&s.ID, &s.Kind, &s.DedupKey, &s.Title, &s.Severity,
			&s.Source, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: reading an open signal: %w", err)
		}
		signals = append(signals, s)
	}
	return signals, rows.Err()
}

// Signal is one thing the Watcher says is worth looking at.
type Signal struct {
	Kind           string
	DedupKey       string
	Title          string
	Detail         string
	Severity       string
	ObligationSlug string
	MetadataJSON   string
}

// ErrUnknownObligation is returned when a signal cites an obligation slug the
// corpus does not have.
var ErrUnknownObligation = errors.New("postgres: no such obligation")

// AgentDedupPrefix namespaces every deduplication key an agent writes, so one
// can never land on a row a deterministic detector owns. See RaiseSignal for
// what happened before it existed.
//
// Exported because the test that proves the property has to know the shape of
// the key it is looking for, and a test that hard-codes "agent:" beside a
// constant is a test that passes after somebody changes the constant.
const AgentDedupPrefix = "agent:"

// RaiseSignal writes one signal through the same function the deterministic
// detectors use.
//
// # THE SAME FUNCTION, DELIBERATELY
//
// `emit_watcher_finding` resolves the organisation from the profile, applies
// the `(profile_id, dedup_key) where status = 'open'` conflict rule, and
// updates in place when the signal is already open. An agent writing its own
// INSERT would be a second way to create a signal with its own opinion about
// deduplication, and the first daily sweep after a divergence would produce
// two rows for one condition. One writer, one rule.
//
// # WHAT IS CHECKED HERE AND WHY IT IS NOT LEFT TO THE CONSTRAINT
//
// The obligation slug. A signal citing an obligation the corpus does not have
// is the same fabrication the citation validator refuses in a narrative,
// arriving by another door: the Analyst turns a signal into a finding and
// resolves that slug, so an invented one becomes a finding that cites nothing
// or no finding at all, days later, with nobody able to say why. Checked here,
// the run is refused while the model that produced it is still on the other
// end of the call.
//
// # AND THE DEDUPLICATION KEY IS NAMESPACED, WHICH IS NOT TIDINESS (ENT-258)
//
// `emit_watcher_finding` upserts on `(profile_id, dedup_key)`, so whoever
// writes a key OWNS the row it lands on. The deterministic detectors use keys
// of their own shape, `gap:obligation:{slug}` and the rest, and a watch is
// shown every open signal with its key, because a run that is not told what is
// already open repeats it.
//
// Those two facts together were a hole, and it was found by
// `scripts/watcher-comparison.py` the first time that gate ran: a model that
// echoes back a key it was shown does not raise a duplicate, it OVERWRITES the
// detector's row. The observed case rewrote "Profile gap: Records of
// Processing Activities" and dropped its severity from high to medium, which
// is a deterministic finding silently restated by a 4B and exactly the failure
// this product cannot have.
//
// Since 00039 (ENT-273) the schema also refuses it: every row records the
// `source` that created it, and a trigger refuses an update that changes one.
// That does not make the prefix redundant, and the two do different jobs. The
// prefix means an agent echoing a detector's key writes its OWN row and the
// run succeeds; without it the run would be refused by the trigger. Keeping
// both is the same stance ENT-272 took on the producer's queries: the caller
// says what it means, and the database is what enforces it.
//
// Prefixing closes it structurally rather than by a check that has to be
// remembered. An agent-raised key cannot collide with a detector's, whatever
// the model emits, including a model deliberately trying to. Deduplication
// among agent-raised signals is unaffected, because they are all prefixed the
// same way, and the prefix is visible in the context a later run reads, which
// tells it which signals were its own.
//
// The alternative was a `source` column and a policy refusing cross-source
// updates. It is the better long-term shape and it is a migration, a backfill
// and a new predicate on the hot path, for a property one prefix already
// gives.
func (a *AgentStore) RaiseSignal(ctx context.Context, orgID string, signal Signal) (id string, raised bool, err error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return "", false, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return "", false, fmt.Errorf("postgres: beginning a signal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return "", false, err
	}

	var profileID string
	err = tx.QueryRow(ctx, `
		select id::text from compliance_profiles order by created_at desc limit 1
	`).Scan(&profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		// The same refusal a manual record write gives for the same state
		// (records_write.go): there is nothing for this to hang off yet.
		return "", false, ErrNoProfile
	}
	if err != nil {
		return "", false, fmt.Errorf("postgres: reading the profile to signal against: %w", err)
	}

	if signal.ObligationSlug != "" {
		var exists bool
		if err := tx.QueryRow(ctx,
			`select exists (select 1 from obligations where slug = $1)`,
			signal.ObligationSlug).Scan(&exists); err != nil {
			return "", false, fmt.Errorf("postgres: resolving the signal's obligation: %w", err)
		}
		if !exists {
			return "", false, fmt.Errorf("%w: %q", ErrUnknownObligation, signal.ObligationSlug)
		}
	}

	// Namespaced before anything reads or writes it, so every lookup below and
	// the upsert itself see the same key. See the header for what happened
	// without this.
	dedupKey := AgentDedupPrefix + signal.DedupKey

	// Was it already open? Read before the write, for the same reason the act
	// path reads a status before acting: afterwards the answer is always yes.
	var existing string
	err = tx.QueryRow(ctx, `
		select id::text from watcher_findings
		 where profile_id = $1::uuid and dedup_key = $2 and status = 'open'
	`, profileID, dedupKey).Scan(&existing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("postgres: looking for an open signal: %w", err)
	}
	alreadyOpen := existing != ""

	metadata := signal.MetadataJSON
	if metadata == "" {
		metadata = "{}"
	}
	// `agent` is the ninth argument, and it is what the row will say produced
	// it (00039, ENT-273). The detectors in `run_watcher` pass nothing and get
	// the `detector` default, which is why this is the only place in the
	// codebase that names a source.
	//
	// It also arms the trigger that refuses a row changing hands. If this
	// landed on a signal a detector owns, the update would state `agent` over
	// `detector` and Postgres would refuse the whole statement. The prefix
	// above means that should not be reachable from here; the point is that it
	// is no longer reachable from anywhere.
	var signalID string
	if err := tx.QueryRow(ctx, `
		select emit_watcher_finding($1::uuid, $2, $3, $4, $5, $6, $7, $8::jsonb, 'agent')::text
	`, profileID, signal.Kind, dedupKey, signal.Title,
		nullIfEmpty(signal.Detail), signal.Severity,
		nullIfEmpty(signal.ObligationSlug), metadata).Scan(&signalID); err != nil {
		return "", false, fmt.Errorf("postgres: raising a signal: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, fmt.Errorf("postgres: committing a signal: %w", err)
	}
	return signalID, !alreadyOpen, nil
}

// StoredObservation is one thing a customer's own system reported, as it was
// stored (ENT-274).
//
// BodyJSON is third-party content. It was redacted by the gateway before
// anything was written and nothing here redacts again, for the reason
// `IngestEvidence` gives: a second implementation is free to disagree with the
// first, and the one that decided is the one that already ran.
type StoredObservation struct {
	EvidenceID   string
	ConnectionID string
	Tool         string
	ObservedAt   time.Time
	FetchedAt    time.Time
	BodyJSON     string
}

// ErrToolNotGranted is a tool the connection has not granted for reading.
var ErrToolNotGranted = errors.New("that tool is not granted on that connection")

// The most observations one read may return, and the default when a caller
// asks for none. Small, because the caller is a run that puts what comes back
// in front of a model: "what does this system say now" is the newest few, and
// a hundred rows is a customer's history rather than an answer.
const (
	maxObservations     = 20
	defaultObservations = 5
)

// EvidenceFor reads what one connection has already reported through one of
// its granted tools (ENT-274).
//
// # THE GRANT IS CHECKED HERE AND NOT ONLY IN THE CALLER
//
// The harness checks the tool against the context the run was shown, which
// catches a model naming something it was never offered. This checks the row,
// which is the invariant: no run reads through a tool the customer has not
// granted, whatever any caller believes. Two checks that refuse different
// things, the same arrangement a citation gets.
//
// A connection outside this organisation is not a special case and gets no
// special code. The agent's policies scope every read to the GUC's
// organisation, so it simply has no tools and no rows, and the answer is
// ErrNoConnection: what a caller must never be able to learn from this is
// whether an id it guessed exists somewhere else.
//
// # SUPERSEDED ROWS ARE LEFT OUT
//
// A Watcher asks what a system says now. An observation something later
// replaced is what it used to say, which is a real question and a different
// one, and answering both here would put two claims in front of a model with
// nothing to tell it which is current.
func (a *AgentStore) EvidenceFor(
	ctx context.Context, orgID, connectionID, tool string, limit int,
) (connectionName string, observations []StoredObservation, err error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}
	connection, err := uuid.Parse(connectionID)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %q is not a uuid", ErrNoConnection, connectionID)
	}
	if limit <= 0 {
		limit = defaultObservations
	}
	if limit > maxObservations {
		limit = maxObservations
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("postgres: reading stored evidence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return "", nil, err
	}

	err = tx.QueryRow(ctx, `
		select display_name
		  from integrations
		 where id = $1::uuid and org_id = $2::uuid
	`, connection, org).Scan(&connectionName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNoConnection
	}
	if err != nil {
		return "", nil, fmt.Errorf("postgres: reading the connection to read evidence for: %w", err)
	}

	var granted bool
	err = tx.QueryRow(ctx, `
		select granted
		  from integration_tools
		 where integration_id = $1::uuid and org_id = $2::uuid and name = $3
	`, connection, org, tool).Scan(&granted)
	if errors.Is(err, pgx.ErrNoRows) {
		// A tool the connection never offered reads the same as one it has not
		// granted, deliberately. The distinction is only interesting to
		// somebody probing for tool names, and it is not information this
		// caller needs to do its job.
		return "", nil, ErrToolNotGranted
	}
	if err != nil {
		return "", nil, fmt.Errorf("postgres: reading the tool grant: %w", err)
	}
	if !granted {
		return "", nil, ErrToolNotGranted
	}

	// `integration.<tool>` is the kind `IngestEvidence` and `RecordObservation`
	// both write, so matching on it is matching what a fetch through that tool
	// deposited. Compared as a whole string rather than with a prefix or a
	// pattern: a tool named `search` must not answer with what `search_all`
	// reported.
	rows, err := tx.Query(ctx, `
		select id::text, coalesce(connection_id::text, ''), observed_at,
		       fetched_at, body::text
		  from org_evidence
		 where org_id = $1::uuid
		   and connection_id = $2::uuid
		   and source = 'integration'
		   and kind = $3
		   and superseded_by is null
		 order by observed_at desc, fetched_at desc
		 limit $4
	`, org, connection, "integration."+tool, limit)
	if err != nil {
		return "", nil, fmt.Errorf("postgres: reading stored evidence: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		observation := StoredObservation{Tool: tool}
		if err := rows.Scan(&observation.EvidenceID, &observation.ConnectionID,
			&observation.ObservedAt, &observation.FetchedAt, &observation.BodyJSON); err != nil {
			return "", nil, fmt.Errorf("postgres: reading a stored observation: %w", err)
		}
		observations = append(observations, observation)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("postgres: reading stored evidence: %w", err)
	}
	return connectionName, observations, nil
}

// What became of one agent's ask for a fetch (ENT-279).
//
// The closed set the RPC answers with. Held here rather than in the service
// because the state is decided inside the transaction below: whether an ask
// queues is a fact about the rows at the moment of asking, and a state decided
// anywhere else would be decided about rows that may have changed.
const (
	FetchAskQueued          = "queued"
	FetchAskAlreadyQueued   = "already_queued"
	FetchAskRecentlyFetched = "recently_fetched"
)

// ErrConnectionRevoked is a connection nothing may be fetched through again.
var ErrConnectionRevoked = errors.New("that connection has been revoked")

// ErrToolWrites is a granted tool that can write, which an agent-requested
// fetch never touches: a person triggering a write from the Integrations page
// is a person deciding, and a model asking is nobody deciding.
var ErrToolWrites = errors.New("that tool can write, and an agent-requested fetch only reads")

// FetchAsk is what one RequestFetch left behind.
type FetchAsk struct {
	// One of the FetchAsk constants above.
	State string
	// The queued request when this ask queued one, and the request already
	// waiting when it did not. Empty for a recent attempt.
	RequestID string
	// For the acknowledgement's sentence.
	ConnectionName string
	// The most recent attempt inside the cooldown, when there was one.
	// LastDetail is the stored sentence and the caller must not relay it to a
	// model: it derives from a customer's endpoint. It is here so the service
	// can log it, never so it can say it.
	LastOutcome   string
	LastDetail    string
	LastAttemptAt time.Time
}

// RequestFetch records the agent's ask for a fetch of one granted tool on one
// connection, or answers why nothing was queued (ENT-279).
//
// # NOTHING HERE DIALS, AND NOTHING HERE COULD
//
// This runs on the producer pool, whose select on `integrations` omits
// `credential_ciphertext` (00025) and stays that way. What it writes is a
// `fetch_requests` row, which the scheduled fetch relay picks up the way it
// picks up a stale observation; the credential is read there, on the
// application role, under the connection's own standing consent. An attacker
// holding this whole method holds a way to ask, bounded by the cooldown below.
//
// # THE CHECKS ARE THE INVARIANT, NOT THE GUARDRAIL
//
// The harness has already checked the connection was shown to the run and the
// tool granted in its context; this checks the rows, so no ask stands on a
// grant the customer has withdrawn, whatever any caller believes. Two checks
// that refuse different things, the same arrangement ReadEvidence has.
//
// # ATTEMPTS COUNT, NOT SUCCESSES
//
// `attemptedSince` is compared against every fetch for the pair whatever its
// outcome, for the reason the scheduled listing counts attempts: keyed on
// successes, a down endpoint would be redialled on every ask forever.
func (a *AgentStore) RequestFetch(
	ctx context.Context, orgID, connectionID, tool, reason string,
	attemptedSince, pendingSince time.Time,
) (FetchAsk, error) {
	org, err := uuid.Parse(orgID)
	if err != nil {
		return FetchAsk{}, fmt.Errorf("%w: %q is not a uuid", ErrBadOrganisation, orgID)
	}
	connection, err := uuid.Parse(connectionID)
	if err != nil {
		return FetchAsk{}, fmt.Errorf("%w: %q is not a uuid", ErrNoConnection, connectionID)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return FetchAsk{}, fmt.Errorf("postgres: asking for a fetch: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLocal(ctx, tx, "app.current_org_id", org.String()); err != nil {
		return FetchAsk{}, err
	}

	ask := FetchAsk{}
	var status string
	err = tx.QueryRow(ctx, `
		select display_name, status
		  from integrations
		 where id = $1::uuid and org_id = $2::uuid
	`, connection, org).Scan(&ask.ConnectionName, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		// The same answer for a connection that does not exist and one that
		// is another organisation's, for the reason EvidenceFor gives: what a
		// caller must never learn here is whether an id it guessed exists
		// somewhere else.
		return FetchAsk{}, ErrNoConnection
	}
	if err != nil {
		return FetchAsk{}, fmt.Errorf("postgres: reading the connection to fetch from: %w", err)
	}
	if status != "active" {
		return FetchAsk{}, ErrConnectionRevoked
	}

	var granted, writeCapable bool
	err = tx.QueryRow(ctx, `
		select granted, write_capable
		  from integration_tools
		 where integration_id = $1::uuid and org_id = $2::uuid and name = $3
	`, connection, org, tool).Scan(&granted, &writeCapable)
	if errors.Is(err, pgx.ErrNoRows) {
		// A tool the connection never offered reads the same as one it has
		// not granted, as it does for EvidenceFor and for the same reason.
		return FetchAsk{}, ErrToolNotGranted
	}
	if err != nil {
		return FetchAsk{}, fmt.Errorf("postgres: reading the tool grant: %w", err)
	}
	if !granted {
		return FetchAsk{}, ErrToolNotGranted
	}
	if writeCapable {
		return FetchAsk{}, ErrToolWrites
	}

	// A recent attempt answers the ask from the record. Newest first, so the
	// age the acknowledgement reports is the age of the answer that stands.
	err = tx.QueryRow(ctx, `
		select outcome, coalesce(detail, ''), requested_at
		  from integration_fetches
		 where integration_id = $1::uuid and tool = $2
		   and requested_at > $3
		 order by requested_at desc
		 limit 1
	`, connection, tool, attemptedSince).Scan(&ask.LastOutcome, &ask.LastDetail, &ask.LastAttemptAt)
	if err == nil {
		ask.State = FetchAskRecentlyFetched
		return ask, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return FetchAsk{}, fmt.Errorf("postgres: reading recent fetches: %w", err)
	}

	// An ask already waiting is not asked twice.
	err = tx.QueryRow(ctx, `
		select id::text
		  from fetch_requests
		 where integration_id = $1::uuid and tool = $2
		   and created_at > $3
		 order by created_at desc
		 limit 1
	`, connection, tool, pendingSince).Scan(&ask.RequestID)
	if err == nil {
		ask.State = FetchAskAlreadyQueued
		return ask, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return FetchAsk{}, fmt.Errorf("postgres: reading pending fetch requests: %w", err)
	}

	err = tx.QueryRow(ctx, `
		insert into fetch_requests (org_id, integration_id, tool, reason)
		values ($1::uuid, $2::uuid, $3, $4)
		returning id::text
	`, org, connection, tool, reason).Scan(&ask.RequestID)
	if err != nil {
		return FetchAsk{}, fmt.Errorf("postgres: queueing a fetch request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return FetchAsk{}, fmt.Errorf("postgres: committing a fetch request: %w", err)
	}
	ask.State = FetchAskQueued
	return ask, nil
}
