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
