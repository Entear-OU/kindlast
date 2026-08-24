"""Per-run budgets, and refusing when one is spent (§26.3, ENT-218, ENT-238).

Seven limits, not four. The design names token budget, model calls, tool calls
and recursion; all four are cost controls, and they are the right ones when
inference is somebody else's API and concurrency is their problem.

ENT-235 made inference local, which changes what is scarce. One `llama-server`
serves one or two requests at a time, so a run can sit inside every token limit
and still hold a slot for eleven minutes while another tenant waits. Hence wall
clock, and hence queue wait beside it.

ENT-274 added the seventh, and it is the only one that is not about cost at
all. `max_evidence_reads` bounds how much of a customer's own systems' output a
run may pull into the conversation it is reasoning in. See the field.

# TIME WAITED AND TIME WORKED ARE TWO BUDGETS, NOT ONE

The obvious implementation is a single clock started when the run is created,
and it is wrong in a way that only shows up under load. Started at enqueue, the
queue spends the model's budget: the run waits, is finally dispatched, and
refuses partway through having already taken a slot from somebody still
waiting. Started at dispatch, the wait is not measured at all, and a record that
says a run took four seconds cannot explain why the customer watched a spinner
for six minutes.

So there are two. `max_queue_seconds` is how long an answer is still worth
having, checked once at admission. `max_seconds` is how long the work itself may
take, and its clock starts when the work does. They have different remedies too:
a queue-wait refusal means buy capacity, a wall-clock refusal means this run is
too big for this model.

# EXHAUSTION IS A REFUSAL, NOT AN ERROR

A budget running out means the guardrail worked. §26.3 makes refusal a
first-class outcome for exactly this reason, and `agent_runs` records it as
one. Raising something that reads as a crash would put "the harness broke" in
the column a customer reads to decide whether to trust a finding.
"""

from __future__ import annotations

import time
from datetime import datetime, timezone
from typing import ClassVar

from pydantic import BaseModel, ConfigDict, Field


class BudgetExhausted(Exception):
    """A limit was reached. The run refuses; it has not failed."""

    def __init__(self, limit: str, detail: str) -> None:
        super().__init__(f"{limit} budget exhausted: {detail}")
        self.limit = limit
        self.detail = detail


class Budget(BaseModel):
    """What one run may spend.

    Defaults are deliberately small. A harness whose limits are generous enough
    never to fire is not a harness, and the failure it exists to prevent, a
    loop calling a tool forever, is only visible when something stops it.

    Every limit is constrained to be positive at construction. A zero or
    negative limit is not a stricter budget, it is a harness that refuses every
    run before it starts, and it should fail where somebody wrote it rather
    than as a run that mysteriously never succeeds.
    """

    model_config = ConfigDict(extra="forbid", validate_assignment=True)

    max_total_tokens: int = Field(default=8_000, gt=0)
    max_model_calls: int = Field(default=6, gt=0)
    max_tool_calls: int = Field(default=12, gt=0)
    # HOW MANY TIMES ONE RUN MAY LOOK AT WHAT A CUSTOMER'S SYSTEM REPORTED
    # (ENT-274).
    #
    # A separate limit from `max_tool_calls`, and the reason is not arithmetic.
    # Every other tool this harness has spends only our own resources; this one
    # pulls text somebody else wrote into the conversation the model is
    # reasoning in. That is the one cost a token budget cannot control in time,
    # because the tokens are already in the context by the moment it fires.
    #
    # Three. Enough to compare two or three connections against each other,
    # which is the whole reason an agent rather than a detector is looking;
    # fewer than `max_model_calls`, so a run that spends every read still has a
    # call left to say what it concluded; and small enough that a compromised
    # or merely chatty endpoint cannot make third-party text the majority of
    # what the model is looking at.
    max_evidence_reads: int = Field(default=3, gt=0)
    max_depth: int = Field(default=4, gt=0)
    # Sized for a 4B on CPU answering a handful of times rather than for a
    # hosted API. On local inference this is the limit that actually fires.
    max_seconds: float = Field(default=120.0, gt=0)
    # Sixty seconds, because that is roughly where a person waiting on a page
    # stops waiting. A batch caller that genuinely wants a long queue should
    # raise this deliberately rather than inherit an interactive default it
    # never thought about.
    max_queue_seconds: float = Field(default=60.0, gt=0)

    # Spent so far. Public because `agent_runs` records these, and the run
    # summary is assembled from the same numbers the limits are checked
    # against rather than from a second count that could disagree.
    total_tokens: int = Field(default=0, ge=0)
    model_calls: int = Field(default=0, ge=0)
    tool_calls: int = Field(default=0, ge=0)
    evidence_reads: int = Field(default=0, ge=0)
    # How long this run waited before the work started. Measured once at
    # admission and then kept, rather than recomputed, so the number in the
    # record is the number the limit was checked against.
    queue_seconds: float = Field(default=0.0, ge=0)
    # When the budget was made, and the fallback the work clock measures from
    # when nothing ever queued this run. A budget constructed at the point of
    # work HAS started working, and reading its work clock as zero would leave
    # the wall-clock limit silently disabled for every caller that does not
    # queue, which is most of them.
    created_monotonic: float = Field(default_factory=time.monotonic)
    # None until `admit` is called, and set to that moment rather than to
    # construction. A budget built at enqueue would otherwise start its work
    # clock in the queue, which is the exact conflation this class exists to
    # avoid.
    started_monotonic: float | None = None

    # The set of fields that describe what a run MAY spend, as opposed to what
    # it HAS spent. Derived once here so `renew` cannot drift from the fields;
    # the hand-written copy this replaced silently dropped any limit added
    # after it was written, and the run then ran on the default.
    LIMITS: ClassVar[tuple[str, ...]] = (
        "max_total_tokens",
        "max_model_calls",
        "max_tool_calls",
        "max_evidence_reads",
        "max_depth",
        "max_seconds",
        "max_queue_seconds",
    )

    def renew(self) -> Budget:
        """A fresh budget with these limits and none of the spend.

        One per run, from a template held by the service. Sharing a budget
        across requests would let a busy morning refuse an afternoon's work.
        """
        return Budget(**self.model_dump(include=set(self.LIMITS)))

    def admit(self, queued_at: datetime | None = None) -> None:
        """Leave the queue and start the work, or refuse to.

        # WALL CLOCK HERE, MONOTONIC EVERYWHERE ELSE

        Monotonic is the right clock for a duration inside one process and is
        useless for this one, because the enqueue may have happened in another
        process on another host. `queued_at` is a UTC instant precisely so it can
        cross that boundary, which is also why the wait is clamped at zero: two
        machines whose clocks differ by a few seconds would otherwise hand the
        caller with the fast clock a longer queue tolerance than everybody else,
        by accident and invisibly.

        Called with nothing when there was no queue, which is the truthful
        reading of a run created at the moment it starts.
        """
        if queued_at is not None:
            waited = (datetime.now(timezone.utc) - queued_at).total_seconds()
            self.queue_seconds = max(0.0, waited)

        # Stamped BEFORE the check, so a refused run still reports what it
        # waited. A refusal that hid the number would be a refusal nobody can
        # size capacity from.
        self.started_monotonic = time.monotonic()

        if self.queue_seconds > self.max_queue_seconds:
            raise BudgetExhausted(
                "queue_wait",
                f"waited {self.queue_seconds:.0f}s of {self.max_queue_seconds:.0f}s "
                "allowed before the work started",
            )

    def spend_model_call(self, tokens: int) -> None:
        # Checked BEFORE incrementing, so the limit is the number of calls
        # allowed rather than the number after which the next one fails.
        if self.model_calls >= self.max_model_calls:
            raise BudgetExhausted(
                "model_calls", f"{self.model_calls} of {self.max_model_calls} used"
            )
        self.model_calls += 1
        self.total_tokens += tokens
        # Tokens are checked AFTER, because a call's cost is not knowable until
        # it returns. Going over on the last call is unavoidable; going over
        # and then making another is not.
        if self.total_tokens > self.max_total_tokens:
            raise BudgetExhausted(
                "tokens", f"{self.total_tokens} of {self.max_total_tokens} used"
            )

    def spend_tool_call(self) -> None:
        if self.tool_calls >= self.max_tool_calls:
            raise BudgetExhausted(
                "tool_calls", f"{self.tool_calls} of {self.max_tool_calls} used"
            )
        self.tool_calls += 1

    def spend_evidence_read(self) -> None:
        """Charged before the read happens, like every other limit here.

        Not folded into `spend_tool_call`: a read is BOTH, so it costs one of
        each. A run that spent its reads has not spent its tool calls and can
        still raise what it concluded, which is the behaviour that makes the
        limit a bound on third-party text rather than a bound on the run.
        """
        if self.evidence_reads >= self.max_evidence_reads:
            raise BudgetExhausted(
                "evidence_reads",
                f"{self.evidence_reads} of {self.max_evidence_reads} used",
            )
        self.evidence_reads += 1

    def enter_depth(self, depth: int) -> None:
        if depth > self.max_depth:
            raise BudgetExhausted("depth", f"depth {depth} exceeds {self.max_depth}")

    def check_clock(self) -> None:
        """Called at every step rather than only between model calls.

        A single generation can outlast the whole budget on a slow box, so
        checking only at the loop head would let one call run indefinitely.
        This cannot interrupt a call already in flight, which is the honest
        limit of doing it in-process. What it guarantees is that no further
        work starts.

        Measures work only. Time spent queued was already accounted for by
        `admit`, and charging it twice would refuse runs that have done nothing
        wrong on a deployment that is merely busy.
        """
        elapsed = self.work_seconds
        if elapsed > self.max_seconds:
            raise BudgetExhausted(
                "wall_clock", f"{elapsed:.1f}s of {self.max_seconds:.0f}s used"
            )

    @property
    def work_seconds(self) -> float:
        """Time since the work started.

        Falls back to construction for a budget that was never admitted, which
        is the truthful reading: nothing queued it, so the work began when it
        was made. The alternative, reporting zero, would turn a caller that
        never queues into a caller with no wall-clock limit.
        """
        since = self.created_monotonic if self.started_monotonic is None else self.started_monotonic
        return time.monotonic() - since
