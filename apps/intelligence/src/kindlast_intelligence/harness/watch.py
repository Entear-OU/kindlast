"""The Watcher run: a step loop with a tool in it (ENT-258, §26.3).

`run.py` drafts: one model call, parse, check, done. This one decides, which
means several calls with an effect between them, and the effect changes what
the next call is told.

# WHAT IS SHARED WITH run.py AND WHAT IS NOT

Shared: `AgentRun`, the outcomes, the budget, and the rule that success,
refusal and failure are all outcomes of the same function rather than
exceptions escaping to a caller. §26.3 makes refusal what a working guardrail
produces, so a harness that raised on a spent budget would be reporting its own
correct behaviour as a crash in the column a customer reads to decide whether
to trust a finding.

Not shared: the critics and the narrative-shaped parsing. A signal's title is
not prose a customer reads as an explanation of the law, so the ClaimCritic has
nothing to say about it, and pointing it at a title would refuse the word
"Article" appearing in a heading somebody wrote.

# THE SIDE EFFECTS ARE REAL BEFORE THE RUN ENDS

A raise happens during the loop, so a run that is refused three steps in has
already written whatever it wrote in steps one and two. That is not a leak: a
signal is deduplicated, reversible by a person, and cheaper to have raised than
to have missed. But it is why `watch` returns what it raised alongside the run,
and why the response reports both. A caller told only "REFUSED" would be told
less than what happened.
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any, Callable

from pydantic import BaseModel, ConfigDict, ValidationError

from ..skills import watcher
from ..skills.watcher import Step
from .budget import Budget, BudgetExhausted
from .citations import Citation, CitationValidator
from .model import Completer, ModelError
from .run import AgentRun, Outcome, ToolCall, call_model, finish_run
from .tools import ToolDispatcher, ToolRefused

# The one action that is not a tool. Everything else the model names is looked
# up in the allow-list, which is what makes a request for `create_finding` a
# recorded refusal rather than a parse error.
DONE = "done"

RAISE_SIGNAL = "raise_signal"


class RaisedSignal(BaseModel):
    """One signal this run put through `RaiseSignal`."""

    model_config = ConfigDict(frozen=True, extra="forbid")

    signal_id: str
    dedup_key: str
    title: str
    severity: str
    # False when the signal already existed and this run touched it rather than
    # created it. Reported either way: a run that raised nothing NEW because
    # everything it noticed was already open is a run that worked, and a
    # response listing only new rows could not be told from one where the model
    # noticed nothing.
    raised: bool


# What the caller must provide to actually write a signal. Returns the row's id
# and whether it was new, which is what core-api's RaiseSignal answers.
SignalWriter = Callable[[dict[str, Any]], tuple[str, bool]]


def watch(
    *,
    context: dict[str, Any],
    model: Completer,
    write_signal: SignalWriter,
    validator: CitationValidator,
    model_name: str,
    model_version: str,
    provider: str = "instance",
    budget: Budget | None = None,
    queued_at: datetime | None = None,
) -> tuple[AgentRun, list[RaisedSignal]]:
    """Watch one organisation, or refuse.

    Admission first, for the reason `draft_narrative` gives: a run dispatched
    after a long wait is one whose asker has probably given up, and holding a
    slot that belongs to somebody still waiting is worse than refusing.
    """
    budget = budget or Budget()
    started = datetime.now(timezone.utc)
    run = AgentRun(
        skill=watcher.NAME,
        skill_version=watcher.VERSION,
        model=model_name,
        model_version=model_version,
        provider=provider,
        outcome=Outcome.FAILED,
        queued_at=queued_at or started,
        started_at=started,
    )
    raised: list[RaisedSignal] = []

    dispatcher = ToolDispatcher(
        allowed=watcher.ALLOWED_TOOLS,
        tools={RAISE_SIGNAL: _writer_tool(write_signal, validator, raised)},
        budget=budget,
    )

    try:
        budget.admit(queued_at=queued_at)
        budget.check_clock()

        messages = watcher.build_messages(context)

        while True:
            completion = call_model(
                model, messages, budget, run, schema=watcher.output_schema()
            )
            if completion.finish_reason == "length":
                raise ValueError(
                    "the model hit its token limit mid-step, so the decision is "
                    "truncated rather than short"
                )
            step = Step.model_validate_json(completion.content)

            if step.action == DONE:
                run.outcome = Outcome.SUCCEEDED
                run.outcome_detail = step.reason
                return _record_calls(run, dispatcher), raised

            # ANYTHING ELSE GOES TO THE DISPATCHER, INCLUDING NONSENSE.
            #
            # Not checked against the allow-list here first. `ToolDispatcher`
            # is the one place that decides, and a second check in front of it
            # is a second place to get it wrong and a place where a refusal
            # could happen without being recorded.
            result = dispatcher.dispatch(step.action, **_arguments(step))

            # WHAT HAPPENED IS FED BACK, WHICH IS THE POINT OF A LOOP.
            #
            # The model's own step goes back as the assistant turn so the
            # conversation is the one it actually had, and the result as a user
            # turn because there is no tool role on this transport. Told it was
            # a repeat, a working model moves on instead of raising it again.
            messages = messages + [
                {"role": "assistant", "content": completion.content},
                {"role": "user", "content": f"Result: {result}\n\nDecide again."},
            ]

    except ToolRefused as exc:
        # The guardrail worked. §26.3: refusal, not failure, and NOT retried:
        # a model that can discover the allow-list by probing it has been
        # handed a way to negotiate with its own guardrail.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), raised

    except _CitationRefused as exc:
        # Ends the run rather than skipping the step, for the same reason a
        # refused tool does. Letting the model try another slug is letting it
        # search the offered set by trial, and `AGENTS.md` calls a fabricated
        # citation worse than nothing delivered.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        run.rejected_citations = exc.rejected
        return _record_calls(run, dispatcher), raised

    except BudgetExhausted as exc:
        # Also the guardrail working. A run that raised two signals and then
        # ran out of model calls is REFUSED and reports both, because the
        # signals are written and saying otherwise would misdescribe the run.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), raised

    except (ModelError, ValidationError, ValueError) as exc:
        # Nobody's policy: the endpoint was unreachable, or answered something
        # that is not the contract.
        run.outcome = Outcome.FAILED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), raised


class _CitationRefused(Exception):
    """A step cited an obligation this run was not offered."""

    def __init__(self, rejected: list[dict[str, str]]) -> None:
        super().__init__(
            "citation(s) did not resolve: "
            + ", ".join(r["slug"] for r in rejected)
        )
        self.rejected = rejected


def _writer_tool(
    write_signal: SignalWriter,
    validator: CitationValidator,
    raised: list[RaisedSignal],
) -> Callable[..., str]:
    """The `raise_signal` tool: validate the citation, then write.

    The citation is checked against what this run was OFFERED rather than
    against the corpus, which is stronger than it looks and is the argument
    `coreapi.py` makes at length: a slug that genuinely exists and was never
    offered is still a fabrication, because the model produced it from
    somewhere other than its context.

    core-api checks the slug against the corpus as well, on the far side. Both
    are wanted. That check is the invariant (no signal cites a slug that is not
    a real obligation) and this one is the guardrail (this run cites nothing it
    was not shown), and they refuse different things.
    """

    def raise_signal(**arguments: object) -> str:
        slug = str(arguments.get("obligation_slug") or "")
        if slug:
            result = validator.validate(
                [Citation(slug=slug, claim=str(arguments.get("title") or ""))]
            )
            if not result.ok:
                raise _CitationRefused(
                    [
                        {"slug": r.citation.slug, "reason": r.reason}
                        for r in result.rejected
                    ]
                )

        signal_id, was_new = write_signal(dict(arguments))
        raised.append(
            RaisedSignal(
                signal_id=signal_id,
                dedup_key=str(arguments.get("dedup_key") or ""),
                title=str(arguments.get("title") or ""),
                severity=str(arguments.get("severity") or ""),
                raised=was_new,
            )
        )
        if was_new:
            return f"raised, id {signal_id}"
        return (
            f"already open as {signal_id}: this condition was known, so it was "
            "updated rather than raised again. Do not raise it a third time."
        )

    return raise_signal


def _arguments(step: Step) -> dict[str, object]:
    """A step's tool arguments.

    A step with no `signal` dispatches with none rather than being rejected
    here, so a model naming a tool and forgetting its arguments is refused by
    the allow-list or by the tool, and either way lands in the record.
    """
    if step.signal is None:
        return {}
    return step.signal.model_dump()


def _record_calls(run: AgentRun, dispatcher: ToolDispatcher) -> AgentRun:
    """Copy what the dispatcher saw into the run, refusals included.

    Every dispatch, not only the ones that worked. A record showing only the
    successful calls would describe a better-behaved run than the one that
    happened, and "it asked for something it was not allowed" is exactly what
    somebody reading `agent_runs` wants to see.
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
