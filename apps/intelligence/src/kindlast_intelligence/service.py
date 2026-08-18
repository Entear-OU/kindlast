"""IntelligenceService: the harness, served (ENT-218, §1.6, §26).

# EVERY REQUEST IS VERIFIED HERE AND NOWHERE ELSE

`verify_internal` runs before any handler body. It is the whole of this
service's authority, and the reason there is exactly one place it happens is
that a second one is a second place to get it wrong.

# A REFUSAL IS A 200

§26.3 makes refusal what a working guardrail produces, so it comes back as an
outcome rather than as an error code. Returning `internal` for a spent budget
would tell the caller the harness broke when it did precisely what it was built
to do, and would put that in the column a customer reads to decide whether to
trust a finding.

Errors are reserved for the things that genuinely went wrong: an unverifiable
token, a malformed request, an unreachable model.
"""

from __future__ import annotations

import logging
import os

from connectrpc.errors import ConnectError
from connectrpc.code import Code
from connectrpc.request import RequestContext
from kindlast.platform.v1 import intelligence_pb2

from .auth.errors import ScopeMissing, VerificationError
from .auth.verifier import Verifier
from .coreapi import CoreAPI, CoreAPIError
from .harness.budget import Budget
from .harness.citations import CitationValidator, OfferedObligations
from .harness.model import ModelClient
from .harness.run import Outcome, draft_narrative

logger = logging.getLogger(__name__)

_OUTCOMES = {
    Outcome.SUCCEEDED: intelligence_pb2.DRAFT_OUTCOME_SUCCEEDED,
    Outcome.REFUSED: intelligence_pb2.DRAFT_OUTCOME_REFUSED,
    Outcome.FAILED: intelligence_pb2.DRAFT_OUTCOME_FAILED,
}


class IntelligenceService:
    """Implements the generated `IntelligenceService` protocol."""

    def __init__(
        self,
        verifier: Verifier,
        model: ModelClient,
        core_api: CoreAPI,
        model_name: str,
        model_version: str,
        budget: Budget | None = None,
    ) -> None:
        self._verifier = verifier
        self._model = model
        self._core_api = core_api
        self._model_name = model_name
        self._model_version = model_version
        self._budget_template = budget

    def draft_narrative(
        self,
        request: intelligence_pb2.DraftNarrativeRequest,
        ctx: RequestContext,
    ) -> intelligence_pb2.DraftNarrativeResponse:
        self._authorise(ctx)

        if not request.org_id:
            raise ConnectError(Code.INVALID_ARGUMENT, "org_id is required")
        if not request.signal.strip():
            raise ConnectError(Code.INVALID_ARGUMENT, "signal is required")

        obligations = [
            {"slug": o.slug, "title": o.title, "summary": o.summary}
            for o in request.obligations
        ]
        if not obligations:
            # Refused rather than attempted. A run with nothing to cite can
            # only produce a narrative citing nothing, and the useful answer to
            # "why is this finding unexplained" is that it was asked without
            # context rather than that the model said little.
            raise ConnectError(
                Code.INVALID_ARGUMENT,
                "at least one obligation is required: a run with nothing to "
                "cite cannot produce a citable narrative",
            )

        run = draft_narrative(
            signal=request.signal,
            obligations=obligations,
            model=self._model,
            validator=CitationValidator(OfferedObligations(obligations)),
            model_name=self._model_name,
            model_version=self._model_version,
            # A fresh budget per run, from the template. Sharing one across
            # requests would let a busy morning refuse an afternoon's work.
            budget=Budget(**self._budget_template.model_dump(
                include={"max_total_tokens", "max_model_calls", "max_tool_calls",
                         "max_depth", "max_seconds"}
            )) if self._budget_template else None,
        )

        # RECORDED BEFORE THE RESPONSE IS BUILT, AND A FAILURE HERE IS FATAL.
        #
        # Not best-effort telemetry. If the caller goes on to store a finding
        # from this narrative, "how this was produced" has to have something
        # behind it. Returning the narrative and shrugging at a failed record
        # produces exactly the finding nobody can check.
        try:
            agent_run_id = self._core_api.record_run(request.org_id, run)
        except CoreAPIError as exc:
            logger.error("recording the run failed", extra={"error": str(exc)})
            raise ConnectError(
                Code.INTERNAL,
                "the run completed but could not be recorded, so it is being "
                "reported as failed rather than returned unprovenanced",
            ) from exc

        return intelligence_pb2.DraftNarrativeResponse(
            outcome=_OUTCOMES[run.outcome],
            # Empty unless it succeeded. A refused narrative is withheld rather
            # than returned with a warning attached, because a caller handed
            # prose plus a note is a caller that eventually shows the prose.
            narrative=run.narrative,
            outcome_detail=run.outcome_detail,
            resolved_citations=run.resolved_citations,
            rejected_citations=[
                intelligence_pb2.RejectedCitation(
                    slug=r["slug"], reason=r["reason"]
                )
                for r in run.rejected_citations
            ],
            agent_run_id=agent_run_id,
        )

    def _authorise(self, ctx: RequestContext) -> None:
        """Verify the bearer token, or refuse.

        Every failure maps to the same code. Telling a caller whether their
        token was expired, forged or minted for another audience is a
        distinction only somebody probing the boundary has a use for.
        """
        header = _first_header(ctx, "authorization")
        if not header or not header.lower().startswith("bearer "):
            raise ConnectError(Code.UNAUTHENTICATED, "a bearer token is required")

        try:
            self._verifier.verify_internal(header[7:].strip())
        except ScopeMissing as exc:
            # Distinguished from the rest because it is the only one that is
            # about authority rather than authenticity: the caller is who they
            # say they are, and may not do this.
            raise ConnectError(Code.PERMISSION_DENIED, str(exc)) from exc
        except VerificationError as exc:
            logger.warning("refusing a token", extra={"reason": str(exc)})
            raise ConnectError(Code.UNAUTHENTICATED, "the token was refused") from exc


def _first_header(ctx: RequestContext, name: str) -> str | None:
    """Read one request header.

    `request_headers` is a METHOD on RequestContext, not an attribute, and
    treating it as one produced a 500 reading "'function' object has no
    attribute 'get'" rather than anything about headers. Worth the note because
    the symptom points nowhere near the cause, and because getattr on an API
    you have not read is how you get there.

    A header may arrive as a list, since HTTP permits repeats. Taking the first
    rather than joining: two Authorization headers is not a credential, it is a
    confused client, and picking one is the least surprising of the bad
    options.
    """
    headers = ctx.request_headers()
    value = headers.get(name) or headers.get(name.title())
    if isinstance(value, (list, tuple)):
        return value[0] if value else None
    return value


def config_from_env() -> dict[str, str]:
    """What a deployment must set.

    NO MODEL API KEY IN THIS LIST, and that absence is the product property
    rather than an omission (§18.1). Inference is local by default, so a
    deployment holding a compliance record needs no third-party credential to
    run the Analyst.
    """
    required = ("KINDLAST_OIDC_ISSUER", "KINDLAST_CORE_API_URL")
    missing = [name for name in required if not os.getenv(name)]
    if missing:
        raise RuntimeError(
            f"missing required configuration: {', '.join(missing)}. "
            "This service refuses to start half-configured, because the "
            "failure would otherwise appear as a refused token much later."
        )

    return {
        "issuer": os.environ["KINDLAST_OIDC_ISSUER"],
        "discovery_url": os.getenv("KINDLAST_OIDC_DISCOVERY_URL", ""),
        "audience": os.getenv("KINDLAST_OIDC_AUDIENCE", "kindlast-intelligence"),
        # WHERE THE CALLER'S GRANTED AUTHORITY ACTUALLY LIVES.
        #
        # Measured, not assumed, and the same fact ENT-221 established on the
        # Go side: Zitadel emits neither `scope` nor `scp` on an access token,
        # whatever is requested. It asserts project roles under
        # `urn:zitadel:iam:org:project:<projectId>:roles`, whose value is an
        # object keyed by role name.
        #
        # A verifier reading only RFC 9068's `scope` therefore finds no
        # authority at all and refuses every genuine caller, which is exactly
        # what happened the first time this service was driven end to end: a
        # correctly minted token carrying internal:intelligence came back as
        # "token does not carry 'internal:intelligence'".
        #
        # Configurable rather than hard-coded, for the §18.2 reason: a
        # self-hoster pointing at their own IdP must not need a code change,
        # and this service must not grow a table of vendor quirks.
        "scope_claim": os.getenv("KINDLAST_OIDC_SCOPE_CLAIM", ""),
        "core_api_url": os.environ["KINDLAST_CORE_API_URL"],
        "model_url": os.getenv("KINDLAST_MODEL_URL", "http://model:8080"),
    }
