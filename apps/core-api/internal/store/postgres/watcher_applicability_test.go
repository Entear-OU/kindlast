package postgres

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/corpuspack"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// An obligation reaches the organisations it says it reaches, and no others
// (ENT-246).
//
// # WHY THESE RUN THE SWEEP RATHER THAN THE PREDICATE
//
// `corpus_vocabulary_test.go` asks `watcher_obligation_applies` directly, which
// is the right shape for a vocabulary guard and is not enough for this one. The
// claim being fixed is about what a customer sees in their feed, and between
// the predicate and the feed sit two detectors, an emit function and a dedup
// key. A predicate that narrows correctly inside a detector that never consults
// it would pass a predicate test and ship the bug.
//
// So each case below builds an organisation, gives it (or withholds) the facts
// the obligation narrows on, runs the real `run_watcher_for_profile`, and reads
// the findings back.
//
// # EVERYTHING RUNS IN ONE TRANSACTION THAT IS ROLLED BACK
//
// The fixtures include an update to a corpus row, which is global rather than
// tenant-scoped, and the compose stack is shared. A committed fixture would be
// visible to every other session on the machine for as long as the test took.
// Nothing here commits.
//
// The connection is the migrator's, which holds BYPASSRLS, so the fixtures can
// be written without inventing a member to write them as. That is a fixture
// convenience and not a claim about the producer's reach: the sweep runs as
// `kindlast_agent`, whose ability to read the facts these obligations now
// depend on is asserted separately below.

// applicabilityFixture is one organisation, its legacy profile and its facts.
type applicabilityFixture struct {
	tx      pgx.Tx
	orgID   uuid.UUID
	profile uuid.UUID
}

// newApplicabilityFixture opens the transaction and creates the organisation.
//
// `aiSystems` feeds the legacy profile column, which the role gate reads: the
// AI Act obligations bind a deployer or a provider, and an organisation with no
// AI systems is neither.
func newApplicabilityFixture(t *testing.T, aiSystems []string) *applicabilityFixture {
	t.Helper()

	conn := vocabularyConn(t)
	tx, err := conn.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the fixture transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(t.Context()) })

	f := &applicabilityFixture{tx: tx, orgID: uuid.New(), profile: uuid.New()}

	if _, err := tx.Exec(t.Context(),
		`insert into organisations (id, slug, name) values ($1, $2, $3)`,
		f.orgID, "ent-246-"+f.orgID.String()[:8], "Applicability test"); err != nil {
		t.Fatalf("seeding the organisation: %v", err)
	}

	// The profile hangs off an onboarding session, which is the legacy shape:
	// a profile was something a session produced.
	var session uuid.UUID
	if err := tx.QueryRow(t.Context(),
		`insert into onboarding_sessions (org_id) values ($1) returning id`,
		f.orgID).Scan(&session); err != nil {
		t.Fatalf("seeding the onboarding session: %v", err)
	}

	// The most exempt profile that can be written, so that anything which does
	// reach it reaches it because of the facts rather than because of a
	// leftover column. One member of staff, no transfers, no vendors, no DPO
	// and no record of processing activities.
	if aiSystems == nil {
		aiSystems = []string{}
	}
	if _, err := tx.Exec(t.Context(), `
		insert into compliance_profiles (
			id, org_id, session_id, industry, has_dpo, has_ropa,
			transfers_outside_eu, ai_systems, vendor_list, staff_count)
		values ($1, $2, $3, 'bakery', 'no', 'no', 'no', $4, '', 1)`,
		f.profile, f.orgID, session, aiSystems); err != nil {
		t.Fatalf("seeding the compliance profile: %v", err)
	}

	return f
}

// believe records one current profile fact.
func (f *applicabilityFixture) believe(t *testing.T, key, valueJSON string) {
	t.Helper()

	// Validated on the way in, so a test cannot assert applicability from a
	// value the product would have refused to store.
	if err := memory.ValidateValue(key, valueJSON); err != nil {
		t.Fatalf("the fixture fact is not one the product would store: %v", err)
	}

	if _, err := f.tx.Exec(t.Context(), `
		insert into org_profile_facts (org_id, key, value, source)
		values ($1, $2, $3::jsonb, 'human')`, f.orgID, key, valueJSON); err != nil {
		t.Fatalf("recording the fact %q: %v", key, err)
	}
}

// due moves one obligation's effective date into the deadline detector's
// 30-day window.
//
// `gdpr-art-35-dpia` came into force in 2018, so the detector that would carry
// it into a feed never considers it as the corpus stands. Moving the date is
// how this test reaches the emit path for an obligation whose applicability is
// the thing under test; nothing about the applicability decision depends on the
// date, and the gap-path case below needs no such help.
func (f *applicabilityFixture) due(t *testing.T, slug string) {
	t.Helper()

	tag, err := f.tx.Exec(t.Context(), `
		update obligations set effective_date = current_date + 7 where slug = $1`, slug)
	if err != nil {
		t.Fatalf("moving %s into the deadline window: %v", slug, err)
	}
	if tag.RowsAffected() == 0 {
		t.Fatalf("the corpus has no obligation %q; the stack needs the corpus ingested "+
			"(see corpus_drift_test.go)", slug)
	}
}

// curate sets one obligation's applicability to what `data/corpus/` says,
// inside the fixture transaction, and returns it.
//
// # WHY THE TEST WRITES THE CORPUS ROW RATHER THAN TRUSTING IT
//
// The obligations table is global rather than tenant-scoped, and every
// developer, every branch and every parallel agent shares one compose stack. It
// is written by `corpus_drift_test.go`, which ingests whatever
// `data/corpus/obligations.json` says on the branch that ran last. So the
// stored applicability is not a stable input: a run from another checkout can
// leave this one asserting against a condition that is not the one it names.
//
// Reading the curated file and writing it into the transaction makes the test
// say what it means. The claim under test is "the obligation as the curator
// wrote it reaches the right organisations", and the curator's text is the
// file.
func (f *applicabilityFixture) curate(t *testing.T, slug, mustNarrowOn string) {
	t.Helper()

	appliesWhen := curatedAppliesWhen(t, slug)

	// The corpus is allowed to change, and dropping the condition would be one
	// legitimate way to resolve an unevaluated token. It is not the way this
	// test assumes, so it says so rather than passing vacuously.
	if !strings.Contains(appliesWhen, mustNarrowOn) {
		t.Fatalf("%s no longer narrows on %q (it says %s), so this test is not exercising "+
			"the condition it names", slug, mustNarrowOn, appliesWhen)
	}

	tag, err := f.tx.Exec(t.Context(),
		`update obligations set applies_when = $1::jsonb where slug = $2`, appliesWhen, slug)
	if err != nil {
		t.Fatalf("writing %s's applicability: %v", slug, err)
	}
	if tag.RowsAffected() == 0 {
		t.Fatalf("the corpus has no obligation %q in this database; run the ingest in "+
			"corpus_drift_test.go first", slug)
	}
}

// curatedAppliesWhen finds one obligation in the repository's corpus.
func curatedAppliesWhen(t *testing.T, slug string) string {
	t.Helper()

	packs, err := corpuspack.All(corpusDir(t))
	if err != nil {
		t.Fatalf("loading the corpus: %v", err)
	}
	for _, pack := range packs {
		for _, obligation := range pack.Obligations {
			if obligation.Slug == slug {
				return obligation.AppliesWhenJSON
			}
		}
	}
	t.Fatalf("data/corpus/ has no obligation %q", slug)
	return ""
}

// sweep runs the Watcher and returns the obligation slugs it raised.
func (f *applicabilityFixture) sweep(t *testing.T) map[string]bool {
	t.Helper()

	if _, err := f.tx.Exec(t.Context(),
		`select public.run_watcher_for_profile($1)`, f.profile); err != nil {
		t.Fatalf("running the watcher: %v", err)
	}

	rows, err := f.tx.Query(t.Context(), `
		select coalesce(obligation_slug, '')
		  from watcher_findings where profile_id = $1`, f.profile)
	if err != nil {
		t.Fatalf("reading the findings: %v", err)
	}
	defer rows.Close()

	raised := map[string]bool{}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			t.Fatalf("scanning a finding: %v", err)
		}
		raised[slug] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the findings: %v", err)
	}
	return raised
}

// applies asks the predicate directly, for the cases where the question is the
// decision rather than the feed.
func (f *applicabilityFixture) applies(t *testing.T, appliesWhen string) bool {
	t.Helper()

	var out bool
	// `p.*` expands the row into the composite argument, which is how the
	// detectors call it too.
	if err := f.tx.QueryRow(t.Context(), `
		select public.watcher_obligation_applies($1::jsonb, p.*)
		  from compliance_profiles p where p.id = $2`,
		appliesWhen, f.profile).Scan(&out); err != nil {
		t.Fatalf("asking whether %s applies: %v", appliesWhen, err)
	}
	return out
}

const (
	dpia              = "gdpr-art-35-dpia"
	breachNotify      = "gdpr-art-33-breach-notification"
	dpoAppointment    = "gdpr-art-37-dpo-appointment"
	consentConditions = "gdpr-art-7-consent-conditions"
	deployerDuties    = "ai-act-art-26-deployer-obligations"
	highRiskFact      = "high_risk_processing"
	monitoringFact    = "large_scale_monitoring"
	unconditionally   = "an obligation with no conditions at all"
)

// THE HEADLINE CASE. A controller doing no high-risk processing is not told it
// owes a Data Protection Impact Assessment.
//
// Article 35 binds a controller "where a type of processing is likely to result
// in a high risk to the rights and freedoms of natural persons". Before
// ENT-246 the Watcher read no such condition, so the obligation reached every
// controller: a five-person bakery was told to commission an assessment that
// costs weeks and a consultant.
//
// The `no` case and the never-answered case are both here, because they fail
// differently. `no` is an organisation that told us; silence is one nobody
// asked, and the argument for treating silence as "does not apply" is in
// 00023's header.
func TestTheDPIAObligationDoesNotReachAControllerWithNoHighRiskProcessing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		facts map[string]string
	}{
		{"the organisation says it does no high-risk processing", map[string]string{
			memory.KeyHighRiskProcessing: `"no"`,
		}},
		{"nobody has ever asked the organisation", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newApplicabilityFixture(t, nil)
			for key, value := range tc.facts {
				f.believe(t, key, value)
			}
			f.curate(t, dpia, highRiskFact)
			f.due(t, dpia)

			// The vacuity guard. An unconditional obligation in the same window
			// must still be raised, or "no DPIA finding" would be satisfied by
			// a sweep that raised nothing for any reason.
			f.due(t, breachNotify)

			raised := f.sweep(t)

			if raised[dpia] {
				t.Errorf("the DPIA obligation reached an organisation with no high-risk "+
					"processing. That is a fabricated obligation, and an expensive one: "+
					"%s narrows on %s and the Watcher is not reading it", dpia, highRiskFact)
			}
			if !raised[breachNotify] {
				t.Fatalf("%s (%s) was not raised either, so this sweep proves nothing about "+
					"the DPIA. The fixture or the detector is broken, not the applicability",
					breachNotify, unconditionally)
			}
		})
	}
}

// The other half, without which the fix would be indistinguishable from
// switching the obligation off.
//
// `unsure` is here on purpose and is not a rounding of `yes`. ENT-228 kept
// `unsure` as its own answer because "we asked and they did not know" is a
// different claim from "they said no", and an organisation that does not know
// whether its processing is high-risk has not done the Article 35(1) screening
// that the obligation exists for.
func TestTheDPIAObligationReachesAControllerDoingHighRiskProcessing(t *testing.T) {
	for _, answer := range []string{`"yes"`, `"unsure"`} {
		t.Run(answer, func(t *testing.T) {
			f := newApplicabilityFixture(t, nil)
			f.believe(t, memory.KeyHighRiskProcessing, answer)
			f.curate(t, dpia, highRiskFact)
			f.due(t, dpia)

			if raised := f.sweep(t); !raised[dpia] {
				t.Errorf("the DPIA obligation did not reach an organisation whose "+
					"high_risk_processing fact is %s. Article 35 is exactly the obligation "+
					"this organisation owes, and hiding it is the mirror of the bug ENT-246 "+
					"fixed", answer)
			}
		})
	}
}

// The same property through the other detector, which needs no help from a
// date.
//
// `gdpr-art-37-dpo-appointment` narrows on large-scale monitoring AND requires
// the `dpo` control, so it is the one obligation of the four that raised a
// finding for every controller in the ordinary course. That was the
// customer-visible half of this bug: a profile with no DPO was told to
// designate one whether or not Article 37(1)(b) applied to it.
func TestTheDPODutyFollowsLargeScaleMonitoringRatherThanEveryController(t *testing.T) {
	// SUBTESTS RATHER THAN TWO FIXTURES IN ONE FUNCTION, and the reason is a
	// deadlock rather than style. Each fixture holds its transaction open until
	// its cleanup runs, and `curate` takes a row lock on the obligation; two
	// fixtures alive at once in the same function wait on each other for ever.
	t.Run("no large-scale monitoring", func(t *testing.T) {
		f := newApplicabilityFixture(t, nil)
		f.curate(t, dpoAppointment, monitoringFact)

		if raised := f.sweep(t); raised[dpoAppointment] {
			t.Error("an organisation that does no large-scale monitoring was told to " +
				"designate a Data Protection Officer")
		}
	})

	t.Run("large-scale monitoring and no DPO", func(t *testing.T) {
		f := newApplicabilityFixture(t, nil)
		f.curate(t, dpoAppointment, monitoringFact)
		f.believe(t, memory.KeyLargeScaleMonitoring, `"yes"`)

		if raised := f.sweep(t); !raised[dpoAppointment] {
			t.Error("an organisation that monitors data subjects on a large scale and has " +
				"no DPO was not told to designate one, so the fix reads as switching the " +
				"obligation off rather than as narrowing it")
		}
	})
}

// Article 7 binds where consent is among the bases relied on, and not
// otherwise.
//
// Containment rather than equality, because a controller relies on several
// bases at once and the obligation attaches to one of them.
func TestConsentConditionsFollowTheRecordedLawfulBases(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bases   string
		applies bool
	}{
		{"consent among several", `["contract","consent"]`, true},
		{"no consent anywhere", `["contract","legitimate_interests"]`, false},
		{"never recorded", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newApplicabilityFixture(t, nil)
			f.curate(t, consentConditions, "lawful_basis_includes")
			if tc.bases != "" {
				f.believe(t, memory.KeyLawfulBases, tc.bases)
			}

			applies := f.applies(t, curatedAppliesWhen(t, consentConditions))
			if applies != tc.applies {
				t.Errorf("Article 7 applies = %v with bases %q, want %v",
					applies, tc.bases, tc.applies)
			}
		})
	}
}

// The AI Act's high-risk question is not the GDPR's, and one answer does not
// stand for both.
//
// `thresholds.high_risk` was written by two GDPR obligations and two AI Act
// obligations. Article 35 asks whether the PROCESSING is likely to result in a
// high risk to people; Annex III asks whether an AI SYSTEM falls in one of
// eight use-case areas. Sharing one fact would have meant a controller's answer
// about profiling deciding whether the AI Act bound it.
func TestTheTwoHighRiskQuestionsAreAnsweredSeparately(t *testing.T) {
	// A deployer, so the role gate lets the AI Act obligation through, and a
	// controller that has said yes to the GDPR question and nothing about the
	// AI one.
	f := newApplicabilityFixture(t, []string{"a hiring recommender"})
	f.curate(t, dpia, highRiskFact)
	f.curate(t, deployerDuties, "high_risk_ai_system")
	f.believe(t, memory.KeyHighRiskProcessing, `"yes"`)

	applies := func(slug string) bool {
		t.Helper()
		return f.applies(t, curatedAppliesWhen(t, slug))
	}

	if !applies(dpia) {
		t.Error("the GDPR high-risk answer did not carry Article 35, which it should")
	}
	if applies(deployerDuties) {
		t.Error("a GDPR answer about high-risk processing pulled in an AI Act obligation " +
			"about high-risk systems; the two questions have been collapsed into one")
	}

	f.believe(t, memory.KeyHighRiskAISystem, `"yes"`)
	if !applies(deployerDuties) {
		t.Error("a deployer that operates a high-risk AI system was not bound by Article 26")
	}
}

// The producer can read the facts it now decides applicability from.
//
// Asserted as its own case because the tests above run as the migrator, which
// holds BYPASSRLS, and would pass with the producer's grant on
// `org_profile_facts` revoked. In production the sweep runs as
// `kindlast_agent`, and a missing grant surfaces there as a failed sweep rather
// than as a wrong answer.
//
// The organisation is a random uuid with no facts: the answer is false either
// way, and what is under test is that asking is permitted at all.
func TestTheProducerRoleMayReadTheFactsApplicabilityDependsOn(t *testing.T) {
	store := agentStore(t)

	var affirmed bool
	if err := store.pool.QueryRow(t.Context(),
		`select public.watcher_fact_affirms($1, $2)`,
		uuid.New(), memory.KeyHighRiskProcessing).Scan(&affirmed); err != nil {
		t.Fatalf("the producer cannot read org_profile_facts, so every sweep would fail "+
			"rather than every obligation applying: %v", err)
	}
	if affirmed {
		t.Fatal("an organisation with no facts affirmed one")
	}
}
