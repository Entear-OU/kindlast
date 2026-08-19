package postgres

import (
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/corpus"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/stackenv"
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
// `watcher_gap_satisfied` reads nothing but its arguments;
// `watcher_obligation_applies` reads `org_profile_facts` since ENT-246, which
// is why it is STABLE rather than IMMUTABLE. The migrator is used because it is
// the connection every db-backed test in this package already has a DSN for,
// and because it can write the fixtures. That the PRODUCER can read those facts
// is a different claim and is asserted separately, as its own test in
// `watcher_applicability_test.go`.
//
// Skips on a laptop and fails in CI, exactly as `ingestStore` does, because a
// self-skipping guard that reports green while testing nothing is how a drift
// check stops covering anything.
func vocabularyConn(t *testing.T) *pgx.Conn {
	t.Helper()

	dsn := stackenv.DSN("migrator")

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

// The conditions answered from the legacy profile row.
//
// `employees_min` used to be in this list and is not in the vocabulary at all
// any more (ENT-246): it was evaluated and no obligation used it, and the one
// it looks like it should serve is Article 30's ROPA, whose 250-employee
// exemption is too narrow to be a headcount test. The conditions answered from
// `org_profile_facts` are in the test below, because they need an organisation
// and a fact rather than a synthetic profile.
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

// Every threshold answered from a fact is read from THAT fact.
//
// This is the test that would have caught ENT-246 on the day the drift was
// introduced, and it is the one that keeps catching it. Two halves, and the
// second is what makes the first mean anything:
//
//   - With the fact unanswered the obligation must NOT apply. A threshold the
//     evaluator has never heard of narrows nothing, which is how Article 35's
//     DPIA came to reach every controller.
//   - With the fact answered `yes` it must apply. Without this half an
//     evaluator that returned false for everything would pass, and "we fixed
//     the over-reporting by switching the obligation off" is not a fix.
//
// Driven off `corpus.ThresholdFacts()` rather than a list written here, so a
// threshold added to the vocabulary is covered the moment it is declared.
func TestEveryFactBackedThresholdIsReadFromItsOwnFact(t *testing.T) {
	for _, pair := range corpus.ThresholdFacts() {
		threshold, fact := pair[0], pair[1]

		t.Run(threshold, func(t *testing.T) {
			// AI systems on the profile, so the role gate is never the reason
			// an answer comes back false.
			f := newApplicabilityFixture(t, []string{"a recommender"})
			appliesWhen := fmt.Sprintf(`{"thresholds":{%q:true}}`, threshold)

			if f.applies(t, appliesWhen) {
				t.Errorf("%q did not narrow applicability for an organisation that has "+
					"never answered it. Either the evaluator does not read the threshold, "+
					"or it reads a fact key other than %q", threshold, fact)
			}

			f.believe(t, fact, `"yes"`)

			if !f.applies(t, appliesWhen) {
				t.Errorf("%q still did not apply after the organisation answered yes to "+
					"%q. The vocabulary says the evaluator reads that fact and it does "+
					"not, so the obligation now applies to nobody", threshold, fact)
			}
		})
	}
}

// Every threshold in the vocabulary is probed by one of the two tests above.
//
// ENT-233's property, kept: a threshold cannot arrive without something here
// asking what it does. The fact-backed ones are covered by the loop above; the
// one answered from the legacy profile row is named here.
func TestEveryThresholdInTheVocabularyIsProbed(t *testing.T) {
	fromProfileRow := map[string]bool{"cross_border_transfers": true}

	fromFact := map[string]bool{}
	for _, pair := range corpus.ThresholdFacts() {
		fromFact[pair[0]] = true
	}

	for _, threshold := range corpus.ThresholdKeys() {
		if !fromFact[threshold] && !fromProfileRow[threshold] {
			t.Errorf("the threshold %q is in the vocabulary and nothing in this file asks "+
				"whether the Watcher reads it. Give it a fact and it joins the loop above, "+
				"or name it here with the profile column it is answered from", threshold)
		}
	}
}

// Nothing is declared without being evaluated (ENT-246).
//
// The domain package asserts the same thing without a database. It is repeated
// here because this file is where somebody looks when they are about to add a
// token, and because the tier it guards against is the one that shipped the
// bug.
func TestTheVocabularyHasNoUnevaluatedTier(t *testing.T) {
	if keys := corpus.UnevaluatedKeys(); len(keys) != 0 {
		t.Fatalf("these tokens are declared and not evaluated: %v", keys)
	}
}
