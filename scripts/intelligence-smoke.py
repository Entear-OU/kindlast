#!/usr/bin/env python3
"""Drive Intelligence end to end, the way core-api will.

ENT-218's last acceptance criterion, as a script rather than as prose in an
issue, so CI runs the same thing a developer runs.

    python3 scripts/intelligence-smoke.py

It needs the compose stack up with the `model` profile, and it reads the
service credentials the seed wrote out of the container that already has them
mounted. Nothing here holds a secret of its own.

# WHAT IT ASSERTS, AND WHAT IT DELIBERATELY DOES NOT

It asserts the harness worked. It does NOT assert that the run succeeded, and
the difference is the whole design of section 26.3.

A local 2B answering a GDPR question cites articles 50, 34 and 54 on three
consecutive tries, where the answer is 30, and all three are schema-valid. So a
run that ends REFUSED because the citation did not resolve is the guardrail
doing its job, not a regression, and a CI gate demanding SUCCEEDED would go red
for the one reason it should stay green. It would also be flaky by design,
which is the failure mode that teaches a team to re-run a job rather than read
it.

What must hold on every run, whatever the model said:

  * an authenticated call reaches the harness and comes back 200
  * the outcome is SUCCEEDED or REFUSED, never FAILED and never unspecified
  * a run was recorded, so the provenance of the answer exists either way
  * a REFUSED run returns no narrative, because withholding the prose is the
    point of refusing it
  * a call with no token is refused before any handler body runs

The last one is why this job exists rather than trusting the unit suite: the
token battery proves the verifier rejects a bad token, and only a running
service on a real network proves the verifier is in front of the handler rather
than beside it.
"""

from __future__ import annotations

import json
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

INTELLIGENCE = "http://localhost:8090"
TOKEN_ENDPOINT = "http://localhost:8300/oauth/v2/token"

# One obligation, and a signal that plainly matches it. Not a hard question:
# this tests the harness, and the model's job here is only to produce something
# for the validator to have an opinion about.
OBLIGATIONS = [
    {
        "slug": "gdpr-art-30",
        "title": "Records of processing activities",
        "summary": (
            "A controller must maintain a record of the processing activities "
            "under its responsibility, and make it available to the "
            "supervisory authority on request."
        ),
    }
]
SIGNAL = (
    "A new customer support tool was connected last week and no record of "
    "processing activities was updated for it."
)


def fail(message: str) -> None:
    print(f"intelligence-smoke: {message}", file=sys.stderr)
    sys.exit(1)


def _in_container(path: str) -> str:
    result = subprocess.run(
        ["docker", "exec", "kindlast-intelligence", "cat", path],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        fail(f"reading {path}: {result.stderr.strip()}")
    return result.stdout.strip()


def read_org_id() -> str:
    """An organisation that actually exists, looked up rather than invented.

    Recording a run is a foreign key into `organisations`, and a run that
    cannot be recorded is fatal by design, so a made-up UUID here fails the
    whole script with a message about the record rather than about the id.
    Asking the database is also the only version of this that survives the seed
    changing its fixtures.
    """
    result = subprocess.run(
        [
            "docker",
            "exec",
            "kindlast-postgres-app",
            "psql",
            "-U",
            "kindlast_migrator",
            "-d",
            "kindlast",
            "-tAc",
            "select id from public.organisations order by created_at limit 1",
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    org_id = result.stdout.strip()
    if result.returncode != 0 or not org_id:
        fail(f"no organisation to run for: {result.stderr.strip()}")
    return org_id


def read_credentials() -> tuple[str, str, str]:
    """Read the seed's credentials out of the container that already holds them.

    Rather than mounting the volume here or copying the values into the
    workflow. A secret that exists in exactly one place has one place to leak
    from, and a CI log is a place.
    """
    client = json.loads(_in_container("/machinekey/core-api-client.json"))
    audience = _in_container("/machinekey/core-api-audience.txt")
    return client["clientId"], client["clientSecret"], audience


def mint(client_id: str, client_secret: str, audience: str) -> str:
    """The same grant the service itself uses, with the same two odd facts.

    `client_id` is the service user's USERNAME rather than its id, and the
    granted roles reach the token only if the plural
    `urn:zitadel:iam:org:projects:roles` scope is requested. Neither is
    guessable; both are written down in the service's `auth/tokens.py` and in
    the Postman collection, which are the two places somebody debugging a
    refused token will be looking.
    """
    body = urllib.parse.urlencode(
        {
            "grant_type": "client_credentials",
            "client_id": client_id,
            "client_secret": client_secret,
            "scope": " ".join(
                (
                    "openid",
                    "urn:zitadel:iam:org:projects:roles",
                    f"urn:zitadel:iam:org:project:id:{audience}:aud",
                )
            ),
        }
    ).encode()
    request = urllib.request.Request(
        TOKEN_ENDPOINT,
        data=body,
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        token = json.load(response).get("access_token")
    if not token:
        fail("the authorization server returned no access_token")
    return token


def draft(token: str | None, org_id: str) -> tuple[int, dict]:
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    request = urllib.request.Request(
        f"{INTELLIGENCE}/kindlast.platform.v1.IntelligenceService/DraftNarrative",
        data=json.dumps(
            {
                "orgId": org_id,
                "signal": SIGNAL,
                "obligations": OBLIGATIONS,
            }
        ).encode(),
        headers=headers,
    )
    # Generous, because a cold llama.cpp on a CI runner is doing prompt
    # evaluation on a CPU. The wall-clock budget inside the harness is the
    # thing that should decide a run is too slow (ENT-238); a timeout here
    # would only decide this script is impatient.
    try:
        with urllib.request.urlopen(request, timeout=300) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, {}


def main() -> None:
    client_id, client_secret, audience = read_credentials()
    token = mint(client_id, client_secret, audience)
    org_id = read_org_id()

    status, payload = draft(token, org_id)
    if status != 200:
        fail(f"an authenticated draft returned {status}, expected 200")

    outcome = payload.get("outcome", "DRAFT_OUTCOME_UNSPECIFIED")
    if outcome not in ("DRAFT_OUTCOME_SUCCEEDED", "DRAFT_OUTCOME_REFUSED"):
        fail(f"outcome was {outcome}: the harness broke rather than decided")

    if not payload.get("agentRunId"):
        fail("no agent_run_id: the run happened and its provenance was not stored")

    if outcome == "DRAFT_OUTCOME_REFUSED" and payload.get("narrative"):
        fail("a refused run returned a narrative, which must be withheld")

    detail = payload.get("outcomeDetail") or ""
    print(f"intelligence-smoke: {outcome} run={payload['agentRunId']} {detail}".strip())
    if outcome == "DRAFT_OUTCOME_REFUSED":
        print("intelligence-smoke: refused is a pass here, see this file's docstring")

    # THE ASSERTION THE UNIT SUITE STRUCTURALLY CANNOT MAKE. It proves the
    # verifier rejects a bad token; only a running service proves the verifier
    # is in front of the handler rather than beside it.
    status, _ = draft(token=None, org_id=org_id)
    if status not in (401, 403):
        fail(f"a call with no token returned {status}, and must be refused")
    print(f"intelligence-smoke: unauthenticated call refused with {status}")


if __name__ == "__main__":
    main()
