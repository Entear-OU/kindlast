"""The liveness endpoint (ENT-296).

This service answered only on its Connect paths, so nothing could ask whether
it was up without holding a client credential and spending a run. The compose
stack therefore had no healthcheck for it, and the console's status dot beside
Kindy was a hardcoded green that meant nothing.

The endpoint is deliberately small, and every test below is about a way it
could stop being small.
"""

from __future__ import annotations

import json

from kindlast_intelligence.health import with_health


class _Recorder:
    """A stand-in for the Connect application, so delegation is observable."""

    def __init__(self) -> None:
        self.calls: list[str] = []

    def __call__(self, environ, start_response):
        self.calls.append(environ["PATH_INFO"])
        start_response("200 OK", [("Content-Type", "application/json")])
        return [b"{}"]


def _call(app, path, method="GET"):
    captured: dict[str, object] = {}

    def start_response(status, headers):
        captured["status"] = status
        captured["headers"] = headers

    body = b"".join(app({"PATH_INFO": path, "REQUEST_METHOD": method}, start_response))
    return captured["status"], dict(captured["headers"]), body


def test_healthz_answers_without_a_credential():
    """The whole point: reachable by something holding nothing.

    A probe that needed a token could not tell "the service is down" from "my
    token expired", and those have different things to do about them.
    """
    status, headers, body = _call(with_health(_Recorder()), "/healthz")

    assert status == "200 OK"
    assert headers["Content-Type"] == "application/json"
    assert json.loads(body) == {"status": "ok"}


def test_healthz_never_reaches_the_service():
    """It is a liveness probe, not a request the harness sees.

    Delegating it would put an unauthenticated path into the Connect
    application, which is the shape of an accident rather than a feature.
    """
    recorder = _Recorder()

    _call(with_health(recorder), "/healthz")

    assert recorder.calls == []


def test_every_other_path_is_delegated_unchanged():
    """The wrapper is a wrapper. Nothing else about the surface may move."""
    recorder = _Recorder()
    app = with_health(recorder)

    _call(app, "/kindlast.platform.v1.IntelligenceService/NarrateFindings", "POST")

    assert recorder.calls == [
        "/kindlast.platform.v1.IntelligenceService/NarrateFindings"
    ]


def test_healthz_discloses_nothing_but_liveness():
    """A health endpoint anybody can reach must not become a status page.

    Versions, model names, queue depths and configuration are all things a
    reader of this file might reasonably want here later, and all of them are
    things an unauthenticated caller should not be told. The assertion is on
    the exact body so adding one fails here first.
    """
    _, _, body = _call(with_health(_Recorder()), "/healthz")

    assert json.loads(body) == {"status": "ok"}


def test_only_get_and_head_are_answered():
    """Anything else is delegated, so a POST to /healthz is a 404 from Connect
    rather than a 200 that a probe would misread as health."""
    recorder = _Recorder()

    _call(with_health(recorder), "/healthz", "POST")

    assert recorder.calls == ["/healthz"]


def test_head_carries_no_body():
    status, _, body = _call(with_health(_Recorder()), "/healthz", "HEAD")

    assert status == "200 OK"
    assert body == b""
