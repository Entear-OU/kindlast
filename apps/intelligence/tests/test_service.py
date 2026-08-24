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
        "why_it_applies_to_you": "You hold employee data, so you need a written "
        "record of what you keep and why.",
        "citations": list(citations),
        "confident": True,
    }


def a_service(auth_server: AuthServer, model=None, core_api=None):
    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()

    fake = model or FakeModel(a_good_answer())
    return IntelligenceService(
        verifier=Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE),
        core_api=core_api or FakeCoreAPI(),
        model_name="Qwen3.5-2B-Q4_K_M",
        model_version="aaf42c8b",
        # Every run's model client is built from the organisation id
        # (ENT-256, part five); here it is the test's fake regardless.
        model_factory=lambda org_id: fake,
        budget=Budget(),
    )


def call(service, request, token: str | None, method: str = "DraftNarrative"):
    """Drive the real WSGI app, so the codec and routing are in the path.

    `method` names the RPC rather than the whole path, because the path is a
    Connect detail and a caller here is choosing which RPC to exercise. It has a
    default so the drafting tests below read as they did before there was a
    second surface to choose between.
    """
    app = intelligence_connect.IntelligenceServiceWSGIApplication(service)

    body = request.SerializeToString()
    environ = {}
    setup_testing_defaults(environ)
    environ.update(
        {
            "REQUEST_METHOD": "POST",
            "PATH_INFO": f"/kindlast.platform.v1.IntelligenceService/{method}",
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
                "why_it_applies_to_you": "You hold employee payroll records — so you need a "
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


# --- Asking about a finding (ENT-270) ---------------------------------------


def an_answer(citations=("gdpr-art-30-ropa",)):
    return {
        "answer": "You hold payroll records for your own staff, so the record "
        "we are asking for is about that data and who else sees it.",
        "citations": list(citations),
        "confident": True,
    }


def a_question(**overrides):
    fields = {
        "org_id": "1961c05f-5e88-4f2f-92a1-d26600e0bcd0",
        "question": "Why does this apply to us?",
        "finding": intelligence_pb2.FindingContext(
            detected="No record of processing activities exists for payroll.",
            proposed_action="Create a record covering payroll.",
            severity="high",
        ),
        "obligations": [OBLIGATION],
    }
    fields.update(overrides)
    return intelligence_pb2.AnswerFindingQuestionRequest(**fields)


def ask(service, auth_server, request=None, token: str | None = ...):
    if token is ...:
        token = auth_server.mint(auth_server.claims())
    return call(
        service,
        request if request is not None else a_question(),
        token=token,
        method="AnswerFindingQuestion",
    )


def test_a_question_with_no_token_never_reaches_the_handler(auth_server):
    """The same property as DraftNarrative, asserted separately.

    A new RPC on a service whose authority is one `_authorise` call is exactly
    where somebody forgets the call, and the test that walks the other RPC
    cannot see it. ENT-245 is the version of this that shipped.
    """
    service = a_service(auth_server, model=FakeModel(an_answer()))
    core_api = FakeCoreAPI()
    service._core_api = core_api

    captured, _ = ask(service, auth_server, token=None)

    assert "200" not in captured["status"]
    assert core_api.recorded == [], "the handler ran without a token"


def test_a_human_token_cannot_ask(auth_server):
    """A person's question reaches this service through core-api and never
    directly. Intelligence has no tenancy GUCs, so it cannot check whether the
    human in front of the question is entitled to the finding it is about."""
    service = a_service(auth_server, model=FakeModel(an_answer()))
    core_api = FakeCoreAPI()
    service._core_api = core_api

    human = auth_server.claims(
        sub="user-subject-1", scope="findings:read agents:ask", client_id="web"
    )
    captured, _ = ask(service, auth_server, token=auth_server.mint(human))

    assert "200" not in captured["status"]
    assert core_api.recorded == []


def test_an_answer_comes_back_with_the_run_that_produced_it(auth_server):
    core_api = FakeCoreAPI()
    service = a_service(auth_server, model=FakeModel(an_answer()), core_api=core_api)

    captured, body = ask(service, auth_server)

    assert "200" in captured["status"]
    response = intelligence_pb2.AnswerFindingQuestionResponse()
    response.ParseFromString(body)

    assert response.outcome == intelligence_pb2.ANSWER_OUTCOME_SUCCEEDED
    assert "payroll" in response.answer
    assert response.agent_run_id
    # The provenance is what makes "how this was produced" showable while
    # `agent_runs` has no read path. A response carrying an id and nothing else
    # would leave a console with a uuid under a heading.
    assert response.provenance.skill == "analyst.answer"
    assert response.provenance.skill_version
    assert response.provenance.provider == "instance"
    assert [org for org, _ in core_api.recorded] == [
        "1961c05f-5e88-4f2f-92a1-d26600e0bcd0"
    ]


def test_an_answer_citing_something_it_was_not_offered_is_a_refusal_not_an_error(
    auth_server,
):
    service = a_service(auth_server, model=FakeModel(an_answer(("gdpr-art-99",))))

    captured, body = ask(service, auth_server)

    assert "200" in captured["status"]
    response = intelligence_pb2.AnswerFindingQuestionResponse()
    response.ParseFromString(body)

    assert response.outcome == intelligence_pb2.ANSWER_OUTCOME_REFUSED
    assert response.answer == "", "a refused answer must not be returned"
    # A refusal is recorded too, because "we tried and it cited an article we
    # never showed it" is what somebody deciding whether to trust this should
    # be able to read.
    assert response.agent_run_id


def test_an_answer_that_could_not_be_recorded_is_not_returned(auth_server):
    """The sharpest version of the rule `draft` states: this answer goes
    straight onto a person's screen, so an answer with no record behind it is
    the unprovenanced claim delivered to the reader most likely to act on it."""
    service = a_service(
        auth_server, model=FakeModel(an_answer()), core_api=FakeCoreAPI(fail=True)
    )

    captured, _ = ask(service, auth_server)

    assert "200" not in captured["status"]


def test_a_question_with_no_obligations_is_refused(auth_server):
    service = a_service(auth_server, model=FakeModel(an_answer()))

    captured, _ = ask(service, auth_server, a_question(obligations=[]))

    assert "200" not in captured["status"]


def test_a_blank_question_is_a_recorded_refusal_rather_than_an_error(auth_server):
    """The person typed nothing, or typed spaces. That is a sentence to show
    them and a run to record, not a transport error with nothing behind it."""
    core_api = FakeCoreAPI()
    service = a_service(auth_server, model=FakeModel(an_answer()), core_api=core_api)

    captured, body = ask(service, auth_server, a_question(question="   "))

    assert "200" in captured["status"]
    response = intelligence_pb2.AnswerFindingQuestionResponse()
    response.ParseFromString(body)

    assert response.outcome == intelligence_pb2.ANSWER_OUTCOME_REFUSED
    assert response.agent_run_id
    assert len(core_api.recorded) == 1
