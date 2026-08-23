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

import json
import logging
import os
from pathlib import Path
from typing import Callable

from connectrpc.errors import ConnectError
from connectrpc.code import Code
from connectrpc.request import RequestContext
from kindlast.platform.v1 import intelligence_pb2

from .auth.errors import ScopeMissing, VerificationError
from .auth.verifier import Verifier
from .coreapi import CoreAPI, CoreAPIError
from .harness.budget import Budget
from .harness.citations import CitationValidator, OfferedObligations
from .harness.model import Completer
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
        core_api: CoreAPI,
        model_name: str,
        model_version: str,
        model_factory: Callable[[str], Completer],
        budget: Budget | None = None,
    ) -> None:
        self._verifier = verifier
        self._core_api = core_api
        self._model_name = model_name
        self._model_version = model_version
        self._budget_template = budget
        # How the model client for one run is built: from the organisation id
        # and nothing else (ENT-256, part five). In production that is a
        # ProxiedModelClient bound to the organisation, asking core-api for
        # every completion; in tests it is whatever fake the test wants. A
        # seam rather than a direct construction, so the tests can assert that
        # the client was bound to the right organisation without a network.
        self._model_factory = model_factory

    def draft_narrative(
        self,
        request: intelligence_pb2.DraftNarrativeRequest,
        ctx: RequestContext,
    ) -> intelligence_pb2.DraftNarrativeResponse:
        """The RPC: authorise, then draft. The Temporal activity (worker.py)
        calls `draft` directly, because its caller is the engine on a queue
        only this deployment's workers poll, and what it is handed was built
        by core-api."""
        self._authorise(ctx)
        return self.draft(request)

    def draft(
        self,
        request: intelligence_pb2.DraftNarrativeRequest,
    ) -> intelligence_pb2.DraftNarrativeResponse:
        """One draft: the harness, the guardrail ring, the citation validator
        and the run record, for a request core-api built. Raises ConnectError
        for a malformed request (INVALID_ARGUMENT) and for a run that could
        not be recorded (INTERNAL)."""
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

        model, model_name, model_version, provider = self._model_for(
            request.org_id, request.model_endpoint
        )

        run = draft_narrative(
            signal=request.signal,
            obligations=obligations,
            model=model,
            validator=CitationValidator(OfferedObligations(obligations)),
            model_name=model_name,
            model_version=model_version,
            provider=provider,
            # A fresh budget per run, from the template. Sharing one across
            # requests would let a busy morning refuse an afternoon's work.
            #
            # `renew` rather than a field list written out here: the list this
            # replaced would have silently dropped `max_queue_seconds` when
            # ENT-238 added it, and the run would have used the default while
            # the operator's configured value sat in the template unread.
            budget=self._budget_template.renew() if self._budget_template else None,
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
            # INTERPOLATED, NOT `extra=`. A dict passed as `extra` is only
            # rendered if the formatter names its keys, and the default format
            # does not, so the first version of this line logged "recording the
            # run failed" and threw the reason away. An error message that
            # omits the error is worse than no log line, because it looks like
            # you already looked.
            logger.error("recording the run failed: %s", exc)
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

    def _model_for(
        self, org_id: str, endpoint: intelligence_pb2.ModelEndpoint
    ) -> tuple[Completer, str, str, str]:
        """Which model serves this run, and what the record should say.

        # THE CREDENTIAL RULE, RESTORED (ENT-256, part five)

        `AGENTS.md` says no third-party credential reaches this service, and
        since ENT-236 it had one recorded exception: an organisation's API
        key arrived in `model_endpoint.api_key` for one call. That exception
        is retired. Every completion goes through core-api's
        CompletionService, bound to the organisation; core-api resolves the
        choice inside the tenant's own rows, opens the key only it holds, and
        makes the call. This process holds no endpoint and no credential, by
        construction, and a request that carries either is REFUSED rather
        than honoured: a key arriving here is the old exception coming back
        through a door that is no longer open, and the right answer to it is
        "no", loudly, so the caller that sent it gets fixed.

        What the request still carries is the provider and model NAMES, for
        the run record, which is what a sub-processor record needs and is
        not a secret. Absent means the deployment's own model.

        # AND NO FALLBACK, EVER

        An organisation that chose a provider gets that provider or gets a
        failed run. The decision is core-api's now, and core-api makes it the
        same way this code used to: a choice that cannot be honoured is an
        error, never a quiet answer from the bundled model.
        """
        if endpoint.api_key or endpoint.base_url:  # deprecated on the wire, refused on purpose
            raise ConnectError(
                Code.INVALID_ARGUMENT,
                "this service holds no model endpoint and no credential: "
                "completions go through core-api's CompletionService, and a "
                "request carrying model_endpoint.base_url or .api_key is "
                "refused (ENT-256, part five)",
            )

        client = self._model_factory(org_id)
        if not endpoint.provider:
            return client, self._model_name, self._model_version, "instance"
        # `provider-managed` rather than an invented digest. A local build
        # records the weights digest because it knows it; a hosted provider
        # serves whatever it is serving today and does not say, and a version
        # string this service made up would be worse than one that says so.
        if endpoint.provider == "instance":
            return client, endpoint.model or self._model_name, self._model_version, "instance"
        return (
            client,
            endpoint.model or self._model_name,
            "provider-managed",
            endpoint.provider,
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
            logger.warning("refusing a token: %s", exc)
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


def _from_file(env_name: str) -> str:
    """Read a value the seed wrote into a shared volume.

    Zitadel generates the project id and the service client's secret per
    environment, into a docker volume, so neither can be baked into a compose
    file or an image. core-api reads its audience the same way, and this
    follows that shape rather than inventing a second one.

    Missing is not an error here: the caller decides whether the value was
    required, so a deployment configuring things by environment variable
    instead does not have to satisfy a file it never wrote.
    """
    path = os.getenv(env_name, "").strip()
    if not path:
        return ""
    try:
        return Path(path).read_text().strip()
    except OSError:
        return ""


def _client_from_file() -> tuple[str, str]:
    """The seed writes `{clientId, clientSecret}` as JSON."""
    raw = _from_file("KINDLAST_INTERNAL_CLIENT_FILE")
    if not raw:
        return "", ""
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return "", ""
    return str(payload.get("clientId", "")), str(payload.get("clientSecret", ""))


def _expand_audience(template: str, audience: str) -> str:
    if not template or "{audience}" not in template:
        return template
    if not audience:
        raise RuntimeError(
            "KINDLAST_OIDC_SCOPE_CLAIM names {audience} but no project id is "
            "available; set KINDLAST_OIDC_AUDIENCE_FILE or "
            "KINDLAST_OIDC_PROJECT_ID. Left unexpanded, the claim name would "
            "match nothing and every genuine caller would be refused for "
            "holding no authority."
        )
    return template.replace("{audience}", audience)


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
        # Zitadel routes by Host, so reaching it at a container address needs
        # the public one in the header or the request lands on the wrong
        # virtual server and 404s. core-api carries the same setting for the
        # same measured reason.
        "host_header": os.getenv("KINDLAST_OIDC_HOST_HEADER", ""),
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
        # `{audience}` is expanded from the audience file, because Zitadel
        # names its roles claim after the project the roles belong to and that
        # id is generated per environment. core-api makes the same
        # substitution for the same reason (ENT-221); without it the claim name
        # is a literal nothing matches, and every genuine caller is refused for
        # holding no authority.
        "scope_claim": _expand_audience(
            os.getenv("KINDLAST_OIDC_SCOPE_CLAIM", ""),
            _from_file("KINDLAST_OIDC_AUDIENCE_FILE")
            or os.getenv("KINDLAST_OIDC_PROJECT_ID", ""),
        ),
        "core_api_url": os.environ["KINDLAST_CORE_API_URL"],
        # No model URL, deliberately (ENT-256, part five). This service dials
        # no model: every completion goes through core-api's
        # CompletionService, and the deployment's own model URL is core-api's
        # setting. test_no_third_party_credential.py asserts the absence.
    }
