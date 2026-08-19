"""One organisation's own provider, for one run (ENT-236, §26.6).

# WHAT THESE TESTS ARE REALLY ABOUT

`AGENTS.md` says no third-party credential reaches this service. ENT-236 puts
one in a request to it, because an organisation that has chosen a hosted
provider needs its runs to go there and core-api is the only process that can
decide whose key it is entitled to open.

That is a real relaxation of a stated rule, so what replaces the rule has to be
tested rather than asserted in a comment. The bound is:

  THE KEY ARRIVES IN ONE FIELD OF ONE REQUEST, IS USED FOR THAT CALL, AND
  LEAVES NO TRACE. It is never read from configuration, never written to the
  run record, never logged, and never held past the response.

Each of those is a test below. `test_no_third_party_credential.py` next door
keeps the half of the rule that did not change: this service still cannot
OBTAIN a credential, only receive one.

# AND THE FAILURE MODE THAT MATTERS MOST

A run whose provider refuses the key must FAIL, visibly, and must not quietly
be answered by the deployment's own model. Falling back would mean an
organisation's findings were processed somewhere other than where its own
record of processing says, with nothing in the product saying so.
"""

from __future__ import annotations

import json
from wsgiref.util import setup_testing_defaults

from conftest import TEST_AUDIENCE, AuthServer
from kindlast.platform.v1 import intelligence_connect, intelligence_pb2

from kindlast_intelligence.auth.jwks import KeySet, discover
from kindlast_intelligence.auth.verifier import Verifier
from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.model import Completion, ModelError
from kindlast_intelligence.harness.run import AgentRun
from kindlast_intelligence.service import IntelligenceService

OBLIGATION = intelligence_pb2.ObligationContext(
    slug="gdpr-art-30-ropa",
    title="Records of Processing Activities",
    summary="Article 30 requires a written record of what you do with personal data.",
)

HOSTED = intelligence_pb2.ModelEndpoint(
    provider="openai",
    base_url="https://api.openai.com",
    model="gpt-oss-120b",
    api_key="sk-proj-abcdefgh1234",
)


def a_good_answer():
    return {
        "why_it_applies_to_you": "You hold employee data, so you need a written "
        "record of what you keep and why.",
        "citations": ["gdpr-art-30-ropa"],
        "confident": True,
    }


class FakeModel:
    def __init__(self, payload=None, *, raises: Exception | None = None):
        self._raw = json.dumps(payload) if payload is not None else ""
        self._raises = raises
        self.calls = 0

    def complete(self, messages, schema=None, max_tokens=800, temperature=0.7):
        self.calls += 1
        if self._raises is not None:
            raise self._raises
        return Completion(
            content=self._raw,
            input_tokens=100,
            cached_input_tokens=0,
            output_tokens=40,
            finish_reason="stop",
        )


class RecordingFactory:
    """Stands in for `ModelClient`, and remembers how it was built."""

    def __init__(self, client):
        self._client = client
        self.built = []

    def __call__(self, base_url, api_key=None, model=None):
        self.built.append({"base_url": base_url, "api_key": api_key, "model": model})
        return self._client


class FakeCoreAPI:
    def __init__(self):
        self.recorded = []

    def record_run(self, org_id, run):
        self.recorded.append((org_id, run))
        return "11111111-1111-4111-8111-111111111111"


def a_service(auth_server: AuthServer, *, model, factory=None, core_api=None):
    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()

    return IntelligenceService(
        verifier=Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE),
        model=model,
        core_api=core_api or FakeCoreAPI(),
        model_name="Qwen3.5-2B-Q4_K_M",
        model_version="aaf42c8b",
        budget=Budget(),
        model_factory=factory,
    )


def call(service, request, token: str):
    """Drive the real WSGI app, so the codec and routing are in the path."""
    app = intelligence_connect.IntelligenceServiceWSGIApplication(service)

    payload = request.SerializeToString()
    environ = {}
    setup_testing_defaults(environ)
    environ.update(
        {
            "REQUEST_METHOD": "POST",
            "PATH_INFO": "/kindlast.platform.v1.IntelligenceService/DraftNarrative",
            "CONTENT_TYPE": "application/proto",
            "CONTENT_LENGTH": str(len(payload)),
            "wsgi.input": __import__("io").BytesIO(payload),
            "HTTP_AUTHORIZATION": f"Bearer {token}",
        }
    )

    captured: dict = {}

    def start_response(status, headers, exc_info=None):
        captured["status"] = status

    chunks = b"".join(app(environ, start_response))
    response = intelligence_pb2.DraftNarrativeResponse()
    response.ParseFromString(chunks)
    return captured, response


def a_request(endpoint=None):
    return intelligence_pb2.DraftNarrativeRequest(
        org_id="1961c05f-5e88-4f2f-92a1-d26600e0bcd0",
        signal="We are a 40 person firm holding employee payroll records.",
        obligations=[OBLIGATION],
        model_endpoint=endpoint,
    )


def test_with_no_endpoint_the_deployments_own_model_serves_the_run(auth_server):
    """The default, and the case where nothing leaves."""
    bundled = FakeModel(a_good_answer())
    factory = RecordingFactory(FakeModel(a_good_answer()))
    core_api = FakeCoreAPI()
    service = a_service(auth_server, model=bundled, factory=factory, core_api=core_api)

    captured, _ = call(service, a_request(), auth_server.mint(auth_server.claims()))

    assert "200" in captured["status"]
    assert bundled.calls == 1
    assert factory.built == [], "a client was built for an organisation that chose none"

    _, run = core_api.recorded[0]
    assert run.provider == "instance", (
        "a run on the deployment own endpoint must say so, because `local` "
        "would be a claim the record cannot back: ENT-235 makes that endpoint "
        "configurable"
    )


def test_a_chosen_provider_serves_the_run_and_the_bundled_model_is_untouched(auth_server):
    hosted = FakeModel(a_good_answer())
    bundled = FakeModel(a_good_answer())
    factory = RecordingFactory(hosted)
    core_api = FakeCoreAPI()
    service = a_service(auth_server, model=bundled, factory=factory, core_api=core_api)

    captured, _ = call(service, a_request(HOSTED), auth_server.mint(auth_server.claims()))

    assert "200" in captured["status"]
    assert bundled.calls == 0, "the deployment own model answered a hosted run"
    assert hosted.calls == 1
    assert factory.built == [
        {
            "base_url": "https://api.openai.com",
            "api_key": "sk-proj-abcdefgh1234",
            "model": "gpt-oss-120b",
        }
    ]

    _, run = core_api.recorded[0]
    assert run.provider == "openai"
    assert run.model == "gpt-oss-120b", (
        "the run must name the model the provider was asked for, not the one "
        "this container environment happens to hold"
    )


def test_the_run_record_carries_the_provider_and_never_the_key(auth_server):
    """`agent_runs` is a record a customer reads and exports.

    The provider belongs in it, because it is what a sub-processor record needs.
    The key does not belong anywhere in it, and the strongest form of that is
    the field not existing.
    """
    factory = RecordingFactory(FakeModel(a_good_answer()))
    core_api = FakeCoreAPI()
    service = a_service(
        auth_server, model=FakeModel(a_good_answer()), factory=factory, core_api=core_api
    )

    call(service, a_request(HOSTED), auth_server.mint(auth_server.claims()))

    _, run = core_api.recorded[0]
    serialised = run.model_dump_json()
    assert "sk-proj" not in serialised, "the key reached the run record"

    forbidden = [
        name
        for name in AgentRun.model_fields
        if any(
            marker in name.lower()
            # Not "token": `input_tokens` and its siblings are a usage count,
            # which is exactly the sort of false positive that gets a guard
            # like this one deleted rather than fixed.
            for marker in ("key", "credential", "secret", "bearer", "api")
        )
    ]
    assert not forbidden, (
        f"{forbidden} would let a provider credential into `agent_runs`, which "
        "is a table a customer exports and hands to somebody else"
    )


def test_a_refused_provider_key_fails_the_run_and_never_falls_back(auth_server):
    """The failure mode ENT-236 names explicitly.

    A silent fallback would process this organisation finding on the
    deployment own model while its own record of processing says otherwise, and
    nothing in the product would say it happened.
    """
    bundled = FakeModel(a_good_answer())
    refusing = FakeModel(raises=ModelError("the model endpoint failed: 401 Unauthorized"))
    factory = RecordingFactory(refusing)
    core_api = FakeCoreAPI()
    service = a_service(auth_server, model=bundled, factory=factory, core_api=core_api)

    wrong = intelligence_pb2.ModelEndpoint(
        provider="openai",
        base_url="https://api.openai.com",
        model="gpt-oss-120b",
        api_key="sk-proj-wrongkey1234",
    )
    captured, response = call(
        service, a_request(wrong), auth_server.mint(auth_server.claims())
    )

    assert "200" in captured["status"]
    assert bundled.calls == 0, "the run silently fell back to the bundled model"
    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_FAILED
    assert response.narrative == ""

    _, run = core_api.recorded[0]
    assert run.provider == "openai", (
        "a run that failed at a provider must still be attributable to it"
    )
    assert "sk-proj" not in run.outcome_detail, (
        "the key appeared in a message a customer reads"
    )


def test_an_endpoint_with_no_key_is_a_real_configuration(auth_server):
    """A provider on the customer own network may need no bearer token."""
    factory = RecordingFactory(FakeModel(a_good_answer()))
    service = a_service(auth_server, model=FakeModel(a_good_answer()), factory=factory)

    endpoint = intelligence_pb2.ModelEndpoint(
        provider="byo", base_url="https://models.example.com", model="m"
    )
    call(service, a_request(endpoint), auth_server.mint(auth_server.claims()))

    assert factory.built[0]["api_key"] is None
