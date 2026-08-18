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

from .auth.jwks import KeySet, discover
from .auth.verifier import Verifier
from .coreapi import CoreAPI
from .harness.budget import Budget
from .harness.model import ModelClient
from .service import IntelligenceService, config_from_env

logger = logging.getLogger(__name__)


def build_app():
    """Wire the service, or refuse to start.

    Discovery and JWKS warming happen here rather than on the first request, so
    a deployment pointed at an unreachable issuer fails at boot with a message
    about the issuer, instead of at 3am with a refused token that looks like a
    client problem.
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
    document = discover(config["discovery_url"] or config["issuer"])
    if document["issuer"] != config["issuer"]:
        # Only reachable when a discovery URL was configured separately, which
        # is exactly the case where getting it wrong is easy and silent.
        logger.warning(
            "the discovery document names a different issuer than configured",
            extra={"document": document["issuer"], "configured": config["issuer"]},
        )

    keys = KeySet(document["jwks_uri"])
    keys.warm()

    # The vendor claim, when one is configured. See config_from_env for why
    # this is not optional on a Zitadel deployment.
    scope_claims = (config["scope_claim"],) if config["scope_claim"] else ()
    verifier = Verifier(
        keys,
        issuer=config["issuer"],
        audience=config["audience"],
        scope_claims=scope_claims,
    )

    core_api_token = os.getenv("KINDLAST_INTERNAL_TOKEN", "")
    if not core_api_token:
        raise RuntimeError(
            "KINDLAST_INTERNAL_TOKEN is required: this service records every "
            "run through core-api, and a deployment that cannot do that would "
            "produce findings with no provenance"
        )

    service = IntelligenceService(
        verifier=verifier,
        model=ModelClient(config["model_url"]),
        core_api=CoreAPI(config["core_api_url"], token=core_api_token),
        model_name=os.getenv("KINDLAST_MODEL_NAME", "unknown"),
        model_version=os.getenv("KINDLAST_MODEL_VERSION", "unknown"),
        budget=Budget(),
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
    logger.info("intelligence listening", extra={"port": port})

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
