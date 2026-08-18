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
                "narrative": "x",
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
            {"narrative": "   ", "citations": [], "confident": True}
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
