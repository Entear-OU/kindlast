"""Per-run budgets, and refusing when one is spent (§26.3, ENT-218, ENT-238).

Five limits, not four. The design names token budget, model calls, tool calls
and recursion; all four are cost controls, and they are the right ones when
inference is somebody else's API and concurrency is their problem.

ENT-235 made inference local, which changes what is scarce. One `llama-server`
serves one or two requests at a time, so a run can sit inside every token limit
and still hold a slot for eleven minutes while another tenant waits. Hence the
fifth, wall clock. ENT-238 covers the queue in front of the model; this covers
the run in front of the queue.

# EXHAUSTION IS A REFUSAL, NOT AN ERROR

A budget running out means the guardrail worked. §26.3 makes refusal a
first-class outcome for exactly this reason, and `agent_runs` records it as
one. Raising something that reads as a crash would put "the harness broke" in
the column a customer reads to decide whether to trust a finding.
"""

from __future__ import annotations

import time

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
    max_depth: int = Field(default=4, gt=0)
    # Sized for a 4B on CPU answering a handful of times rather than for a
    # hosted API. On local inference this is the limit that actually fires.
    max_seconds: float = Field(default=120.0, gt=0)

    # Spent so far. Public because `agent_runs` records these, and the run
    # summary is assembled from the same numbers the limits are checked
    # against rather than from a second count that could disagree.
    total_tokens: int = Field(default=0, ge=0)
    model_calls: int = Field(default=0, ge=0)
    tool_calls: int = Field(default=0, ge=0)
    started_monotonic: float = Field(default_factory=time.monotonic)

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
        """
        elapsed = time.monotonic() - self.started_monotonic
        if elapsed > self.max_seconds:
            raise BudgetExhausted(
                "wall_clock", f"{elapsed:.1f}s of {self.max_seconds:.0f}s used"
            )

    @property
    def elapsed_seconds(self) -> float:
        return time.monotonic() - self.started_monotonic
