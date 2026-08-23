"""One organisation's own provider, for one run (ENT-236, §26.6), as it
stands after ENT-256 part five: names in, nothing else.

# WHAT THESE TESTS ARE REALLY ABOUT

`AGENTS.md` says no third-party credential reaches this service. ENT-236 put
one in a request to it for one call, and tested the bound on that relaxation.
ENT-256 retired the relaxation: every completion goes through core-api's
CompletionService, bound to the organisation, and core-api resolves the choice,
opens the key and makes the call. So the bound these tests keep is now:

  THE REQUEST CARRIES PROVIDER AND MODEL NAMES FOR THE RUN RECORD AND NOTHING
  ELSE. A request carrying an endpoint or a key is REFUSED, before any model
  call, and the model client built for the run is bound to the organisation
  the run is for and to nothing the request said.

`test_no_third_party_credential.py` next door keeps the config-layer half:
this service reads no model URL and no key from anywhere.

# AND THE FAILURE MODE THAT MATTERS MOST

A run whose provider refuses, or whose organisation's choice cannot be
honoured, FAILS, visibly: core-api answers the completion with an error and
the run records that. Nothing in this process can fall back to the bundled
model, because this process has no bundled model to fall back to.
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

# What core-api sends now: names only.
HOSTED = intelligence_pb2.ModelEndpoint(provider="openai", model="gpt-oss-120b")

# What a caller that has not heard would send, and is refused.
WITH_KEY = intelligence_pb2.ModelEndpoint(
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
    """Stands in for `ProxiedModelClient`'s construction, and remembers which
    organisation each client was bound to."""

    def __init__(self, client):
        self._client = client
        self.built = []

    def __call__(self, org_id):
        self.built.append(org_id)
        return self._client


class FakeCoreAPI:
    def __init__(self):
        self.recorded = []

    def record_run(self, org_id, run):
        self.recorded.append((org_id, run))
        return "11111111-1111-4111-8111-111111111111"


def a_service(auth_server: AuthServer, *, factory, core_api=None):
    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()

    return IntelligenceService(
        verifier=Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE),
        core_api=core_api or FakeCoreAPI(),
        model_name="Qwen3.5-2B-Q4_K_M",
        model_version="aaf42c8b",
        model_factory=factory,
        budget=Budget(),
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
    if "200" not in captured["status"]:
        # A Connect error envelope, not a response message. The tests that
        # expect a refusal read the status and the absence of side effects.
        return captured, None
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


ORG = "1961c05f-5e88-4f2f-92a1-d26600e0bcd0"


def test_the_model_client_is_bound_to_the_organisation_and_to_nothing_the_request_said(auth_server):
    # The one thing core-api needs to decide whose model answers is the
    # organisation, so the one thing the client is built from is the
    # organisation. Not a URL, not a key: there is nowhere in the request for
    # either to come from any more.
    proxied = FakeModel(a_good_answer())
    factory = RecordingFactory(proxied)
    service = a_service(auth_server, factory=factory)

    captured, response = call(service, a_request(), auth_server.mint(auth_server.claims()))

    assert "200" in captured["status"]
    assert proxied.calls == 1
    assert factory.built == [ORG], "the client was bound to something other than the organisation"
    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_SUCCEEDED


def test_with_no_endpoint_the_record_names_the_deployments_own_model(auth_server):
    factory = RecordingFactory(FakeModel(a_good_answer()))
    core_api = FakeCoreAPI()
    service = a_service(auth_server, factory=factory, core_api=core_api)

    captured, _ = call(service, a_request(), auth_server.mint(auth_server.claims()))

    assert "200" in captured["status"]
    _, run = core_api.recorded[0]
    assert run.provider == "instance", (
        "an organisation that chose nothing is recorded against the instance model"
    )
    assert run.model == "Qwen3.5-2B-Q4_K_M"
    assert run.model_version == "aaf42c8b"


def test_a_chosen_provider_is_recorded_by_name_and_the_client_is_still_bound_to_the_org(auth_server):
    factory = RecordingFactory(FakeModel(a_good_answer()))
    core_api = FakeCoreAPI()
    service = a_service(auth_server, factory=factory, core_api=core_api)

    captured, _ = call(service, a_request(HOSTED), auth_server.mint(auth_server.claims()))

    assert "200" in captured["status"]
    assert factory.built == [ORG]
    _, run = core_api.recorded[0]
    assert run.provider == "openai"
    assert run.model == "gpt-oss-120b", (
        "the record names the model the provider serves, not the instance model"
    )
    assert run.model_version == "provider-managed"


def test_a_request_carrying_an_endpoint_or_a_key_is_refused_before_any_model_call(auth_server):
    # THE TEST THE RULE RESTS ON. A key arriving here is the retired exception
    # coming back through a door that is no longer open, and it is refused
    # rather than used, so the caller that sent it gets fixed.
    proxied = FakeModel(a_good_answer())
    factory = RecordingFactory(proxied)
    core_api = FakeCoreAPI()
    service = a_service(auth_server, factory=factory, core_api=core_api)

    captured, _ = call(service, a_request(WITH_KEY), auth_server.mint(auth_server.claims()))

    assert "400" in captured["status"], captured
    assert proxied.calls == 0, "a model call was made for a request carrying a key"
    assert factory.built == [], "a client was built for a request carrying a key"
    assert core_api.recorded == [], "a run was recorded for a refused request"

    # An endpoint alone, with no key, is refused for the same reason: this
    # process dials nothing, so an endpoint is a caller that has not heard.
    endpoint_only = intelligence_pb2.ModelEndpoint(
        provider="byo", base_url="https://models.example.com", model="m"
    )
    captured, _ = call(service, a_request(endpoint_only), auth_server.mint(auth_server.claims()))
    assert "400" in captured["status"], captured
    assert proxied.calls == 0


def test_the_run_record_carries_the_provider_and_never_a_key(auth_server):
    # Belt and braces on the record itself: even a request that carries names
    # only produces a record whose every string is free of anything key-shaped.
    core_api = FakeCoreAPI()
    service = a_service(auth_server, factory=RecordingFactory(FakeModel(a_good_answer())), core_api=core_api)

    call(service, a_request(HOSTED), auth_server.mint(auth_server.claims()))

    _, run = core_api.recorded[0]
    serialised = json.dumps(
        {k: v for k, v in vars(run).items() if isinstance(v, (str, int, float, bool, list, dict))},
        default=str,
    )
    assert "sk-proj" not in serialised
    assert "api_key" not in serialised
    forbidden = [
        name
        for name in AgentRun.model_fields
        if "key" in name.lower() or "secret" in name.lower() or "url" in name.lower()
    ]
    assert not forbidden, f"AgentRun has a field that could hold a credential: {forbidden}"


def test_a_completion_core_api_refuses_fails_the_run_and_says_why(auth_server):
    # core-api answers the completion with an error when the organisation's
    # provider cannot be honoured or refuses the key. This process records a
    # failed run with the reason and does not, cannot, fall back: it has no
    # other model to fall back to.
    refusing = FakeModel(
        raises=ModelError(
            "core-api could not complete the call: failed_precondition: "
            "the openai endpoint refused this organisation credential (HTTP 401)"
        )
    )
    core_api = FakeCoreAPI()
    service = a_service(auth_server, factory=RecordingFactory(refusing), core_api=core_api)

    captured, response = call(service, a_request(HOSTED), auth_server.mint(auth_server.claims()))

    assert "200" in captured["status"]
    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_FAILED
    assert response.narrative == ""
    assert refusing.calls == 1
    _, run = core_api.recorded[0]
    assert run.provider == "openai", "the failed run is recorded against the provider that refused"
    assert "refused" in run.outcome_detail
