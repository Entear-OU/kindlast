package records

import (
	"strings"
	"testing"
)

// The deterministic drafter, proved the way the register is: against the facts
// an organisation actually recorded, and against the columns the Executor
// actually reads.
//
// Every case here is a table row rather than a test function, except the three
// invariants at the bottom, which are properties of every draft rather than
// facts about one.

func fact(key string, values ...string) Fact { return Fact{Key: key, Values: values} }

func TestDraftingFromFacts(t *testing.T) {
	ropa, _ := RegisterFor("create_ropa")

	cases := []struct {
		name string
		// The register to draft into.
		register Register
		facts    []Fact
		// field name -> the values expected, and the fact expected behind them.
		filled map[string][]string
		from   map[string]string
		// The columns expected to be left, in any order.
		left []string
	}{
		{
			name:     "the three columns a fact restates are filled and named",
			register: ropa,
			facts: []Fact{
				fact("data_categories", "names", "contact details", "payroll data"),
				fact("vendor_list", "Acme Payroll", "Stripe"),
				fact("lawful_bases", "contract"),
			},
			filled: map[string][]string{
				"data_categories": {"names", "contact details", "payroll data"},
				"recipients":      {"Acme Payroll", "Stripe"},
				"legal_basis":     {"contract"},
			},
			from: map[string]string{
				"data_categories": "data_categories",
				"recipients":      "vendor_list",
				"legal_basis":     "lawful_bases",
			},
			left: []string{"name", "purpose", "retention_period"},
		},
		{
			// A ROPA column holds ONE basis and the fact holds every basis the
			// organisation relies on. Picking one would be the drafter
			// deciding which activity this entry is about, which is the
			// person's to say.
			name:     "more than one lawful basis is left rather than chosen between",
			register: ropa,
			facts: []Fact{
				fact("lawful_bases", "consent", "contract"),
			},
			filled: map[string][]string{},
			left: []string{
				"name", "purpose", "legal_basis",
				"data_categories", "recipients", "retention_period",
			},
		},
		{
			name:     "an organisation that has recorded nothing has every column left for it",
			register: ropa,
			facts:    nil,
			filled:   map[string][]string{},
			left: []string{
				"name", "purpose", "legal_basis",
				"data_categories", "recipients", "retention_period",
			},
		},
		{
			// A recorded fact holding no values is the same practical state as
			// an unrecorded one, and a column filled from it would carry a
			// value nobody wrote.
			name:     "a fact recorded with nothing in it fills nothing",
			register: ropa,
			facts:    []Fact{fact("data_categories"), fact("vendor_list", "")},
			filled:   map[string][]string{},
			left: []string{
				"name", "purpose", "legal_basis",
				"data_categories", "recipients", "retention_period",
			},
		},
		{
			// The AI Act register has no mapping, deliberately: a
			// classification is a legal judgement under Annex III and a
			// vendor is not a system. The Hands is what reaches these.
			name:     "the AI systems register drafts nothing",
			register: mustRegister(t, "create_ai_system"),
			facts: []Fact{
				fact("ai_systems", "a support triage model"),
				fact("vendor_list", "Acme"),
			},
			filled: map[string][]string{},
			left: []string{
				"name", "vendor", "purpose",
				"risk_classification", "documentation_status",
			},
		},
		{
			// `received_at` is the statutory clock and 00010 refuses to guess
			// it. Nothing about a data-subject request is derivable from a
			// profile fact: the requester is a person, not a column.
			name:     "the DSAR register drafts nothing, and never a receipt date",
			register: mustRegister(t, "create_dsar"),
			facts:    []Fact{fact("data_categories", "names")},
			filled:   map[string][]string{},
			left:     []string{"requester", "request_type", "handler", "received_at"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draft := DraftFromFacts(tc.register, tc.facts)

			if len(draft.Fields) != len(tc.filled) {
				t.Fatalf("filled %d fields, want %d: %+v", len(draft.Fields), len(tc.filled), draft.Fields)
			}
			for _, field := range draft.Fields {
				want, expected := tc.filled[field.Name]
				if !expected {
					t.Fatalf("filled %q, which no fact supports", field.Name)
				}
				if len(field.Values) != len(want) {
					t.Fatalf("%q got %v, want %v", field.Name, field.Values, want)
				}
				for i, v := range field.Values {
					if v != want[i] {
						t.Fatalf("%q got %v, want %v", field.Name, field.Values, want)
					}
				}
				if field.FromFact != tc.from[field.Name] {
					t.Fatalf("%q says it came from %q, want %q",
						field.Name, field.FromFact, tc.from[field.Name])
				}
			}

			left := map[string]bool{}
			for _, l := range draft.LeftForYou {
				left[l.Name] = true
				if l.Why == "" {
					t.Fatalf("%q was left with no reason, which reads as an oversight", l.Name)
				}
			}
			if len(left) != len(tc.left) {
				t.Fatalf("left %v, want %v", left, tc.left)
			}
			for _, name := range tc.left {
				if !left[name] {
					t.Fatalf("%q was neither filled nor left", name)
				}
			}
		})
	}
}

// A draft is a claim about where every value came from, and the invariant the
// whole surface rests on is that the claim survives the same validator a
// model's plan is put through. If this can pass while ValidatePrepared refuses
// the same draft, then the deterministic path is a way around the guard.
func TestEveryDraftPassesTheValidatorAModelsPlanIsPutThrough(t *testing.T) {
	ropa, _ := RegisterFor("create_ropa")
	facts := []Fact{
		fact("data_categories", "names"),
		fact("vendor_list", "Acme"),
		fact("lawful_bases", "consent"),
		// A fact the register has no column for. It must not reach the draft.
		fact("industry", "software"),
	}

	draft := DraftFromFacts(ropa, facts)
	if len(draft.Fields) == 0 {
		t.Fatal("drafted nothing, so this proves nothing")
	}

	known := map[string]bool{}
	for _, f := range facts {
		known[f.Key] = true
	}
	if err := ValidatePrepared(ropa, draft.Fields, known); err != nil {
		t.Fatalf("the drafter wrote a plan the validator refuses: %v", err)
	}
}

// A draft that filled a value from a fact the organisation does not hold would
// be the fabrication the whole surface exists to not produce. The drafter
// reads only the facts it is given, so this is the property stated as a test
// rather than left to the reading.
func TestNoDraftedValueNamesAFactThatWasNotGiven(t *testing.T) {
	ropa, _ := RegisterFor("create_ropa")
	given := []Fact{fact("data_categories", "names")}

	draft := DraftFromFacts(ropa, given)
	for _, field := range draft.Fields {
		found := false
		for _, f := range given {
			if f.Key == field.FromFact {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q claims to come from %q, which was not offered",
				field.Name, field.FromFact)
		}
	}
}

// The Hands' rule 6, as an invariant of the deterministic path: a plan that is
// silent about a column reads as a plan that finished it.
func TestEveryColumnIsEitherFilledOrLeftWithAReason(t *testing.T) {
	for _, actionType := range []string{"create_ropa", "create_ai_system", "create_dsar"} {
		register := mustRegister(t, actionType)
		draft := DraftFromFacts(register, []Fact{
			fact("data_categories", "names"),
			fact("vendor_list", "Acme"),
			fact("lawful_bases", "consent"),
		})

		accounted := map[string]bool{}
		for _, f := range draft.Fields {
			accounted[f.Name] = true
		}
		for _, l := range draft.LeftForYou {
			if accounted[l.Name] {
				t.Fatalf("%s: %q is both filled and left", actionType, l.Name)
			}
			accounted[l.Name] = true
		}
		for _, field := range register.Fields {
			if !accounted[field.Name] {
				t.Fatalf("%s: %q is neither filled nor left", actionType, field.Name)
			}
		}
	}
}

// The sentence a person reads above the decision counts what is there, and
// never claims the entry is finished.
func TestTheExplanationCountsBothHalvesAndReadsAsEnglish(t *testing.T) {
	ropa, _ := RegisterFor("create_ropa")

	one := DraftFromFacts(ropa, []Fact{fact("lawful_bases", "contract")}).Explanation(ropa)
	if want := "1 column is filled"; !contains(one, want) {
		t.Fatalf("got %q, want it to contain %q", one, want)
	}
	if want := "5 columns are left"; !contains(one, want) {
		t.Fatalf("got %q, want it to contain %q", one, want)
	}
	if !contains(one, ropa.Label) {
		t.Fatalf("got %q, which does not name the register it writes to", one)
	}

	none := DraftFromFacts(ropa, nil).Explanation(ropa)
	if want := "0 columns are filled"; !contains(none, want) {
		t.Fatalf("got %q, want it to contain %q", none, want)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && strings.Contains(haystack, needle)
}

// A register nothing creates (today: `review`) drafts nothing at all, so
// approving one is unchanged.
func TestAnEmptyRegisterDraftsNothing(t *testing.T) {
	draft := DraftFromFacts(Register{}, []Fact{fact("data_categories", "names")})
	if len(draft.Fields) != 0 || len(draft.LeftForYou) != 0 {
		t.Fatalf("drafted %+v for a register with no columns", draft)
	}
	if draft.Empty() != true {
		t.Fatal("a draft with nothing in it does not report itself empty")
	}
}

func mustRegister(t *testing.T, actionType string) Register {
	t.Helper()
	register, ok := RegisterFor(actionType)
	if !ok {
		t.Fatalf("%q names no register", actionType)
	}
	return register
}
