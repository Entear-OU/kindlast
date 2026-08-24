package records_test

import (
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
)

// The register vocabulary the Hands is shown (ENT-261).

func TestEveryActionThatCreatesARecordHasARegister(t *testing.T) {
	// `findings.Executes` decides whether approving enqueues an execution. A
	// register missing for one of those action types would mean an approval
	// that creates a record the Hands cannot describe, which is exactly the
	// state ENT-261 was filed about.
	for _, action := range []string{
		findings.ActionCreateROPA,
		findings.ActionCreateDSAR,
		findings.ActionCreateAISystem,
	} {
		register, ok := records.RegisterFor(action)
		if !ok {
			t.Errorf("%s creates a record and has no register", action)
			continue
		}
		if register.Name == "" || register.Label == "" || len(register.Fields) == 0 {
			t.Errorf("%s has an empty register: %+v", action, register)
		}
	}
}

func TestAnActionThatCreatesNothingHasNoRegister(t *testing.T) {
	// `review` is the ordinary case: the approval records the decision and
	// creates nothing. Present-with-no-fields would be worse than absent,
	// because a run would then be asked to prepare a record that will not
	// exist.
	if _, ok := records.RegisterFor("review"); ok {
		t.Fatal("review has a register; approving it creates nothing")
	}
	if _, ok := records.RegisterFor(""); ok {
		t.Fatal("an empty action type has a register")
	}
}

// TestTheRegisterFieldNamesAreTheOnesTheExecutorReads is the test the comment
// in registers.go promises rather than merely asserts.
//
// # WHY THIS READS A SOURCE FILE, WHICH IS UNUSUAL AND DELIBERATE
//
// The failure it guards against is silent and expensive. The Executor reads
// `metadata -> 'payload' ->> 'purpose'` and friends with hand-written SQL, so a
// field name here that the Executor does not read produces a plan that looks
// prepared and a record saying "Not recorded" in that column. Nothing else in
// the suite would notice: the plan would be accepted, the payload written, the
// record created, and every test green. That is the exact failure ENT-261 was
// filed to fix, arriving by a new door.
//
// There is no shared constant to compare against, because the Executor's
// version of these names lives inside SQL strings. So the test reads them out
// of the SQL, which is where they actually are. If the Executor moves, this
// fails loudly and says so, which is the right outcome: somebody moving it
// should be told that a second file depends on its payload keys.
func TestTheRegisterFieldNamesAreTheOnesTheExecutorReads(t *testing.T) {
	const executor = "../../store/postgres/executor.go"

	source, err := os.ReadFile(executor)
	if err != nil {
		t.Fatalf("cannot read %s: %v.\n\nThis test reads the Executor's SQL to "+
			"find which payload keys it consumes. If the Executor has moved, "+
			"point this at its new home rather than deleting the test: the "+
			"agreement it checks is what stops the Hands preparing a column "+
			"nothing reads.", executor, err)
	}

	// Both spellings the Executor uses: `->> 'key'` for a scalar, and
	// `-> 'key'` for a list handed to `jsonb_text_array`.
	keys := map[string]bool{}
	for _, match := range regexp.MustCompile(
		`'payload'\s*->>?\s*'([a-z_]+)'`,
	).FindAllStringSubmatch(string(source), -1) {
		keys[match[1]] = true
	}
	if len(keys) == 0 {
		t.Fatal("found no payload keys in the Executor's SQL; the shape it " +
			"reads has changed and this test can no longer see it")
	}

	for _, action := range []string{
		findings.ActionCreateROPA,
		findings.ActionCreateDSAR,
		findings.ActionCreateAISystem,
	} {
		register, _ := records.RegisterFor(action)
		for _, field := range register.Fields {
			if !keys[field.Name] {
				t.Errorf(
					"%s offers the column %q, which the Executor never reads out "+
						"of the payload. A run filling it would produce a plan "+
						"that looks prepared and a record that says nothing. The "+
						"keys the Executor reads are %v",
					register.Name, field.Name, sorted(keys))
			}
		}
	}
}

func TestASingleValuedColumnGivenSeveralValuesIsRefused(t *testing.T) {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	err := records.ValidatePrepared(register, []records.PreparedField{
		{Name: "purpose", Values: []string{"one", "two"}, FromFact: "industry"},
	}, map[string]bool{"industry": true})

	var refusal records.ErrTooManyValues
	if !asError(err, &refusal) {
		t.Fatalf("got %v; want ErrTooManyValues", err)
	}
}

func TestAListColumnMayHoldSeveralValues(t *testing.T) {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	err := records.ValidatePrepared(register, []records.PreparedField{
		{
			Name:     "data_categories",
			Values:   []string{"names", "bank details"},
			FromFact: "data_categories",
		},
	}, map[string]bool{"data_categories": true})
	if err != nil {
		t.Fatalf("a list column was refused several values: %v", err)
	}
}

func TestAColumnTheRegisterDoesNotHaveIsRefused(t *testing.T) {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	err := records.ValidatePrepared(register, []records.PreparedField{
		{Name: "annual_revenue", Values: []string{"a lot"}, FromFact: "industry"},
	}, map[string]bool{"industry": true})

	var refusal records.ErrUnknownField
	if !asError(err, &refusal) {
		t.Fatalf("got %v; want ErrUnknownField", err)
	}
	if !strings.Contains(refusal.Error(), "annual_revenue") {
		t.Errorf("the refusal does not name the column: %s", refusal)
	}
}

// TestAValueFromAFactThisOrganisationDoesNotHoldIsRefused is the invariant half
// of the provenance check, and it is the one that matters most.
//
// A prepared value is a claim about where it came from, and the product's whole
// value is that a human can check a claim. A value attributed to a fact that
// does not exist is the same fabrication a citation to an invented obligation
// is, arriving by a different door, and it is worse because it lands in a
// compliance record rather than beside one.
func TestAValueFromAFactThisOrganisationDoesNotHoldIsRefused(t *testing.T) {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	err := records.ValidatePrepared(register, []records.PreparedField{
		{Name: "purpose", Values: []string{"Paying people"}, FromFact: "staff_count"},
	}, map[string]bool{"industry": true})

	var refusal records.ErrUnknownFact
	if !asError(err, &refusal) {
		t.Fatalf("got %v; want ErrUnknownFact", err)
	}
}

func TestAValueWithNoFactAtAllIsRefused(t *testing.T) {
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	err := records.ValidatePrepared(register, []records.PreparedField{
		{Name: "purpose", Values: []string{"Paying people"}},
	}, map[string]bool{"industry": true})

	var refusal records.ErrNoFact
	if !asError(err, &refusal) {
		t.Fatalf("got %v; want ErrNoFact", err)
	}
}

func TestAColumnPreparedWithNoValueIsRefused(t *testing.T) {
	// An empty prepared column is worse than an absent one: it occupies a slot
	// in the plan, so a person reading it sees the column accounted for, and
	// the record is created with nothing in it.
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	for name, values := range map[string][]string{
		"no values at all": nil,
		"one empty string": {""},
	} {
		err := records.ValidatePrepared(register, []records.PreparedField{
			{Name: "purpose", Values: values, FromFact: "industry"},
		}, map[string]bool{"industry": true})

		var refusal records.ErrNoValue
		if !asError(err, &refusal) {
			t.Errorf("%s: got %v; want ErrNoValue", name, err)
		}
	}
}

func TestTheLawfulBasisDescriptionNamesTheArticleSixVocabulary(t *testing.T) {
	// The description is what tells a model which spellings exist, and the
	// spelling is what decides whether an obligation applies (see
	// `domain/memory.LawfulBases`). Read from that map rather than written out
	// again, so a seventh basis cannot arrive in one place only.
	register, _ := records.RegisterFor(findings.ActionCreateROPA)
	field, ok := register.Field("legal_basis")
	if !ok {
		t.Fatal("the ROPA register has no legal_basis column")
	}
	for _, basis := range []string{"consent", "contract", "legal_obligation"} {
		if !strings.Contains(field.Description, basis) {
			t.Errorf("the legal_basis description does not name %q: %s",
				basis, field.Description)
		}
	}
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// asError names errors.As, so the tests above read as assertions about which
// refusal fired rather than about the standard library. It goes through
// errors.As rather than a type assertion because a refusal that is ever
// wrapped on its way out must still be recognisable, and a bare assertion
// would quietly start reporting the wrong thing on the day one is.
func asError[T error](err error, target *T) bool {
	return errors.As(err, target)
}
