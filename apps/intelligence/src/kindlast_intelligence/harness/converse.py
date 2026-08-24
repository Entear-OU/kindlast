"""One question about one finding, budgeted, validated, recorded (ENT-270).

# THE SAME RING, IN THE SAME ORDER, ON PURPOSE

Admission, the clock, the model call against its budget, typed output,
citations, then the critics. Not a similar ring: literally the same objects.
`CRITICS` is imported from `run` rather than rebuilt here, `call_model` and
`finish_run` are shared, and the citation validator is the one the caller
constructed from the offered set.

That matters more than it looks. ENT-248 made a single refusing-critic seam an
acceptance criterion because two hand-written call sites is how the second one
ends up with its own excerpt format and its own idea of what a refusal reads
like. A second harness for the conversation would be that failure at a larger
scale: the place a customer's question reaches a model would be guarded by a
copy of the ring rather than by the ring.

# WHAT IS ACTUALLY NEW HERE

One check, and it is the one this surface adds: a question has a length the
harness will accept. `Budget` refuses a run that spends too much, but only after
the call that spent it, because the cost is not knowable beforehand. A person
pasting a novel into a text box would therefore buy a completion before anything
said no. The check runs before the model, and the test asserts the model was
never called.

Everything else is the narrative's ring answering a different question.

# AND WHAT COMES BACK IS ALWAYS AN AgentRun

Success, refusal and failure alike, none of them as an exception. §26.3 makes
refusal what a working guardrail produces, so raising on a spent budget would
report correct behaviour as a crash in the column a customer reads to decide
whether to trust this.
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from pydantic import ValidationError

from ..skills import conversation
from ..skills.conversation import Answer
from .budget import Budget, BudgetExhausted
from .citations import Citation, CitationValidator
from .critics import first_breach
from .model import Completer, ModelError
from .run import CRITICS, AgentRun, Outcome, call_model, finish_run


def answer_question(
    *,
    question: str,
    finding: dict[str, Any],
    obligations: list[dict[str, Any]],
    model: Completer,
    validator: CitationValidator,
    model_name: str,
    model_version: str,
    provider: str = "instance",
    budget: Budget | None = None,
    queued_at: datetime | None = None,
) -> AgentRun:
    """Answer one question about one finding, or refuse.

    `finding` and `obligations` are caller-fetched inputs (§26.2): core-api read
    them through the asker's own transaction, where RLS decided what may be
    read, so nothing in here has to know whose finding this is. What it does
    know is which obligations it was offered, and that is what a citation is
    checked against.
    """
    budget = budget or Budget()
    started = datetime.now(timezone.utc)
    run = AgentRun(
        skill=conversation.NAME,
        skill_version=conversation.VERSION,
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

        # BEFORE THE MODEL, WHICH IS THE POINT OF HAVING IT AT ALL.
        #
        # Recorded as a refusal rather than raised at the caller, so the person
        # who asked gets a run record and a sentence rather than a transport
        # error. A blank question is the same shape of problem: there is nothing
        # to answer, and spending a completion to discover that is worse than
        # saying so.
        refusal = _refuse_question(question)
        if refusal:
            run.outcome = Outcome.REFUSED
            run.outcome_detail = refusal
            return finish_run(run)

        messages = conversation.build_messages(question, finding, obligations)
        completion = call_model(
            model, messages, budget, run, schema=conversation.output_schema()
        )
        parsed = _parse(completion)

        result = validator.validate(
            [Citation(slug=s, claim=parsed.answer) for s in parsed.citations]
        )

        run.resolved_citations = [c.slug for c in result.resolved]
        run.rejected_citations = [
            {"slug": r.citation.slug, "reason": r.reason} for r in result.rejected
        ]

        # ONE BAD CITATION REFUSES THE WHOLE ANSWER, for the reason
        # `draft_narrative` gives: an answer citing one real obligation and one
        # invented one is not partly trustworthy, it is a paragraph somebody
        # checks, finds wrong, and then stops believing the rest of.
        #
        # It is also the control that makes a prompt injection fail. A model
        # that did exactly what an injected instruction asked cites something it
        # was never offered, and nothing about the words of the answer needs to
        # be inspected for that to be caught.
        if not result.ok:
            run.outcome = Outcome.REFUSED
            run.outcome_detail = (
                f"{len(result.rejected)} citation(s) did not resolve: "
                + ", ".join(r.citation.slug for r in result.rejected)
            )
            return finish_run(run)

        breach = first_breach(parsed.answer, CRITICS)
        if breach is not None:
            run.outcome = Outcome.REFUSED
            run.outcome_detail = breach.detail
            run.refused_by = breach.critic
            run.refused_patterns = sorted({b.pattern for b in breach.breaches})
            run.rejected_text = parsed.answer
            return finish_run(run)

        # `narrative` is the field on AgentRun that carries a run's free text,
        # whatever the skill called it. Renaming it for this skill would mean
        # `agent_runs` storing an answer in a column the narrative uses and
        # nothing else, or a second column that is the same thing.
        run.narrative = parsed.answer
        run.outcome = Outcome.SUCCEEDED
        return finish_run(run)

    except BudgetExhausted as exc:
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return finish_run(run)

    except (ModelError, ValidationError, ValueError) as exc:
        run.outcome = Outcome.FAILED
        run.outcome_detail = str(exc)
        return finish_run(run)


def _refuse_question(question: str) -> str:
    """Why this question cannot be put to a model, or an empty string.

    Written for the person who asked, because unlike every other refusal in this
    package somebody is sitting in front of this one waiting for it.
    """
    if not question.strip():
        return "there was no question to answer"
    if len(question) > conversation.MAX_QUESTION_CHARS:
        return (
            f"the question is too long: {len(question)} characters against a "
            f"limit of {conversation.MAX_QUESTION_CHARS}. Ask about one thing "
            "at a time and the Analyst has more room to answer it"
        )
    return ""


def _parse(completion) -> Answer:
    """Turn the response into the contract, or refuse to.

    `finish_reason` before the content, for the reason `run._parse` gives: the
    grammar keeps a truncated response well formed right up to the cut, so a
    length-stopped answer parses cleanly and reads as a short one.
    """
    if completion.finish_reason == "length":
        raise ValueError(
            "the model hit its token limit mid-answer, so the answer is "
            "truncated rather than short"
        )

    return Answer.model_validate_json(completion.content)
