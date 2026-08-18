package narrative

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/store/postgres"
	platformv1 "github.com/Entear-OU/kindlast/gen/go/kindlast/platform/v1"
)

// Narrating findings (ENT-245).
//
// Every assertion here is about a case that would otherwise look like success.
// A narrator that recorded nothing, or that treated a refusal as a failure, or
// that stopped the whole pass on one bad finding, produces a response nobody
// reading it would question.

type fakeFindings struct {
	pending   []postgres.PendingFinding
	narrated  map[uuid.UUID]string
	refused   map[uuid.UUID]string
	runIDs    map[uuid.UUID]string
	readErr   error
	writeErr  error
	readLimit int32
}

func newFindings(pending ...postgres.PendingFinding) *fakeFindings {
	return &fakeFindings{
		pending:  pending,
		narrated: map[uuid.UUID]string{},
		refused:  map[uuid.UUID]string{},
		runIDs:   map[uuid.UUID]string{},
	}
}

func (f *fakeFindings) FindingsAwaitingNarrative(
	_ context.Context, _ string, limit int32,
) ([]postgres.PendingFinding, error) {
	f.readLimit = limit
	return f.pending, f.readErr
}

func (f *fakeFindings) RecordNarrative(
	_ context.Context, _ string, id uuid.UUID, narrative, runID string,
) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.narrated[id] = narrative
	f.runIDs[id] = runID
	return nil
}

func (f *fakeFindings) RecordNarrativeRefusal(
	_ context.Context, _ string, id uuid.UUID, reason, runID string,
) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.refused[id] = reason
	f.runIDs[id] = runID
	return nil
}

type fakeDrafter struct {
	response *platformv1.DraftNarrativeResponse
	err      error
	calls    int
	offered  [][]string
	// The whole obligation context of the last call, so a test can assert what
	// the model was actually given rather than only which slug it may cite.
	context []*platformv1.ObligationContext
}

func (d *fakeDrafter) DraftNarrative(
	_ context.Context,
	req *connect.Request[platformv1.DraftNarrativeRequest],
) (*connect.Response[platformv1.DraftNarrativeResponse], error) {
	d.calls++
	slugs := make([]string, 0, len(req.Msg.GetObligations()))
	for _, o := range req.Msg.GetObligations() {
		slugs = append(slugs, o.GetSlug())
	}
	d.offered = append(d.offered, slugs)
	d.context = req.Msg.GetObligations()
	if d.err != nil {
		return nil, d.err
	}
	return connect.NewResponse(d.response), nil
}

func finding() postgres.PendingFinding {
	return postgres.PendingFinding{
		ID:                uuid.New(),
		Detected:          "No record of processing activities",
		ProposedAction:    "Create one",
		Severity:          "high",
		ObligationSlug:    "gdpr-art-30-ropa",
		ObligationTitle:   "Records of processing activities",
		ObligationSummary: "A controller must maintain a record.",
		// The real row's conditions, so the grounds this test asserts are the
		// ones the ROPA finding actually carries.
		ObligationAppliesWhen: `{"role": "controller", "requires": ["ropa"]}`,
	}
}

func request(org string) *connect.Request[platformv1.NarrateFindingsRequest] {
	return connect.NewRequest(&platformv1.NarrateFindingsRequest{OrgId: org})
}

const org = "a0000000-0000-4000-8000-000000000001"

func TestNoIntelligenceIsReportedRatherThanFailed(t *testing.T) {
	// A deployment without the model profile is supported. Answering with an
	// error would tell an operator something is broken when nothing is, and the
	// findings still carry the text the sweep wrote.
	service := New(newFindings(finding()), nil, nil)

	response, err := service.NarrateFindings(t.Context(), request(org))
	if err != nil {
		t.Fatalf("no drafter should not be an error: %v", err)
	}
	if response.Msg.GetIntelligenceAvailable() {
		t.Fatal("reported Intelligence as available with no drafter")
	}
	if response.Msg.GetAttempted() != 0 {
		t.Fatalf("attempted %d findings with no drafter", response.Msg.GetAttempted())
	}
}

func TestASuccessfulRunStoresTheNarrativeAndItsRun(t *testing.T) {
	f := finding()
	findings := newFindings(f)
	drafter := &fakeDrafter{response: &platformv1.DraftNarrativeResponse{
		Outcome:    platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED,
		Narrative:  "You process personal data and keep no record of it.",
		AgentRunId: "11111111-1111-4111-8111-111111111111",
	}}

	response, err := New(findings, drafter, nil).NarrateFindings(t.Context(), request(org))
	if err != nil {
		t.Fatalf("narrating: %v", err)
	}
	if response.Msg.GetNarrated() != 1 {
		t.Fatalf("narrated %d, want 1", response.Msg.GetNarrated())
	}
	if findings.narrated[f.ID] == "" {
		t.Fatal("the narrative was not stored")
	}
	// THE RUN ID MATTERS AS MUCH AS THE TEXT. A narrative whose provenance was
	// not recorded is exactly the finding nobody can check, which AGENTS.md
	// calls worse than nothing.
	if findings.runIDs[f.ID] == "" {
		t.Fatal("the run that produced it was not recorded")
	}
}

func TestARefusalIsRecordedAndIsNotAFailure(t *testing.T) {
	f := finding()
	findings := newFindings(f)
	drafter := &fakeDrafter{response: &platformv1.DraftNarrativeResponse{
		Outcome:       platformv1.DraftOutcome_DRAFT_OUTCOME_REFUSED,
		OutcomeDetail: "citation gdpr-art-50 was not offered",
		AgentRunId:    "22222222-2222-4222-8222-222222222222",
	}}

	response, err := New(findings, drafter, nil).NarrateFindings(t.Context(), request(org))
	if err != nil {
		t.Fatalf("a refusal must not be an error: %v", err)
	}

	// §26.3: refusal is what a working guardrail produces. Counting it as a
	// failure would put "the harness broke" in the column an operator reads to
	// decide whether to trust the product, on the occasions it worked best.
	if response.Msg.GetRefused() != 1 || response.Msg.GetFailed() != 0 {
		t.Fatalf("refused=%d failed=%d, want 1 and 0",
			response.Msg.GetRefused(), response.Msg.GetFailed())
	}
	if findings.refused[f.ID] == "" {
		t.Fatal("the refusal was not recorded")
	}
	if findings.narrated[f.ID] != "" {
		t.Fatal("a refused run stored a narrative")
	}
}

func TestARefusalIsRecordedSoItIsNotRetriedForever(t *testing.T) {
	f := finding()
	findings := newFindings(f)
	drafter := &fakeDrafter{response: &platformv1.DraftNarrativeResponse{
		Outcome: platformv1.DraftOutcome_DRAFT_OUTCOME_REFUSED,
	}}

	if _, err := New(findings, drafter, nil).NarrateFindings(t.Context(), request(org)); err != nil {
		t.Fatalf("narrating: %v", err)
	}

	// Without a stored reason the finding is picked up by every subsequent
	// pass, and one finding the model cannot narrate correctly burns the whole
	// budget in a loop. A reason is written even when the run gave none.
	if findings.refused[f.ID] == "" {
		t.Fatal("a refusal with no detail recorded nothing, so it will be retried forever")
	}
}

func TestOnlyTheFindingsOwnObligationIsOffered(t *testing.T) {
	f := finding()
	drafter := &fakeDrafter{response: &platformv1.DraftNarrativeResponse{
		Outcome:   platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED,
		Narrative: "text",
	}}

	if _, err := New(newFindings(f), drafter, nil).NarrateFindings(t.Context(), request(org)); err != nil {
		t.Fatalf("narrating: %v", err)
	}

	// THE STRONGEST FORM OF THE CITATION CHECK, and the reason it works.
	//
	// The validator refuses any citation outside what was offered. Offering
	// exactly one obligation means a narrative citing any other article is
	// refused even when that article genuinely exists, which is the failure the
	// local model actually exhibits: asked which GDPR article requires a record
	// of processing activities it answers 50, then 34, then 54.
	//
	// Widening this to the whole corpus would make that fabrication
	// indistinguishable from a good citation, silently, and no test elsewhere
	// would notice.
	if len(drafter.offered) != 1 || len(drafter.offered[0]) != 1 {
		t.Fatalf("offered %v, want exactly one obligation", drafter.offered)
	}
	if drafter.offered[0][0] != f.ObligationSlug {
		t.Fatalf("offered %q, want the finding's own obligation %q",
			drafter.offered[0][0], f.ObligationSlug)
	}
}

func TestOneBadFindingDoesNotStopThePass(t *testing.T) {
	first, second := finding(), finding()
	findings := newFindings(first, second)
	drafter := &fakeDrafter{err: errors.New("the model is unreachable")}

	response, err := New(findings, drafter, nil).NarrateFindings(t.Context(), request(org))
	if err != nil {
		t.Fatalf("a failing finding must not fail the pass: %v", err)
	}

	// Both attempted. A job that abandoned the batch on the first error would
	// let one unlucky finding block every other explanation in the
	// organisation, and the symptom would be "narration stopped working".
	if drafter.calls != 2 {
		t.Fatalf("attempted %d findings, want both", drafter.calls)
	}
	if response.Msg.GetFailed() != 2 {
		t.Fatalf("failed=%d, want 2", response.Msg.GetFailed())
	}
}

func TestAnOrganisationIsRequired(t *testing.T) {
	// There is deliberately no "every organisation" mode: a job sweeping every
	// tenant in one call is one bug away from putting one organisation's signal
	// in another's prompt.
	_, err := New(newFindings(), &fakeDrafter{}, nil).NarrateFindings(
		t.Context(), connect.NewRequest(&platformv1.NarrateFindingsRequest{}))
	if err == nil {
		t.Fatal("narrated with no organisation")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code is %v, want invalid_argument", connect.CodeOf(err))
	}
}

func TestABadOrganisationIsTheCallersFault(t *testing.T) {
	findings := newFindings()
	findings.readErr = postgres.ErrBadOrganisation

	_, err := New(findings, &fakeDrafter{}, nil).NarrateFindings(t.Context(), request("not-a-uuid"))
	// invalid_argument rather than internal. An operator reading "internal"
	// goes looking at the service; the problem is in the request they sent.
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code is %v, want invalid_argument", connect.CodeOf(err))
	}
}

// TestTheModelIsToldWhyTheObligationWasRaised is ENT-248's first half.
//
// Two live narrations on the 2B tier stated the law wrongly beside a citation
// that resolved. Neither had been told why the Watcher thought the obligation
// applied, so both worked it out: one asserted the obligation binds every
// controller regardless of size, the other reasoned from a missing record to a
// headcount exemption. A model with no grounds invents grounds.
//
// Asserted here rather than only in `domain/corpus` because the wiring is the
// part that goes missing. `AppliesBecause` returning good sentences that nobody
// puts in the request is a passing unit test and an unchanged product.
func TestTheModelIsToldWhyTheObligationWasRaised(t *testing.T) {
	drafter := &fakeDrafter{response: &platformv1.DraftNarrativeResponse{
		Outcome:   platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED,
		Narrative: "text",
	}}

	if _, err := New(newFindings(finding()), drafter, nil).NarrateFindings(
		t.Context(), request(org),
	); err != nil {
		t.Fatalf("narrating: %v", err)
	}

	if len(drafter.context) != 1 {
		t.Fatalf("offered %d obligations, want one", len(drafter.context))
	}
	grounds := drafter.context[0].GetAppliesBecause()
	if len(grounds) == 0 {
		t.Fatal("the model was given no grounds, so it will supply its own")
	}

	// Facts about the organisation, not statements of law. The claim critic on
	// the far side refuses a narrative that states the law, so handing the
	// model a statement of law to paraphrase would produce a refusal on every
	// run for this obligation.
	for _, ground := range grounds {
		for _, legal := range []string{"Article", "must maintain"} {
			if strings.Contains(ground, legal) {
				t.Errorf("a ground states the law: %q", ground)
			}
		}
	}
}

// TestTheAuthoredSummaryStillReachesTheModelUnchanged guards the other half.
//
// The model is forbidden to state the law, and the corpus summary is where the
// statement comes from. It has to reach the run, verbatim, or the model is
// being asked to explain an obligation whose content it has not been shown, and
// the honest answer to that is a refusal on every finding.
func TestTheAuthoredSummaryStillReachesTheModelUnchanged(t *testing.T) {
	f := finding()
	drafter := &fakeDrafter{response: &platformv1.DraftNarrativeResponse{
		Outcome:   platformv1.DraftOutcome_DRAFT_OUTCOME_SUCCEEDED,
		Narrative: "text",
	}}

	if _, err := New(newFindings(f), drafter, nil).NarrateFindings(
		t.Context(), request(org),
	); err != nil {
		t.Fatalf("narrating: %v", err)
	}

	if got := drafter.context[0].GetSummary(); got != f.ObligationSummary {
		t.Fatalf("the model was given %q, want the stored summary %q",
			got, f.ObligationSummary)
	}
}
