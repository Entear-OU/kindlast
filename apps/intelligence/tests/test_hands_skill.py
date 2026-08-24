"""The Hands skill and its tool loop (ENT-261).

The acceptance criterion this issue exists for is a negative one: the Hands
never decides. A negative is only worth asserting if the assertion can fail, so
the tests below are arranged around making the model TRY, and around what the
run record says afterwards.

Every test here runs in milliseconds with no stack and no model, which is the
property `harness/run.py` set out to keep: a run is a pure function of its
inputs and its collaborators.
"""

from __future__ import annotations

import json
from typing import Any

import pytest
from connectrpc.code import Code

from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.model import Completion
from kindlast_intelligence.harness.prepare import prepare
from kindlast_intelligence.harness.remote import CoreAPIError
from kindlast_intelligence.harness.run import Outcome
from kindlast_intelligence.harness.skill import Skill
from kindlast_intelligence.harness.tools import ToolDispatcher
from kindlast_intelligence.skills import analyst, hands, watcher

# The ROPA register, as core-api's `domain/records.registers` describes it.
# Written out here rather than imported, because Go is on the other side of the
# wire: what this asserts is that the harness behaves correctly given a
# register, and a shared fixture would only prove the two files agree with each
# other.
FIELDS: list[dict[str, Any]] = [
    {
        "name": "name",
        "label": "what the activity is called",
        "required": True,
        "list_valued": False,
        "description": "A short name a colleague would recognise.",
    },
    {
        "name": "purpose",
        "label": "why you process this data",
        "required": True,
        "list_valued": False,
        "description": "What the organisation is trying to achieve.",
    },
    {
        "name": "data_categories",
        "label": "the kinds of personal data involved",
        "required": True,
        "list_valued": True,
        "description": "The categories of personal data.",
    },
    {
        "name": "retention_period",
        "label": "how long you keep it",
        "required": True,
        "list_valued": False,
        "description": "How long the data is kept.",
    },
]

CONTEXT: dict[str, Any] = {
    "register": "processing_activities",
    "register_label": "your Article 30 record of processing activities",
    "finding": {
        "finding_id": "f1",
        "status": "pending",
        "severity": "high",
        "detected": "You have no record of processing activities.",
        "proposed_action": "Create an Article 30 entry for payroll.",
        "action_type": "create_ropa",
        "obligation_slug": "gdpr-art-30-ropa",
        "obligation_title": "Records of processing activities",
        "obligation_summary": "Controllers keep a record of their processing.",
        "citation_label": "GDPR Art. 30(1)",
    },
    "fields": FIELDS,
    "facts": [
        {"key": "industry", "value_json": '"payroll services"', "source": "onboarding"},
        {
            "key": "data_categories",
            "value_json": '["names", "bank details"]',
            "source": "onboarding",
        },
    ],
    "already_proposed": [],
}


class ScriptedModel:
    """Answers with a scripted sequence, one reply per call.

    A fake rather than a mock, for the reason `test_watcher_skill.py` gives:
    what these tests control is the model's ANSWER, and stubbing the transport
    would leave the parsing and the loop untested while looking like coverage.

    It keeps what it was asked, because half of what this skill has to get
    right is what reaches the model: the customer's own text must arrive in a
    user message and never in the system prompt.
    """

    def __init__(self, *replies: Any) -> None:
        self._replies = [r if isinstance(r, str) else json.dumps(r) for r in replies]
        self.seen: list[list[dict[str, str]]] = []
        self.schemas: list[dict[str, Any] | None] = []

    def complete(self, messages, schema=None, max_tokens=800, temperature=0.7):
        self.seen.append(list(messages))
        self.schemas.append(schema)
        if not self._replies:
            raise AssertionError("the loop called the model more times than scripted")
        return Completion(
            content=self._replies.pop(0),
            input_tokens=100,
            cached_input_tokens=0,
            output_tokens=50,
            finish_reason="stop",
        )


class Writer:
    """A stand-in for core-api's PrepareRecord.

    IT CANNOT APPROVE AND IT CANNOT CREATE A RECORD, and that is not a
    simplification of the real thing: `HandsService` has no such RPC, and the
    Python client has no method that would reach one. What it records is what
    was written, so a test asserting a refusal can assert that nothing was.
    """

    def __init__(self, *, refuse: CoreAPIError | None = None) -> None:
        self.written: list[dict[str, Any]] = []
        self._refuse = refuse

    def __call__(self, plan: dict[str, Any]) -> tuple[int, int]:
        if self._refuse is not None:
            raise self._refuse
        self.written.append(plan)
        return len(plan.get("fields") or []), len(plan.get("left_for_you") or [])


def run(model: ScriptedModel, writer: Writer | None = None, **kwargs):
    writer = writer or Writer()
    return (
        prepare(
            context=CONTEXT,
            model=model,
            write_plan=writer,
            model_name="test",
            model_version="0",
            **kwargs,
        ),
        writer,
    )


def a_plan(**overrides: Any) -> dict[str, Any]:
    plan = {
        "explanation": (
            "Approving this adds one entry to your record of processing "
            "activities, for payroll. We can fill in the kinds of data you "
            "hold, and you will need to say how long you keep it."
        ),
        "fields": [
            {
                "name": "data_categories",
                "values": ["names", "bank details"],
                "from_fact": "data_categories",
            }
        ],
        "left_for_you": [
            {
                "name": "retention_period",
                "why": "You have not told us how long you keep payroll records.",
            }
        ],
    }
    plan.update(overrides)
    return plan


# --------------------------------------------------------------------------
# The loop


def test_a_hands_run_that_prepares_a_record_and_stops():
    model = ScriptedModel(
        {"action": "prepare_record", "reason": "we can fill two columns", "plan": a_plan()},
        {"action": "done", "reason": "nothing else to prepare"},
    )
    (run_record, plan), writer = run(model)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert run_record.skill == "hands.prepare"
    assert len(writer.written) == 1
    assert plan is not None
    assert [f["name"] for f in plan.fields] == ["data_categories"]
    assert [item["name"] for item in plan.left_for_you] == ["retention_period"]


def test_the_result_of_a_prepare_comes_back_to_the_model():
    """Without this the loop is a single call with extra steps, and the
    argument for a tool over a returned structure is false."""
    model = ScriptedModel(
        {"action": "prepare_record", "reason": "filling", "plan": a_plan()},
        {"action": "done", "reason": "done"},
    )
    (run_record, _), _ = run(model)

    assert run_record.outcome is Outcome.SUCCEEDED
    second_call = model.seen[1]
    assert any("recorded: the plan now fills 1" in m["content"] for m in second_call)


def test_the_tool_result_says_nothing_was_approved():
    """The model is told what its own call did NOT do.

    Not a guardrail: the guardrail is the allow-list. This is so a model that
    would otherwise reach for a second, decisive step is told the honest state
    of the world after the first, which is that a person still has to decide.
    """
    model = ScriptedModel(
        {"action": "prepare_record", "reason": "filling", "plan": a_plan()},
        {"action": "done", "reason": "done"},
    )
    run(model)

    assert any(
        "Nothing has been approved and no record exists yet" in m["content"]
        for m in model.seen[1]
    )


def test_a_run_that_prepares_nothing_is_a_correct_run():
    """An organisation whose memory supports no column is a real case, and the
    honest outcome is an explanation that fills nothing."""
    model = ScriptedModel({"action": "done", "reason": "nothing here supports a column"})
    (run_record, plan), writer = run(model)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert plan is None
    assert writer.written == []


# --------------------------------------------------------------------------
# Never decides: the acceptance criterion, and the tests that can fail


@pytest.mark.parametrize(
    "tool",
    ["approve_finding", "create_record", "execute_job", "reject_finding"],
    ids=["approve", "create", "execute", "reject"],
)
def test_a_hands_run_asking_to_decide_is_refused(tool):
    """THE ACCEPTANCE CRITERION, and the reason `action` is not a Literal.

    A grammar that could not express `approve_finding` would not refuse a model
    that wanted to approve; it would hide that it wanted to and leave nothing
    in the record. Here the ask reaches `ToolDispatcher`, is refused against
    the allow-list, ends the run, and lands in `agent_runs`.

    PROVEN ABLE TO FAIL by adding the tool name to `hands.ALLOWED_TOOLS` and
    watching this go green on a dispatch instead of a refusal. See
    `test_the_hands_holds_exactly_one_tool` for the half that pins the list.
    """
    model = ScriptedModel({"action": tool, "reason": "this is clearly fine", "plan": a_plan()})
    (run_record, plan), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert tool in run_record.outcome_detail
    assert writer.written == [], "a refused tool must not have written anything"
    assert plan is None
    # AND IT IS IN THE RECORD, which is the half that matters to a customer
    # reading how a run was produced.
    assert [c.tool for c in run_record.tool_calls] == [tool]
    assert run_record.tool_calls[0].refused is True
    assert '"refused": true' in run_record.tool_calls_json()


def test_a_refused_tool_ends_the_run_rather_than_being_retried():
    """§26.3: a model that can discover the allow-list by probing it has been
    handed a way to negotiate with its own guardrail."""
    model = ScriptedModel(
        {"action": "approve_finding", "reason": "trying", "plan": None},
        {"action": "done", "reason": "fine, stopping"},
    )
    (run_record, _), _ = run(model)

    assert run_record.outcome is Outcome.REFUSED
    # The second reply was never asked for.
    assert len(model.seen) == 1


def test_the_hands_holds_exactly_one_tool_and_none_of_them_decides():
    """The allow-list is the guard, so the list itself is worth pinning.

    `test_a_hands_run_asking_to_decide_is_refused` walks a list of tools and
    proves the members. It cannot prove the list, which is the failure mode
    AGENTS.md names: a tool added to ALLOWED_TOOLS would make that test pass by
    dispatching rather than by refusing, and nothing there would notice.
    """
    assert hands.ALLOWED_TOOLS == ("prepare_record",)
    for tool in hands.ALLOWED_TOOLS:
        assert "approve" not in tool
        assert "execute" not in tool
        assert "create" not in tool


def test_wiring_a_tool_the_skill_was_never_granted_fails_at_construction():
    """The other half of the same property, from the harness side.

    A capability the skill was never granted cannot be quietly wired by a
    caller: `ToolDispatcher` refuses at construction rather than at the first
    call, because finding that out mid-run tells you much less about who did
    it.
    """
    with pytest.raises(ValueError, match="approve_finding"):
        ToolDispatcher(
            allowed=hands.ALLOWED_TOOLS,
            tools={"approve_finding": lambda **_: "approved"},
            budget=Budget(),
        )


# --------------------------------------------------------------------------
# A value with nothing behind it is a guess


def test_a_value_from_a_fact_this_run_was_not_shown_is_refused():
    """The citation validator's argument, in another register.

    A fact key that genuinely exists in this organisation's memory and was
    never offered to this run is still a fabrication: the model produced it
    from somewhere other than its context. core-api checks the same key against
    the rows, and that check would wave this one through.
    """
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                fields=[
                    {
                        "name": "purpose",
                        "values": ["Paying people"],
                        "from_fact": "staff_count",
                    }
                ]
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert "staff_count" in run_record.outcome_detail
    assert writer.written == []


def test_a_value_with_no_fact_at_all_is_refused():
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                fields=[
                    {"name": "purpose", "values": ["Paying people"], "from_fact": ""}
                ]
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert "from_fact" in run_record.outcome_detail
    assert writer.written == []


def test_a_column_the_register_does_not_have_is_refused():
    """A plan naming a column that does not exist describes a record that
    cannot exist, and the Executor would read nothing from it."""
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                fields=[
                    {
                        "name": "annual_revenue",
                        "values": ["a lot"],
                        "from_fact": "industry",
                    }
                ]
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert "annual_revenue" in run_record.outcome_detail
    assert writer.written == []


def test_a_single_valued_column_given_several_is_refused_rather_than_joined():
    """Joining would invent a spelling nobody chose, and the record would then
    carry a value no human wrote."""
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                fields=[
                    {
                        "name": "purpose",
                        "values": ["Paying people", "Reporting tax"],
                        "from_fact": "industry",
                    }
                ]
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert "purpose" in run_record.outcome_detail
    assert writer.written == []


def test_a_column_prepared_with_no_value_is_refused():
    """An empty prepared column is worse than an absent one: it occupies a slot
    in the plan, so a person reading it sees the column accounted for."""
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                fields=[{"name": "purpose", "values": [], "from_fact": "industry"}]
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert writer.written == []


def test_a_refused_plan_is_recorded_as_a_refused_tool_call():
    """The refusal happens inside the tool, and `tools.py` records a tool that
    raised (ENT-277). Without that, the one call a customer most needs to see
    would be the one missing from the record."""
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                fields=[
                    {"name": "purpose", "values": ["x"], "from_fact": "invented"}
                ]
            ),
        }
    )
    (run_record, _), _ = run(model)

    assert [c.tool for c in run_record.tool_calls] == ["prepare_record"]
    assert run_record.tool_calls[0].refused is True


# --------------------------------------------------------------------------
# The explanation is prose a customer reads


def test_an_explanation_that_states_the_law_is_refused():
    """ENT-248's failure, arriving through a different skill.

    A Hands run has the authored statement of the law in front of it and is
    told not to restate it. The prompt is not what enforces that: the claim
    critic is.
    """
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                explanation=(
                    "Article 30 requires every controller to keep a record of "
                    "processing activities, regardless of size."
                )
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert writer.written == []


def test_an_explanation_with_an_em_dash_is_refused():
    """House style, and it is enforced rather than requested: this text is
    stored and rendered to a customer."""
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(
                explanation=(
                    "Approving this adds one entry to your record — you "
                    "will still need to say how long you keep the data."
                )
            ),
        }
    )
    (run_record, _), writer = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert writer.written == []


def test_a_refused_explanation_is_not_returned_to_the_caller():
    """A caller handed prose plus a note is a caller that shows the prose."""
    model = ScriptedModel(
        {
            "action": "prepare_record",
            "reason": "filling",
            "plan": a_plan(explanation="Article 30 requires every controller to."),
        }
    )
    (run_record, plan), _ = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert plan is None


# --------------------------------------------------------------------------
# What reaches the model, and what does not


def test_the_organisation_own_words_never_reach_the_system_prompt():
    """`AGENTS.md`: anything a customer typed is data, never instruction. Here
    the finding text and every profile fact are customer-controlled."""
    messages = hands.build_messages(CONTEXT)
    system = next(m for m in messages if m["role"] == "system")["content"]

    assert "payroll services" not in system
    assert "You have no record of processing activities." not in system

    user = next(m for m in messages if m["role"] == "user")["content"]
    assert "payroll services" in user
    assert "<approval_context>" in user


def test_the_register_columns_are_in_the_system_prompt_and_the_facts_are_not():
    """The half that is identical between runs goes first, because prefix
    caching is an exact match."""
    messages = hands.build_messages(CONTEXT)
    system = next(m for m in messages if m["role"] == "system")["content"]

    assert "retention_period" in system
    assert "how long you keep it" in system
    assert "onboarding" not in system


def test_an_empty_section_says_so_rather_than_being_omitted():
    """An omitted section reads as "not supplied", which is a different claim
    from "there are none", and here the difference decides whether a column is
    left because nothing is known or because nothing was sent."""
    rendered = hands.render_context(
        {"finding": CONTEXT["finding"], "fields": FIELDS, "facts": [], "already_proposed": []}
    )

    assert "nothing recorded" in rendered
    assert "Already proposed for this record: nothing" in rendered


def test_what_is_already_proposed_is_shown_so_a_run_adds_rather_than_restates():
    rendered = hands.render_context(
        {
            **CONTEXT,
            "already_proposed": [{"name": "name", "values": ["Payroll"]}],
        }
    )

    assert "Already proposed" in rendered
    assert "name = Payroll" in rendered


def test_the_loop_asks_the_model_for_the_hands_grammar():
    """The seam that was wrong on the Watcher path and that nothing local could
    see (ENT-258): `call_model` used to name one skill's schema inside itself,
    so every run on every path was constrained to the Analyst's grammar."""
    model = ScriptedModel({"action": "done", "reason": "nothing"})
    run(model)

    assert model.schemas[0] == hands.output_schema()
    assert "action" in model.schemas[0]["properties"]


# --------------------------------------------------------------------------
# The record exists whatever happened


def test_a_refusal_from_core_api_is_a_recorded_refusal_and_not_a_crash():
    """ENT-277, which was found on the watch path and would have arrived here
    for four more reasons: core-api refuses this tool for an unknown field, a
    fact the organisation does not hold, a plan arriving after the approval,
    and a finding whose action creates no record."""
    model = ScriptedModel(
        {"action": "prepare_record", "reason": "filling", "plan": a_plan()}
    )
    writer = Writer(
        refuse=CoreAPIError(
            "this finding has been approved and its execution enqueued",
            code=Code.FAILED_PRECONDITION,
        )
    )
    (run_record, plan), _ = run(model, writer)

    assert run_record.outcome is Outcome.REFUSED
    assert "enqueued" in run_record.outcome_detail
    assert plan is None
    assert [c.tool for c in run_record.tool_calls] == ["prepare_record"]
    assert run_record.tool_calls[0].refused is True


def test_core_api_being_unreachable_is_a_failure_and_not_a_refusal():
    """A refusal is a guardrail working and a failure is not. Reporting a
    network problem as REFUSED would put a guardrail's name on it in the column
    a customer reads to decide whether to trust the result."""
    model = ScriptedModel(
        {"action": "prepare_record", "reason": "filling", "plan": a_plan()}
    )
    writer = Writer(refuse=CoreAPIError("connection refused"))
    (run_record, _), _ = run(model, writer)

    assert run_record.outcome is Outcome.FAILED


def test_a_model_that_never_stops_is_refused_by_the_budget():
    model = ScriptedModel(
        *[
            {"action": "prepare_record", "reason": "again", "plan": a_plan()}
            for _ in range(20)
        ]
    )
    (run_record, _), _ = run(model, budget=Budget(max_model_calls=3))

    assert run_record.outcome is Outcome.REFUSED


@pytest.mark.parametrize(
    "context_key",
    ["field name", "fact key", "fields absent", "facts absent"],
)
def test_a_context_missing_a_key_still_leaves_a_run_record(context_key):
    """ENT-277, guarded at the one place in this runner that could reproduce it.

    The offered sets are built BEFORE the try block, so anything they raise
    leaves `prepare` entirely and takes the RPC with it, and no `agent_runs`
    row is written for a run that really happened. That is the one outcome the
    harness must never produce.

    It is not reachable from the RPC path, because `_approval_context` builds
    every key from proto scalars that default to empty. It is reachable from
    any caller assembling the dict itself, which is every test here and will be
    the Temporal activity. So the run must still end as a run.
    """
    context = {
        **CONTEXT,
        "fields": [dict(f) for f in FIELDS],
        "facts": [dict(f) for f in CONTEXT["facts"]],
    }
    if context_key == "field name":
        del context["fields"][0]["name"]
    elif context_key == "fact key":
        del context["facts"][0]["key"]
    elif context_key == "fields absent":
        del context["fields"]
    else:
        del context["facts"]

    model = ScriptedModel({"action": "done", "reason": "nothing to fill"})
    run_record, _ = prepare(
        context=context,
        model=model,
        write_plan=Writer(),
        model_name="test",
        model_version="0",
    )

    assert run_record.outcome in (Outcome.SUCCEEDED, Outcome.REFUSED, Outcome.FAILED)
    assert run_record.skill == "hands.prepare"


def test_a_model_that_answers_something_that_is_not_the_contract_fails():
    model = ScriptedModel({"not": "a step"})
    (run_record, _), _ = run(model)

    assert run_record.outcome is Outcome.FAILED


# --------------------------------------------------------------------------
# The skill contract


@pytest.mark.parametrize(
    "skill", [analyst, watcher, hands], ids=["analyst", "watcher", "hands"]
)
def test_every_skill_satisfies_the_protocol(skill):
    assert isinstance(skill, Skill)
    assert skill.NAME and skill.VERSION
    assert isinstance(skill.ALLOWED_TOOLS, tuple)
    assert skill.output_schema()["type"] == "object"


def test_the_prompt_describes_the_schema_in_words():
    """A grammar is not sent to the model (ENT-235), so the field names have to
    be in the prompt or a small model answers in a shape nothing parses."""
    for field in ("action", "reason", "plan"):
        assert field in hands.SYSTEM_PROMPT


def test_the_skill_version_is_pinned_and_the_name_is_stable():
    """`agent_runs` records which version answered, and a run is only
    reproducible if that means something."""
    assert hands.NAME == "hands.prepare"
    assert hands.VERSION.count(".") == 2
