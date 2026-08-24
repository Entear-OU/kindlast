"""The Hands run: a step loop whose one tool cannot decide anything (ENT-261).

`watch.py` is the model for this and most of the shape is shared with it
deliberately: one skill, one allow-list, one dispatcher, budgets, and success,
refusal and failure as outcomes of the same function rather than exceptions
escaping to a caller.

# WHAT IS DIFFERENT, AND IT IS THE WHOLE ISSUE

A watch decides what is worth raising. This decides nothing. It is shown a
decision a person is about to make and prepares the material for it, and the
one thing it must never be able to do is make any part of that decision.

Three guards, in the order they fire, and none of them is a prompt:

  the allow-list      one tool, `prepare_record`. `approve_finding`,
                      `create_record` and everything else reaches
                      `ToolDispatcher`, is refused, is recorded, and ends the
                      run. There is no core-api RPC that would have helped
                      anyway: approving is `findings:act`, which only a
                      human's token carries, and a register entry is created
                      by `ExecutorService.ExecuteJob` from a job row that
                      exists only because a human approved (00036).

  the offered fields  a column name this run was not shown is refused, the
                      way a citation to a slug this run was not offered is.
                      A register has the columns it has, and a plan naming
                      another describes a record that cannot exist.

  the offered facts   a value naming a fact this run was not shown is
                      refused, and the argument is the citation validator's
                      exactly. A fact that genuinely exists and was never
                      offered is still a fabrication: the model produced the
                      key from somewhere other than its context. core-api
                      checks the same key against the organisation's own rows,
                      which is the invariant; this is the guardrail, and they
                      refuse different things.

# AND THEN THE CRITICS, BECAUSE THE EXPLANATION IS PROSE A CUSTOMER READS

`draft_narrative` runs `ClaimCritic` then `ProseCritic` over the one free-text
field a customer sees. The explanation here is exactly that kind of field, and
the claim critic is not decoration on it: the failure ENT-248 was filed for was
a model that, asked to explain something to an organisation, explained the law
instead and got it wrong. A Hands run has the statement of the law in front of
it and is told not to restate it, and this is what enforces that.

The order is the ring's: what was fabricated outranks what was misstated, and
what was misstated outranks typography.

# THE SIDE EFFECT IS REAL BEFORE THE RUN ENDS

A prepare happens during the loop, so a run refused afterwards has already
written its plan. That is not a leak: a plan is a proposal on a finding, it is
visible beside the decision it informs, it is refused outright once an approval
has been enqueued, and a person editing the record supersedes it. But it is why
`prepare` returns what it wrote alongside the run, and why the response reports
both. A caller told only "REFUSED" would be told less than what happened.
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any, Callable

from pydantic import BaseModel, ConfigDict, ValidationError

from ..skills import hands
from ..skills.hands import Step
from .budget import Budget, BudgetExhausted
from .claims import ClaimCritic
from .critics import first_breach
from .model import Completer, ModelError
from .prose import ProseCritic
from .remote import CoreAPIError
from .run import AgentRun, Outcome, ToolCall, call_model, finish_run
from .tools import ToolDispatcher, ToolRefused

# The one action that is not a tool. Everything else the model names is looked
# up in the allow-list, which is what makes a request for `approve_finding` a
# recorded refusal rather than a parse error.
DONE = "done"

PREPARE_RECORD = "prepare_record"

# The same ring `run.CRITICS` fires, over the same kind of field: the one piece
# of free prose a customer reads. Instances rather than the module functions,
# so a skill needing a differently configured critic configures one instead of
# forking one.
CRITICS = (ClaimCritic(), ProseCritic())


class PreparedPlan(BaseModel):
    """The plan this run put through `PrepareRecord`."""

    model_config = ConfigDict(frozen=True, extra="forbid")

    explanation: str
    # What was filled, and from which fact. Both halves reported, because a
    # value without its source is the thing this skill exists not to produce.
    fields: list[dict[str, Any]]
    # What was left, and why. Reported alongside rather than derived by the
    # caller from what is missing: a caller computing the gap would report a
    # column the run never considered identically to one it considered and
    # could not fill.
    left_for_you: list[dict[str, str]]


# What the caller must provide to actually record a plan. Returns how many
# fields the finding's plan now fills and how many it leaves, which is what
# core-api's PrepareRecord answers.
PlanWriter = Callable[[dict[str, Any]], tuple[int, int]]


def prepare(
    *,
    context: dict[str, Any],
    model: Completer,
    write_plan: PlanWriter,
    model_name: str,
    model_version: str,
    provider: str = "instance",
    budget: Budget | None = None,
    queued_at: datetime | None = None,
) -> tuple[AgentRun, PreparedPlan | None]:
    """Explain one approval and prepare its record, or refuse.

    Admission first, for the reason `draft_narrative` gives: a run dispatched
    after a long wait is one whose asker has probably given up, and holding a
    slot that belongs to somebody still waiting is worse than refusing. It is
    sharper here than anywhere else, because the asker is a person sitting in
    front of a decision panel.
    """
    budget = budget or Budget()
    started = datetime.now(timezone.utc)
    run = AgentRun(
        skill=hands.NAME,
        skill_version=hands.VERSION,
        model=model_name,
        model_version=model_version,
        provider=provider,
        outcome=Outcome.FAILED,
        queued_at=queued_at or started,
        started_at=started,
    )
    written: list[PreparedPlan] = []

    # THE OFFERED SETS, READ WITH `.get` AND NOT WITH `[]` (ENT-277).
    #
    # These are built before the `try` below, so anything they raise leaves
    # `prepare` entirely and takes the RPC with it, and no `agent_runs` row is
    # written for a run that really happened. That is the one outcome the
    # harness must never produce, and it is exactly the shape of the bug
    # ENT-277 was filed for: a `CoreAPIError` matched no handler in `watch`,
    # escaped, and the whole run vanished.
    #
    # A `KeyError` here is not reachable from the RPC path, because
    # `service._approval_context` builds every one of these keys from proto
    # scalars that default to empty. It is reachable from a caller that hands
    # this function a dict it assembled itself, which is every test and will be
    # the Temporal activity. Defaulting costs nothing and removes the class.
    fields = context.get("fields") or []
    facts = context.get("facts") or []
    offered_fields = {str(f.get("name", "")) for f in fields}
    single_valued = {
        str(f.get("name", "")) for f in fields if not f.get("list_valued")
    }
    offered_facts = {str(f.get("key", "")) for f in facts}
    # An empty name is not a field anybody may fill, and letting one into the
    # offered set would make a nameless column pass the check that exists to
    # refuse a column the register does not have.
    offered_fields.discard("")
    single_valued.discard("")
    offered_facts.discard("")

    dispatcher = ToolDispatcher(
        allowed=hands.ALLOWED_TOOLS,
        tools={
            PREPARE_RECORD: _writer_tool(
                write_plan, offered_fields, single_valued, offered_facts, written
            )
        },
        budget=budget,
    )

    try:
        budget.admit(queued_at=queued_at)
        budget.check_clock()

        messages = hands.build_messages(context)

        while True:
            completion = call_model(
                model, messages, budget, run, schema=hands.output_schema()
            )
            if completion.finish_reason == "length":
                raise ValueError(
                    "the model hit its token limit mid-step, so the plan is "
                    "truncated rather than short"
                )
            step = Step.model_validate_json(completion.content)

            if step.action == DONE:
                run.outcome = Outcome.SUCCEEDED
                run.outcome_detail = step.reason
                return _record_calls(run, dispatcher), _last(written)

            # ANYTHING ELSE GOES TO THE DISPATCHER, INCLUDING NONSENSE, and
            # including `approve_finding`. Not checked against the allow-list
            # here first: `ToolDispatcher` is the one place that decides, and a
            # second check in front of it is a second place to get it wrong and
            # a place where a refusal could happen without being recorded.
            result = dispatcher.dispatch(step.action, **_arguments(step))

            messages = messages + [
                {"role": "assistant", "content": completion.content},
                {"role": "user", "content": f"Result: {result}\n\nDecide again."},
            ]

    except _PlanRefused as exc:
        # A field the register does not have, a value with no offered fact
        # behind it, or a single-valued column given several. The guardrail
        # working, and the run ends rather than the step being skipped: letting
        # the model try another key is letting it search the offered set by
        # trial, which is the thing the check exists to stop.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(written)

    except ToolRefused as exc:
        # §26.3: refusal, not failure, and NOT retried. A model that can
        # discover the allow-list by probing it has been handed a way to
        # negotiate with its own guardrail.
        #
        # This is the clause the whole issue turns on. A Hands run that asked
        # to approve ends here, having approved nothing, with the ask written
        # into `agent_runs` for a customer to read.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(written)

    except BudgetExhausted as exc:
        # Also the guardrail working. A run that recorded a plan and then ran
        # out of model calls is REFUSED and reports the plan, because the plan
        # is written and saying otherwise would misdescribe the run.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(written)

    except CoreAPIError as exc:
        # CORE-API SAYING NO IS AN OUTCOME, NOT AN ESCAPE (ENT-277).
        #
        # `watch` was missing this clause and the omission produced the worst
        # result the harness can: a refusal from core-api matched no handler,
        # left the runner entirely, took the RPC with it, and no `agent_runs`
        # row was written. Every run leaves a record a customer can read, and a
        # run that vanished is a run they cannot ask about.
        #
        # It matters more here than there, because core-api refuses this tool
        # for four reasons a well-behaved deployment will actually hit: an
        # unknown field, a fact the organisation does not hold, a plan arriving
        # after the approval, and a finding whose action creates no record.
        #
        # Which outcome it is comes from the code rather than from the message.
        # The far side applying a rule is a refusal; the far side failing to
        # answer is a failure. See `CoreAPIError.refused`, which defaults an
        # unclassified error to failure rather than flattering the run.
        run.outcome = Outcome.REFUSED if exc.refused else Outcome.FAILED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(written)

    except (ModelError, ValidationError, ValueError) as exc:
        # Nobody's policy: the endpoint was unreachable, or answered something
        # that is not the contract.
        run.outcome = Outcome.FAILED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(written)


class _PlanRefused(Exception):
    """A plan named something this run was not offered, or was malformed."""


def _writer_tool(
    write_plan: PlanWriter,
    offered_fields: set[str],
    single_valued: set[str],
    offered_facts: set[str],
    written: list[PreparedPlan],
) -> Callable[..., str]:
    """The `prepare_record` tool: check the plan, criticise the prose, then
    write.

    In that order, and the order is the ring's. A plan that fabricates a source
    is refused before its prose is read, because a customer served a
    well-written record built on an invented fact is worse off than one served
    nothing.
    """

    def prepare_record(**arguments: object) -> str:
        explanation = str(arguments.get("explanation") or "")
        fields = _as_dicts(arguments.get("fields"))
        left = _as_dicts(arguments.get("left_for_you"))

        for field in fields:
            name = str(field.get("name") or "")
            if name not in offered_fields:
                raise _PlanRefused(
                    f"{name!r} is not one of the columns this run was shown "
                    f"({sorted(offered_fields)})"
                )
            values = [str(v) for v in _as_list(field.get("values")) if str(v)]
            if not values:
                raise _PlanRefused(
                    f"{name!r} was prepared with no value; a column nothing "
                    "could fill belongs in left_for_you with a reason"
                )
            if name in single_valued and len(values) > 1:
                raise _PlanRefused(
                    f"{name!r} holds one value and was given {len(values)}"
                )
            source = str(field.get("from_fact") or "")
            if not source:
                raise _PlanRefused(
                    f"{name!r} was filled with no from_fact; a value with no "
                    "source behind it is a guess presented as a fact"
                )
            if source not in offered_facts:
                # THE CITATION VALIDATOR'S ARGUMENT, IN ANOTHER REGISTER. A key
                # that exists in this organisation's memory and was never
                # offered to this run is still a fabrication: the model
                # produced it from somewhere other than its context. core-api
                # checks the same key against the rows, and that check would
                # wave this one through.
                raise _PlanRefused(
                    f"{name!r} was filled from {source!r}, which is not one of "
                    "the facts this run was shown"
                )
            field["values"] = values

        breach = first_breach(explanation, CRITICS)
        if breach is not None:
            raise _PlanRefused(breach.detail)

        filled, remaining = write_plan(
            {
                "explanation": explanation,
                "fields": fields,
                "left_for_you": left,
            }
        )
        written.append(
            PreparedPlan(
                explanation=explanation,
                fields=fields,
                left_for_you=[
                    {
                        "name": str(item.get("name") or ""),
                        "why": str(item.get("why") or ""),
                    }
                    for item in left
                ],
            )
        )
        return (
            f"recorded: the plan now fills {filled} column(s) and leaves "
            f"{remaining} for a person. Nothing has been approved and no record "
            "exists yet."
        )

    return prepare_record


def _arguments(step: Step) -> dict[str, object]:
    """A step's tool arguments.

    A step with no `plan` dispatches with none rather than being rejected here,
    so a model naming a tool and forgetting its arguments is refused by the
    allow-list or by the tool, and either way lands in the record.
    """
    if step.plan is None:
        return {}
    return step.plan.model_dump()


def _as_dicts(value: object) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        return []
    return [dict(item) for item in value if isinstance(item, dict)]


def _as_list(value: object) -> list[Any]:
    if isinstance(value, list):
        return value
    if value is None:
        return []
    return [value]


def _last(written: list[PreparedPlan]) -> PreparedPlan | None:
    return written[-1] if written else None


def _record_calls(run: AgentRun, dispatcher: ToolDispatcher) -> AgentRun:
    """Copy what the dispatcher saw into the run, refusals included.

    Every dispatch, not only the ones that worked. "It asked to approve the
    finding it was explaining" is exactly what somebody reading `agent_runs`
    wants to see, and a record showing only the successful calls would describe
    a better-behaved run than the one that happened.
    """
    run.tool_calls = [
        ToolCall(
            tool=c.tool,
            arguments=c.arguments,
            result_summary=c.result_summary,
            refused=c.refused,
        )
        for c in dispatcher.calls
    ]
    return finish_run(run)
