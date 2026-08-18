"""The agent run: one skill, budgeted, validated, recorded (§26.3, ENT-218).

# ACTIVITY-SHAPED, THOUGH TEMPORAL IS NOT HERE YET

`run()` is idempotent given the same input, takes its budget as an argument
rather than reading a clock somewhere global, and produces exactly one
`AgentRun` describing what happened. That is the shape a Temporal activity
needs, so wrapping it at build-order step 8 is mechanical rather than a
rewrite. Nothing here imports Temporal, and nothing should until then.

# WHAT COMES BACK IS ALWAYS AN AgentRun

Success, refusal and failure are all outcomes of the same function, and none of
them is an exception escaping to a caller. §26.3 makes refusal what a working
guardrail produces, so a harness that raised on a spent budget would be
reporting its own correct behaviour as a crash, in the column a customer reads
to decide whether to trust a finding.
"""

from __future__ import annotations

import json
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

from ..skills import analyst
from .budget import Budget, BudgetExhausted
from .citations import Citation, CitationValidator
from .model import Completion, ModelClient, ModelError


class Outcome:
    SUCCEEDED = "succeeded"
    REFUSED = "refused"
    FAILED = "failed"


@dataclass
class ToolCall:
    """One tool invocation, recorded in order."""

    tool: str
    arguments: dict[str, Any]
    # A summary rather than the whole result. The record is for a person to
    # read, and pasting a full corpus row into every run would make the useful
    # part unfindable.
    result_summary: str


@dataclass
class AgentRun:
    """Everything `RecordAgentRun` needs, and nothing it does not."""

    skill: str
    skill_version: str
    model: str
    model_version: str
    outcome: str
    outcome_detail: str = ""
    narrative: str = ""
    tool_calls: list[ToolCall] = field(default_factory=list)
    resolved_citations: list[str] = field(default_factory=list)
    rejected_citations: list[dict[str, str]] = field(default_factory=list)
    input_tokens: int = 0
    cached_input_tokens: int = 0
    output_tokens: int = 0
    queued_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    started_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))
    finished_at: datetime = field(default_factory=lambda: datetime.now(timezone.utc))

    def citations_json(self) -> str:
        return json.dumps(
            {"resolved": self.resolved_citations, "rejected": self.rejected_citations}
        )

    def tool_calls_json(self) -> str:
        return json.dumps(
            [
                {"tool": c.tool, "args": c.arguments, "result": c.result_summary}
                for c in self.tool_calls
            ]
        )


def draft_narrative(
    *,
    signal: str,
    obligations: list[dict[str, str]],
    model: ModelClient,
    validator: CitationValidator,
    model_name: str,
    model_version: str,
    budget: Budget | None = None,
    queued_at: datetime | None = None,
) -> AgentRun:
    """Draft one narrative, or refuse.

    The whole guardrail ring in the order it fires: clock, then the model call
    against its budget, then typed output, then citations. Cheapest checks
    first, and the one that can invent a claim last.
    """
    budget = budget or Budget()
    started = datetime.now(timezone.utc)
    run = AgentRun(
        skill=analyst.NAME,
        skill_version=analyst.VERSION,
        model=model_name,
        model_version=model_version,
        outcome=Outcome.FAILED,
        queued_at=queued_at or started,
        started_at=started,
    )

    try:
        budget.check_clock()

        messages = analyst.build_messages(signal, obligations)
        completion = _call_model(model, messages, budget, run)

        # Typed before anything reads a field. §26.3 requires typed output
        # ahead of any IngestService call, and this is where "the model said
        # something" becomes "the model said this".
        parsed = _parse(completion)

        raw_citations = [
            Citation(slug=str(s), claim=parsed.get("narrative", ""))
            for s in parsed.get("citations", [])
        ]
        result = validator.validate(raw_citations)

        run.resolved_citations = [c.slug for c in result.resolved]
        run.rejected_citations = [
            {"slug": c.slug, "reason": reason} for c, reason in result.rejected
        ]

        # ONE BAD CITATION REFUSES THE WHOLE NARRATIVE.
        #
        # Not "keep the good ones". A narrative citing one real obligation and
        # one invented one is not partially trustworthy: it is a document a
        # customer checks, finds wrong, and then stops believing the rest of.
        # `AGENTS.md` calls a fabricated citation worse than nothing, and the
        # cheapest way to honour that is to refuse rather than to curate.
        if not result.ok:
            run.outcome = Outcome.REFUSED
            run.outcome_detail = (
                f"{len(result.rejected)} citation(s) did not resolve: "
                + ", ".join(c.slug for c, _ in result.rejected)
            )
            return _finish(run, budget)

        run.narrative = parsed["narrative"]
        run.outcome = Outcome.SUCCEEDED
        return _finish(run, budget)

    except BudgetExhausted as exc:
        # The guardrail worked. Refusal, not failure.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _finish(run, budget)

    except (ModelError, ValueError) as exc:
        # Something went wrong that was nobody's policy: the endpoint was
        # unreachable, or answered something that is not the contract.
        run.outcome = Outcome.FAILED
        run.outcome_detail = str(exc)
        return _finish(run, budget)


def _call_model(
    model: ModelClient, messages: list[dict[str, str]], budget: Budget, run: AgentRun
) -> Completion:
    completion = model.complete(messages, schema=analyst.OUTPUT_SCHEMA)

    # Charged after the call, because the cost is not knowable before it. This
    # raises BudgetExhausted when the run has now spent too much, which stops
    # the NEXT call rather than un-making this one.
    budget.spend_model_call(completion.total_tokens)

    run.input_tokens += completion.input_tokens
    run.cached_input_tokens += completion.cached_input_tokens
    run.output_tokens += completion.output_tokens
    return completion


def _parse(completion: Completion) -> dict[str, Any]:
    """Turn the response into the contract, or refuse to.

    `finish_reason` is checked before the content, because a truncated response
    can still be parseable: the grammar keeps it well-formed right up to the
    cut, so a length-stopped answer looks like a short one. Reading it as a
    complete narrative would store half a sentence as a finished claim.
    """
    if completion.finish_reason == "length":
        raise ValueError(
            "the model hit its token limit mid-answer, so the narrative is "
            "truncated rather than short"
        )

    try:
        parsed = json.loads(completion.content)
    except json.JSONDecodeError as exc:
        raise ValueError(f"the model did not return JSON: {exc}") from exc

    if not isinstance(parsed, dict):
        raise ValueError(f"expected an object, got {type(parsed).__name__}")
    if not isinstance(parsed.get("narrative"), str) or not parsed["narrative"].strip():
        raise ValueError("narrative is missing or empty")
    if not isinstance(parsed.get("citations"), list):
        raise ValueError("citations is missing or not a list")

    return parsed


def _finish(run: AgentRun, budget: Budget) -> AgentRun:
    run.finished_at = datetime.now(timezone.utc)
    # Latency comes off the run's own timestamps rather than the budget's
    # monotonic clock, because those are the numbers `agent_runs` stores and
    # two sources for one measurement is two things to disagree.
    _ = budget.elapsed_seconds
    return run


def _now_monotonic() -> float:
    return time.monotonic()
