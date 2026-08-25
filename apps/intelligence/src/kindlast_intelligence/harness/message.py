"""The Messenger run: a step loop whose one tool hands a draft to the dispatch
path and cannot send it (ENT-260).

`prepare.py` is the model for this and most of the shape is shared with it on
purpose: one skill, one allow-list, one dispatcher, budgets, and success,
refusal and failure as outcomes of the same function rather than exceptions
escaping to a caller.

# WHAT IS DIFFERENT, AND IT IS THE WHOLE ISSUE

Everything the other three skills write is read on a page, behind the reader's
own sign-in. This writes what arrives in somebody's mailbox or chat. So the
question the issue's title asks, can it send any other way, has to be answered
in code, and it is answered in four places, none of which is a prompt.

  the allow-list      one entry, `queue_message`. `send_email`,
                      `send_telegram`, `deliver_now` and anything else reach
                      `ToolDispatcher`, are refused, are recorded, and end the
                      run. See `skills/messenger.py` for why the grammar lets
                      the model ask.

  the process         this service holds no SMTP client, no Telegram token and
                      no Slack token, and can obtain none:
                      `test_no_third_party_credential.py` asserts it over the
                      whole package and `test_no_database.py` asserts there is
                      no handle to look one up with.

  the queue           `queue_message` hands a subject and a body to core-api,
                      which puts them on the message it had already decided to
                      send. Who hears about it and where comes from
                      memberships and `notification_preferences` at delivery
                      time, and `notify.RouteFor` answers a linked but
                      unverified Telegram chat with the remaining channel or
                      with nowhere. A run cannot reach any of that.

  the critics         below, and the first of them is the one this skill
                      exists to have.

# THE RING, AND WHY LINKS COME BEFORE EVERYTHING

`run.CRITICS` orders by how badly a customer is served: a fabricated citation,
then a false statement of law, then typography. That order was written for copy
read on a page beside the finding it is about, and outbound mail moves one
thing to the front of it.

A link in a message we send is a link a recipient has every reason to click,
from a product they trust, under our From: header. That is a worse day for them
than a sentence about Article 30 that is wrong, which they can check against
the finding the message points at. So `LinkCritic` runs first, and a draft that
both invents a URL and misstates the law is refused for the URL, because a
record reporting the misstatement would send somebody to fix the smaller thing.

# THE SIDE EFFECT IS REAL BEFORE THE RUN ENDS, AND HERE THAT IS CHEAP

A queue happens during the loop, so a run refused afterwards has already handed
its draft over. That is not a leak. The draft is prose attached to a message
that was going out anyway, it is used only if the run SUCCEEDED (see
`service.run_draft_message`, which withholds it otherwise, exactly as a refused
narrative is withheld), and the template it replaces is still there. But it is
why this returns what it queued alongside the run: a caller told only "REFUSED"
would be told less than what happened.
"""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any, Callable

from pydantic import BaseModel, ConfigDict, ValidationError

from ..skills import messenger
from ..skills.messenger import Step
from .budget import Budget, BudgetExhausted
from .claims import ClaimCritic
from .critics import first_breach
from .links import LinkCritic
from .model import Completer, ModelError
from .prose import ProseCritic
from .remote import CoreAPIError
from .run import AgentRun, Outcome, ToolCall, call_model, finish_run
from .tools import ToolDispatcher, ToolRefused

# The one action that is not a tool. Everything else the model names is looked
# up in the allow-list, which is what makes a request for `send_email` a
# recorded refusal rather than a parse error.
DONE = "done"

QUEUE_MESSAGE = "queue_message"

# See the header for why LinkCritic is first. Instances rather than the module
# functions, so a skill needing a differently configured critic configures one
# instead of forking one.
CRITICS = (LinkCritic(), ClaimCritic(), ProseCritic())

# HOW LONG A DOORBELL MAY BE, AND WHY THERE IS A LIMIT AT ALL.
#
# Not a cost control: the token budget already bounds what a run may generate.
# These bound what a RECIPIENT is handed. A subject longer than this is
# truncated by every mail client into something that reads as broken, and a
# body longer than this is a wall of text in a chat window on a phone, which is
# the channel ENT-263 just added and the one the template serves worst.
#
# Refused rather than trimmed, for the reason every other control here refuses:
# a sentence cut off mid-clause and sent anyway is copy no author wrote.
MAX_SUBJECT = 120
MAX_BODY = 700


class QueuedMessage(BaseModel):
    """The draft this run handed to the dispatch path."""

    model_config = ConfigDict(frozen=True, extra="forbid")

    subject: str
    body: str


# What the caller must provide to actually hand a draft over. Takes the subject
# and the body; returns nothing, because there is nothing a Messenger run is
# entitled to learn about the message afterwards. It does not find out who it
# went to, whether it was held for somebody's quiet hours, or whether it was
# delivered at all: those are the dispatch path's answers and a run that could
# read them would be a run that could probe an organisation's membership one
# draft at a time.
MessageQueue = Callable[[str, str], None]


def draft_message(
    *,
    context: dict[str, Any],
    model: Completer,
    queue_message: MessageQueue,
    model_name: str,
    model_version: str,
    provider: str = "instance",
    budget: Budget | None = None,
    queued_at: datetime | None = None,
) -> tuple[AgentRun, QueuedMessage | None]:
    """Draft one doorbell, or refuse.

    Admission first, for the reason `draft_narrative` gives, and it is sharper
    here than anywhere: a doorbell drafted after a long wait is one whose
    finding has been sitting undecided for that long as well, and the template
    would have gone out immediately. Refusing before `call_model` is what makes
    a slow model cost nothing rather than cost the notification.
    """
    budget = budget or Budget()
    started = datetime.now(timezone.utc)
    run = AgentRun(
        skill=messenger.NAME,
        skill_version=messenger.VERSION,
        model=model_name,
        model_version=model_version,
        provider=provider,
        outcome=Outcome.FAILED,
        queued_at=queued_at or started,
        started_at=started,
    )
    queued: list[QueuedMessage] = []

    dispatcher = ToolDispatcher(
        allowed=messenger.ALLOWED_TOOLS,
        tools={QUEUE_MESSAGE: _queue_tool(queue_message, queued)},
        budget=budget,
    )

    try:
        budget.admit(queued_at=queued_at)
        budget.check_clock()

        messages = messenger.build_messages(context)

        while True:
            completion = call_model(
                model, messages, budget, run, schema=messenger.output_schema()
            )
            if completion.finish_reason == "length":
                raise ValueError(
                    "the model hit its token limit mid-step, so the message is "
                    "truncated rather than short"
                )
            step = Step.model_validate_json(completion.content)

            if step.action == DONE:
                run.outcome = Outcome.SUCCEEDED
                run.outcome_detail = step.reason
                return _record_calls(run, dispatcher), _last(queued)

            # ANYTHING ELSE GOES TO THE DISPATCHER, INCLUDING NONSENSE, and
            # including `send_email`. Not checked against the allow-list here
            # first: `ToolDispatcher` is the one place that decides, and a
            # second check in front of it is a second place to get it wrong and
            # a place where a refusal could happen without being recorded.
            result = dispatcher.dispatch(step.action, **_arguments(step))

            messages = messages + [
                {"role": "assistant", "content": completion.content},
                {"role": "user", "content": f"Result: {result}\n\nDecide again."},
            ]

    except _DraftRefused as exc:
        # A link, a claim about the law, a forbidden dash, an empty field or a
        # message longer than a person will read. The guardrail working, and
        # the run ends rather than the step being skipped: letting the model
        # try again is letting it search for a phrasing that gets past the
        # critic, which is the thing the critic exists to stop.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = exc.detail
        run.refused_by = exc.critic
        run.refused_patterns = exc.patterns
        run.rejected_text = exc.text
        return _record_calls(run, dispatcher), _last(queued)

    except ToolRefused as exc:
        # §26.3: refusal, not failure, and NOT retried. A model that can
        # discover the allow-list by probing it has been handed a way to
        # negotiate with its own guardrail.
        #
        # This is the clause the whole issue turns on. A Messenger run that
        # asked to send ends here, having sent nothing, with the ask written
        # into `agent_runs` for a customer to read.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(queued)

    except BudgetExhausted as exc:
        # Also the guardrail working. A run that queued a draft and then ran
        # out of model calls is REFUSED and reports the draft, because the
        # draft was handed over and saying otherwise would misdescribe the run.
        run.outcome = Outcome.REFUSED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(queued)

    except CoreAPIError as exc:
        # CORE-API SAYING NO IS AN OUTCOME, NOT AN ESCAPE (ENT-277). `watch`
        # was missing this clause and the omission produced the worst result
        # the harness can: a refusal matched no handler, left the runner
        # entirely, took the RPC with it, and no `agent_runs` row was written.
        #
        # Which outcome it is comes from the code rather than from the message.
        # The far side applying a rule is a refusal; the far side failing to
        # answer is a failure. See `CoreAPIError.refused`, which defaults an
        # unclassified error to failure rather than flattering the run.
        run.outcome = Outcome.REFUSED if exc.refused else Outcome.FAILED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(queued)

    except (ModelError, ValidationError, ValueError) as exc:
        # Nobody's policy: the endpoint was unreachable, or answered something
        # that is not the contract.
        run.outcome = Outcome.FAILED
        run.outcome_detail = str(exc)
        return _record_calls(run, dispatcher), _last(queued)


class _DraftRefused(Exception):
    """A draft a recipient must not be sent.

    Carries the critic and the named patterns as well as the sentence, because
    `agent_runs` is read by a maintainer counting how often each control fires
    and by a customer asking why their notification looked ordinary. Parsing
    English out of a detail string to answer the first is the mistake AGENTS.md
    names as one of the reasons decisions left plpgsql.
    """

    def __init__(
        self, detail: str, *, critic: str = "", patterns: list[str] | None = None,
        text: str = "",
    ) -> None:
        super().__init__(detail)
        self.detail = detail
        self.critic = critic
        self.patterns = patterns or []
        self.text = text


def _queue_tool(
    queue_message: MessageQueue, queued: list[QueuedMessage]
) -> Callable[..., str]:
    """The `queue_message` tool: read the draft, criticise it, then hand it
    over.

    In that order, and the order is the ring's. A message carrying a link is
    refused before anything else is considered, because a recipient served a
    well-written notification with somebody's URL in it is worse off than one
    served the template.
    """

    def queue(**arguments: object) -> str:
        subject = str(arguments.get("subject") or "").strip()
        body = str(arguments.get("body") or "").strip()

        # The empty cases first, and separately, because "the model queued
        # nothing" and "the model queued something we refused" are different
        # facts about the run and a customer reading the record should not have
        # to tell them apart from a critic's vocabulary.
        if not subject:
            raise _DraftRefused(
                "the message was queued with no subject, so there is nothing "
                "for a person to read in a list of unread mail"
            )
        if not body:
            raise _DraftRefused(
                "the message was queued with no body, so a recipient would be "
                "told only that something happened and not what to do"
            )
        if len(subject) > MAX_SUBJECT:
            raise _DraftRefused(
                f"the subject is {len(subject)} characters and no more than "
                f"{MAX_SUBJECT} survive a mail client's list view"
            )
        if len(body) > MAX_BODY:
            raise _DraftRefused(
                f"the body is {len(body)} characters and no more than "
                f"{MAX_BODY} is what a doorbell may be; this one is an article"
            )

        # BOTH FIELDS, AND THE SUBJECT FIRST. A link in a subject line is the
        # one a recipient reads without opening anything, and a ring that only
        # ever saw the body would have left the cheapest place to put one
        # unguarded.
        for text in (subject, body):
            breach = first_breach(text, CRITICS)
            if breach is not None:
                raise _DraftRefused(
                    breach.detail,
                    critic=breach.critic,
                    patterns=sorted({b.pattern for b in breach.breaches}),
                    text=text,
                )

        queue_message(subject, body)
        queued.append(QueuedMessage(subject=subject, body=body))
        return (
            "queued. It will go only to people who asked to hear about "
            "findings this serious, only on a channel they verified, and only "
            "when their own settings allow. Nothing has been sent, and you "
            "have no way to send it."
        )

    return queue


def _arguments(step: Step) -> dict[str, object]:
    """A step's tool arguments.

    A step with no `message` dispatches with none rather than being rejected
    here, so a model naming a tool and forgetting its arguments is refused by
    the allow-list or by the tool, and either way lands in the record.
    """
    if step.message is None:
        return {}
    return step.message.model_dump()


def _last(queued: list[QueuedMessage]) -> QueuedMessage | None:
    return queued[-1] if queued else None


def _record_calls(run: AgentRun, dispatcher: ToolDispatcher) -> AgentRun:
    """Copy what the dispatcher saw into the run, refusals included.

    Every dispatch, not only the ones that worked. "It asked to send the
    message itself" is exactly what somebody reading `agent_runs` wants to see,
    and a record showing only the successful calls would describe a
    better-behaved run than the one that happened.
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
