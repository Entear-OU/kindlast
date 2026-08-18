"""The tool-dispatch seam (§26.2, §26.3, ENT-218).

# EXERCISED BY A TEST SKILL, NOT BY THE ANALYST

The Analyst v0 declares inputs and no tools, so nothing here runs for it. That
is the design rather than a gap: inputs are fetched by the caller and passed
in, tools are what the model decides to call mid-loop, and the Analyst needs
only the first.

The seam exists so the skills that will need it, the Watcher and the rail, do
not arrive requiring the loop to be redesigned. It is tested against a skill
invented for the purpose, because exercising it through the Analyst would mean
giving the Analyst a tool it does not use, which is how a guardrail ends up
protecting something nobody does.
"""

from __future__ import annotations

import pytest

from kindlast_intelligence.harness.budget import Budget, BudgetExhausted
from kindlast_intelligence.harness.tools import ToolDispatcher, ToolRefused
from kindlast_intelligence.skills import analyst


# A skill that exists only here.
ALLOWED = ("get_obligation", "list_recent_findings")


def a_dispatcher(budget: Budget | None = None, **tools):
    return ToolDispatcher(
        allowed=ALLOWED,
        tools=tools or {"get_obligation": lambda slug: f"obligation {slug}"},
        budget=budget or Budget(),
    )


def test_an_allowed_tool_runs_and_is_recorded():
    dispatcher = a_dispatcher()

    result = dispatcher.dispatch("get_obligation", slug="gdpr-art-30-ropa")

    assert "gdpr-art-30-ropa" in result
    assert len(dispatcher.calls) == 1
    assert dispatcher.calls[0].tool == "get_obligation"
    assert dispatcher.calls[0].refused is False


def test_a_tool_outside_the_allow_list_is_refused():
    """§26.3: refused, never retried.

    A model that can discover the allow-list by probing it has been handed a
    way to negotiate with its own guardrail.
    """
    dispatcher = a_dispatcher()

    with pytest.raises(ToolRefused) as exc:
        dispatcher.dispatch("read_file", path="/etc/passwd")

    assert exc.value.tool == "read_file"


def test_a_refused_call_is_still_recorded():
    """A record showing only successful calls describes a better-behaved run
    than the one that happened.

    "It asked for something it was not allowed" is exactly the kind of thing a
    customer reading how a finding was produced would want to see.
    """
    dispatcher = a_dispatcher()

    with pytest.raises(ToolRefused):
        dispatcher.dispatch("read_file", path="/etc/passwd")

    assert len(dispatcher.calls) == 1
    assert dispatcher.calls[0].refused is True
    assert dispatcher.calls[0].tool == "read_file"


def test_a_refused_call_does_not_spend_the_tool_budget():
    """Allow-list before budget.

    Charging for a refusal would let a model exhaust a well-behaved run's
    budget by asking for things it cannot have.
    """
    budget = Budget(max_tool_calls=1)
    dispatcher = a_dispatcher(budget)

    with pytest.raises(ToolRefused):
        dispatcher.dispatch("read_file")

    # The one call it was entitled to is still available.
    assert dispatcher.dispatch("get_obligation", slug="x")
    assert budget.tool_calls == 1


def test_the_tool_call_limit_fires_through_the_dispatcher():
    budget = Budget(max_tool_calls=1)
    dispatcher = a_dispatcher(budget)

    dispatcher.dispatch("get_obligation", slug="one")

    with pytest.raises(BudgetExhausted) as exc:
        dispatcher.dispatch("get_obligation", slug="two")
    assert exc.value.limit == "tool_calls"


def test_a_tool_allowed_but_not_wired_is_refused_distinctly():
    """A different fault from an unlisted tool, and the skill did nothing
    wrong."""
    dispatcher = a_dispatcher()

    with pytest.raises(ToolRefused):
        dispatcher.dispatch("list_recent_findings")

    assert "not implemented" in dispatcher.calls[0].result_summary


def test_wiring_a_tool_the_skill_may_not_use_fails_at_construction():
    """A capability the skill was never granted is a configuration mistake.

    Failing at construction rather than at the first call, because finding out
    mid-run tells you much less about who wired it.
    """
    with pytest.raises(ValueError):
        ToolDispatcher(
            allowed=("get_obligation",),
            tools={"get_obligation": lambda: "", "read_file": lambda: ""},
            budget=Budget(),
        )


def test_a_long_result_is_summarised_not_stored_whole():
    """The record is for a person to read.

    It also stops a tool response being a place to smuggle a large payload
    into a record nobody looks at closely.
    """
    dispatcher = a_dispatcher(
        None, **{"get_obligation": lambda: "x" * 5000}
    )

    dispatcher.dispatch("get_obligation")

    assert len(dispatcher.calls[0].result_summary) < 600


def test_the_analyst_declares_no_tools():
    """Inputs and tools are different things, and the Analyst has only inputs.

    An earlier draft declared `get_obligation` here, which named a tool the
    skill never called and invited the loop to fetch its own inputs. That would
    have made the run impure and its tests need a stack.
    """
    assert analyst.ALLOWED_TOOLS == ()
