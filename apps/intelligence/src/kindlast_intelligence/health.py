"""A liveness endpoint in front of the Connect application (ENT-296).

# WHY THIS EXISTS AT ALL

This service used to answer only on its Connect paths, every one of which
requires `internal:intelligence` on the call. So there was no way to ask
whether the process was up without holding a client credential, which meant
three things were true at once: the compose stack carried no healthcheck for
it, `docker compose ps` reported it `Up` and never `(healthy)`, and the
console's status dot beside Kindy was a hardcoded green that could not go out.

# WHY IT IS UNAUTHENTICATED

A probe that needs a token cannot distinguish "the service is down" from "my
token expired", and those are different incidents with different things to do
about them. core-api holds a client credential and could have used it, but
then the credential's health would be part of the signal, which is precisely
the confusion this is meant to remove.

That is only safe because the answer discloses nothing. It is the fact that a
process is accepting connections and nothing else: no version, no model name,
no queue depth, no configuration. `test_health.py` asserts the exact body so
that adding any of those fails a test before it reaches a deployment.

# WHY IT IS A WRAPPER RATHER THAN A ROUTE ON THE SERVICE

The Connect application is generated, and `IngestService`'s neighbours are all
scope-guarded by construction. Putting an unauthenticated path inside it would
mean one hand-maintained exception in a surface whose whole safety argument is
that it has none. A wrapper keeps the generated application exactly as
generated, and keeps the exception in one file with the reasoning attached.
"""

from __future__ import annotations

# The whole body, as bytes, because it never varies. A caller that wants to
# know anything more than this is asking the wrong endpoint.
_OK = b'{"status": "ok"}'

_PATH = "/healthz"

# GET for a probe that reads the answer, HEAD for one that only wants the
# status line. Anything else falls through to Connect, which answers 404, so a
# POST to this path can never be misread by a probe as health.
_METHODS = frozenset({"GET", "HEAD"})


def with_health(app):
    """Wrap a WSGI application so `GET /healthz` answers without reaching it.

    Every other request is passed through byte for byte. The wrapper is a
    wrapper: it must not become a router, and the test that asserts delegation
    is what holds that line.
    """

    def wrapped(environ, start_response):
        if environ.get("PATH_INFO") == _PATH and environ.get("REQUEST_METHOD") in _METHODS:
            start_response(
                "200 OK",
                [
                    ("Content-Type", "application/json"),
                    ("Content-Length", str(len(_OK))),
                    # A probe reading a cached 200 from an intermediary would
                    # report health for a process that had since stopped, which
                    # is worse than no probe.
                    ("Cache-Control", "no-store"),
                ],
            )
            if environ.get("REQUEST_METHOD") == "HEAD":
                return [b""]
            return [_OK]
        return app(environ, start_response)

    return wrapped
