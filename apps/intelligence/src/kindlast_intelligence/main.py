"""The Intelligence service's entrypoint (ENT-218).

WSGI rather than ASGI, deliberately. The harness is synchronous and CPU-bound
waiting on a model that serves one or two requests at a time (ENT-235), so
async would buy concurrency the thing underneath cannot use, at the cost of
every guardrail becoming reentrant. ENT-238 is where concurrency gets solved,
by putting more model replicas behind a balancer rather than more coroutines in
front of one.
"""

from __future__ import annotations

import logging
import os
import sys

from kindlast.platform.v1 import intelligence_connect

from .auth.jwks import KeySet, _rewrite_host, discover
from .auth.tokens import ClientCredentialsToken
from .auth.verifier import Verifier
from .coreapi import CoreAPI
from .harness.budget import Budget
from .harness.model import ProxiedModelClient
from .worker import start_in_background
from .service import (
    IntelligenceService,
    _client_from_file,
    _from_file,
    config_from_env,
)

logger = logging.getLogger(__name__)


def build_app():
    """Wire the service, or refuse to start.

    Discovery happens here rather than on the first request, so a deployment
    pointed at an unreachable issuer fails at boot with a message about the
    issuer, instead of at 3am with a refused token that looks like a client
    problem.

    Warming the key cache is the other half and is NOT the same kind of check.
    A discovery document that cannot be read is a configuration mistake; a JWKS
    that cannot be read yet is usually just a stack starting up, and a freshly
    seeded Zitadel legitimately serves no keys at all. So one is fatal and the
    other is a warning (ENT-253).
    """
    logging.basicConfig(
        level=os.getenv("KINDLAST_LOG_LEVEL", "INFO"),
        format="%(asctime)s %(levelname)s %(name)s %(message)s",
    )

    config = config_from_env()

    # The issuer as tokens carry it, which may not be where this container
    # reaches it. Same split-horizon shape core-api uses: the document must
    # still declare the issuer, so this changes the address and never the
    # trust.
    document = discover(
        config["issuer"],
        reachable_at=config["discovery_url"],
        host_header=config["host_header"],
    )
    if document["issuer"] != config["issuer"]:
        # Only reachable when a discovery URL was configured separately, which
        # is exactly the case where getting it wrong is easy and silent.
        logger.warning(
            "the discovery document names issuer %s, configured %s",
            document["issuer"],
            config["issuer"],
        )

    keys = KeySet(
        document["jwks_uri"],
        reachable_at=config["discovery_url"],
        host_header=config["host_header"],
    )
    try:
        keys.warm()
    except Exception as exc:  # noqa: BLE001 - a boot race, not a misconfiguration
        # NOT FATAL, DELIBERATELY, and the same call core-api makes for the
        # same reason. `auth` and this service come up together in a compose
        # stack, so losing that race is ordinary; the first token to arrive
        # drives the refetch that finds the key. Exiting here would turn a
        # startup ordering detail into a container that never comes back.
        #
        # A misconfigured issuer still fails at boot, loudly: `discover` above
        # is the check that catches it, and it does raise.
        logger.warning(
            "could not warm the JWKS cache at boot, so the first token will "
            "drive a refetch: %s",
            exc,
        )

    # Environment first, then the volume the seed writes into, because the
    # project id and the client secret are generated per environment and have
    # no host-side path to bake into a compose file. core-api reads its
    # audience the same way.
    file_id, file_secret = _client_from_file()
    client_id = os.getenv("KINDLAST_INTERNAL_CLIENT_ID", "") or file_id
    client_secret = os.getenv("KINDLAST_INTERNAL_CLIENT_SECRET", "") or file_secret
    project_id = (
        os.getenv("KINDLAST_OIDC_PROJECT_ID", "")
        or _from_file("KINDLAST_OIDC_AUDIENCE_FILE")
        or config["audience"]
    )
    if not client_id or not client_secret:
        raise RuntimeError(
            "KINDLAST_INTERNAL_CLIENT_ID and KINDLAST_INTERNAL_CLIENT_SECRET "
            "are required: this service records every run through core-api, "
            "and a deployment that cannot do that would produce findings with "
            "no provenance"
        )

    # The vendor claim, when one is configured. See config_from_env for why
    # this is not optional on a Zitadel deployment.
    scope_claims = (config["scope_claim"],) if config["scope_claim"] else ()
    # THE AUDIENCE IS THE PROJECT ID ON THIS STACK, measured rather than
    # assumed. §1.4 names `kindlast-intelligence` as the audience a dedicated
    # Intelligence application would carry, and Zitadel stamps the project id
    # until one exists. Configuring it rather than hard-coding either keeps
    # §18.2's promise that a self-hoster on another IdP needs no code change.
    audience = config["audience"]
    if audience == "kindlast-intelligence" and project_id:
        audience = project_id

    verifier = Verifier(
        keys,
        issuer=config["issuer"],
        audience=audience,
        scope_claims=scope_claims,
    )

    # Client credentials rather than a token, because §1.2 makes access tokens
    # live ten minutes. See auth/tokens.py for why an environment variable
    # cannot work here.
    tokens = ClientCredentialsToken(
        # Rewritten to where this container can reach it, for the third time in
        # this function. See auth/tokens.py.
        token_endpoint=_rewrite_host(
            document.get("token_endpoint")
            or f"{config['issuer'].rstrip('/')}/oauth/v2/token",
            config["discovery_url"],
        ),
        client_id=client_id,
        client_secret=client_secret,
        project_id=project_id,
        host_header=config["host_header"],
    )

    # Every completion goes through core-api, bound to the organisation the
    # run is for (ENT-256, part five). No model URL is read here, by design.
    core_api_url = config["core_api_url"]
    service = IntelligenceService(
        verifier=verifier,
        core_api=CoreAPI(core_api_url, tokens=tokens),
        model_name=os.getenv("KINDLAST_MODEL_NAME", "unknown"),
        model_version=os.getenv("KINDLAST_MODEL_VERSION", "unknown"),
        model_factory=lambda org_id: ProxiedModelClient(core_api_url, tokens, org_id),
        budget=Budget(),
    )

    # The worker half (ENT-256, part five): the same service, polling the
    # `intelligence` task queue for drafts the Go worker's sweep workflow
    # schedules. Empty means no worker, which is the RPC half alone.
    temporal_addr = os.getenv("KINDLAST_TEMPORAL_ADDR", "").strip()
    if temporal_addr:
        start_in_background(
            service,
            temporal_addr,
            os.getenv("KINDLAST_TEMPORAL_NAMESPACE", "default"),
            int(os.getenv("KINDLAST_INTELLIGENCE_CONCURRENCY", "2")),
        )
    else:
        logging.getLogger("kindlast.intelligence").warning(
            "no temporal worker: KINDLAST_TEMPORAL_ADDR is not set, so findings are "
            "narrated only when NarrateFindings is called by hand"
        )

    return intelligence_connect.IntelligenceServiceWSGIApplication(service)


def main() -> int:
    try:
        app = build_app()
    except Exception as exc:  # noqa: BLE001 - a startup failure must be readable
        print(f"intelligence: refusing to start: {exc}", file=sys.stderr)
        return 1

    from waitress import serve

    port = int(os.getenv("KINDLAST_INTELLIGENCE_PORT", "8090"))
    logger.info("intelligence listening on port %s", port)

    # WAITRESS, NOT wsgiref.simple_server, AND THE REFERENCE SERVER WAS TRIED.
    #
    # The first version used `wsgiref.simple_server` on the reasoning that the
    # model behind this serves one request at a time anyway, so a
    # single-threaded server costs nothing. It does not work: the app answers
    # correctly in-process, and the same app behind the reference server hangs
    # with the connection open and no response. wsgiref is a specification
    # example rather than a server, and Connect's client is enough of a real
    # HTTP client to find that out.
    #
    # The symptom is the bad part: a hang rather than an error, which reads as
    # "the model is slow" and sends you looking in entirely the wrong place.
    #
    # `threads` stays low deliberately. Concurrency here is bounded by the
    # model, not by this process, and a pool larger than the model's slot count
    # only converts a queue you can see into memory you cannot. ENT-238 is
    # where that queue gets designed properly.
    serve(
        app,
        host="0.0.0.0",  # noqa: S104
        port=port,
        threads=int(os.getenv("KINDLAST_INTELLIGENCE_THREADS", "4")),
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
