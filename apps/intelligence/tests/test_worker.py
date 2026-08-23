"""The Temporal activity (ENT-256, part five): the same draft the RPC
performs, reached from the `intelligence` task queue instead of over HTTP.

What is asserted: the activity is registered under the name the Go workflow
schedules, it maps a request core-api built through `IntelligenceService.draft`
to the same response the RPC returns, a malformed request is a non-retryable
failure (retrying core-api's bug produces the same refusal), and a run that
could not be recorded is a retryable one (the record is what gives the draft
provenance, and core-api being briefly away is not a reason to give up).
"""

from __future__ import annotations

import json

import pytest
from conftest import TEST_AUDIENCE, AuthServer
from kindlast.platform.v1 import intelligence_pb2
from temporalio.exceptions import ApplicationError
from temporalio.testing import ActivityEnvironment

from kindlast_intelligence import worker
from kindlast_intelligence.auth.jwks import KeySet, discover
from kindlast_intelligence.auth.verifier import Verifier
from kindlast_intelligence.coreapi import CoreAPIError
from kindlast_intelligence.harness.budget import Budget
from kindlast_intelligence.harness.model import Completion
from kindlast_intelligence.service import IntelligenceService

OBLIGATION = intelligence_pb2.ObligationContext(
    slug="gdpr-art-30-ropa",
    title="Records of Processing Activities",
    summary="Article 30 requires a written record of what you do with personal data.",
)


class FakeModel:
    def __init__(self, payload):
        self._raw = json.dumps(payload)

    def complete(self, messages, schema=None, max_tokens=800, temperature=0.7):
        return Completion(
            content=self._raw, input_tokens=100, cached_input_tokens=0, output_tokens=40, finish_reason="stop"
        )


class FakeCoreAPI:
    def __init__(self, fail=False):
        self.recorded = []
        self.fail = fail

    def record_run(self, org_id, run):
        if self.fail:
            raise CoreAPIError("core-api is restarting")
        self.recorded.append((org_id, run))
        return "11111111-1111-4111-8111-111111111111"


def a_service(auth_server: AuthServer, core_api=None):
    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()
    good = FakeModel(
        {
            "why_it_applies_to_you": "You hold employee data, so you need a written record of what you keep and why.",
            "citations": ["gdpr-art-30-ropa"],
            "confident": True,
        }
    )
    return IntelligenceService(
        verifier=Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE),
        core_api=core_api or FakeCoreAPI(),
        model_name="Qwen3.5-2B-Q4_K_M",
        model_version="aaf42c8b",
        model_factory=lambda org_id: good,
        budget=Budget(),
    )


def a_request(**overrides):
    fields = dict(
        org_id="1961c05f-5e88-4f2f-92a1-d26600e0bcd0",
        signal="We are a 40 person firm holding employee payroll records.",
        obligations=[OBLIGATION],
    )
    fields.update(overrides)
    return intelligence_pb2.DraftNarrativeRequest(**fields)


def test_the_activity_is_registered_under_the_name_the_go_workflow_schedules():
    assert worker.ACTIVITY_NAME == "DraftNarrative"
    assert worker.TASK_QUEUE == "intelligence"


def test_the_activity_drafts_and_records_exactly_as_the_rpc_does(auth_server):
    core_api = FakeCoreAPI()
    fn = worker.make_activity(a_service(auth_server, core_api=core_api))

    response = ActivityEnvironment().run(fn, a_request())

    assert response.outcome == intelligence_pb2.DRAFT_OUTCOME_SUCCEEDED
    assert response.narrative
    assert response.agent_run_id == "11111111-1111-4111-8111-111111111111"
    assert len(core_api.recorded) == 1, "the run was not recorded through core-api"


def test_a_malformed_request_is_a_non_retryable_failure(auth_server):
    fn = worker.make_activity(a_service(auth_server))

    with pytest.raises(ApplicationError) as failure:
        ActivityEnvironment().run(fn, a_request(obligations=[]))

    assert failure.value.non_retryable, "retrying core-api's bug would refuse it again"
    assert failure.value.type == "bad-request"


def test_a_run_that_could_not_be_recorded_is_retryable(auth_server):
    fn = worker.make_activity(a_service(auth_server, core_api=FakeCoreAPI(fail=True)))

    with pytest.raises(ApplicationError) as failure:
        ActivityEnvironment().run(fn, a_request())

    assert not failure.value.non_retryable, "a record that failed because core-api was away is worth a retry"
