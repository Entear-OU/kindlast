"""The eval gate, and proof that it can go red (ENT-229).

A gate nobody has watched fail is a gate nobody knows is connected, and that is
worse here than elsewhere: this one exists to notice when a guardrail stops
firing, so a gate that always passes would be reporting the guardrails as
working precisely when they are not.

So most of this file breaks something on purpose and asserts the suite reports
it. The breakages are monkeypatched rather than committed, which makes them
permanent proof instead of a paragraph in a pull request saying somebody tried
it once.
"""

from __future__ import annotations

import pytest

from kindlast_intelligence.evals import cases as case_module
from kindlast_intelligence.evals.cases import GUARDRAILS, load_cases
from kindlast_intelligence.evals.gate import (
    Baseline,
    Report,
    TierScores,
    evaluate,
    run_suite,
)
from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.citations import (
    Citation,
    CitationValidator,
    ValidationResult,
)
from kindlast_intelligence.skills import analyst


@pytest.fixture(scope="module")
def golden():
    return load_cases(case_module.default_golden_dir())


@pytest.fixture(scope="module")
def baseline():
    return Baseline.load(case_module.default_baseline_path())


# --- The suite passes on the committed set, so the failures below mean
#     something -------------------------------------------------------------


def test_the_committed_golden_set_passes(golden, baseline):
    report = run_suite(golden)

    assert report.failures == []
    assert evaluate(report, baseline) == []


def test_no_fabrication_survives_the_ring_on_either_tier(golden):
    """The absolute score, not a relative one.

    A regression threshold is the wrong instrument for this number: zero is the
    only acceptable value, because one fabricated citation reaching a stored
    finding is the failure `AGENTS.md` calls worse than nothing.
    """
    report = run_suite(golden)

    for tier, scores in report.tiers.items():
        assert scores.guarded_unsafe == 0, f"{tier} let something through"


# --- Guardrail coverage: deleting a case is as visible as deleting a check --


def test_every_guardrail_in_the_ring_has_at_least_one_case(golden):
    """ENT-229: each control has at least one test that would fail if the
    control were removed.

    The registry is the second half of that property. Without it, deleting a
    guardrail AND its golden case would leave a green suite; with it, the
    deletion has to be made twice, in two files, deliberately.
    """
    covered = {case.guardrail for case in golden if case.guardrail}

    assert covered == set(GUARDRAILS), (
        "every guardrail needs a case and every case needs a guardrail: "
        f"uncovered={set(GUARDRAILS) - covered}, unknown={covered - set(GUARDRAILS)}"
    )


def test_every_case_names_an_owasp_row(golden):
    """The mapping is kept true by the suite rather than by a table in a
    README that nobody edits when the code changes."""
    for case in golden:
        assert case.owasp, f"{case.id} maps to no OWASP row"


# --- Each guardrail, proven able to fail, by breaking it ---------------------


def test_the_gate_fails_when_the_citation_validator_stops_refusing(
    golden, monkeypatch
):
    """The one that matters most, and the reason the harness exists.

    Measured during ENT-235: asked three times which GDPR article requires a
    record of processing activities, the 2B tier answered 50, then 34, then 54.
    The answer is 30 and all three were schema-valid. With the validator
    disabled those become stored findings, and this is what says so.
    """
    monkeypatch.setattr(
        CitationValidator,
        "validate",
        lambda self, citations: ValidationResult(
            resolved=[Citation(slug=c.slug, claim=c.claim) for c in citations]
        ),
    )

    report = run_suite(golden)

    assert report.failures, "a disabled citation validator produced a green suite"
    assert report.tiers["weak"].guarded_unsafe > 0


def test_the_gate_fails_when_the_token_budget_stops_firing(golden, monkeypatch):
    monkeypatch.setattr(Budget, "spend_model_call", lambda self, tokens: None)

    report = run_suite(golden)

    assert any("token_budget" in failure for failure in report.failures)


def test_the_gate_fails_when_the_queue_wait_stops_firing(golden, monkeypatch):
    monkeypatch.setattr(Budget, "admit", lambda self, queued_at=None: None)

    report = run_suite(golden)

    assert any("queue_wait" in failure for failure in report.failures)


def test_the_gate_fails_when_the_truncation_check_stops_firing(golden, monkeypatch):
    """A `finish_reason: length` response parses cleanly, because the grammar
    keeps it well-formed right up to the cut.

    Removing the check therefore breaks no shape test and files half a sentence
    as a finished claim, which is why it needs a case of its own.
    """
    from kindlast_intelligence.harness import run as run_module

    monkeypatch.setattr(
        run_module,
        "_parse",
        lambda completion: analyst.Narrative.model_validate_json(completion.content),
    )

    report = run_suite(golden)

    assert any("truncation" in failure for failure in report.failures)


def test_the_gate_fails_when_customer_text_reaches_the_system_prompt(
    golden, monkeypatch
):
    """LLM01. The control is that a signal is data in a user message, never
    instruction in the system prompt.

    Breaking it here concatenates the signal into the system message, which is
    exactly the shape of the mistake: it looks like giving the model more
    context and it hands an organisation's profile the authority of the prompt.
    """
    original = analyst.build_messages

    def leaky(signal: str, obligations):
        messages = original(signal, obligations)
        messages[0]["content"] += f"\n\nAbout this organisation: {signal}"
        return messages

    monkeypatch.setattr(analyst, "build_messages", leaky)

    report = run_suite(golden)

    assert any("prompt_injection" in failure for failure in report.failures)


# --- The weak-versus-strong metric ------------------------------------------


def test_the_harness_narrows_the_gap_between_the_tiers(golden):
    """ENT-229's harness metric, which is the interesting number.

    Unguarded, the weak tier is measurably worse: it fabricates where the
    strong tier does not. Guarded, both are at zero, so the gap the harness
    leaves is nothing. That difference is what "the harness is carrying the
    small model" means as a number rather than as an opinion.
    """
    report = run_suite(golden)

    assert report.delta.unguarded > 0, (
        "the golden set no longer distinguishes the tiers, so the metric is "
        "measuring nothing"
    )
    assert report.delta.guarded == 0.0
    assert report.delta.narrowed == report.delta.unguarded


def test_the_utility_gap_is_reported_rather_than_hidden(golden):
    """What the harness does NOT close, said out loud.

    The ring makes a weak model safe, not good. It refuses more of the weak
    tier's answers, so fewer runs produce a usable narrative, and that is the
    honest cost of the safety above. Reporting only the safety number would
    make the harness look free.
    """
    report = run_suite(golden)

    assert report.delta.utility > 0, (
        "the tiers now produce equally usable answers, which means either the "
        "golden set stopped distinguishing them or something stopped refusing"
    )


def test_a_change_that_only_helps_the_strong_tier_violates_the_baseline():
    """ENT-229: "a change that only helps strong models is visible".

    Built from synthetic scores rather than from the golden set, because what
    is under test is the comparison, and driving it from real cases would mean
    inventing a change to the harness that has this effect in order to assert
    that the arithmetic notices.
    """
    report = Report(
        tiers={
            "weak": TierScores(cases=10, succeeded=2, refused=8),
            "strong": TierScores(cases=10, succeeded=9, refused=1),
        }
    )
    baseline = Baseline(
        narrowed_at_least=0.0, utility_delta_at_most=0.3, usable_at_least={}
    )

    violations = evaluate(report, baseline)

    assert any("utility" in v for v in violations)


def test_a_harness_that_stops_carrying_the_weak_model_violates_the_baseline():
    report = Report(
        tiers={
            "weak": TierScores(cases=10, succeeded=5, unguarded_unsafe=5, guarded_unsafe=4),
            "strong": TierScores(cases=10, succeeded=9, unguarded_unsafe=1),
        }
    )
    baseline = Baseline(
        narrowed_at_least=0.4, utility_delta_at_most=1.0, usable_at_least={}
    )

    violations = evaluate(report, baseline)

    assert any("narrowed" in v for v in violations)


# --- The gate's own failure modes -------------------------------------------


def test_a_missing_baseline_is_a_failure_rather_than_a_pass(tmp_path):
    """A gate that passes when its threshold file is absent is a gate that
    passes when somebody deletes the threshold file."""
    with pytest.raises(FileNotFoundError):
        Baseline.load(tmp_path / "nothing.json")


def test_a_case_naming_an_unknown_guardrail_is_refused(tmp_path):
    """The registry is only meaningful if it is closed.

    A typo in the guardrail name would otherwise create a case covering
    something that does not exist while leaving the real guardrail uncovered,
    and the coverage assertion above would still be green.
    """
    (tmp_path / "bad.json").write_text(
        '{"id": "x", "guardrail": "not_a_guardrail", "owasp": ["LLM01"], '
        '"why": "y", "signal": "s", "obligations": [{"slug": "a", "title": "t", '
        '"summary": "s"}], "tiers": {"any": {"response": "{}", "expect": "failed"}}}'
    )

    with pytest.raises(ValueError, match="not_a_guardrail"):
        load_cases(tmp_path)


def test_a_prompt_injection_case_that_never_reaches_the_model_is_a_failure(golden):
    """The injection check reads the messages the run actually built, so a case
    refused before the model is called would pass it vacuously.

    Asserted here rather than trusted, because a vacuous pass on the injection
    row is the one failure mode that would look exactly like coverage.
    """
    for case in golden:
        if case.guardrail != "prompt_injection":
            continue
        assert case.queued_seconds_ago == 0.0, (
            f"{case.id} would refuse at admission and never build a prompt"
        )
