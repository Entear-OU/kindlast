"""The core-api client: this service's only way to reach anything (§1.6, §26.3).

# ONE RPC, AND THE SHORTNESS IS THE POINT

ENT-218 says an agent's tools are core-api RPCs and nothing else. No
filesystem, no shell, no database handle, no third-party credential. That is
not a limitation to work around later; it is the reason a model is allowed near
a compliance record at all, because everything it can reach is something
core-api already checks.

This wraps exactly one call, `RecordAgentRun`, and offers no way to make a
second. Adding one means adding a method here, which means a reviewer sees it.

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

from kindlast.platform.v1 import ingest_connect, ingest_pb2

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


class CoreAPIError(Exception):
    """core-api refused or could not be reached."""


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
            raise CoreAPIError(f"recording the agent run: {exc}") from exc

        return response.id
