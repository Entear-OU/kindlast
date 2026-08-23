package postgres

import (
	"cmp"
	"context"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/stackenv"
)

// The Go sweep produces what the plpgsql produced (ENT-259).
//
// # WHY THIS TEST EXISTS AND WHAT IT WOULD MISS WITHOUT IT
//
// ENT-259's acceptance criterion is that each detector produces the same
// signals the plpgsql produced for the same fixtures, "asserted by running both
// against the fixture set before the SQL is dropped". A table test over the Go
// functions says the Go functions do what their author believes; only this says
// the product's behaviour did not change under a customer who was mid-sweep
// when the deployment happened.
//
// So two organisations are built identically, one is swept by `run_watcher()`
// and `run_analyst()`, the other by the Go path, and every row either produced
// is compared field by field with the identifiers and timestamps stripped.
//
// # BOTH RUN AS `kindlast_agent`, WHICH IS HALF THE POINT
//
// The bug class ENT-259 names is a detector reading a table the producer role
// was never granted, and PR #223 shipped exactly that. Running the Go path on
// the agent pool means a read the Go code adds is a `42501` here on the commit
// that adds it. Running it as the migrator would prove the rules and hide the
// grants, which is the arrangement that let the original bug through.
//
// # THE FIXTURES COMMIT, AND ARE DELETED AFTERWARDS
//
// Unlike `watcher_applicability_test.go`, this cannot roll back: the agent pool
// is a different connection from the one that writes the fixtures, so an
// uncommitted organisation is invisible to it. Nothing global is touched, only
// organisations this test created, and the cleanup deletes them by id.
//
// Proven able to fail: changing DetectGaps to omit the "Recurring gap" wording
// turns the recurring case red while every other case stays green, and
// switching Effort's deadline answer from days to hours turns the Analyst
// comparison red and nothing else.

// sweepFixture is one organisation seeded identically to its twin.
type sweepFixture struct {
	orgID   uuid.UUID
	profile uuid.UUID
}

func equivalenceConn(t *testing.T, role string) *pgx.Conn {
	t.Helper()

	dsn := stackenv.DSN(role)
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: "+
				"%s unreachable (%v)", dsn, err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", dsn, err)
	}
	t.Cleanup(func() { _ = conn.Close(context.WithoutCancel(t.Context())) })
	return conn
}

// dsarFixture is one request to seed, described relative to today so the test
// means the same thing whenever it runs.
type dsarFixture struct {
	subject *string
	dueDays int
}

// seedSweepOrg builds one organisation, its profile and its requests.
func seedSweepOrg(
	t *testing.T, conn *pgx.Conn, label string,
	hasDPO, hasROPA string, aiSystems, dataCategories []string, dsars []dsarFixture,
) sweepFixture {
	t.Helper()

	f := sweepFixture{orgID: uuid.New(), profile: uuid.New()}

	// The columns are NOT NULL with an empty-array default, and a nil slice
	// binds as NULL rather than as the default.
	if aiSystems == nil {
		aiSystems = []string{}
	}
	if dataCategories == nil {
		dataCategories = []string{}
	}

	if _, err := conn.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		f.orgID, "ent-259-"+f.orgID.String()[:8], "Sweep equivalence "+label,
	); err != nil {
		t.Fatalf("seeding the organisation: %v", err)
	}
	t.Cleanup(func() {
		// Cascades to the profile, the signals and the findings.
		_, _ = conn.Exec(context.WithoutCancel(t.Context()),
			`delete from organisations where id = $1`, f.orgID)
	})

	var session uuid.UUID
	if err := conn.QueryRow(t.Context(),
		`insert into onboarding_sessions (org_id) values ($1) returning id`,
		f.orgID).Scan(&session); err != nil {
		t.Fatalf("seeding the onboarding session: %v", err)
	}

	if _, err := conn.Exec(t.Context(), `
		insert into compliance_profiles (
			id, org_id, session_id, industry, has_dpo, has_ropa,
			transfers_outside_eu, ai_systems, data_categories, vendor_list, staff_count)
		values ($1, $2, $3, 'saas', $4, $5, 'no', $6, $7, '', 12)`,
		f.profile, f.orgID, session, hasDPO, hasROPA, aiSystems, dataCategories,
	); err != nil {
		t.Fatalf("seeding the compliance profile: %v", err)
	}

	for i, d := range dsars {
		if _, err := conn.Exec(t.Context(), `
			insert into dsars (org_id, subject_name, request_type, status,
			                   received_at, response_due_at)
			values ($1, $2, 'access', 'open', now(), now() + make_interval(days => $3))`,
			f.orgID, d.subject, d.dueDays,
		); err != nil {
			t.Fatalf("seeding data-subject request %d: %v", i, err)
		}
	}

	return f
}

// signalRow is one signal with everything identifying stripped.
//
// The `dsar:{uuid}` deduplication key and the `dsar_id` in the metadata name a
// row that differs between the two organisations by construction, so they are
// replaced with a stable marker rather than compared. Everything a person
// actually reads is compared verbatim.
type signalRow struct {
	Kind       string
	DedupKey   string
	Severity   string
	Title      string
	Detail     string
	Obligation string
	Metadata   string
}

func readSignals(t *testing.T, conn *pgx.Conn, profile uuid.UUID) []signalRow {
	t.Helper()

	rows, err := conn.Query(t.Context(), `
		select kind, dedup_key, severity, title, coalesce(detail, ''),
		       coalesce(obligation_slug, ''),
		       (metadata - 'dsar_id' - 'response_due_at')::text
		  from watcher_findings
		 where profile_id = $1
		 order by dedup_key, title
	`, profile)
	if err != nil {
		t.Fatalf("reading signals: %v", err)
	}
	defer rows.Close()

	var out []signalRow
	for rows.Next() {
		var s signalRow
		if err := rows.Scan(&s.Kind, &s.DedupKey, &s.Severity, &s.Title,
			&s.Detail, &s.Obligation, &s.Metadata); err != nil {
			t.Fatalf("scanning a signal: %v", err)
		}
		s.DedupKey = maskDSARKey(s.DedupKey)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading signals: %v", err)
	}
	// Sorted AFTER masking. The `dsar:{uuid}` keys differ between the two
	// organisations by construction, so sorting in SQL orders the two sides by
	// two different uuids and the comparison below reports a difference that
	// is only a permutation.
	slices.SortFunc(out, func(a, b signalRow) int {
		return cmp.Or(
			cmp.Compare(a.DedupKey, b.DedupKey),
			cmp.Compare(a.Title, b.Title),
		)
	})
	return out
}

// findingRow is one finding with everything identifying stripped.
type findingRow struct {
	Detected   string
	Severity   string
	Action     string
	Regulation string
	URL        string
	Context    string
	Effort     string
	ActionType string
	Metadata   string
}

func readFindings(t *testing.T, conn *pgx.Conn, profile uuid.UUID) []findingRow {
	t.Helper()

	rows, err := conn.Query(t.Context(), `
		select f.detected, f.severity::text, f.proposed_action,
		       coalesce(f.regulatory_obligation, ''), coalesce(f.citation_url, ''),
		       coalesce(f.supporting_context, ''), f.effort_estimate::text,
		       f.action_type,
		       jsonb_build_object(
		         'signal_kind', f.metadata -> 'signal_kind',
		         'signal_dedup_key', f.metadata -> 'signal_dedup_key',
		         'signal_metadata',
		           (f.metadata -> 'signal_metadata') - 'dsar_id' - 'response_due_at'
		       )::text
		  from findings f
		 where f.profile_id = $1
		 order by f.detected, f.severity
	`, profile)
	if err != nil {
		t.Fatalf("reading findings: %v", err)
	}
	defer rows.Close()

	var out []findingRow
	for rows.Next() {
		var f findingRow
		if err := rows.Scan(&f.Detected, &f.Severity, &f.Action, &f.Regulation,
			&f.URL, &f.Context, &f.Effort, &f.ActionType, &f.Metadata); err != nil {
			t.Fatalf("scanning a finding: %v", err)
		}
		f.Metadata = maskDSARKeyInJSON(f.Metadata)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading findings: %v", err)
	}
	slices.SortFunc(out, func(a, b findingRow) int {
		return cmp.Or(
			cmp.Compare(a.Detected, b.Detected),
			cmp.Compare(a.Severity, b.Severity),
			cmp.Compare(a.Metadata, b.Metadata),
		)
	})
	return out
}

// The fixture set. Each case is one shape of organisation, seeded twice.
var sweepFixtures = []struct {
	name           string
	hasDPO         string
	hasROPA        string
	aiSystems      []string
	dataCategories []string
	dsars          []dsarFixture
	// wantSignals guards against a case that produces nothing and therefore
	// compares nothing. A test that would pass with both sides empty is the
	// shape ENT-259 was filed to stop shipping.
	wantSignals bool
}{
	{
		name:        "gaps, from a profile with no controls in place",
		hasDPO:      "no",
		hasROPA:     "no",
		wantSignals: true,
	},
	{
		name:    "gaps against an organisation operating AI",
		hasDPO:  "no",
		hasROPA: "no",
		// Turns on the AI Act obligations, whose role gate is "deployer or
		// provider", and makes `ai_register` unsatisfied.
		aiSystems:   []string{"a support triage model"},
		wantSignals: true,
	},
	{
		name:           "sensitive data, which the Analyst's severity reads",
		hasDPO:         "no",
		hasROPA:        "no",
		dataCategories: []string{"customer health records", "names"},
		wantSignals:    true,
	},
	{
		name:        "a request inside the deadline window but outside escalation",
		hasDPO:      "yes",
		hasROPA:     "yes",
		dsars:       []dsarFixture{{subject: strptr("Ada Lovelace"), dueDays: 20}},
		wantSignals: true,
	},
	{
		name:        "a request with no subject name",
		hasDPO:      "yes",
		hasROPA:     "yes",
		dsars:       []dsarFixture{{dueDays: 20}},
		wantSignals: true,
	},
	{
		name:    "a request inside the escalation window, which overwrites the first signal",
		hasDPO:  "yes",
		hasROPA: "yes",
		dsars:   []dsarFixture{{subject: strptr("Grace Hopper"), dueDays: 3}},
		// The severity here is the property the detector order buys. Both
		// implementations must land on critical, not medium.
		wantSignals: true,
	},
	{
		name:        "an overdue request",
		hasDPO:      "yes",
		hasROPA:     "yes",
		dsars:       []dsarFixture{{subject: strptr("Alan Turing"), dueDays: -5}},
		wantSignals: true,
	},
	{
		name:    "several requests at once",
		hasDPO:  "no",
		hasROPA: "no",
		dsars: []dsarFixture{
			{subject: strptr("Ada Lovelace"), dueDays: 25},
			{dueDays: 2},
			{subject: strptr("Alan Turing"), dueDays: -12},
		},
		wantSignals: true,
	},
	{
		// Nothing to raise. Compared anyway, because "both produced nothing"
		// is only meaningful beside the cases above that produce plenty.
		name:        "a profile with everything in place and nothing outstanding",
		hasDPO:      "yes",
		hasROPA:     "yes",
		wantSignals: false,
	},
}

func TestTheGoSweepAgreesWithThePlpgsqlItReplaces(t *testing.T) {
	migrator := equivalenceConn(t, "migrator")
	agent := equivalenceConn(t, "agent")

	store := agentStoreForEquivalence(t)

	for _, tc := range sweepFixtures {
		t.Run(tc.name, func(t *testing.T) {
			legacy := seedSweepOrg(t, migrator, "legacy", tc.hasDPO, tc.hasROPA,
				tc.aiSystems, tc.dataCategories, tc.dsars)
			ported := seedSweepOrg(t, migrator, "ported", tc.hasDPO, tc.hasROPA,
				tc.aiSystems, tc.dataCategories, tc.dsars)

			// The plpgsql, as the agent, exactly as agent-role.test.ts calls it.
			pointAgentAt(t, agent, legacy.orgID)
			var swept, converted int
			if err := agent.QueryRow(t.Context(),
				`select public.run_watcher()`).Scan(&swept); err != nil {
				t.Fatalf("running the plpgsql watcher: %v", err)
			}
			if err := agent.QueryRow(t.Context(),
				`select public.run_analyst()`).Scan(&converted); err != nil {
				t.Fatalf("running the plpgsql analyst: %v", err)
			}
			if swept != 1 {
				t.Fatalf("the plpgsql swept %d profiles, want 1", swept)
			}

			// The Go path, on the producer pool, through the RPC's own entry
			// point rather than the internals.
			result, err := store.RunSweep(t.Context(), ported.orgID.String(), false)
			if err != nil {
				t.Fatalf("running the Go sweep: %v", err)
			}

			legacySignals := readSignals(t, migrator, legacy.profile)
			portedSignals := readSignals(t, migrator, ported.profile)

			if tc.wantSignals && len(legacySignals) == 0 {
				t.Fatal("the fixture produced no signals at all, so this case " +
					"compares nothing; fix the fixture rather than the expectation")
			}
			compare(t, "signal", legacySignals, portedSignals)

			legacyFindings := readFindings(t, migrator, legacy.profile)
			portedFindings := readFindings(t, migrator, ported.profile)
			compare(t, "finding", legacyFindings, portedFindings)

			// THE COUNTS ARE NOT COMPARED TO THE PLPGSQL, AND THAT IS THE ONE
			// DELIBERATE DIVERGENCE.
			//
			// `run_watcher()` and `run_analyst()` both returned the number of
			// profiles they walked, which under the agent's GUC is always 1. So
			// `converted` here is 1 for every case above, including the one
			// that produced nothing at all. The Go path returns what the proto
			// fields are named after, so these are compared to the rows that
			// were actually written instead.
			if converted != 1 {
				t.Errorf("the plpgsql analyst reported %d; it counts profiles, "+
					"so this should be 1 and the premise of the check below "+
					"has changed", converted)
			}
			if int(result.Signals) != len(portedSignals) {
				t.Errorf("signals reported: %d, signals written: %d",
					result.Signals, len(portedSignals))
			}
			if int(result.Findings) != len(portedFindings) {
				t.Errorf("findings reported: %d, findings written: %d",
					result.Findings, len(portedFindings))
			}
		})
	}
}

// compare reports every difference rather than the first, because a divergence
// is usually a rule that shifted and shows up in several rows at once.
func compare[T comparable](t *testing.T, what string, legacy, ported []T) {
	t.Helper()

	if len(legacy) != len(ported) {
		t.Errorf("%s count: the plpgsql produced %d, Go produced %d\n"+
			"plpgsql: %+v\nGo:      %+v", what, len(legacy), len(ported), legacy, ported)
		return
	}
	for i := range legacy {
		if legacy[i] != ported[i] {
			t.Errorf("%s %d differs:\nplpgsql: %+v\nGo:      %+v",
				what, i, legacy[i], ported[i])
		}
	}
}

// The Analyst's citation rendering agrees with the plpgsql over the whole
// corpus.
//
// The obligation page and the finding both read `corpus.Citation`'s Label and
// URL now, so a divergence between the two is structurally impossible. What is
// not impossible is the pair of them agreeing with each other and disagreeing
// with every label already stored on a customer's findings, which is what this
// compares against.
//
// Read-only, and over the real corpus rather than fixtures, because the
// interesting inputs are the ones a curator actually wrote.
func TestTheGoCitationRendererAgreesWithThePlpgsql(t *testing.T) {
	conn := equivalenceConn(t, "migrator")

	rows, err := conn.Query(t.Context(), `
		select slug, citation_kind, citation_celex,
		       coalesce(citation_article, 0), coalesce(citation_recital, 0),
		       coalesce(citation_annex, ''), coalesce(citation_paragraph, ''),
		       coalesce(public.analyst_citation_label(
		         citation_celex, citation_kind, citation_article,
		         citation_recital, citation_annex, citation_paragraph), ''),
		       coalesce(public.analyst_citation_url(
		         citation_celex, citation_kind, citation_article,
		         citation_recital, citation_annex), '')
		  from obligations
		 order by slug
	`)
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		var slug, wantLabel, wantURL string
		var citation citationParts
		if err := rows.Scan(&slug, &citation.kind, &citation.celex,
			&citation.article, &citation.recital, &citation.annex,
			&citation.paragraph, &wantLabel, &wantURL); err != nil {
			t.Fatalf("scanning an obligation: %v", err)
		}

		if got := citation.toCorpus().Label(); got != wantLabel {
			t.Errorf("%s label: the plpgsql renders %q, Go renders %q",
				slug, wantLabel, got)
		}
		if got := citation.toCorpus().URL(); got != wantURL {
			t.Errorf("%s url: the plpgsql renders %q, Go renders %q",
				slug, wantURL, got)
		}
		checked++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}

	// A corpus that has not been ingested would make this pass over nothing.
	if checked == 0 {
		t.Fatal("no obligations in the corpus, so this compared nothing; " +
			"the stack needs the corpus ingested (see corpus_drift_test.go)")
	}
}

func pointAgentAt(t *testing.T, conn *pgx.Conn, org uuid.UUID) {
	t.Helper()
	// No user GUC. A sweep is started by the system and there is no member to
	// name, which is what the agent's policies expect.
	if _, err := conn.Exec(t.Context(),
		`select set_config('app.current_org_id', $1, false)`, org.String()); err != nil {
		t.Fatalf("pointing the agent at %s: %v", org, err)
	}
}

func agentStoreForEquivalence(t *testing.T) *AgentStore {
	t.Helper()

	dsn := stackenv.DSN("agent")
	store, err := NewAgent(t.Context(), dsn)
	if err != nil {
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: "+
				"%s unreachable (%v)", dsn, err)
		}
		t.Skipf("agent pool not reachable at %s (%v)", dsn, err)
	}
	t.Cleanup(store.Close)
	return store
}

// maskDSARKey replaces the uuid inside a `dsar:{id}` key, which differs between
// the two organisations by construction.
func maskDSARKey(key string) string {
	if len(key) == len("dsar:")+36 && key[:5] == "dsar:" {
		return "dsar:{id}"
	}
	return key
}

func maskDSARKeyInJSON(metadata string) string {
	for i := 0; i+41 <= len(metadata); i++ {
		if metadata[i:i+5] != "dsar:" {
			continue
		}
		if _, err := uuid.Parse(metadata[i+5 : i+41]); err == nil {
			return metadata[:i] + "dsar:{id}" + maskDSARKeyInJSON(metadata[i+41:])
		}
	}
	return metadata
}

func strptr(s string) *string { return &s }

// citationParts is the row shape, kept out of the loop so the scan reads in
// column order.
type citationParts struct {
	kind      string
	celex     string
	article   int
	recital   int
	annex     string
	paragraph string
}

func (c citationParts) toCorpus() corpus.Citation {
	return corpus.Citation{
		Kind:           c.kind,
		Celex:          c.celex,
		ArticleNumber:  c.article,
		RecitalNumber:  c.recital,
		AnnexLabel:     c.annex,
		ParagraphLabel: c.paragraph,
	}
}
