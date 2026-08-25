"""The core-api client: this service's only way to reach anything (§1.6, §26.3).

# THREE RPCs, AND THE SHORTNESS IS STILL THE POINT

ENT-218 says an agent's tools are core-api RPCs and nothing else. No
filesystem, no shell, no database handle, no third-party credential. That is
not a limitation to work around later; it is the reason a model is allowed near
a compliance record at all, because everything it can reach is something
core-api already checks.

This wraps three calls and offers no way to make a fourth. Adding one means
adding a method here, which means a reviewer sees it, which is the mechanism
rather than the number.

`RecordAgentRun` writes the provenance of every run. `RaiseSignal` and
`ReadEvidence` are the Watcher's two tools (ENT-258, ENT-274), and they are here
rather than reached some other way for exactly the reason above: an agent's
tools are core-api RPCs, so a tool has to be a method on this object, in front
of a reviewer, and not an HTTP call assembled inside a skill.

`ReadEvidence` is the one whose ANSWER is a customer's own systems' output, and
it is worth saying what it does not change. This service still holds no
third-party credential, dials no customer endpoint and declares no client
library that could. What comes back is rows core-api already stored, redacted
by the gateway before they were written, read under the producer role's
policies for the organisation core-api named. The Python half of the boundary
is that the content goes into a user turn and nowhere else, and that lives in
`harness/watch.py` rather than here.

What `RaiseSignal` does NOT let this service do is worth stating, because the
first question about giving an agent a write is what it can now reach. It
cannot write a finding: no such RPC exists on any surface this holds. It cannot
choose an organisation core-api did not name: the id comes from the request the
engine handed this worker. And it cannot write outside the vocabulary, without
a deduplication key, or citing an obligation that does not exist, because
core-api validates all three and this client cannot ask it not to.

# WHY THERE IS NO CORPUS READ HERE, WHICH THE FIRST DRAFT HAD

The first version wrapped `CorpusService.GetObligation` so the citation
validator could ask core-api whether a slug was real. It failed against the
running stack:

    token does not carry the "corpus:read" scope

`corpus:read` is a tenant-facing human scope. Reaching it needs an organisation
header and a membership check, and this service has neither a session nor a
member. Granting it would have meant handing the Intelligence principal a
human-facing capability to work around a design mistake.

The design mistake was mine, and §26.2 already had the answer: obligations are
INPUTS, not tools. The caller assembles them, in Go, where the corpus is
already readable, and passes them in. The validator then checks citations
against what the model was actually shown.

That turns out to be a STRONGER check than asking the corpus, not a weaker
one. A citation to an obligation that genuinely exists but was never offered to
this run is still a fabrication: the model produced it from somewhere other
than its context, which is exactly the failure the validator exists to catch.
Asking core-api "does this exist" would have accepted it.

# gen/python HOLDS THE CONTRACT; THIS HOLDS THE NARROW VIEW OF IT

Generated types and stubs live there and nothing else, per the rule that
already applies to `gen/go`.
"""

from __future__ import annotations

from typing import Protocol

from kindlast.platform.v1 import (
    hands_connect,
    hands_pb2,
    ingest_connect,
    ingest_pb2,
    watcher_connect,
    watcher_pb2,
)

from .harness.remote import CoreAPIError, code_of
from .harness.run import AgentRun, Outcome


class TokenSource(Protocol):
    """Something that can produce a currently-valid access token."""

    def get(self) -> str: ...

# What core-api calls the outcomes, matching 00019's check constraint. Mapped
# rather than passed through, so the enum here and the constraint there cannot
# drift into disagreeing about a value.
_OUTCOMES = {
    Outcome.SUCCEEDED: ingest_pb2.AGENT_RUN_OUTCOME_SUCCEEDED,
    Outcome.REFUSED: ingest_pb2.AGENT_RUN_OUTCOME_REFUSED,
    Outcome.FAILED: ingest_pb2.AGENT_RUN_OUTCOME_FAILED,
}


class CoreAPI:
    """Everything this service may do to the outside world."""

    def __init__(self, base_url: str, tokens: TokenSource) -> None:
        # A SOURCE RATHER THAN A TOKEN, because §1.2 makes access tokens live
        # ten minutes. Holding a string here would mean a service that works
        # for ten minutes after every deploy and then reports that core-api
        # refused it, which gets diagnosed as a network problem several times
        # before somebody checks an expiry.
        self._tokens = tokens
        self._ingest = ingest_connect.IngestServiceClientSync(base_url)
        self._watcher = watcher_connect.WatcherServiceClientSync(base_url)
        self._hands = hands_connect.HandsServiceClientSync(base_url)

    def record_run(self, org_id: str, run: AgentRun) -> str:
        """Store the run, and treat a failure here as serious.

        Not best-effort telemetry. A run that failed to record is not a run
        that did not happen: the finding it produced may already exist, and
        "how this was produced" would then have nothing behind it. The caller
        gets an exception rather than a shrug.

        NO ORGANISATION HEADER. The organisation travels in the message,
        because this caller holds no session and a run happens for whichever
        tenant the work belonged to. See the RPC's own comment for why that is
        safe and what would stop it being so.
        """
        request = ingest_pb2.RecordAgentRunRequest(
            org_id=org_id,
            skill=run.skill,
            skill_version=run.skill_version,
            model=run.model,
            model_version=run.model_version,
            # Who served it (ENT-236). The provider name and nothing else: the
            # key that reached the provider is not in `AgentRun` at all, which
            # is a stronger guarantee than remembering not to send it.
            provider=run.provider,
            request_json="{}",
            tool_calls_json=run.tool_calls_json(),
            citations_json=run.citations_json(),
            outcome=_OUTCOMES[run.outcome],
            outcome_detail=run.outcome_detail,
            # What a critic refused, and which named rule refused it
            # (ENT-248). Separate from `outcome_detail` because that
            # string is shown to the customer beside the finding, and a
            # narrative refused for stating the law wrongly must not be
            # printed under the heading explaining that it was refused.
            refusal_json=run.refusal_json(),
            usage=ingest_pb2.AgentRunUsage(
                input_tokens=run.input_tokens,
                cached_input_tokens=run.cached_input_tokens,
                output_tokens=run.output_tokens,
                # Zero for a local model, which is a true statement about
                # marginal cost rather than a missing value.
                cost_micros=0,
            ),
        )
        request.queued_at.FromDatetime(run.queued_at)
        request.started_at.FromDatetime(run.started_at)
        request.finished_at.FromDatetime(run.finished_at)

        try:
            response = self._ingest.record_agent_run(
                request, headers={"Authorization": f"Bearer {self._tokens.get()}"}
            )
        except Exception as exc:
            raise CoreAPIError(
                f"recording the agent run: {exc}", code=code_of(exc)
            ) from exc

        return response.id

    def read_evidence(
        self, org_id: str, connection_id: str, tool: str, limit: int = 0
    ) -> list[dict[str, object]]:
        """Read what one connection has already reported (ENT-274).

        Everything that decides whether this is allowed happens on the far
        side: that the connection is this organisation's and that the tool is
        granted on it are core-api's checks under the producer role's policies,
        and this client cannot ask it to skip either. A `limit` of zero takes
        core-api's default, which is small on purpose.

        Returns plain dicts rather than the generated messages, for the reason
        `service._watch_context` gives: the skill and the harness deal in plain
        data, so nothing under `harness/` needs the generated package importable
        to be testable.

        NO ORGANISATION HEADER, for the reason `record_run` gives.
        """
        request = watcher_pb2.ReadEvidenceRequest(
            org_id=org_id,
            connection_id=connection_id,
            tool=tool,
            limit=limit,
        )
        try:
            response = self._watcher.read_evidence(
                request, headers={"Authorization": f"Bearer {self._tokens.get()}"}
            )
        except Exception as exc:
            raise CoreAPIError(
                f"reading the evidence: {exc}", code=code_of(exc)
            ) from exc

        return [
            {
                "evidence_id": o.evidence_id,
                "connection_id": o.connection_id,
                "tool": o.tool,
                "observed_at": (
                    o.observed_at.ToDatetime().isoformat()
                    if o.HasField("observed_at")
                    else ""
                ),
                "fetched_at": (
                    o.fetched_at.ToDatetime().isoformat()
                    if o.HasField("fetched_at")
                    else ""
                ),
                # THE ONE FIELD THAT IS SOMEBODY ELSE'S TEXT. Carried across
                # unread and unparsed: code here that branched on it would be
                # code a customer's system can steer, which is the whole reason
                # the contract carries it as a string.
                "body_json": o.body_json,
            }
            for o in response.observations
        ]

    def request_fetch(
        self, org_id: str, connection_id: str, tool: str, reason: str
    ) -> dict[str, str]:
        """Ask core-api to queue a fetch of one granted tool (ENT-279).

        An ask, never a fetch: this client holds no credential, no endpoint
        and no way to dial, and the answer is an acknowledgement rather than a
        payload. Everything that decides whether the ask stands happens on the
        far side: that the connection is this organisation's and active, that
        the tool is granted and read-only, and how recently the pair was
        attempted are core-api's checks, and this client cannot ask it to
        skip any of them. A refusal arrives as a `CoreAPIError` whose code
        says whether a rule worked or something broke, exactly as a read's
        does.

        NO ORGANISATION HEADER, for the reason `record_run` gives.
        """
        request = watcher_pb2.RequestFetchRequest(
            org_id=org_id,
            connection_id=connection_id,
            tool=tool,
            reason=reason,
        )
        try:
            response = self._watcher.request_fetch(
                request, headers={"Authorization": f"Bearer {self._tokens.get()}"}
            )
        except Exception as exc:
            raise CoreAPIError(
                f"asking for a fetch: {exc}", code=code_of(exc)
            ) from exc

        return {
            "state": response.state,
            "detail": response.detail,
            "request_id": response.request_id,
        }

    def raise_signal(self, org_id: str, signal: dict[str, object]) -> tuple[str, bool]:
        """Raise one signal, and answer whether it was new.

        The Watcher's one tool (ENT-258). Everything that decides whether this
        is allowed happens on the far side: the vocabulary, the deduplication
        key, the citation and the producer role's policies are core-api's, and
        this client cannot ask it to skip any of them.

        NO ORGANISATION HEADER, for the reason `record_run` gives: this caller
        holds no session, and the organisation is the one core-api named when
        it built the request this worker was handed.

        Returns `(signal_id, raised)`. `raised` is false when the deduplication
        key already existed, which is not a failure and is the answer the loop
        most needs: it is how a run learns that a condition it noticed was
        already known.
        """
        request = watcher_pb2.RaiseSignalRequest(
            org_id=org_id,
            kind=str(signal.get("kind") or ""),
            dedup_key=str(signal.get("dedup_key") or ""),
            title=str(signal.get("title") or ""),
            detail=str(signal.get("detail") or ""),
            severity=str(signal.get("severity") or ""),
            obligation_slug=str(signal.get("obligation_slug") or ""),
        )
        try:
            response = self._watcher.raise_signal(
                request, headers={"Authorization": f"Bearer {self._tokens.get()}"}
            )
        except Exception as exc:
            raise CoreAPIError(
                f"raising the signal: {exc}", code=code_of(exc)
            ) from exc

        return response.signal_id, response.raised

    def prepare_record(
        self, org_id: str, finding_id: str, plan: dict[str, object]
    ) -> tuple[int, int]:
        """Record what the Hands prepared for one finding: its one tool
        (ENT-261).

        # WHAT THIS ADDS TO WHAT THIS SERVICE CAN REACH, WHICH IS THE QUESTION

        One write, onto a finding, of a plan a person reads before deciding and
        of the payload the Executor would use if they approve. It adds no way
        to approve anything: `FindingsService.ApproveFinding` is `findings:act`
        and is not on this client, is not on this principal's scopes, and reads
        its approver from a session GUC this service does not have. It adds no
        way to create a register entry: that is `ExecutorService.ExecuteJob`,
        acting on a job row written inside the approving transaction (00036),
        and this client cannot write one.

        Everything that decides whether this call is allowed happens on the far
        side, and this client cannot ask core-api to skip any of it: the field
        names are checked against the register the finding's action type names,
        every value must name a fact the organisation holds, and the whole call
        is refused once an approval has been enqueued.

        NO ORGANISATION HEADER, for the reason `record_run` gives: this caller
        holds no session, and the organisation is the one core-api named when
        it built the request this worker was handed.

        Returns `(filled, left)`: how many columns the finding's plan now
        fills, and how many it leaves for a person. Both halves, because a
        model told only how much it filled has been told the flattering half.
        """
        request = hands_pb2.PrepareRecordRequest(
            org_id=org_id,
            finding_id=finding_id,
            explanation=str(plan.get("explanation") or ""),
            fields=[
                hands_pb2.PreparedField(
                    name=str(f.get("name") or ""),
                    values=[str(v) for v in f.get("values") or []],
                    from_fact=str(f.get("from_fact") or ""),
                )
                for f in plan.get("fields") or []  # type: ignore[union-attr]
            ],
            left_for_you=[
                hands_pb2.LeftForYou(
                    name=str(item.get("name") or ""),
                    why=str(item.get("why") or ""),
                )
                for item in plan.get("left_for_you") or []  # type: ignore[union-attr]
            ],
        )
        try:
            response = self._hands.prepare_record(
                request, headers={"Authorization": f"Bearer {self._tokens.get()}"}
            )
        except Exception as exc:
            raise CoreAPIError(
                f"recording the prepared plan: {exc}", code=code_of(exc)
            ) from exc

        return response.filled, response.left
