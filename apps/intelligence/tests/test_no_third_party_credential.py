"""This service holds no third-party credential and reaches no customer system.

ENT-231 lists it as an acceptance criterion and asks that it be asserted AT THE
CONFIG LAYER, which is the right place: a credential does not appear in a
service by being used, it appears by being configured, and every one of these
tests fails on the setting rather than on the call that would eventually use
it.

# WHY THIS IS A REAL RISK RATHER THAN A HYPOTHETICAL ONE

The harness would happily hold an MCP client. It is the obvious way to let an
agent look something up in a customer's helpdesk, it is fewer moving parts than
a gateway, and every framework in this space ships an example doing exactly
that. What it costs is four things at once: a customer's credential in the
process that runs model output, a tool surface defined by a customer's server
rather than by the customer, unlabelled third-party text one hop from the
model, and no stored picture anybody can inspect.

So integrations live in `workers`, behind a policy gateway, and evidence
reaches Intelligence as stored rows through core-api tools. The property that
keeps that true is the absence tested here.

# THE SHAPE OF THESE TESTS MIRRORS test_no_database.py

Against the manifest and the source text rather than by importing and probing,
for the reason that file gives: a package that is not declared cannot be
imported by accident later, and a setting that is not read cannot be set by
accident either.

# WHAT ENT-236 CHANGED, AND WHAT ENT-256 CHANGED BACK

ENT-236 put one thing through the rule: an organisation that chose a hosted
model provider had its API key arrive in one field of one `DraftNarrative`
request, used for that call and not held. It was a recorded relaxation, with
the half that mattered preserved: this service could not OBTAIN a credential.

ENT-256 part five retired the relaxation rather than keeping it. Every
completion now goes through core-api's CompletionService, bound to the
organisation; core-api resolves the choice, opens the key and makes the call.
This service dials no model, reads no model URL, and refuses a request that
carries an endpoint or a key. The tests below assert the config-layer half (no
model URL, no credential in `config_from_env`), and `test_model_endpoint.py`
asserts the request-layer half (a key in a request is refused before any model
call, and the client built for a run is bound to the organisation alone).
"""

from __future__ import annotations

import re
import tomllib
from pathlib import Path

from kindlast_intelligence.service import config_from_env

PYPROJECT = Path(__file__).resolve().parent.parent / "pyproject.toml"
SRC = Path(__file__).resolve().parent.parent / "src"

# The one credential this service legitimately holds: its own OAuth client, for
# calling core-api as itself. Named here so the assertions below can be a
# blanket rule with one written-down exception rather than a list somebody
# extends without noticing.
OWN_CLIENT_MARKERS = ("KINDLAST_INTERNAL_CLIENT",)

# Packages that speak a third party's protocol or hold a third party's token.
# An MCP client is the one somebody would actually reach for; the OAuth
# libraries are the shape the connectors would take if they landed here instead
# of in the gateway.
THIRD_PARTY_PACKAGES = {
    "mcp",
    "fastmcp",
    "mcp-python",
    "modelcontextprotocol",
    "authlib",
    "oauthlib",
    "requests-oauthlib",
    "google-auth",
    "google-auth-oauthlib",
    "google-api-python-client",
    "slack-sdk",
    "slack-bolt",
    "pygithub",
    "atlassian-python-api",
}


def _declared_dependencies() -> set[str]:
    manifest = tomllib.loads(PYPROJECT.read_text())
    declared: list[str] = list(manifest["project"].get("dependencies", []))
    for group in manifest.get("dependency-groups", {}).values():
        declared.extend(g for g in group if isinstance(g, str))

    names = set()
    for spec in declared:
        name = spec.split(";")[0]
        for sep in (">=", "<=", "==", "~=", "!=", ">", "<", "["):
            name = name.split(sep)[0]
        names.add(name.strip().lower())
    return names


def test_no_third_party_client_library_is_declared():
    declared = _declared_dependencies()
    found = declared.intersection(THIRD_PARTY_PACKAGES)

    assert not found, (
        f"{sorted(found)} would let this service reach a customer's system "
        "directly. Integrations live in the workers gateway, behind an egress "
        "allow-list and a per-connection tool allow-list, and evidence reaches "
        "this service as stored rows through core-api (ENT-231). If a genuine "
        "need has appeared, it is a design change to raise, not a dependency "
        "to add."
    )


def test_the_configuration_carries_no_third_party_credential(monkeypatch):
    """The config layer, which is where a credential would have to arrive.

    Every value this service reads is listed by `config_from_env`, so a
    credential for somebody else's system would have to appear as a key here
    before it could be used anywhere. Asserting on the returned mapping rather
    than on a list of names means a key added later is caught by this test
    instead of by nobody.
    """
    monkeypatch.setenv("KINDLAST_OIDC_ISSUER", "http://localhost:8300")
    monkeypatch.setenv("KINDLAST_CORE_API_URL", "http://edge:80")

    settings = config_from_env()

    forbidden = (
        "api_key",
        "apikey",
        "client_secret",
        "access_token",
        "refresh_token",
        "credential",
        "mcp",
        "oauth_token",
        "integration",
    )
    offenders = [
        key for key in settings if any(marker in key.lower() for marker in forbidden)
    ]

    assert not offenders, (
        f"{offenders} appear in this service's configuration. It holds no "
        "third-party credential: the gateway is handed one per call by "
        "core-api, which is the only process with the key that seals them."
    )


def test_no_source_file_reads_an_integration_credential():
    """The same problem arriving by another door.

    Catches a credential read straight from the environment somewhere in the
    source rather than declared in the config layer, which is how this would
    actually get added: one `os.getenv` inside a skill, to solve a real problem
    quickly.
    """
    offenders = []

    for path in SRC.rglob("*.py"):
        text = path.read_text()
        for marker in (
            "KINDLAST_INTEGRATION",
            "KINDLAST_MCP",
            "KINDLAST_GATEWAY_TOKEN",
            "SLACK_TOKEN",
            "GITHUB_TOKEN",
            "GOOGLE_APPLICATION_CREDENTIALS",
        ):
            if marker in text:
                offenders.append(f"{path.name}: {marker}")

    assert not offenders, (
        f"third-party credential markers found: {offenders}. This service "
        "holds none by design; integrations are the gateway's (ENT-231)."
    )


def test_the_only_credential_it_holds_is_its_own_client():
    """The exception, stated rather than implied.

    Intelligence does hold one credential: its own OAuth client, used to call
    core-api as itself so that a run record is written by something that can be
    trusted to write the org_id honestly. Asserting that it is still there
    keeps the rule above from being read as "no credentials at all", which
    would be wrong and would make the next person delete the wrong thing.
    """
    text = (SRC / "kindlast_intelligence" / "service.py").read_text()

    for marker in OWN_CLIENT_MARKERS:
        assert marker in text, (
            f"{marker} is gone. This service authenticates to core-api with "
            "its own client credentials; without them a run cannot be recorded "
            "and findings would have no provenance."
        )


def test_no_source_file_opens_a_connection_to_an_arbitrary_host():
    """Outbound HTTP is to core-api, to the model, and to nothing else.

    core-api is a configured endpoint this service is told about at boot. The
    model is that too, until ENT-236: an organisation that chose a hosted
    provider has its endpoint resolved by core-api and handed over in the
    request, which is the ONE message-derived URL this service dials.

    That one is bounded elsewhere and not by this test, and the boundary is
    worth naming because it is not here. core-api checks the provider against
    the operator allow-list and resolves the host, refusing private, loopback
    and link-local addresses, on every use rather than once at write time. This
    service performs no such check and must not be relied on to: it receives an
    endpoint that has already been decided.

    What must still not exist is a request built from any OTHER message field,
    which is what an integration inside the harness would look like: a tool
    argument becoming a host.
    """
    offenders = []

    for path in SRC.rglob("*.py"):
        text = path.read_text()
        # A request whose URL comes from a request field rather than from
        # configuration or from the one endpoint core-api resolves. Crude on
        # purpose: the point is to make the pattern visible in review, not to
        # prove absence by static analysis.
        for marker in (
            "request.endpoint",
            "msg.endpoint",
            "request.base_url",
            "params['url']",
            'params["url"]',
        ):
            if marker in text:
                offenders.append(f"{path.name}: {marker}")

    assert not offenders, (
        f"a request URL taken from a message: {offenders}. The only one is "
        "`model_endpoint`, which core-api resolved and checked; a customer own "
        "system is dialled by the gateway, behind an egress allow-list."
    )


def test_the_provider_key_arrives_in_a_request_and_never_in_configuration():
    """The ENT-236 exception, bounded rather than merely documented.

    A provider key exists in this process only for the length of one call, and
    only because core-api put it in that call. The property that keeps this from
    growing into "this service holds credentials" is that there is no other
    door: no environment variable, no file, no lookup. Asserting the absence of
    the doors is stronger than asserting the presence of good behaviour.
    """
    offenders = []

    for path in SRC.rglob("*.py"):
        text = path.read_text()
        for marker in (
            "KINDLAST_MODEL_API_KEY",
            "KINDLAST_PROVIDER_KEY",
            "OPENAI_API_KEY",
            "ANTHROPIC_API_KEY",
            "AZURE_OPENAI_KEY",
        ):
            if marker in text:
                offenders.append(f"{path.name}: {marker}")

    assert not offenders, (
        f"a provider key read from the environment: {offenders}. The only key "
        "this service ever sees arrives in a DraftNarrative request, for one "
        "call, from core-api, which is the only process holding the key that "
        "seals them (ENT-236)."
    )


def test_the_configuration_carries_no_model_endpoint(monkeypatch):
    """Since ENT-256 part five this service dials no model at all: the
    deployment's own model URL is core-api's setting (`KINDLAST_MODEL_URL` on
    core-api), and a URL read here would be a second place to dial from, and
    the first step back towards holding a key to dial with."""
    monkeypatch.setenv("KINDLAST_OIDC_ISSUER", "http://localhost:8300")
    monkeypatch.setenv("KINDLAST_CORE_API_URL", "http://edge:80")
    monkeypatch.setenv("KINDLAST_MODEL_URL", "http://model:8080")

    settings = config_from_env()

    assert not any("model" in key.lower() and "url" in key.lower() for key in settings), (
        f"the service reads a model URL from its configuration: {sorted(settings)}"
    )
    # And the source that wires the service builds no direct model client.
    wiring = (SRC / "kindlast_intelligence" / "main.py").read_text()
    assert not re.search(r"(?<![A-Za-z])ModelClient\(", wiring), (
        "main.py builds a direct model client; the service's completions go "
        "through core-api (ProxiedModelClient)"
    )
    assert "KINDLAST_MODEL_URL" not in wiring
