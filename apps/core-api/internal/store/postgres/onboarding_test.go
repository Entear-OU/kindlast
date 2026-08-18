package postgres

import (
	"encoding/json"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/onboarding"
)

// The first conversation, through the code path that will serve requests
// (ENT-212).
//
// Every test here runs as Bob in Beta, inside one transaction that is rolled
// back, which is also how a request runs. Beta rather than Alpha because Alpha
// carries a compliance profile in the shared development stack, and the
// properties below are about what happens to an organisation that has none.
//
// What these cover that the database suite cannot: the suite proves the
// constraints exist, and these prove the store obeys them. The failure mode for
// most of them is quiet. A confirmation that wrote facts and no projection
// leaves the console agreeing with the customer while the Watcher goes on
// reasoning from nothing, with no error anywhere.

func TestStartingOnboardingTwiceResumesRatherThanForking(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	first, created, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}
	if !created {
		t.Fatal("an organisation with no session reported that one already existed")
	}

	// The retry a person produces by refreshing, opening a second tab, or
	// losing a connection mid-request. Two sessions would mean two transcripts
	// and, at confirmation, two profiles.
	second, created, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding again: %v", err)
	}
	if created {
		t.Fatal("starting twice opened a second interview")
	}
	if second.ID != first.ID {
		t.Fatalf("resumed session %s, want %s", second.ID, first.ID)
	}
}

func TestAnAnswerKeepsBothTheWordsAndWhatTheyWereTakenToMean(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	session, _, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	if _, err := tenant.AppendOnboardingTurn(
		ctx, session.ID, onboarding.RoleAssistant, "Which countries?", memory.KeyEUJurisdictions, "",
	); err != nil {
		t.Fatalf("asking a question: %v", err)
	}
	if _, err := tenant.AppendOnboardingTurn(
		ctx, session.ID, onboarding.RoleUser, "Ireland, Spain", memory.KeyEUJurisdictions,
		`["Ireland","Spain"]`,
	); err != nil {
		t.Fatalf("recording an answer: %v", err)
	}

	transcript, err := tenant.OnboardingTranscript(ctx, session.ID)
	if err != nil {
		t.Fatalf("reading the transcript: %v", err)
	}
	if len(transcript) != 2 {
		t.Fatalf("the transcript has %d turns, want 2", len(transcript))
	}
	// Assigned by the database in the insert, so two tabs answering at once
	// collide rather than sharing a position.
	if transcript[0].Ordering != 0 || transcript[1].Ordering != 1 {
		t.Fatalf("ordering is %d then %d, want 0 then 1",
			transcript[0].Ordering, transcript[1].Ordering)
	}
	if transcript[1].Content != "Ireland, Spain" {
		t.Fatalf("the transcript lost what the person typed: %q", transcript[1].Content)
	}
	// Decoded rather than string-compared, because jsonb normalises on the way
	// out: what was written as `["Ireland","Spain"]` reads back as
	// `["Ireland", "Spain"]`. That is fine everywhere it matters, since the
	// comparison in CorrectFact is a jsonb one, and it is worth writing down
	// because a string equality here would have looked like a data bug.
	var stored []string
	if err := json.Unmarshal([]byte(transcript[1].ValueJSON), &stored); err != nil {
		t.Fatalf("the stored value is not the list it was written as: %v", err)
	}
	if len(stored) != 2 || stored[0] != "Ireland" || stored[1] != "Spain" {
		t.Fatalf("the stored value is %v", stored)
	}
	// Who said it, from the verified subject on the transaction rather than
	// from anything a caller sent.
	if transcript[1].CreatedBy != bobUser {
		t.Fatalf("the turn is attributed to %q, want %q", transcript[1].CreatedBy, bobUser)
	}
}

func TestTheProductCannotAnswerItsOwnQuestion(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	session, _, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	// The whole design rests on every value in the profile having come from a
	// person's typed answer. An assistant turn carrying a value would be the
	// product filling the profile in on the customer's behalf, so 00026 refuses
	// it in the database rather than in whichever handler happens to write it.
	if _, err := tenant.AppendOnboardingTurn(
		ctx, session.ID, onboarding.RoleAssistant, "Which countries?",
		memory.KeyEUJurisdictions, `["Ireland"]`,
	); err == nil {
		t.Fatal("an assistant turn was allowed to carry an answer")
	}
}

func TestConfirmingRecordsFactsSourcedToOnboardingAndAProfileTheWatcherReads(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	session, _, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	facts := map[string]string{
		memory.KeyIndustry:           `"a bakery"`,
		memory.KeyHasROPA:            `"no"`,
		memory.KeyVendorList:         `["Stripe","Hetzner"]`,
		memory.KeyStaffCount:         `7`,
		memory.KeyTransfersOutsideEU: `"yes"`,
	}
	profileID, err := tenant.ConfirmOnboarding(ctx, session.ID, facts)
	if err != nil {
		t.Fatalf("confirming: %v", err)
	}
	if profileID == "" {
		t.Fatal("confirming produced no profile")
	}

	stored, err := tenant.ProfileFacts(ctx)
	if err != nil {
		t.Fatalf("reading the profile facts: %v", err)
	}
	byKey := map[string]string{}
	for _, fact := range stored {
		byKey[fact.Key] = fact.ValueJSON
		// THE PROVENANCE IS THE POINT. A fact stamped `human` would be
		// indistinguishable from a later correction, and the memory page shows
		// this against every value.
		if fact.Source != "onboarding" {
			t.Errorf("fact %q is sourced to %q, want onboarding", fact.Key, fact.Source)
		}
		if fact.RecordedBy != bobUser {
			t.Errorf("fact %q is recorded by %q, want %q", fact.Key, fact.RecordedBy, bobUser)
		}
	}
	if byKey[memory.KeyIndustry] != `"a bakery"` {
		t.Fatalf("industry stored as %s", byKey[memory.KeyIndustry])
	}

	// And the projection, which is what `run_watcher()` actually reads.
	var industry, vendorList, hasDPO string
	var staff *int32
	if err := tenant.Tx().QueryRow(ctx, `
		select industry, vendor_list, has_dpo, staff_count
		  from public.compliance_profiles where id = $1::uuid`, profileID,
	).Scan(&industry, &vendorList, &hasDPO, &staff); err != nil {
		t.Fatalf("reading the projected profile: %v", err)
	}
	if industry != "a bakery" {
		t.Fatalf("the projected industry is %q", industry)
	}
	// The list becomes the line `watcher_obligation_applies` tests with
	// `btrim(vendor_list) <> ''` to decide whether the processor obligations
	// apply at all.
	if vendorList != "Stripe, Hetzner" {
		t.Fatalf("the projected vendor list is %q", vendorList)
	}
	// Never asked, so unsure rather than no: both raise the gap, and only one
	// of them claims somebody said so.
	if hasDPO != "unsure" {
		t.Fatalf("an unanswered has_dpo projected to %q", hasDPO)
	}
	if staff == nil || *staff != 7 {
		t.Fatalf("the projected staff count is %v", staff)
	}
}

func TestConfirmingTwiceLeavesOneProfileAndOneHistoryEntry(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	session, _, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}

	facts := map[string]string{memory.KeyHasROPA: `"no"`}
	first, err := tenant.ConfirmOnboarding(ctx, session.ID, facts)
	if err != nil {
		t.Fatalf("confirming: %v", err)
	}
	// The double-clicked button, and the retry after a dropped response.
	second, err := tenant.ConfirmOnboarding(ctx, session.ID, facts)
	if err != nil {
		t.Fatalf("confirming again: %v", err)
	}
	if first != second {
		t.Fatalf("a second confirmation produced profile %s, want %s", second, first)
	}

	var profiles int
	if err := tenant.Tx().QueryRow(ctx,
		`select count(*)::int from public.compliance_profiles`).Scan(&profiles); err != nil {
		t.Fatalf("counting profiles: %v", err)
	}
	if profiles != 1 {
		t.Fatalf("Beta holds %d profiles after two confirmations, want 1", profiles)
	}

	// And no "changed from no to no" in the history a customer reads.
	history, err := tenant.FactHistory(ctx, memory.KeyHasROPA)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("the history holds %d values after two confirmations, want 1", len(history))
	}
}

func TestCorrectingAFactAfterOnboardingIsWhatTheWatcherThenReads(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	session, _, err := tenant.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}
	profileID, err := tenant.ConfirmOnboarding(ctx, session.ID, map[string]string{
		memory.KeyHasROPA: `"unsure"`,
	})
	if err != nil {
		t.Fatalf("confirming: %v", err)
	}

	// THE PROPERTY ENT-228 PROMISED AND ENT-212 HAD TO MAKE TRUE.
	// `run_watcher()` reads `compliance_profiles`, in plpgsql, and knows
	// nothing about the fact store. Without the refresh in CorrectFact, a
	// customer would correct this on the memory page, watch the console agree,
	// and watch the Watcher go on raising the same gap forever.
	if _, _, err := tenant.CorrectFact(
		ctx, memory.KeyHasROPA, `"yes"`, "human", "we wrote one in June"); err != nil {
		t.Fatalf("correcting the fact: %v", err)
	}

	var hasROPA string
	if err := tenant.Tx().QueryRow(ctx,
		`select has_ropa from public.compliance_profiles where id = $1::uuid`, profileID,
	).Scan(&hasROPA); err != nil {
		t.Fatalf("re-reading the projected profile: %v", err)
	}
	if hasROPA != "yes" {
		t.Fatalf("the Watcher still reads has_ropa = %q after the correction", hasROPA)
	}
}

func TestOneOrganisationsInterviewIsInvisibleToAnother(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	bob, err := store.BeginTenant(ctx, bobUser, betaOrg)
	if err != nil {
		t.Fatalf("Bob's transaction: %v", err)
	}
	defer bob.Rollback(ctx)

	session, _, err := bob.StartOnboardingSession(ctx)
	if err != nil {
		t.Fatalf("starting onboarding: %v", err)
	}
	if _, err := bob.AppendOnboardingTurn(
		ctx, session.ID, onboarding.RoleUser, "we sell bread", memory.KeyIndustry,
		`"we sell bread"`,
	); err != nil {
		t.Fatalf("recording an answer: %v", err)
	}

	// Ada is in Alpha and has no business seeing Beta's interview. She is
	// asking for a session id she should not be able to guess, which is the
	// point: guessing it must not help.
	ada, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer ada.Rollback(ctx)

	transcript, err := ada.OnboardingTranscript(ctx, session.ID)
	if err != nil {
		t.Fatalf("reading across organisations should return nothing, not fail: %v", err)
	}
	if len(transcript) != 0 {
		t.Fatalf("Ada read %d turns of Beta's interview", len(transcript))
	}
}
