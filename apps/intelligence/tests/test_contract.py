"""One definition of the output contract, not two (§26.3, ENT-218).

`Narrative` is the declaration. The grammar the runtime constrains decoding
with is generated from it, and the reply is parsed by it. These assert that
nothing has grown a second description of the same thing.

The first draft of the skill had a hand-written schema dict AND a hand-written
parser checking the same fields again. Two descriptions of one contract with
nothing keeping them in step: add a field to the schema and forget the parser,
and the model dutifully fills in something nobody reads; do the reverse and the
parser demands a field the grammar never asked for. Neither shows up as an
error, which is why these are tests rather than a comment.
"""

from __future__ import annotations

import pytest
from pydantic import ValidationError

from kindlast_intelligence.skills import analyst


def test_the_grammar_and_the_parser_come_from_one_declaration():
    schema = analyst.output_schema()

    assert set(schema["required"]) == set(analyst.Narrative.model_fields)
    assert schema["additionalProperties"] is False


def test_an_unexpected_field_is_refused_rather_than_carried():
    """`extra="forbid"`. A key nobody validates must not ride along into a
    stored finding."""
    with pytest.raises(ValidationError):
        analyst.Narrative.model_validate(
            {
                "why_it_applies_to_you": "x",
                "citations": [],
                "confident": True,
                "severity": "critical",
            }
        )


def test_a_whitespace_only_narrative_is_empty():
    """`min_length=1` alone accepts three spaces, because three spaces are
    three characters.

    Found by an existing test going red when the hand-rolled parser was
    replaced with pydantic, which is the only reason `str_strip_whitespace` is
    set. Worth keeping as its own case, because the guardrail test that caught
    it asserts an outcome rather than this rule.
    """
    with pytest.raises(ValidationError):
        analyst.Narrative.model_validate(
            {"why_it_applies_to_you": "   ", "citations": [], "confident": True}
        )


def test_the_schema_does_not_ship_our_design_notes_to_the_model():
    """Pydantic uses the class docstring as the schema `description`, and the
    schema goes over the wire.

    A docstring explaining implementation choices would be sent to a 4B on
    every call as though it were guidance about what to write. Found by
    generating the schema and reading it, which is worth doing once whenever
    the model changes. The rationale lives in comments beside the class for
    that reason, and this fails if somebody moves it back into the docstring.
    """
    description = analyst.output_schema().get("description", "")

    assert len(description) < 200, (
        "the model's schema description has grown into design notes; keep the "
        "rationale in comments beside the class"
    )
    for leaked in ("pydantic", "load-bearing", "configdict"):
        assert leaked not in description.lower()


# --- The validator checks the offered set, not the corpus ------------------


def test_a_real_obligation_that_was_not_offered_is_still_refused():
    """The finding that came out of hitting a scope wall.

    The first validator asked core-api whether a slug named a stored
    obligation. That failed for a mundane reason, `corpus:read` being a
    tenant-facing human scope this service does not hold, and the mundane
    failure exposed a better design.

    Checking the corpus would ACCEPT a citation to an obligation that genuinely
    exists but was never shown to this run. That is still a fabrication: the
    model produced it from somewhere other than its context, which is exactly
    what the validator exists to catch.

    The system prompt says "the obligations you may cite, and no others".
    Validating against the offered set is what enforces that sentence.
    """
    from kindlast_intelligence.harness.citations import (
        Citation,
        CitationValidator,
        OfferedObligations,
    )

    offered = [{"slug": "gdpr-art-30-ropa", "title": "t", "summary": "s"}]
    validator = CitationValidator(OfferedObligations(offered))

    # A real obligation in the shipped corpus, and not one this run was shown.
    result = validator.validate([Citation(slug="gdpr-art-28-processor-contracts")])

    assert not result.ok
    assert "not among the obligations" in result.rejected[0].reason


# --- The output splits, so the model is never asked to state the law -------


def test_the_free_text_field_is_named_and_described_as_being_about_the_org():
    """ENT-248's structural half, asserted on the contract itself.

    The field is what the model is asked for, so a field called "narrative"
    gets a narrative and a narrative contains whatever the model thinks belongs
    in one. Renaming it and describing it is not a control, and this test is
    not pretending it is: the control is `harness/claims.py`. This is the
    property that makes the control rarely have to fire.

    The description is checked because it is the only part of the schema a
    runtime may send to the model, and because deleting it would break no shape
    test. Both halves would then silently be gone.
    """
    schema = analyst.output_schema()
    fields = schema["properties"]

    assert "narrative" not in fields, (
        "the free-text field is scoped to this organisation, and a field named "
        "narrative invites a narrative"
    )

    described = fields["why_it_applies_to_you"]["description"].lower()

    assert "this organisation" in described
    for promised in ("never state what the law requires", "article", "exemption"):
        assert promised in described, (
            f"the schema description no longer tells the model {promised!r}; "
            "the grammar is not injected into the prompt, so this string and "
            "the system prompt are the only two places the narrowing is said"
        )


def test_the_prompt_tells_the_model_the_law_is_not_its_to_state():
    """The same narrowing in the channel that is definitely sent.

    ENT-235 measured that llama.cpp constrains decoding with the schema and
    does NOT put it in the prompt. So the system prompt carries the narrowing
    too, and this fails if somebody trims it back to the old wording.
    """
    prompt = analyst.SYSTEM_PROMPT.lower()

    assert "you do not state the law" in prompt
    assert "exemptions, exceptions or thresholds" in prompt
