"""The Messenger skill and its tool loop (ENT-260).

The acceptance criterion this issue exists for is in its title: it drafts, and
it sends only through the dispatch path. That is a negative, and a negative is
only worth asserting if the assertion can fail, so the tests below are arranged
around making the model TRY to send, and around what the run record says
afterwards.

The second half is about what a person is handed. What the Messenger writes
leaves the building, under our From: header, to somebody who did not ask for it
at that moment, so the tests that matter most are the ones that refuse a draft
carrying a link, a claim about the law, or the exposure §17.1 keeps out of a
doorbell.

Every test here runs in milliseconds with no stack and no model, which is the
property `harness/run.py` set out to keep.
"""

from __future__ import annotations

import json
from typing import Any

import pytest
from connectrpc.code import Code

from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.links import LinkCritic, review_links
from kindlast_intelligence.harness.message import (
    MAX_BODY,
    MAX_SUBJECT,
    draft_message,
)
from kindlast_intelligence.harness.model import Completion
from kindlast_intelligence.harness.remote import CoreAPIError
from kindlast_intelligence.harness.run import Outcome
from kindlast_intelligence.harness.skill import Skill
from kindlast_intelligence.harness.tools import ToolDispatcher
from kindlast_intelligence.skills import analyst, conversation, hands, messenger, watcher

# One notification's context, in the shape `service._message_context` builds.
#
# WHAT IS NOT IN IT IS THE POINT OF IT. No detected text, no proposed action, no
# obligation summary and no recipient: §17.1 keeps the first three out of a
# doorbell and the fourth is resolved at delivery time. See
# `skills/messenger.py` for the disagreement between that rule and this issue's
# own description, which is reported rather than resolved.
CONTEXT: dict[str, Any] = {
    "org_name": "Acme Ltd",
    "severity": "high",
    "open_findings": 4,
    "first_for_org": False,
    "has_approve_link": True,
    "channels": ["email", "telegram"],
}


class ScriptedModel:
    """Answers with a scripted sequence, one reply per call.

    A fake rather than a mock, for the reason `test_hands_skill.py` gives: what
    these tests control is the model's ANSWER, and stubbing the transport would
    leave the parsing and the loop untested while looking like coverage.

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


class Queue:
    """A stand-in for core-api taking a draft onto the message it is sending.

    IT CANNOT SEND, AND THAT IS NOT A SIMPLIFICATION OF THE REAL THING. There
    is no RPC on any surface this service can reach that delivers a message,
    the Python client has no method that would call one, and the process holds
    no SMTP client and no bot token (`test_no_third_party_credential.py`). What
    it records is what was handed over, so a test asserting a refusal can
    assert that nothing was.
    """

    def __init__(self, *, refuse: CoreAPIError | None = None) -> None:
        self.queued: list[tuple[str, str]] = []
        self._refuse = refuse

    def __call__(self, subject: str, body: str) -> None:
        if self._refuse is not None:
            raise self._refuse
        self.queued.append((subject, body))


def run(model: ScriptedModel, queue: Queue | None = None, **kwargs):
    queue = queue or Queue()
    return (
        draft_message(
            context=kwargs.pop("context", CONTEXT),
            model=model,
            queue_message=queue,
            model_name="test",
            model_version="0",
            **kwargs,
        ),
        queue,
    )


def a_message(**overrides: Any) -> dict[str, Any]:
    message = {
        "subject": "A high severity finding is waiting in Acme Ltd",
        "body": (
            "Something in Acme Ltd's compliance record needs a decision from "
            "you, and it is the most serious of the five now open. You can "
            "open it to read what was found, or approve it straight from this "
            "message if you already know what you want to do."
        ),
    }
    message.update(overrides)
    return message


# --------------------------------------------------------------------------
# The loop


def test_a_messenger_run_that_queues_a_message_and_stops():
    model = ScriptedModel(
        {"action": "queue_message", "reason": "it is ready", "message": a_message()},
        {"action": "done", "reason": "queued"},
    )
    (run_record, queued), queue = run(model)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert len(queue.queued) == 1
    assert queued is not None
    assert queued.subject == "A high severity finding is waiting in Acme Ltd"
    assert [c.tool for c in run_record.tool_calls] == ["queue_message"]
    assert run_record.skill == "messenger.draft"


def test_the_result_of_a_queue_comes_back_to_the_model():
    model = ScriptedModel(
        {"action": "queue_message", "reason": "ready", "message": a_message()},
        {"action": "done", "reason": "queued"},
    )
    run(model)

    # The loop is a loop: the second call sees its own step and what came of
    # it, or the model is being asked to decide again with no idea what it did.
    last = model.seen[-1]
    assert last[-2]["role"] == "assistant"
    assert "queued" in last[-1]["content"]


def test_the_tool_result_says_the_run_cannot_send_it():
    model = ScriptedModel(
        {"action": "queue_message", "reason": "ready", "message": a_message()},
        {"action": "done", "reason": "queued"},
    )
    run(model)

    result = model.seen[-1][-1]["content"]
    # The sentence a model reads after a successful queue has to close the
    # door rather than leave it ajar. "Queued" on its own invites a next step
    # that tries to deliver, which would be refused and recorded, which is
    # working as designed and is still a wasted call and a confusing record.
    assert "have no way to send it" in result
    assert "verified" in result


def test_a_run_that_queues_nothing_is_a_correct_run():
    # A model that reads the context and decides the template says it better is
    # not a failure. The doorbell still goes out; core-api uses the template
    # when no draft comes back.
    model = ScriptedModel({"action": "done", "reason": "nothing to add"})
    (run_record, queued), queue = run(model)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert queued is None
    assert queue.queued == []
    assert run_record.tool_calls == []


# --------------------------------------------------------------------------
# Sends only through the dispatch path: the acceptance criterion, and the
# tests that can fail


@pytest.mark.parametrize(
    "tool",
    ["send_email", "send_telegram", "deliver_now", "send_message", "notify_user"],
    ids=["email", "telegram", "deliver", "send", "notify"],
)
def test_a_messenger_run_asking_to_send_is_refused(tool: str):
    model = ScriptedModel({"action": tool, "reason": "I will send it myself",
                           "message": a_message()})
    (run_record, queued), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert tool in run_record.outcome_detail
    # Nothing was handed over and nothing was sent. The second half is
    # structural: `Queue` has no way to send and neither has the service.
    assert queue.queued == []
    assert queued is None
    # AND THE ASK IS IN THE RECORD. A run that tried to send and left no trace
    # of having tried would be the worst outcome of all, because the customer
    # reading `agent_runs` would see a well-behaved run.
    assert [c.tool for c in run_record.tool_calls] == [tool]
    assert run_record.tool_calls[0].refused is True
    assert '"refused": true' in run_record.tool_calls_json()


def test_a_refused_tool_ends_the_run_rather_than_being_retried():
    # §26.3: not retried. A model that can discover the allow-list by probing
    # it has been handed a way to negotiate with its own guardrail. The second
    # reply is scripted and must never be reached.
    model = ScriptedModel(
        {"action": "send_email", "reason": "sending", "message": a_message()},
        {"action": "queue_message", "reason": "fine, queueing", "message": a_message()},
    )
    (run_record, _), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert len(model.seen) == 1
    assert queue.queued == []


def test_the_messenger_holds_exactly_one_tool_and_it_does_not_send():
    # The list a customer reads in the console is the proof of the title, so it
    # is asserted rather than described. One entry, and it is the one that
    # hands a draft to the dispatch path.
    assert messenger.ALLOWED_TOOLS == ("queue_message",)


def test_wiring_a_sending_tool_the_skill_was_never_granted_fails_at_construction():
    # A capability wired but not granted is a configuration mistake worth
    # failing on where somebody wrote it. Finding out at the first call tells
    # you much less about who did it.
    with pytest.raises(ValueError, match="send_email"):
        ToolDispatcher(
            allowed=messenger.ALLOWED_TOOLS,
            tools={"queue_message": lambda **_: "", "send_email": lambda **_: ""},
            budget=Budget(),
        )


# --------------------------------------------------------------------------
# The draft is copy a person is handed


@pytest.mark.parametrize(
    "text",
    [
        "Open it at https://kindlast.example/o/acme/feed to decide.",
        "Reply to compliance@acme.example if this is wrong.",
        "See www.kindlast.example for what to do next.",
        "Details at kindlast.example and nowhere else.",
        "Write to mailto:help@acme.test with questions.",
    ],
    ids=["scheme", "address", "www", "bare-host", "mailto"],
)
def test_a_message_carrying_a_link_is_refused(text: str):
    model = ScriptedModel(
        {"action": "queue_message", "reason": "ready", "message": a_message(body=text)}
    )
    (run_record, queued), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert run_record.refused_by == "no_links"
    assert queue.queued == []
    assert queued is None


def test_a_link_in_the_subject_is_refused_as_well_as_one_in_the_body():
    # The subject is the line a recipient reads without opening anything, which
    # makes it the cheapest place to put a link and the one a ring that only
    # ever read the body would leave unguarded.
    model = ScriptedModel(
        {
            "action": "queue_message",
            "reason": "ready",
            "message": a_message(subject="Acme Ltd: act now at https://acme.example"),
        }
    )
    (run_record, _), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert run_record.refused_by == "no_links"
    assert queue.queued == []


def test_ordinary_copy_is_not_mistaken_for_a_link():
    # A critic that fires on prose is one somebody eventually switches off, so
    # the false-positive case is asserted rather than hoped for. A full stop
    # with a missing space after it is what a small model does constantly.
    for text in [
        "A high severity finding is waiting in Acme Ltd.",
        "Four others are open.The most serious is this one.",
        "Decide within 2-4 days.",
        "Acme Ltd. has one finding at high severity.",
    ]:
        assert review_links(text).ok, text


def test_a_link_refusal_names_the_rule_that_fired_and_quotes_it():
    # `agent_runs` is read by a maintainer counting which control fires and by
    # a customer asking why their notification looked ordinary. A boolean
    # answers neither.
    result = review_links("Open https://acme.example now")
    assert not result.ok
    assert result.breaches[0].pattern == "a link with a scheme"
    assert "https://" in result.breaches[0].matched
    assert "https://" in result.detail


def test_the_link_critic_reports_every_match_in_reading_order():
    # Every one rather than the first, so a single rewrite fixes the draft
    # instead of costing a model call per occurrence, and in the order somebody
    # reading the message would find them.
    result = review_links("Mail us@acme.example or open https://acme.example")
    assert [b.pattern for b in result.breaches] == [
        "an email address",
        "a link with a scheme",
    ]


def test_a_message_that_states_the_law_is_refused():
    # The Messenger is given no obligation to cite and must not reach for one
    # from memory. The claim critic is what enforces that, and it is the same
    # instance the Analyst and the Hands use.
    model = ScriptedModel(
        {
            "action": "queue_message",
            "reason": "ready",
            "message": a_message(
                body=(
                    "Article 30 requires every controller to keep a record of "
                    "processing activities, without exception, so you must act."
                )
            ),
        }
    )
    (run_record, _), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert run_record.refused_by == "legal_claim"
    assert queue.queued == []


def test_a_message_with_an_em_dash_is_refused():
    model = ScriptedModel(
        {
            "action": "queue_message",
            "reason": "ready",
            "message": a_message(
                # Written as an escape so this file obeys the rule it tests,
                # the way `prose.py` writes its own forbidden characters.
                body="Something needs you \u2014 the most serious one is open."
            ),
        }
    )
    (run_record, _), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert run_record.refused_by == "house_style"
    assert queue.queued == []


def test_a_link_outranks_a_dash_because_a_recipient_clicks_one_of_them():
    # The ring's order, asserted rather than described. A record reporting the
    # typography would send somebody to fix the smaller thing.
    model = ScriptedModel(
        {
            "action": "queue_message",
            "reason": "ready",
            "message": a_message(
                # The escape, not the character, for the reason above.
                body="Open https://acme.example \u2014 it is waiting for you."
            ),
        }
    )
    (run_record, _), _ = run(model)

    assert run_record.refused_by == "no_links"


@pytest.mark.parametrize(
    "message,fragment",
    [
        ({"subject": "", "body": "Something needs you."}, "no subject"),
        ({"subject": "Acme Ltd needs you", "body": ""}, "no body"),
        ({"subject": "x" * (MAX_SUBJECT + 1), "body": "Fine."}, "survive a mail"),
        ({"subject": "Fine", "body": "y" * (MAX_BODY + 1)}, "an article"),
    ],
    ids=["no-subject", "no-body", "long-subject", "long-body"],
)
def test_a_message_a_person_cannot_read_is_refused(message, fragment):
    model = ScriptedModel(
        {"action": "queue_message", "reason": "ready", "message": message}
    )
    (run_record, _), queue = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert fragment in run_record.outcome_detail
    assert queue.queued == []


def test_a_refused_draft_is_recorded_as_a_refused_tool_call():
    model = ScriptedModel(
        {
            "action": "queue_message",
            "reason": "ready",
            "message": a_message(body="Go to https://acme.example"),
        }
    )
    (run_record, _), _ = run(model)

    assert [c.tool for c in run_record.tool_calls] == ["queue_message"]
    assert run_record.tool_calls[0].refused is True
    assert run_record.refused_patterns == ["a link with a scheme"]
    assert "https://acme.example" in run_record.rejected_text
    # And the refusal is machine readable, so counting how often each control
    # fires does not mean parsing English out of a detail column.
    assert json.loads(run_record.refusal_json())["critic"] == "no_links"


# --------------------------------------------------------------------------
# What reaches the model, and what does not


def test_the_organisations_own_name_never_reaches_the_system_prompt():
    model = ScriptedModel({"action": "done", "reason": "nothing to add"})
    run(model)

    system = model.seen[0][0]
    user = model.seen[0][1]
    assert system["role"] == "system"
    assert "Acme Ltd" not in system["content"]
    assert user["role"] == "user"
    assert "Acme Ltd" in user["content"]
    assert "<notification>" in user["content"]


def test_the_run_is_never_shown_what_the_finding_says():
    # §17.1 enforced by what the run is OFFERED, which is the citation
    # validator's argument arriving through another door: a model cannot
    # restate exposure it was never given, and no critic has to guess whether a
    # sentence came too close.
    #
    # It fails if somebody adds the finding's own words to the context builder,
    # which is exactly the change ENT-260's description asks for and which
    # §17.1 refuses. See `skills/messenger.py`.
    rendered = messenger.render_context(
        {
            **CONTEXT,
            "detected": "No record of processing activities exists",
            "proposed_action": "Create a ROPA entry for payroll",
            "obligation_summary": "Article 30 requires a record",
        }
    )
    assert "record of processing" not in rendered
    assert "ROPA" not in rendered
    assert "Article 30" not in rendered


def test_the_channels_are_in_the_system_prompt_and_the_customer_is_not():
    # The split the other three skills use: the half that is identical between
    # runs first, because prefix caching is an exact match. Which channels a
    # deployment has is nearly constant; the organisation is not.
    model = ScriptedModel({"action": "done", "reason": "nothing"})
    run(model)

    system = model.seen[0][0]["content"]
    assert "email, telegram" in system
    assert "high" not in system.split("going out on")[1]


def test_an_empty_section_says_so_rather_than_being_omitted():
    # "Not supplied" and "there are none" are different claims, and here the
    # difference decides whether a draft may say this is their first.
    rendered = messenger.render_context({**CONTEXT, "first_for_org": True,
                                         "open_findings": 0})
    assert "first finding this organisation has ever had" in rendered

    rendered = messenger.render_context({**CONTEXT, "has_approve_link": False})
    assert "There is no approve link" in rendered


def test_the_loop_asks_the_model_for_the_messenger_grammar():
    # The Watcher shipped constrained to the Analyst's grammar and every run
    # failed, invisibly, because the fakes ignored the schema they were handed.
    model = ScriptedModel({"action": "done", "reason": "nothing"})
    run(model)

    assert model.schemas[0] == messenger.output_schema()
    assert "action" in model.schemas[0]["properties"]


# --------------------------------------------------------------------------
# The record exists whatever happened


def test_a_refusal_from_core_api_is_a_recorded_refusal_and_not_a_crash():
    # ENT-277: a `CoreAPIError` that matches no handler leaves the runner, takes
    # the RPC with it, and no `agent_runs` row is written for a run that really
    # happened. That is the one outcome the harness must never produce.
    model = ScriptedModel(
        {"action": "queue_message", "reason": "ready", "message": a_message()}
    )
    (run_record, _), _ = run(
        model,
        Queue(refuse=CoreAPIError("the message is already sent",
                                  code=Code.FAILED_PRECONDITION)),
    )

    assert run_record.outcome is Outcome.REFUSED
    assert "already sent" in run_record.outcome_detail
    assert run_record.tool_calls[0].refused is True


def test_core_api_being_unreachable_is_a_failure_and_not_a_refusal():
    # A refusal is a claim that the guardrails worked. Claiming that about a
    # connection nobody could open would make the record flattering.
    model = ScriptedModel(
        {"action": "queue_message", "reason": "ready", "message": a_message()}
    )
    (run_record, _), _ = run(model, Queue(refuse=CoreAPIError("connection refused")))

    assert run_record.outcome is Outcome.FAILED


def test_a_model_that_never_stops_is_refused_by_the_budget():
    model = ScriptedModel(
        *[
            {"action": "queue_message", "reason": "again", "message": a_message()}
            for _ in range(5)
        ]
    )
    (run_record, queued), queue = run(model, budget=Budget(max_model_calls=3))

    assert run_record.outcome is Outcome.REFUSED
    assert "budget exhausted" in run_record.outcome_detail
    # And what it did queue before the budget ran out is reported, because it
    # was handed over and saying otherwise would misdescribe the run.
    assert queued is not None
    assert len(queue.queued) >= 1


def test_a_context_missing_a_key_still_leaves_a_run_record():
    # Reachable from any caller that assembles a dict itself, which is every
    # test and will be the Temporal activity. A `KeyError` escaping here is the
    # shape of ENT-277.
    model = ScriptedModel({"action": "done", "reason": "nothing"})
    (run_record, _), _ = run(model, context={})

    assert run_record.outcome is Outcome.SUCCEEDED


def test_a_model_that_answers_something_that_is_not_the_contract_fails():
    model = ScriptedModel({"subject": "hello"})
    (run_record, _), _ = run(model)

    assert run_record.outcome is Outcome.FAILED


# --------------------------------------------------------------------------
# The skill contract


@pytest.mark.parametrize(
    "skill",
    [analyst, watcher, hands, conversation, messenger],
    ids=["analyst", "watcher", "hands", "conversation", "messenger"],
)
def test_every_skill_satisfies_the_protocol(skill: Skill):
    assert isinstance(skill, Skill)


def test_the_prompt_describes_the_schema_in_words():
    # A grammar constrains the shape and says nothing about what the fields
    # mean, so a prompt that does not name them leaves a small model guessing.
    for field in ("action", "reason", "message"):
        assert field in messenger.SYSTEM_PROMPT


def test_the_skill_version_is_pinned_and_the_name_is_stable():
    # `agent_runs` records both, and a run is only reproducible if they mean
    # something. The console repeats them and `catalog.test.ts` fails when the
    # two disagree.
    assert messenger.NAME == "messenger.draft"
    assert messenger.VERSION == "1.0.0"
