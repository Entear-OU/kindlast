"""Throughput as a guardrail: queue wait, work time, and fairness (ENT-238).

The existing ring counts cost. Tokens, model calls, tool calls, recursion: all
four are the right controls when inference is somebody else's API, because what
you are rationing is spend and concurrency is their problem.

ENT-235 made inference local, and the scarce thing became a slot on one
`llama-server`. A run can satisfy every cost limit and still be a bad citizen,
so the limits here measure the resource that actually runs out. Nothing in this
file talks to a model or starts a thread: the queue is a data structure and the
clocks are injected, so these run in milliseconds and cannot flake.
"""

from __future__ import annotations

import json
import time
from datetime import datetime, timedelta, timezone

import pytest

from kindlast_intelligence.harness.budget import Budget, BudgetExhausted
from kindlast_intelligence.harness.citations import CitationValidator
from kindlast_intelligence.harness.queue import FairQueue, QueueFull
from kindlast_intelligence.harness.run import Outcome, draft_narrative

from test_guardrails import OBLIGATIONS, Corpus, FakeModel, a_good_answer


def _ago(seconds: float) -> datetime:
    return datetime.now(timezone.utc) - timedelta(seconds=seconds)


# --- Queue wait is not work, and conflating them hides the problem ----------


def test_the_queue_wait_limit_fires():
    """The sixth limit, and the one ENT-238 says the ring was missing.

    Proven able to fail the same way the other five are: this is the named test
    that goes red if `admit` stops checking.
    """
    budget = Budget(max_queue_seconds=30.0)

    with pytest.raises(BudgetExhausted) as exc:
        budget.admit(queued_at=_ago(600))
    assert exc.value.limit == "queue_wait"


def test_waiting_in_the_queue_does_not_spend_the_work_budget():
    """The distinction is the whole point of the issue.

    A budget whose single clock starts when the run is enqueued charges the
    queue for the model's time: the run then waits, starts, and refuses partway
    through having already paid for a slot. The work clock has to start when
    the work does.
    """
    budget = Budget(max_seconds=5.0, max_queue_seconds=3_600.0)
    budget.admit(queued_at=_ago(600))

    # 600 seconds of waiting, and a work budget of five, so a single clock
    # would have fired here.
    budget.check_clock()
    assert budget.queue_seconds == pytest.approx(600, abs=5)
    assert budget.work_seconds < 1


def test_a_clock_skew_between_two_machines_cannot_buy_budget():
    """`queued_at` is stamped by whoever enqueued the work, which on a real
    deployment is a different process and may be a different host.

    A clock a few seconds ahead makes the wait negative. Negative wait must
    read as no wait rather than as credit, or a skewed caller is handed a
    longer queue tolerance than everybody else by accident.
    """
    budget = Budget(max_queue_seconds=30.0)
    budget.admit(queued_at=datetime.now(timezone.utc) + timedelta(seconds=120))

    assert budget.queue_seconds == 0.0


def test_the_work_clock_still_fires_after_admission():
    """The fifth limit must survive the fourth being added beside it."""
    budget = Budget(max_seconds=0.01)
    budget.admit()
    time.sleep(0.02)

    with pytest.raises(BudgetExhausted) as exc:
        budget.check_clock()
    assert exc.value.limit == "wall_clock"


# --- The run refuses rather than taking the slot anyway ----------------------


def test_a_run_that_waited_too_long_refuses_before_calling_the_model():
    """ENT-238's acceptance criterion, and the reason it is not just a metric.

    If the harness merely recorded a long wait and then ran, the person who
    asked has already gone and the slot they are now occupying belongs to
    somebody who is still waiting. `model.calls == 0` is the assertion that
    matters: the refusal happens before the expensive part.
    """
    model = FakeModel(a_good_answer())

    run = draft_narrative(
        signal="We process employee data.",
        obligations=OBLIGATIONS,
        model=model,
        validator=CitationValidator(Corpus("gdpr-art-30-ropa")),
        model_name="m",
        model_version="1",
        budget=Budget(max_queue_seconds=30.0),
        queued_at=_ago(600),
    )

    assert run.outcome == Outcome.REFUSED
    assert "queue_wait" in run.outcome_detail
    assert model.calls == 0, "the run refused after paying for a slot anyway"


def test_the_record_carries_queue_wait_and_latency():
    """`agent_runs` cannot explain a slow run from tokens alone (ENT-238).

    The three stamps already cross the wire in `coreapi.record_run`. What was
    missing was that `started_at` meant anything: stamped at admission it
    separates "waited" from "took", and the two have completely different
    remedies.
    """
    run = draft_narrative(
        signal="x",
        obligations=OBLIGATIONS,
        model=FakeModel(a_good_answer()),
        validator=CitationValidator(Corpus("gdpr-art-30-ropa")),
        model_name="m",
        model_version="1",
        queued_at=_ago(4),
    )

    assert run.outcome == Outcome.SUCCEEDED
    assert run.queued_at < run.started_at <= run.finished_at
    assert run.queue_seconds == pytest.approx(4, abs=1)
    assert run.work_seconds < 1


# --- The queue in front of the model ----------------------------------------


def test_the_queue_has_a_bounded_depth():
    """An unbounded queue does not remove the limit, it moves it into memory.

    Backpressure is the honest version: the caller is told now that the work
    will not be done, rather than told nothing and left waiting.
    """
    queue = FairQueue(max_depth=2)
    queue.submit("org-a", "one")
    queue.submit("org-a", "two")

    with pytest.raises(QueueFull) as exc:
        queue.submit("org-a", "three")
    assert exc.value.limit == "depth"


def test_one_org_cannot_fill_the_whole_queue():
    """Fair scheduling with a single shared cap is not fair.

    A sweep that fills every slot starves the next organisation at ADMISSION
    rather than at dispatch, which is the same starvation one step earlier and
    is invisible to a scheduler that only rotates what it already holds.
    """
    queue = FairQueue(max_depth=10, max_depth_per_org=2)
    queue.submit("org-a", "one")
    queue.submit("org-a", "two")

    with pytest.raises(QueueFull) as exc:
        queue.submit("org-a", "three")
    assert exc.value.limit == "depth_per_org"

    # The other tenant is unaffected, which is the property being bought.
    queue.submit("org-b", "one")
    assert queue.depth == 3


def test_a_sweep_cannot_starve_an_interactive_request():
    """ENT-238: one organisation's sweep must not occupy the whole model.

    FIFO would serve org-b's single request twentieth. Round-robin over
    organisations serves it second, and this assertion is what goes red if
    somebody replaces the rotation with a plain deque.
    """
    queue = FairQueue(max_depth=100, max_depth_per_org=100)
    for i in range(20):
        queue.submit("org-a", f"sweep-{i}")
    queue.submit("org-b", "a person is waiting")

    served = [queue.take() for _ in range(2)]

    assert [t.org_id for t in served] == ["org-a", "org-b"]


def test_within_one_org_the_order_is_the_order_it_was_asked_in():
    """Fairness is between tenants, not inside one.

    Reordering an organisation's own work would make a sweep's results arrive
    in an order nobody chose, for no gain: there is no second tenant to protect
    from it.
    """
    queue = FairQueue(max_depth=10)
    queue.submit("org-a", "first")
    queue.submit("org-a", "second")

    assert [queue.take().item for _ in range(2)] == ["first", "second"]


def test_an_empty_queue_yields_nothing_rather_than_blocking():
    """`take` is a data-structure operation, not a wait.

    Blocking here would put a scheduling policy inside the structure and make
    every test above need a thread.
    """
    assert FairQueue(max_depth=1).take() is None


def test_a_refused_submission_says_what_it_hit():
    """A queue that refuses without saying why produces a support ticket
    rather than a capacity decision."""
    queue = FairQueue(max_depth=1)
    queue.submit("org-a", "one")

    with pytest.raises(QueueFull) as exc:
        queue.submit("org-b", "one")

    assert "org-b" in str(exc.value)
    assert exc.value.depth == 1


def test_the_budget_template_carries_every_limit_when_it_is_renewed():
    """A per-request copy assembled from a hand-written field list drops any
    limit added later, silently, and the run then uses the default.

    Renewal derives the list from the model instead, so a new limit is carried
    by construction. `max_queue_seconds` is the one that would have been lost.
    """
    template = Budget(max_queue_seconds=7.0, max_total_tokens=99, max_seconds=11.0)
    fresh = template.renew()

    assert fresh.max_queue_seconds == 7.0
    assert fresh.max_total_tokens == 99
    assert fresh.max_seconds == 11.0
    assert fresh.total_tokens == 0, "a renewed budget must not inherit the spend"


def test_a_queue_refusal_is_readable_in_the_record():
    """The refusal has to survive into `agent_runs` as something a person can
    act on, not as a generic timeout."""
    run = draft_narrative(
        signal="x",
        obligations=OBLIGATIONS,
        model=FakeModel(a_good_answer()),
        validator=CitationValidator(Corpus("gdpr-art-30-ropa")),
        model_name="m",
        model_version="1",
        budget=Budget(max_queue_seconds=1.0),
        queued_at=_ago(60),
    )

    stored = json.loads(run.citations_json())
    assert stored == {"resolved": [], "rejected": []}
    assert "queue_wait" in run.outcome_detail
    assert "60" in run.outcome_detail or "of 1s" in run.outcome_detail
