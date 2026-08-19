"""The agent run: one skill, budgeted, validated, recorded (§26.3, ENT-218).

# ACTIVITY-SHAPED, THOUGH TEMPORAL IS NOT HERE YET

`draft_narrative` is idempotent given the same input, takes its budget as an
argument rather than reading a clock somewhere global, and produces exactly one
`AgentRun` describing what happened. That is the shape a Temporal activity
needs, so wrapping it at build-order step 8 is mechanical rather than a
rewrite. Nothing here imports Temporal, and nothing should until then.

# WHAT COMES BACK IS ALWAYS AN AgentRun

Success, refusal and failure are all outcomes of the same function, and none of
them is an exception escaping to a caller. §26.3 makes refusal what a working
guardrail produces, so a harness that raised on a spent budget would be
reporting its own correct behaviour as a crash, in the column a customer reads
to decide whether to trust a finding.

# THE MODEL'S OUTPUT IS PARSED BY ITS OWN CONTRACT

There is no hand-written parser here. `Narrative.model_validate_json` is the
same declaration the grammar was generated from, so the thing that constrains
the model and the thing that reads it cannot drift apart. The first draft had
both, written separately, which is the shape of bug this codebase keeps paying
for elsewhere.
"""

from __future__ import annotations

import json
from datetime import datetime, timezone
from enum import StrEnum

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from ..skills import analyst
from ..skills.analyst import Narrative
from .budget import Budget, BudgetExhausted
from .citations import Citation, CitationValidator
from .claims import ClaimCritic
from .critics import Critic, first_breach
from .model import Completion, ModelClient, ModelError
from .prose import ProseCritic

# THE CRITICS, IN THE ORDER THEY FIRE (ENT-248).
#
# One tuple rather than a chain of `if not x.ok` blocks, because ENT-248 makes a
# single refusing-critic seam an acceptance criterion: two hand-written call
# sites is how the second critic ends up with its own excerpt format, its own
# truncation rule and its own idea of what a refusal reads like.
#
# The order is how badly a customer is served by what each one catches. A false
# statement of law is the failure `AGENTS.md` calls worse than nothing delivered
# with a citation that checks out, and an em dash is only wrong. A narrative
# that does both is refused for the claim, because a record reporting the
# typography would send somebody to fix the wrong thing.
#
# Instances rather than the module functions, so a skill whose free-text field
# needs a differently configured critic configures one instead of forking one.
CRITICS: tuple[Critic, ...] = (ClaimCritic(), ProseCritic())


class Outcome(StrEnum):
    """The three ways a run ends.

    An enum rather than strings, so a typo is an error where it is written
    rather than a value the database's check constraint refuses much later.
    """

    SUCCEEDED = "succeeded"
    # A guardrail stopped it. NOT a kind of failure: §26.3 makes this what a
    # working ring produces, and the distinction is the one that matters most
    # for trust.
    REFUSED = "refused"
    FAILED = "failed"


class ToolCall(BaseModel):
    """One tool invocation, recorded in order."""

    model_config = ConfigDict(frozen=True, extra="forbid")

    tool: str
    arguments: dict[str, object] = Field(default_factory=dict)
    # A summary rather than the whole result. The record is for a person to
    # read, and pasting a full corpus row into every run would make the useful
    # part unfindable.
    result_summary: str = ""


class AgentRun(BaseModel):
    """Everything `RecordAgentRun` needs, and nothing it does not."""

    model_config = ConfigDict(extra="forbid")

    skill: str
    skill_version: str
    model: str
    model_version: str

    # WHO SERVED THIS RUN (ENT-236).
    #
    # `instance` for the deployment own endpoint, otherwise the organisation
    # chosen provider. It is a separate field from `model` because the two
    # answer different questions: `model` is what was asked for, and this is
    # whose infrastructure processed the customer text, which is the fact a
    # sub-processor record needs.
    #
    # `instance` rather than `local` as the default, because ENT-235 makes the
    # deployment endpoint configurable and `local` would be a claim this field
    # cannot back.
    provider: str = "instance"

    outcome: Outcome
    outcome_detail: str = ""
    narrative: str = ""

    # WHAT A CRITIC REFUSED, AND WHICH RULE REFUSED IT (ENT-248).
    #
    # Three fields rather than one sentence, because they answer three
    # questions and only the first of them belongs in front of a customer.
    #
    # `outcome_detail` is the short human reason, and it is what the feed shows
    # beside a finding whose narrative was refused. `rejected_text` is the whole
    # field the model wrote, and it is deliberately NOT in `outcome_detail`: a
    # narrative refused for stating the law wrongly would otherwise be printed
    # on the finding page under the heading explaining that it was refused,
    # which is the sentence reaching the customer by a different door.
    #
    # `refused_by` and `refused_patterns` are the critic and the named rules, so
    # a maintainer reading `agent_runs` can count how often each rule fires
    # without parsing English out of a detail string. That is the same mistake
    # the records store made with `check_violation` messages, which AGENTS.md
    # names as one of the reasons decisions moved out of plpgsql.
    #
    # Empty on every outcome except a critic refusal, including a citation
    # refusal: nothing was rejected as prose there, and reporting a pattern that
    # did not fire would make the counts meaningless.
    refused_by: str = ""
    refused_patterns: list[str] = Field(default_factory=list)
    rejected_text: str = ""
    tool_calls: list[ToolCall] = Field(default_factory=list)
    resolved_citations: list[str] = Field(default_factory=list)
    rejected_citations: list[dict[str, str]] = Field(default_factory=list)
    input_tokens: int = Field(default=0, ge=0)
    cached_input_tokens: int = Field(default=0, ge=0)
    output_tokens: int = Field(default=0, ge=0)
    # WHY THREE STAMPS AND NOT A DURATION (ENT-238)
    #
    # `agent_runs` has all three columns, and the reason is that "it took four
    # seconds" cannot explain a customer who watched a spinner for six minutes.
    # Queued to started is capacity. Started to finished is the run. They have
    # different remedies, and a single latency number loses the difference.
    queued_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    started_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))
    finished_at: datetime = Field(default_factory=lambda: datetime.now(timezone.utc))

    @property
    def queue_seconds(self) -> float:
        return (self.started_at - self.queued_at).total_seconds()

    @property
    def work_seconds(self) -> float:
        return (self.finished_at - self.started_at).total_seconds()

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

    def refusal_json(self) -> str:
        """What a critic refused, for `agent_runs.refusal` (ENT-248).

        `{}` when no critic refused, which the column's default already is, so a
        reader can tell "no critic objected" from "a critic objected and we did
        not say what to". An empty object rather than null for the same reason
        `tool_calls` defaults to an empty array: the shape is always the shape.
        """
        if not self.refused_by:
            return "{}"
        return json.dumps(
            {
                "critic": self.refused_by,
                "patterns": self.refused_patterns,
                "text": self.rejected_text,
            }
        )


def draft_narrative(
    *,
    signal: str,
    obligations: list[dict[str, Any]],
    model: ModelClient,
    validator: CitationValidator,
    model_name: str,
    model_version: str,
    provider: str = "instance",
    budget: Budget | None = None,
    queued_at: datetime | None = None,
) -> AgentRun:
    """Draft one narrative, or refuse.

    The whole guardrail ring in the order it fires: admission, then the clock,
    then the model call against its budget, then typed output, then citations,
    then house style. Cheapest checks first, and then in the order of how badly
    a customer is served by what each one catches: a fabricated citation is
    worse than nothing, and an em dash is only wrong.

    Admission is first for a reason that is not tidiness. A run dispatched after
    a long wait is one whose asker has probably given up, and running it anyway
    means holding a slot that belongs to somebody still waiting. Refusing before
    `_call_model` is what makes that true rather than merely recorded, and
    `test_a_run_that_waited_too_long_refuses_before_calling_the_model` asserts
    the model was never called.
    """
    budget = budget or Budget()
    started = datetime.now(timezone.utc)
    run = AgentRun(
        skill=analyst.NAME,
        skill_version=analyst.VERSION,
        model=model_name,
        model_version=model_version,
        provider=provider,
        outcome=Outcome.FAILED,
        queued_at=queued_at or started,
        started_at=started,
    )

    try:
        budget.admit(queued_at=queued_at)
        budget.check_clock()

        messages = analyst.build_messages(signal, obligations)
        completion = _call_model(model, messages, budget, run)
        parsed = _parse(completion)

        result = validator.validate(
            [
                Citation(slug=s, claim=parsed.why_it_applies_to_you)
                for s in parsed.citations
            ]
        )

        run.resolved_citations = [c.slug for c in result.resolved]
        run.rejected_citations = [
            {"slug": r.citation.slug, "reason": r.reason} for r in result.rejected
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
                + ", ".join(r.citation.slug for r in result.rejected)
            )
            return _finish(run)

        # THE CRITICS AFTER THE CITATIONS, BECAUSE A FABRICATED CITATION
        # OUTRANKS EVERYTHING THEY CATCH (ENT-163, ENT-248).
        #
        # They read the free-text field and not the citations. A slug is checked
        # against the offered set, so a dash or a stray article number inside
        # one is already refused by the validator, and this field is the only
        # free prose a customer ever sees.
        #
        # THE RECORD CARRIES THE REJECTED TEXT AS WELL AS THE REASON.
        #
        # ENT-248 asks for both, and the reason is what somebody does next. The
        # detail says which rule fired and quotes the words that fired it, and
        # `rejected_text` holds the whole field, because a customer asking why
        # they have no narrative is entitled to see what was written rather than
        # only that something was. The finding itself keeps the deterministic
        # sentence the Watcher wrote, which is why withholding this costs
        # nothing and showing it costs nothing either.
        breach = first_breach(parsed.why_it_applies_to_you, CRITICS)
        if breach is not None:
            run.outcome = Outcome.REFUSED
            run.outcome_detail = breach.detail
            run.refused_by = breach.critic
            run.refused_patterns = sorted({b.pattern for b in breach.breaches})
            run.rejected_text = parsed.why_it_applies_to_you
            return _finish(run)

        run.narrative = parsed.why_it_applies_to_you
        run.outcome = Outcome.SUCCEEDED
        return _finish(run)

    except BudgetExhausted as exc:
        # The guardrail worked. Refusal, not failure.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _finish(run)

    except (ModelError, ValidationError, ValueError) as exc:
        # Something went wrong that was nobody's policy: the endpoint was
        # unreachable, or answered something that is not the contract.
        run.outcome = Outcome.FAILED
        run.outcome_detail = str(exc)
        return _finish(run)


def _call_model(
    model: ModelClient, messages: list[dict[str, str]], budget: Budget, run: AgentRun
) -> Completion:
    completion = model.complete(messages, schema=analyst.output_schema())

    # Charged after the call, because the cost is not knowable before it. This
    # raises BudgetExhausted when the run has now spent too much, which stops
    # the NEXT call rather than un-making this one.
    budget.spend_model_call(completion.total_tokens)

    # AND THE CLOCK IS CHARGED THE SAME WAY (ENT-238).
    #
    # Checked before the call as well, where it stops work from starting. Here it
    # catches the generation that alone outlasted the budget, which on a
    # saturated box is the normal way to blow it and is invisible to a check that
    # only runs at the loop head.
    #
    # This discards a completed answer, which looks wasteful and is the
    # established stance: `spend_model_call` above already throws away a
    # narrative whose tokens went over. A run that finished after everybody
    # stopped waiting should not be recorded as a success, because the record is
    # what a customer reads to understand what they experienced.
    budget.check_clock()

    run.input_tokens += completion.input_tokens
    run.cached_input_tokens += completion.cached_input_tokens
    run.output_tokens += completion.output_tokens
    return completion


def _parse(completion: Completion) -> Narrative:
    """Turn the response into the contract, or refuse to.

    `finish_reason` is checked before the content, because a truncated response
    can still be parseable: the grammar keeps it well-formed right up to the
    cut, so a length-stopped answer looks like a short one. Reading it as a
    complete narrative would store half a sentence as a finished claim.

    Everything after that is `Narrative`'s own validation, which is the same
    declaration the grammar came from.
    """
    if completion.finish_reason == "length":
        raise ValueError(
            "the model hit its token limit mid-answer, so the narrative is "
            "truncated rather than short"
        )

    return Narrative.model_validate_json(completion.content)


def _finish(run: AgentRun) -> AgentRun:
    run.finished_at = datetime.now(timezone.utc)
    return run
