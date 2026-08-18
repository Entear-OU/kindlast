"""The service, exercised over the wire (ENT-218).

These go through the real WSGI application, the real Connect codec and the real
generated stubs, rather than calling the handler as a function. §13.2's
reasoning about the verifier applies to the surface in front of it too: calling
the method directly would leave the interceptor, the codec and the routing
untested while looking like coverage, and every one of those has been where a
bug hid at least once in this codebase.

The model and core-api are fakes, because what these assert is the SERVICE's
behaviour: that a refusal is a 200 with an outcome, that an unverified token
never reaches a handler, and that a run nobody could record is not returned as
a success. The model's opinions are tested elsewhere, against the real one.
"""

from __future__ import annotations

import json
from wsgiref.util import setup_testing_defaults

import pytest
from conftest import TEST_AUDIENCE, AuthServer
from connectrpc.errors import ConnectError
from kindlast.platform.v1 import intelligence_connect, intelligence_pb2

from kindlast_intelligence.auth.jwks import KeySet, discover
from kindlast_intelligence.auth.verifier import Verifier
from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.model import Completion
from kindlast_intelligence.service import IntelligenceService

OBLIGATION = intelligence_pb2.ObligationContext(
    slug="gdpr-art-30-ropa",
    title="Records of Processing Activities",
    summary="Article 30 requires a written record of what you do with personal data.",
)


class FakeModel:
    def __init__(self, payload=None, *, raw=None):
        self._raw = raw if raw is not None else json.dumps(payload)

    def complete(self, messages, schema=None, max_tokens=800, temperature=0.7):
        return Completion(
            content=self._raw,
            input_tokens=100,
            cached_input_tokens=0,
            output_tokens=40,
            finish_reason="stop",
        )


class FakeCoreAPI:
    """Records runs, or refuses to, on demand."""

    def __init__(self, *, fail: bool = False) -> None:
        self._fail = fail
        self.recorded = []

    def record_run(self, org_id, run):
        if self._fail:
            from kindlast_intelligence.coreapi import CoreAPIError

            raise CoreAPIError("core-api is unreachable")
        self.recorded.append((org_id, run))
        return "11111111-1111-4111-8111-111111111111"


def a_good_answer(citations=("gdpr-art-30-ropa",)):
    return {
        "narrative": "You hold employee data, so Article 30 requires a written record of it.",
        "citations": list(citations),
        "confident": True,
    }


def a_service(auth_server: AuthServer, model=None, core_api=None):
    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()

    return IntelligenceService(
        verifier=Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE),
        model=model or FakeModel(a_good_answer()),
        core_api=core_api or FakeCoreAPI(),
        model_name="Qwen3.5-2B-Q4_K_M",
        model_version="aaf42c8b",
        budget=Budget(),
    )


def call(service, request, token: str | None):
    """Drive the real WSGI app, so the codec and routing are in the path."""
    app = intelligence_connect.IntelligenceServiceWSGIApplication(service)

    body = request.SerializeToString()
    environ = {}
    setup_testing_defaults(environ)
    environ.update(
        {
            "REQUEST_METHOD": "POST",
            "PATH_INFO": "/kindlast.platform.v1.IntelligenceService/DraftNarrative",
            "CONTENT_TYPE": "application/proto",
            "CONTENT_LENGTH": str(len(body)),
            "wsgi.input": __import__("io").BytesIO(body),
        }
    )
    if token is not None:
        environ["HTTP_AUTHORIZATION"] = f"Bearer {token}"

    captured: dict = {}

    def start_response(status, headers, exc_info=None):
        captured["status"] = status
        captured["headers"] = dict(headers)

    chunks = b"".join(app(environ, start_response))
    return captured, chunks


def a_request(**overrides):
    fields = {
        "org_id": "1961c05f-5e88-4f2f-92a1-d26600e0bcd0",
        "signal": "We are a 40 person firm holding employee payroll records.",
        "obligations": [OBLIGATION],
    }
    fields.update(overrides)
    return intelligence_pb2.DraftNarrativeRequest(**fields)


# --- Authority, before anything else ---------------------------------------


def test_no_token_never_reaches_the_handler(auth_server):
    service = a_service(auth_server)
    core_api = FakeCoreAPI()
    service._core_api = core_api

    captured, _ = call(service, a_request(), token=None)

    assert "200" not in captured["status"]
    assert core_api.recorded == [], "the handler ran without a token"


def test_a_human_token_is_refused(auth_server):
    """The property this whole service rests on, over the wire.

    A real, correctly signed, unexpired token carrying the scopes a person
    actually holds. It must not reach a handler on any path, because
    Intelligence has no tenancy GUCs and cannot check whether this human
    should see what it is about to process.
    """
    service = a_service(auth_server)
    core_api = FakeCoreAPI()
    service._core_api = core_api

    human = auth_server.claims(
        sub="user-subject-1", scope="findings:read records:read", client_id="web"
    )
    captured, _ = call(service, a_request(), token=auth_server.mint(human))

    assert "200" not in captured["status"]
    assert core_api.recorded == []


# --- A refusal is a 200 -----------------------------------------------------


def test_an_invented_citation_comes_back_as_a_refusal_not_an_error(auth_server):
    """§26.3: refusal is what a working guardrail produces.

    Returning an error code here would tell the caller the harness broke when
    it did exactly what it was built to do.
    """
    service = a_service(auth_server, model=FakeModel(a_good_answer(("gdpr-art-99",))))

    captured, body = call(
        service, a_request(), token=auth_server.mint(auth_server.claims())
    )

    assert "200" in captured["status"]
    response = intelligence_pb2.DraftNarrativeResponse()
    response.ParseFromString(body)

    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_REFUSED
    assert response.narrative == "", "a refused narrative must not be returned"
    assert [r.slug for r in response.rejected_citations] == ["gdpr-art-99"]


def test_an_em_dash_comes_back_as_a_refusal_and_is_recorded_as_one(auth_server):
    """ENT-163, over the wire and into the record.

    Two properties in one test because they are the same property seen from
    either end. The caller gets `REFUSED` and no prose, so nothing downstream
    can render a narrative the house style forbids; and `agent_runs` gets the
    same outcome with a detail naming the character, so a customer asking why
    they have no finding reads an answer rather than a blank.
    """
    core_api = FakeCoreAPI()
    service = a_service(
        auth_server,
        model=FakeModel(
            {
                "narrative": "You hold employee payroll records — so you need a "
                "written record of that processing.",
                "citations": ["gdpr-art-30-ropa"],
                "confident": True,
            }
        ),
        core_api=core_api,
    )

    captured, body = call(
        service, a_request(), token=auth_server.mint(auth_server.claims())
    )

    assert "200" in captured["status"], "a guardrail firing is not an error"
    response = intelligence_pb2.DraftNarrativeResponse()
    response.ParseFromString(body)

    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_REFUSED
    assert response.narrative == ""
    assert "em dash (U+2014)" in response.outcome_detail

    _, recorded = core_api.recorded[0]
    assert recorded.outcome == "refused"
    assert "em dash (U+2014)" in recorded.outcome_detail
    assert recorded.narrative == ""


def test_a_successful_draft_returns_the_narrative_and_the_run_id(auth_server):
    service = a_service(auth_server)

    captured, body = call(
        service, a_request(), token=auth_server.mint(auth_server.claims())
    )

    assert "200" in captured["status"]
    response = intelligence_pb2.DraftNarrativeResponse()
    response.ParseFromString(body)

    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_SUCCEEDED
    assert response.narrative
    assert response.resolved_citations == ["gdpr-art-30-ropa"]
    assert response.agent_run_id


# --- Provenance is not optional ---------------------------------------------


def test_a_run_that_could_not_be_recorded_is_not_returned_as_a_success(auth_server):
    """The narrative is withheld when its provenance could not be stored.

    Returning it and shrugging at a failed record produces exactly the thing
    the record exists to prevent: a finding nobody can check.
    """
    service = a_service(auth_server, core_api=FakeCoreAPI(fail=True))

    captured, _ = call(
        service, a_request(), token=auth_server.mint(auth_server.claims())
    )

    assert "200" not in captured["status"]


# --- Inputs the caller must supply ------------------------------------------


def test_a_request_with_no_obligations_is_refused(auth_server):
    """A run with nothing to cite can only produce a narrative citing nothing.

    Refused as a bad request rather than attempted, so "why is this finding
    unexplained" answers "it was asked without context" rather than "the model
    said little".
    """
    service = a_service(auth_server)

    captured, _ = call(
        service,
        a_request(obligations=[]),
        token=auth_server.mint(auth_server.claims()),
    )

    assert "200" not in captured["status"]


def test_an_empty_signal_is_refused(auth_server):
    service = a_service(auth_server)

    captured, _ = call(
        service, a_request(signal="   "), token=auth_server.mint(auth_server.claims())
    )

    assert "200" not in captured["status"]
