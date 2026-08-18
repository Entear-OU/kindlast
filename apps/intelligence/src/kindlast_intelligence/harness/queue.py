"""The queue in front of the model: bounded, and fair between tenants (ENT-238).

# WHY THERE IS A QUEUE HERE AT ALL

`llama-server` is one process with a fixed slot count, so the way to serve more
concurrency is more of them behind a balancer rather than a bigger one. That is
how capacity grows. This is what happens when capacity runs out, and both are
needed: scaling out raises the ceiling, it does not create backpressure. Without
something bounded in front, the failure mode stops being "slow" and becomes
unbounded memory in whatever holds the pending work.

The self-hoster is the case that makes this non-optional. A small firm running
Kindlast on one box cannot add replicas, so for them these limits are the
mechanism rather than a safety net, and the degraded mode has to be honest
instead of silent.

# FAIRNESS IS A TENANCY CONCERN, THOUGH NOT A DATA ONE

Nothing here is a security boundary: RLS is, and this cannot see a row. What it
can do is let one organisation's overnight sweep occupy every slot while
another organisation's console request waits behind it. On a shared deployment
that is a tenant property even though no data crosses, and it does not come free
from adding replicas: round-robin across N servers still serves the sweep first
N times.

So the rotation is over ORGANISATIONS and the order within one is untouched.
There is no second tenant to protect an organisation from itself.

# NO THREADS, NO BLOCKING, NO CLOCK

`take` returns `None` on an empty queue rather than waiting. Blocking would put
a scheduling policy inside a data structure, make every test need a thread, and
make the fairness property the hardest kind to assert. Whoever owns the worker
loop owns the waiting.
"""

from __future__ import annotations

from collections import OrderedDict, deque
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Deque


class QueueFull(Exception):
    """The work was refused at the door, on purpose.

    §26.3 makes refusal what a working guardrail produces, and this is the
    admission-time version of it: telling a caller now that their work will not
    be done is kinder than telling them nothing and leaving them waiting, and
    ENT-212 already established that a degraded instance says so rather than
    hanging.

    `limit` names which cap was hit, because "depth" and "depth_per_org" have
    completely different remedies: one is buy more capacity, the other is one
    tenant asking for too much of what there is.
    """

    def __init__(self, limit: str, org_id: str, depth: int) -> None:
        super().__init__(
            f"queue is full for {org_id!r}: {limit} reached at depth {depth}"
        )
        self.limit = limit
        self.org_id = org_id
        self.depth = depth


@dataclass(frozen=True)
class Ticket:
    """One piece of pending work, and when it was asked for.

    `queued_at` is stamped here rather than read at dispatch, because the whole
    point of recording queue wait is to measure from the moment somebody asked.
    It is what `Budget.admit` is given later, and what makes the wait visible in
    `agent_runs` rather than inferrable from a log.
    """

    org_id: str
    item: Any
    queued_at: datetime = field(
        default_factory=lambda: datetime.now(timezone.utc)
    )


class FairQueue:
    """A bounded queue that rotates between organisations.

    Two caps rather than one, and the second is the one that is easy to leave
    out. A single shared depth lets one organisation's sweep fill every slot, so
    the next tenant is refused at ADMISSION. That is the same starvation the
    rotation exists to prevent, one step earlier, and a scheduler that only
    rotates what it already holds cannot see it.
    """

    def __init__(self, max_depth: int, max_depth_per_org: int | None = None) -> None:
        if max_depth <= 0:
            raise ValueError("max_depth must be positive: a queue of zero is not a queue")

        self._max_depth = max_depth
        # OFF unless configured, rather than defaulted to the whole queue.
        #
        # Defaulting it to `max_depth` would make every single-tenant refusal
        # report `depth_per_org`, sending an operator to look for a per-tenant
        # share nobody set. A cap that is not there should not name itself in
        # the error; the honest default is that a shared deployment sets one.
        self._max_depth_per_org = max_depth_per_org
        # Ordered so the rotation is deterministic and testable. An org with no
        # pending work is removed entirely rather than left as an empty deque,
        # so a long-lived process does not accumulate a slot per organisation
        # it has ever served.
        self._orgs: OrderedDict[str, Deque[Ticket]] = OrderedDict()
        self._depth = 0

    @property
    def depth(self) -> int:
        return self._depth

    def submit(self, org_id: str, item: Any, queued_at: datetime | None = None) -> Ticket:
        # The per-org cap is checked FIRST, so a tenant filling their own share
        # is told that rather than being told the system is full. The two
        # messages send an operator to different places.
        pending = self._orgs.get(org_id)
        if (
            self._max_depth_per_org is not None
            and pending is not None
            and len(pending) >= self._max_depth_per_org
        ):
            raise QueueFull("depth_per_org", org_id, len(pending))

        if self._depth >= self._max_depth:
            raise QueueFull("depth", org_id, self._depth)

        ticket = Ticket(org_id=org_id, item=item, queued_at=queued_at or datetime.now(timezone.utc))
        if pending is None:
            pending = deque()
            self._orgs[org_id] = pending
        pending.append(ticket)
        self._depth += 1
        return ticket

    def take(self) -> Ticket | None:
        """The next piece of work, rotating between organisations.

        The org at the front of the rotation gives up one ticket and goes to the
        back, so N organisations each get one in N takes regardless of how much
        any of them submitted. Replace this with a plain deque and
        `test_a_sweep_cannot_starve_an_interactive_request` goes red.
        """
        if not self._orgs:
            return None

        org_id, pending = next(iter(self._orgs.items()))
        ticket = pending.popleft()
        self._depth -= 1

        if pending:
            self._orgs.move_to_end(org_id)
        else:
            del self._orgs[org_id]

        return ticket
