package onboarding_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/claims"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/memory"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/onboarding"
)

// The reconciled question set (ENT-254).
//
// Two interviews existed and overlapped: thirteen tapped questions on the
// public readiness page, and about eleven typed ones here. Asking a customer
// both was the obvious problem, so there is one set now and it is this one.
// The rules below are the properties that made the merge safe rather than
// merely shorter, and each of them is a bug that has a name.

func TestEveryQuestionIsAnsweredByTapping(t *testing.T) {
	// A free-text question in this interview would be a value nothing
	// downstream can match, and it would be the one place a person could type
	// a sentence into a flow whose answers decide which obligations they see.
	for _, question := range onboarding.Script() {
		switch question.Shape() {
		case memory.KindTriState:
			if len(question.Options) != 0 {
				t.Errorf("%q is a tri-state and should offer no options of its own", question.Key)
			}
		case memory.KindList:
			if len(question.Options) == 0 {
				t.Errorf("%q is a list question with nothing to pick from", question.Key)
			}
		default:
			t.Errorf("%q is answered by typing, which this interview no longer does", question.Key)
		}
	}
}

func TestNoQuestionIsAskedTwice(t *testing.T) {
	seen := map[string]bool{}
	for _, question := range onboarding.Script() {
		if seen[question.Key] {
			t.Errorf("%q is asked more than once", question.Key)
		}
		seen[question.Key] = true
	}
}

func TestEveryOptionOffersAValueAndAWordForIt(t *testing.T) {
	for _, question := range onboarding.Script() {
		for _, option := range question.Options {
			if option.Value == "" || option.Label == "" {
				t.Errorf("%q offers an option with no value or no label: %+v", question.Key, option)
			}
		}
	}
}

func TestAnOptionOutsideTheVocabularyIsRefused(t *testing.T) {
	// The closed vocabulary is the whole reason the answers can be matched
	// against the corpus. A console offering a token this file does not
	// declare produces an answer the server refuses, which is the direction
	// the disagreement should fail in.
	if _, err := onboarding.Parse(memory.KeyDataCategories, "astrology_charts"); err == nil {
		t.Fatal("an answer nothing offered was accepted")
	}
}

func TestNoneRecordsAnEmptyList(t *testing.T) {
	// "Nobody outside the company touches it" is an answer, and the value it
	// records is the empty list rather than the word.
	// `watcher_obligation_applies` reads `btrim(vendor_list) <> ''`, so a
	// stored "none" would tell the Watcher a processor is engaged by the very
	// organisation that just said none is.
	got, err := onboarding.Parse(memory.KeyVendorList, onboarding.NoneChoice)
	if err != nil {
		t.Fatalf("none was refused: %v", err)
	}
	if got != `[]` {
		t.Errorf("none recorded %s, want []", got)
	}
}

func TestAnExclusiveAnswerCannotBeCombined(t *testing.T) {
	if _, err := onboarding.Parse(memory.KeyVendorList, "none, hosting"); err == nil {
		t.Fatal("none was accepted alongside a named processor")
	}
}

func TestUnsureIsRecordedWhereItChangesTheAnswer(t *testing.T) {
	// "I could not say what AI is in use" is not "none", and both evaluators
	// read a non-empty list as AI being in use, so the token is stored.
	got, err := onboarding.Parse(memory.KeyAISystems, onboarding.UnsureChoice)
	if err != nil {
		t.Fatalf("unsure was refused for AI systems: %v", err)
	}
	if got != `["unsure"]` {
		t.Errorf(`unsure recorded %s, want ["unsure"]`, got)
	}
}

func TestUnsureIsNotOfferedWhereStoringItWouldSatisfyAGap(t *testing.T) {
	// `watcher_gap_satisfied('transfer_safeguards')` counts any destination at
	// all, so a stored "unsure" would satisfy the safeguards gap for an
	// organisation that told us it does not know where its data goes. The
	// console's port of the same rule drops the sentinel first and would
	// disagree. Rather than let two evaluators differ, the question does not
	// offer the token: declining is the answer there, and an absent fact reads
	// identically to both.
	//
	// `lawful_bases` is the same shape for a different reason: the memory
	// vocabulary refuses any value outside the Article 6(1) set, so an
	// "unsure" token could not be stored even if it were offered.
	for _, key := range []string{memory.KeyTransferDestination, memory.KeyLawfulBases} {
		question, asked := onboarding.QuestionFor(key)
		if !asked {
			t.Fatalf("%q is not asked", key)
		}
		for _, option := range question.Options {
			if option.Value == onboarding.UnsureChoice {
				t.Errorf("%q offers unsure, which it must not", key)
			}
		}
	}
}

func TestTheLawfulBasesOfferedAreTheArticle6Set(t *testing.T) {
	question, asked := onboarding.QuestionFor(memory.KeyLawfulBases)
	if !asked {
		t.Fatal("the interview never asks what grounds are relied on")
	}
	offered := 0
	for _, option := range question.Options {
		if option.Exclusive {
			continue
		}
		offered++
		if !memory.LawfulBases[option.Value] {
			t.Errorf("%q is offered as a lawful basis and is not one", option.Value)
		}
	}
	if offered != len(memory.LawfulBases) {
		t.Errorf("the interview offers %d lawful bases, the vocabulary holds %d",
			offered, len(memory.LawfulBases))
	}
}

func TestTheHighRiskAIQuestionIsNotAskedOfSomebodyWithNoAI(t *testing.T) {
	none := onboarding.Answers{
		memory.KeyAISystems: {Text: onboarding.NoneChoice, ValueJSON: `[]`},
	}
	if onboarding.Applicable(memory.KeyHighRiskAISystem, none) {
		t.Error("an organisation that told us it runs no AI was asked whether its AI is high-risk")
	}

	// "I could not say" is not "none", so the question stays. Only a definite
	// answer removes one, which is the rule the transfers branch already ran.
	unsure := onboarding.Answers{
		memory.KeyAISystems: {Text: onboarding.UnsureChoice, ValueJSON: `["unsure"]`},
	}
	if !onboarding.Applicable(memory.KeyHighRiskAISystem, unsure) {
		t.Error("an organisation that could not say what AI it runs was not asked")
	}
}

func TestEveryQuestionThatQuotesTheLawNamesAnObligationTheCorpusHolds(t *testing.T) {
	// The `basis` slug is what the console renders a corpus summary from, and
	// that summary is the only statement of law the interview shows. A slug
	// the corpus does not hold renders nothing, so the question would ask for
	// something sensitive with its justification silently missing, which is
	// the failure ENT-248 exists to stop.
	held := corpusSlugs(t)
	quoted := 0
	for _, question := range onboarding.Script() {
		if question.Basis == "" {
			continue
		}
		quoted++
		if !held[question.Basis] {
			t.Errorf("%q quotes %q, which the corpus does not hold", question.Key, question.Basis)
		}
	}
	if quoted == 0 {
		t.Error("no question quotes the corpus, so the interview states nothing it can be checked against")
	}
}

func TestNothingTheInterviewSaysStatesWhatTheLawRequires(t *testing.T) {
	// ENT-248's ruling, on the surface where a customer meets it first. Before
	// ENT-254 the assessment's prompts lived in TypeScript and the console's
	// detector walked them; moving the interview here moved the prompts out of
	// its reach, so the same detector walks them from Go instead.
	//
	// What is checked is every sentence this package writes for itself: the
	// prompt, the note under it, and the label on every option. What is NOT
	// checked, and must not be, is the corpus summary the console renders
	// beside the question, because stating what the law requires is exactly
	// that row's job.
	check := func(what, text string) {
		t.Helper()
		if text == "" {
			return
		}
		if assertions := claims.LegalAssertions(text); len(assertions) > 0 {
			t.Errorf("%s states law and should quote the corpus instead: %q %v",
				what, text, assertions)
		}
	}

	for _, question := range onboarding.Script() {
		check("a prompt", question.Prompt)
		check("a note", question.Help)
		for _, option := range question.Options {
			check("an option label", option.Label)
		}
	}
}

// corpusSlugs reads the obligations the repository holds.
//
// The file rather than the database, deliberately: this is a pure-function
// test with no stack behind it, and what it is guarding is that a slug written
// in Go matches one written in JSON by a curator. Those two files are what
// have to agree.
func corpusSlugs(t *testing.T) map[string]bool {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "data", "corpus", "obligations.json"))
	if err != nil {
		t.Fatalf("reading the corpus: %v", err)
	}

	var file struct {
		Obligations []struct {
			Slug string `json:"slug"`
		} `json:"obligations"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decoding the corpus: %v", err)
	}

	slugs := make(map[string]bool, len(file.Obligations))
	for _, obligation := range file.Obligations {
		slugs[obligation.Slug] = true
	}
	return slugs
}
