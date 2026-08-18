package postgres

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
)

// The applicability vocabulary declared in Go is the one the Watcher evaluates
// (ENT-233).
//
// # WHY THIS TEST IS DB-BACKED WHEN THE VOCABULARY IS PURE GO
//
// `domain/corpus/applieswhen.go` declares which gap tokens and thresholds the
// Watcher reads. It cannot check that claim: the evaluator is two plpgsql
// functions in 00001, in a different language, and a declaration that merely
// asserts what another file does is the same arrangement that produced the
// drift this issue is about. So the guard calls the functions.
//
// This is the check that would have caught the existing drift on the day it was
// introduced, which is the standard AGENTS.md sets: a test that cannot fail is
// worse than no test, so each case below was watched going red by changing the
// expectation before it was committed.
//
// # WHEN ENT-225 MOVES THE EVALUATOR INTO GO
//
// These functions are decisions, not invariants, by db/README.md's test: a
// second process that did not know them would make a different product
// decision about who an obligation binds, not write wrong data. ENT-225 owns
// the move. When it happens this file should keep asserting the same
// properties against the Go evaluator and stop needing a database, and the
// vocabulary declaration is what the move inherits rather than rediscovers.

// vocabularyConn opens a connection for the vocabulary probes.
//
// The functions are IMMUTABLE and read no tables, so any role may call them and
// the migrator is used only because it is the connection every db-backed test
// in this package already has a DSN for. Skips on a laptop and fails in CI,
// exactly as `ingestStore` does, because a self-skipping guard that reports
// green while testing nothing is how a drift check stops covering anything.
func vocabularyConn(t *testing.T) *pgx.Conn {
	t.Helper()

	dsn := os.Getenv("PG_MIGRATOR_URL")
	if dsn == "" {
		dsn = "postgres://kindlast_migrator:migrator-dev-password@127.0.0.1:5433/kindlast"
	}

	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		if os.Getenv("KINDLAST_REQUIRE_STACK") != "" {
			t.Fatalf("KINDLAST_REQUIRE_STACK is set, so this must not skip: %s unreachable (%v)", dsn, err)
		}
		t.Skipf("compose stack not reachable at %s (%v); "+
			"run: docker compose -f deploy/compose.yaml up -d", dsn, err)
	}
	t.Cleanup(func() { _ = conn.Close(t.Context()) })
	return conn
}

// gapSatisfied calls `watcher_gap_satisfied` against a profile built from JSON.
//
// `jsonb_populate_record` rather than a `row(...)` literal on purpose: a
// positional composite would silently re-map every field the day somebody adds
// a column to `compliance_profiles`, and this test would go on passing while
// asking a different question.
func gapSatisfied(t *testing.T, conn *pgx.Conn, token, profile string) bool {
	t.Helper()

	var satisfied bool
	err := conn.QueryRow(t.Context(), `
		select public.watcher_gap_satisfied(
			$1, jsonb_populate_record(null::public.compliance_profiles, $2::jsonb))
	`, token, profile).Scan(&satisfied)
	if err != nil {
		t.Fatalf("calling watcher_gap_satisfied(%q): %v", token, err)
	}
	return satisfied
}

func obligationApplies(t *testing.T, conn *pgx.Conn, appliesWhen, profile string) bool {
	t.Helper()

	var applies bool
	err := conn.QueryRow(t.Context(), `
		select public.watcher_obligation_applies(
			$1::jsonb, jsonb_populate_record(null::public.compliance_profiles, $2::jsonb))
	`, appliesWhen, profile).Scan(&applies)
	if err != nil {
		t.Fatalf("calling watcher_obligation_applies(%s): %v", appliesWhen, err)
	}
	return applies
}

// Every gap token the corpus may use is one the Watcher can answer.
//
// This is the headline guard. `watcher_gap_satisfied` returns TRUE for a token
// it does not recognise, and satisfied means no gap, which means no finding. So
// a token that is in the Go vocabulary but not in the plpgsql `case` produces
// an obligation that ingests, reports as applying, and never fires. Nothing
// else in the system goes red for that.
//
// Each token is probed with a profile in which the gap is genuinely open, so an
// honest evaluator must answer false. An unrecognised token answers true and
// fails here.
func TestEveryGapTokenInTheVocabularyIsOneTheWatcherEvaluates(t *testing.T) {
	conn := vocabularyConn(t)

	// A profile with the gap open, per token. Written out rather than derived,
	// because the point is to state what "this organisation has not done it"
	// looks like for each one.
	openGap := map[string]string{
		"ropa":                `{"has_ropa":"no"}`,
		"dpo":                 `{"has_dpo":"no"}`,
		"ai_register":         `{"ai_systems":["a recommender"]}`,
		"transfer_safeguards": `{"transfer_destinations":[]}`,
	}

	for _, token := range corpus.GapTokens() {
		profile, ok := openGap[token]
		if !ok {
			t.Errorf("gap token %q is in the vocabulary but this test has no profile that "+
				"leaves its gap open, so nothing here proves the Watcher evaluates it", token)
			continue
		}

		if gapSatisfied(t, conn, token, profile) {
			t.Errorf("watcher_gap_satisfied(%q) answered satisfied for a profile with the gap "+
				"open. Either the function does not know the token (its `case` falls through "+
				"to `return true`, which raises no finding, for ever) or the profile above no "+
				"longer describes an open gap", token)
		}
	}
}

// The other half: a token nobody evaluates answers "satisfied".
//
// Asserting the failure mode rather than describing it, so the comment in
// applieswhen.go about silence is checkable rather than folklore. If this ever
// starts failing, the function grew a stricter default and the Go vocabulary
// can drop its no-unevaluated-tier rule.
func TestAnUnknownGapTokenIsSilentlyTreatedAsSatisfied(t *testing.T) {
	conn := vocabularyConn(t)

	if !gapSatisfied(t, conn, "soc2_access_review", `{"has_ropa":"no","has_dpo":"no"}`) {
		t.Fatal("an unknown gap token no longer answers satisfied; the silent-miss failure " +
			"mode this vocabulary guards against has changed, so update applieswhen.go")
	}
}

// The thresholds the vocabulary claims are evaluated, are.
func TestTheEvaluatedThresholdsNarrowApplicability(t *testing.T) {
	conn := vocabularyConn(t)

	for _, tc := range []struct {
		name        string
		appliesWhen string
		profile     string
	}{
		{
			"cross_border_transfers",
			`{"thresholds":{"cross_border_transfers":true}}`,
			`{"transfers_outside_eu":"no"}`,
		},
		{
			"employees_min",
			`{"thresholds":{"employees_min":250}}`,
			`{"staff_count":12}`,
		},
		{
			"engages_processor",
			`{"engages_processor":true}`,
			`{"vendor_list":""}`,
		},
		{
			"role deployer against an organisation with no AI",
			`{"role":"deployer"}`,
			`{"ai_systems":[]}`,
		},
		{
			"role provider against an organisation with no AI",
			`{"role":"provider"}`,
			`{"ai_systems":[]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if obligationApplies(t, conn, tc.appliesWhen, tc.profile) {
				t.Errorf("%s did not narrow applicability; the vocabulary marks it evaluated, "+
					"so either the function stopped reading it or the vocabulary is wrong", tc.name)
			}
		})
	}
}

// The known drift, asserted against the running function rather than trusted.
//
// Each of these is a condition the curator wrote and the Watcher does not read.
// The obligation therefore applies to organisations it was never meant to bind:
// Article 35's DPIA reaches every controller rather than high-risk processing,
// and Article 37's DPO duty reaches every controller rather than those doing
// large-scale monitoring.
//
// That over-reports, which is visible and dismissible, unlike the gap-token
// case above. It is still wrong, and pinning it here means the day somebody
// implements one of these the test says so instead of the drift silently
// changing direction.
func TestTheUnevaluatedKeysDoNotNarrowApplicability(t *testing.T) {
	conn := vocabularyConn(t)

	// The profile is deliberately the most exempt one that can be written: no
	// AI, no transfers, no vendors, one member of staff. If a condition still
	// does not narrow against this, nothing narrows it.
	exempt := `{"ai_systems":[],"transfers_outside_eu":"no","vendor_list":"","staff_count":1,` +
		`"transfer_destinations":[],"has_ropa":"no","has_dpo":"no"}`

	for _, tc := range []struct{ key, appliesWhen string }{
		{"high_risk", `{"thresholds":{"high_risk":true}}`},
		{"large_scale_monitoring", `{"thresholds":{"large_scale_monitoring":true}}`},
		{"lawful_basis_includes", `{"lawful_basis_includes":"consent"}`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if !obligationApplies(t, conn, tc.appliesWhen, exempt) {
				t.Errorf("%q now narrows applicability, so the Watcher has learned to read it. "+
					"Mark it evaluated in applieswhen.go, drop it from UnevaluatedKeys, and "+
					"update docs/regulation-packs.md", tc.key)
			}
		})
	}
}

// UnevaluatedKeys is the whole list, not a sample.
//
// Guards the case where somebody adds a fourth unevaluated key and this file
// keeps passing because it only ever probes the three it knows about.
func TestTheUnevaluatedKeyListMatchesWhatThisFileProbes(t *testing.T) {
	probed := map[string]bool{
		"high_risk":              true,
		"large_scale_monitoring": true,
		"lawful_basis_includes":  true,
	}

	for _, key := range corpus.UnevaluatedKeys() {
		if !probed[key] {
			t.Errorf("%q is declared unevaluated but nothing here probes it, so its direction "+
				"of failure is undocumented. Add a case above", key)
		}
	}
}
