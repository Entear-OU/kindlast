package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
)

// Organisation memory through the code path that will serve requests
// (ENT-228).
//
// The database suite already proves the invariants over the catalogue: the
// column-level grant, the partial unique index and the immutability trigger.
// What it cannot prove is that the store USES them correctly, and the failure
// mode there is quiet. A CorrectFact that inserted without closing would be
// refused by the index and would surface as an error; one that closed without
// inserting would leave the organisation believing nothing, silently, with
// every read simply returning one fewer fact.

func TestCorrectingAFactClosesThePreviousValue(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	// Rolled back rather than committed, so the fixture leaves nothing behind.
	// The whole test runs inside one transaction, which is also how a request
	// runs.
	defer tenant.Rollback(ctx)

	if _, _, err := tenant.CorrectFact(ctx, memory.KeyHasDPO, `"unsure"`, "human", ""); err != nil {
		t.Fatalf("first assertion of a fact: %v", err)
	}

	stored, changed, err := tenant.CorrectFact(
		ctx, memory.KeyHasDPO, `"yes"`, "human", "appointed in June")
	if err != nil {
		t.Fatalf("correcting the fact: %v", err)
	}
	if !changed {
		t.Fatal("correcting unsure to yes reported no change")
	}
	if stored.ValueJSON != `"yes"` {
		t.Fatalf("stored value is %s, want \"yes\"", stored.ValueJSON)
	}
	if stored.Note != "appointed in June" {
		t.Fatalf("note is %q, want the one that was sent", stored.Note)
	}

	history, err := tenant.FactHistory(ctx, memory.KeyHasDPO)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history has %d entries, want 2", len(history))
	}
	// Newest first, and the older one closed. Both halves matter: an open
	// previous value would mean two current answers, and the partial unique
	// index would have refused the insert before we got here.
	if !history[0].Current() {
		t.Fatal("the newest value is not the open one")
	}
	if history[1].Current() {
		t.Fatal("the superseded value is still open")
	}
	if history[1].ValueJSON != `"unsure"` {
		t.Fatalf("the superseded value is %s, want \"unsure\"", history[1].ValueJSON)
	}
}

func TestTheIntervalsMeetWithNoGap(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	if _, _, err := tenant.CorrectFact(ctx, memory.KeyIndustry, `"saas"`, "human", ""); err != nil {
		t.Fatalf("first value: %v", err)
	}
	if _, _, err := tenant.CorrectFact(ctx, memory.KeyIndustry, `"fintech"`, "human", ""); err != nil {
		t.Fatalf("second value: %v", err)
	}

	history, err := tenant.FactHistory(ctx, memory.KeyIndustry)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}

	// THE PROPERTY: no instant where an as-of query finds nothing.
	//
	// If the close used a different `now()` from the open, there would be a
	// window, however small, in which the organisation is recorded as having
	// believed no industry at all. A run stamped inside that window would
	// reconstruct a profile with a hole in it, and nobody would ever look for
	// the cause in a timestamp.
	if !history[1].ValidTo.Equal(history[0].ValidFrom) {
		t.Fatalf("the old value closed at %s and the new one opened at %s: a gap",
			history[1].ValidTo, history[0].ValidFrom)
	}
}

func TestCorrectingAFactToWhatItAlreadySaysWritesNothing(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	if _, _, err := tenant.CorrectFact(ctx, memory.KeyHasROPA, `"no"`, "human", ""); err != nil {
		t.Fatalf("first value: %v", err)
	}

	_, changed, err := tenant.CorrectFact(ctx, memory.KeyHasROPA, `"no"`, "human", "")
	if err != nil {
		t.Fatalf("re-asserting the same value: %v", err)
	}
	if changed {
		t.Fatal("re-asserting the same value reported a change")
	}

	history, err := tenant.FactHistory(ctx, memory.KeyHasROPA)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	// A console re-submitting a form would otherwise fill the history with
	// "changed from no to no", and a history whose rows are mostly noise is one
	// nobody scrolls.
	if len(history) != 1 {
		t.Fatalf("history has %d entries, want 1", len(history))
	}
}

func TestKeyOrderIsNotAChange(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// A list, whose JSON encoding is order-sensitive as text and not as jsonb
	// for objects. The comparison is `value = $2::jsonb` rather than a string
	// compare precisely so the database decides what "the same value" means.
	if _, _, err := tenant.CorrectFact(
		ctx, memory.KeyEUJurisdictions, `["DE","EE"]`, "human", ""); err != nil {
		t.Fatalf("first value: %v", err)
	}

	_, changed, err := tenant.CorrectFact(
		ctx, memory.KeyEUJurisdictions, `["DE", "EE"]`, "human", "")
	if err != nil {
		t.Fatalf("re-asserting with different whitespace: %v", err)
	}
	if changed {
		t.Fatal("whitespace in the encoding was treated as a change of fact")
	}
}

func TestAFactWithTheWrongShapeIsRefusedBeforeItIsStored(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	// jsonb would store this happily, and every reader would then have to cope
	// with a staff count that is sometimes "about fifty".
	if _, _, err := tenant.CorrectFact(
		ctx, memory.KeyStaffCount, `"about fifty"`, "human", ""); err == nil {
		t.Fatal("text was accepted for a numeric fact")
	}

	if _, _, err := tenant.CorrectFact(
		ctx, "favourite_colour", `"blue"`, "human", ""); err == nil {
		t.Fatal("a key outside the vocabulary was accepted")
	}

	if _, _, err := tenant.CorrectFact(
		ctx, memory.KeyHasDPO, `"yes"`, "vibes", ""); err == nil {
		t.Fatal("a source outside the schema's set was accepted")
	}
}

func TestObservationsComeBackNewestFirst(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()

	tenant, err := store.BeginTenant(ctx, adaUser, alphaOrg)
	if err != nil {
		t.Fatalf("Ada's transaction: %v", err)
	}
	defer tenant.Rollback(ctx)

	base := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	// Inserted out of order, so an implementation returning insertion order
	// fails rather than passing by coincidence.
	for _, offset := range []int{2, 0, 4, 1, 3} {
		if _, err := tenant.Tx().Exec(ctx, `
			insert into org_evidence (org_id, source, kind, observed_at)
			values ($1, 'integration', 'ropa_export', $2)
		`, alphaOrg, base.AddDate(0, 0, offset)); err != nil {
			t.Fatalf("seeding an observation: %v", err)
		}
	}

	observations, err := tenant.Observations(ctx, 10, time.Time{})
	if err != nil {
		t.Fatalf("listing observations: %v", err)
	}
	if len(observations) < 5 {
		t.Fatalf("got %d observations, want at least the 5 seeded", len(observations))
	}
	for i := 1; i < 5; i++ {
		if observations[i].ObservedAt.After(observations[i-1].ObservedAt) {
			t.Fatalf("observation %d is newer than the one before it", i)
		}
	}
}

// The vocabulary tests need no stack: they are the decision half, and the whole
// argument for keeping the key set in Go rather than in a check constraint is
// that it can be exercised like this.
func TestTheVocabularyRefusesTheWrongShape(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{"text for a number", memory.KeyStaffCount, `"fifty"`, "holds a number"},
		{"number for text", memory.KeyIndustry, `50`, "holds text"},
		{"a list for a tri-state", memory.KeyHasDPO, `["yes"]`, "yes, no or unsure"},
		{"maybe is not a tri-state", memory.KeyHasDPO, `"maybe"`, "yes, no or unsure"},
		{"numbers in a text list", memory.KeyEUJurisdictions, `["DE",2]`, "list of text"},
		{"an unknown key", "favourite_colour", `"blue"`, "not a fact this product understands"},
		{"not JSON at all", memory.KeyIndustry, `{`, "not valid JSON"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := memory.ValidateValue(c.key, c.value)
			if err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error is %q, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

func TestUnsureIsARealAnswer(t *testing.T) {
	// Not a formality. "We do not know whether we have a record of processing
	// activities" is a finding in itself, and a vocabulary that refused it
	// would push every such organisation to "no", which is a different claim.
	for _, value := range []string{`"yes"`, `"no"`, `"unsure"`} {
		if err := memory.ValidateValue(memory.KeyHasDPO, value); err != nil {
			t.Fatalf("%s was refused: %v", value, err)
		}
	}
}
