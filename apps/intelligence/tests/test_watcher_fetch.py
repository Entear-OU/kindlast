"""The Watcher's ask for a fetch (ENT-279).

The mediated shape, tested from the side that holds no authority: the agent
asks, core-api decides, the gateway fetches. What these tests hold open is
that the ask really is only an ask. The requester is handed nothing but a
connection id, a tool name and a sentence; the answer is an acknowledgement
the model reads, not a payload; and every way core-api or the customer's own
policy says no lands in the run's record as the outcome it actually was.

Every test runs in milliseconds with no stack and no model, the property the
whole harness suite keeps.
"""

from __future__ import annotations

from typing import Any

from connectrpc.code import Code

from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.remote import CoreAPIError
from kindlast_intelligence.harness.run import Outcome

from test_watcher_skill import CONTEXT, Reader, ScriptedModel, Writer, run


class Requester:
    """A stand-in for core-api's RequestFetch.

    Answers with whatever acknowledgement the test wants, and KEEPS what it
    was asked, because the property under test is that the connection, tool
    and reason the model named are what reach core-api and that NOTHING else
    does: no arguments, no endpoint, no credential, because the callable has
    no parameters to carry them.
    """

    def __init__(self, **acknowledgement: str) -> None:
        self._acknowledgement = acknowledgement or {
            "state": "queued",
            "detail": "a fetch will run in the background; "
            "this run will not see the result",
            "request_id": "r1",
        }
        self.error: Exception | None = None
        self.asked: list[tuple[str, str, str]] = []

    def __call__(self, connection_id: str, tool: str, reason: str) -> dict[str, str]:
        self.asked.append((connection_id, tool, reason))
        if self.error is not None:
            raise self.error
        return dict(self._acknowledgement)


def a_fetch_step(**overrides: Any) -> dict[str, Any]:
    fetch = {
        "connection_id": "c1",
        "tool": "search_tickets",
        "reason": "the stored answer is a week old",
    }
    fetch.update(overrides)
    return {"action": "request_fetch", "reason": "need a fresh look", "fetch": fetch}


def run_with_requester(model: ScriptedModel, requester: Requester, **kwargs):
    kwargs.setdefault("request_fetch", requester)
    return run(model, **kwargs)


# --------------------------------------------------------------------------
# The ask that stands


def test_an_ask_reaches_core_api_with_what_the_model_named_and_nothing_else():
    requester = Requester()
    model = ScriptedModel(
        a_fetch_step(),
        {"action": "done", "reason": "asked for the refresh"},
    )

    (run_record, _), _ = run_with_requester(model, requester)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert requester.asked == [
        ("c1", "search_tickets", "the stored answer is a week old")
    ]


def test_the_acknowledgement_comes_back_to_the_model_as_an_acknowledgement():
    """The model is told what became of the ASK, never handed a payload.

    The feedback turn carries core-api's state and sentence, so a working
    model can move on rather than asking again or waiting for a result that
    will never arrive in this run.
    """
    requester = Requester()
    model = ScriptedModel(
        a_fetch_step(),
        {"action": "done", "reason": "done"},
    )

    run_with_requester(model, requester)

    feedback = model.seen[1][-1]["content"]
    assert "fetch queued" in feedback
    assert "will not see the result" in feedback


def test_the_ask_is_recorded_in_the_run_with_its_reason():
    requester = Requester()
    model = ScriptedModel(a_fetch_step(), {"action": "done", "reason": "done"})

    (run_record, _), _ = run_with_requester(model, requester)

    calls = [c for c in run_record.tool_calls if c.tool == "request_fetch"]
    assert len(calls) == 1
    assert not calls[0].refused
    assert calls[0].arguments["reason"] == "the stored answer is a week old"


# --------------------------------------------------------------------------
# The asks that do not stand, and what each costs


def test_a_connection_the_run_was_never_shown_ends_the_run_refused():
    """An id from anywhere but the context is a fabrication, the same claim
    the citation validator makes about a slug, and it must end the run before
    anything leaves the process."""
    requester = Requester()
    model = ScriptedModel(a_fetch_step(connection_id="c9"))

    (run_record, _), _ = run_with_requester(model, requester)

    assert run_record.outcome is Outcome.REFUSED
    assert "produced rather than read" in run_record.outcome_detail
    assert requester.asked == [], "a fabricated id reached core-api"


def test_an_ungranted_tool_is_declined_and_the_loop_goes_on():
    requester = Requester()
    model = ScriptedModel(
        a_fetch_step(tool="close_ticket"),
        {"action": "done", "reason": "understood"},
    )

    (run_record, _), _ = run_with_requester(model, requester)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert requester.asked == [], "a declined ask reached core-api anyway"
    refused = [c for c in run_record.tool_calls if c.refused]
    assert len(refused) == 1
    assert "not granted" in refused[0].result_summary


def test_a_write_capable_tool_is_declined_even_when_granted():
    """`close_ticket` in the shared context is both ungranted and
    write-capable, so this test grants it in a copy: the decline must then
    come from the write check alone, which is the check that matters. A fetch
    a model asked for is nobody deciding, so a tool that can do things is
    never fetched on a model's say-so, whatever the customer granted."""
    import copy

    context = copy.deepcopy(CONTEXT)
    tools = context["connections"][0]["tools"]
    assert tools[1]["name"] == "close_ticket"
    tools[1]["granted"] = True

    requester = Requester()
    model = ScriptedModel(
        a_fetch_step(tool="close_ticket"),
        {"action": "done", "reason": "understood"},
    )
    from kindlast_intelligence.harness.citations import (
        CitationValidator,
        OfferedObligations,
    )
    from kindlast_intelligence.harness.watch import watch

    run_record, _ = watch(
        context=context,
        model=model,
        write_signal=Writer(),
        validator=CitationValidator(OfferedObligations(context["obligations"])),
        model_name="test",
        model_version="0",
        read_evidence=Reader(),
        request_fetch=requester,
    )

    assert run_record.outcome is Outcome.SUCCEEDED
    assert requester.asked == []
    refused = [c for c in run_record.tool_calls if c.refused]
    assert len(refused) == 1
    assert "can write" in refused[0].result_summary


def test_a_revoked_connection_is_declined_with_the_read_path_pointed_at():
    """Stored evidence from a revoked connection is still readable; a fresh
    fetch of it is not. The decline says both, so the model is told what it
    can still do rather than only what it cannot."""
    requester = Requester()
    model = ScriptedModel(
        a_fetch_step(connection_id="c2", tool="list_pages"),
        {"action": "done", "reason": "understood"},
    )

    (run_record, _), _ = run_with_requester(model, requester)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert requester.asked == []
    refused = [c for c in run_record.tool_calls if c.refused]
    assert len(refused) == 1
    assert "revoked" in refused[0].result_summary
    assert "read_evidence" in refused[0].result_summary


def test_core_api_refusing_the_ask_is_a_recorded_refusal_and_not_a_crash():
    """The double-check disagreeing: the context says granted, core-api says
    no, and the far side wins. permission_denied maps to REFUSED (a rule
    worked) and the run keeps its record, which is ENT-277's whole lesson."""
    requester = Requester()
    requester.error = CoreAPIError(
        "asking for a fetch: the tool is not granted",
        code=Code.PERMISSION_DENIED,
    )
    model = ScriptedModel(a_fetch_step())

    (run_record, _), _ = run_with_requester(model, requester)

    assert run_record.outcome is Outcome.REFUSED
    calls = [c for c in run_record.tool_calls if c.tool == "request_fetch"]
    assert len(calls) == 1
    assert calls[0].refused


def test_core_api_being_unreachable_is_a_failure_and_not_a_refusal():
    requester = Requester()
    requester.error = CoreAPIError("asking for a fetch: connection refused")
    model = ScriptedModel(a_fetch_step())

    (run_record, _), _ = run_with_requester(model, requester)

    assert run_record.outcome is Outcome.FAILED


def test_asking_is_refused_when_the_deployment_wired_no_requester():
    """Allowed and absent is the caller's fault and says so, the same honest
    answer the reader gives: quietly pretending the ask was queued would tell
    a model a fetch is coming that never is."""
    model = ScriptedModel(a_fetch_step())

    (run_record, _), _ = run(model)

    assert run_record.outcome is Outcome.REFUSED
    assert "not implemented" in str(run_record.tool_calls[0].result_summary)


# --------------------------------------------------------------------------
# The budget, which is the bound on network effects


def test_the_number_of_asks_is_bounded_by_its_own_budget():
    requester = Requester()
    budget = Budget(max_fetch_requests=1)
    model = ScriptedModel(
        a_fetch_step(),
        a_fetch_step(tool="search_tickets", reason="again"),
    )

    (run_record, _), _ = run_with_requester(model, requester, budget=budget)

    assert run_record.outcome is Outcome.REFUSED
    assert "fetch_requests" in run_record.outcome_detail
    assert len(requester.asked) == 1, "the bound did not hold"


def test_a_declined_ask_does_not_spend_the_fetch_budget():
    """The order of checks is the design: a model asking for things the
    customer's policy declines must not be able to exhaust a well-behaved
    run's one real ask."""
    requester = Requester()
    budget = Budget(max_fetch_requests=1)
    model = ScriptedModel(
        a_fetch_step(tool="close_ticket"),
        a_fetch_step(),
        {"action": "done", "reason": "done"},
    )

    (run_record, _), _ = run_with_requester(model, requester, budget=budget)

    assert run_record.outcome is Outcome.SUCCEEDED
    assert len(requester.asked) == 1, (
        "the declined ask spent the budget the granted one needed"
    )


def test_a_default_run_gets_fewer_asks_than_reads():
    """Two asks against three reads, and the ordering is the argument: a run
    may look at three stored answers and cause at most two customer-side
    calls, so the network effects a model can cause are bounded below what it
    can read. If either default moves, this is where the reasoning has to be
    revisited rather than silently outgrown."""
    budget = Budget()
    assert budget.max_fetch_requests == 2
    assert budget.max_fetch_requests < budget.max_evidence_reads


def test_the_fetch_budget_survives_renewal():
    """`renew` derives from LIMITS, and the eighth limit has to be in it: the
    hand-written copy this replaced silently dropped any limit added after it
    was written, and a run then ran on the default."""
    template = Budget(max_fetch_requests=1)
    renewed = template.renew()
    assert renewed.max_fetch_requests == 1
    assert renewed.fetch_requests == 0
