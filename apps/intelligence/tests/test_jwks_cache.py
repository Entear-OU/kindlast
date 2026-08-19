"""When the key cache goes back to the network, which is the whole of it.

ENT-253. `Intelligence end to end` failed in CI on every run it ever got to
execute, and passed on a laptop every time, and the difference was not the code
under test: it was that CI's stack was seconds old and the laptop's was a day
old.

A freshly seeded Zitadel has generated no signing key yet and serves
`{"keys": []}`. Intelligence boots immediately after the seed container exits,
warms its cache against that empty document, and then refuses the first token
it is ever shown with `no signing key for kid '3869...'`, because the boot fetch
had started the refetch cooldown and no second fetch was permitted for a
minute. On a stack that has been up since yesterday the cooldown lapsed long
ago, the first token drives a refetch, and everything works.

`libs/chassis/oidc` gets this right on the Go side and says so at length: **the
boot fetch must never be the last fetch.** These tests are that rule, here,
because the property has to hold in both resource servers and neither should be
trusting the other's suite.
"""

from __future__ import annotations

import pytest
from conftest import TEST_AUDIENCE, AuthServer  # type: ignore[import-not-found]

from kindlast_intelligence.auth.errors import TokenInvalid
from kindlast_intelligence.auth.jwks import KeySet, discover
from kindlast_intelligence.auth.verifier import Verifier


def test_the_boot_fetch_is_never_the_last_fetch(auth_server: AuthServer):
    """The ENT-253 regression, at the smallest scale that can hold it.

    Warm against an authorization server that has generated no key yet, then
    show the cache a token. It must go back to the network rather than report
    the key as unknown, and it must do that on the first token rather than
    after a cooldown nobody would connect to the symptom.
    """
    auth_server.serves_keys = False

    keys = KeySet(auth_server.jwks_uri)
    keys.warm()
    assert auth_server.jwks_requests == 1

    # The server generates its key, exactly as Zitadel does on the first token
    # it issues, which is the token this service is about to be shown.
    auth_server.serves_keys = True

    assert keys.key_for("key-1") is not None
    assert auth_server.jwks_requests == 2


def test_a_token_verifies_on_a_stack_that_is_seconds_old(auth_server: AuthServer):
    """The same property through the verifier, which is where it was observed.

    Worth asserting at this level too: the unit above could pass with the
    verifier still failing, because `key_for` is called outside the block that
    turns library errors into typed refusals.
    """
    auth_server.serves_keys = False

    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()

    auth_server.serves_keys = True
    verifier = Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE)

    claims = verifier.verify(auth_server.mint(auth_server.claims()))
    assert claims.subject == "service-user-1"


def test_an_unknown_kid_costs_one_fetch_per_cooldown(auth_server: AuthServer):
    """The other half, and the reason the cooldown exists at all.

    An unknown kid is what a rotation looks like and equally what an attacker
    sends to make this service hammer the authorization server. Rotation costs
    one fetch and the attack costs one fetch, which is the same thing, which is
    the point. Fixing the boot-fetch bug must not cost this.
    """
    keys = KeySet(auth_server.jwks_uri)
    keys.warm()
    assert auth_server.jwks_requests == 1

    for _ in range(5):
        with pytest.raises(TokenInvalid):
            keys.key_for("key-nobody-has")

    # One refetch for the first miss, and nothing for the four that followed.
    assert auth_server.jwks_requests == 2


def test_a_refetch_that_fails_refuses_the_token_rather_than_erroring(
    auth_server: AuthServer,
):
    """A 401, not a 500.

    The transport error must not escape the verifier: a caller that gets a 500
    when the authorization server is down reads it as "Intelligence is broken",
    and an unhandled exception on a security boundary is one instruction away
    from a path that assumed the token was good.
    """
    keys = KeySet(auth_server.jwks_uri)
    keys.warm()

    auth_server.keys_status = 500
    before = auth_server.jwks_requests

    with pytest.raises(TokenInvalid):
        keys.key_for("key-nobody-has")

    # The refusal has to be the one that came back from a real attempt, not the
    # cooldown declining to make one. Otherwise this test passes while the
    # transport error it is about is still uncaught.
    assert auth_server.jwks_requests == before + 1


def test_a_failed_refetch_still_starts_the_cooldown(auth_server: AuthServer):
    """Otherwise a down authorization server gets one request per request.

    The bookkeeping cannot depend on the fetch succeeding, or the amplification
    this cache exists to prevent arrives precisely when the authorization
    server is least able to absorb it.
    """
    keys = KeySet(auth_server.jwks_uri)
    keys.warm()
    auth_server.keys_status = 500
    before = auth_server.jwks_requests

    for _ in range(5):
        with pytest.raises(TokenInvalid):
            keys.key_for("key-nobody-has")

    assert auth_server.jwks_requests - before == 1


def test_a_warm_that_fails_does_not_start_the_cooldown(auth_server: AuthServer):
    """A boot that lost the race to the authorization server must still recover.

    Same rule as the empty document, one step earlier: if the JWKS was
    unreachable at boot, the first token has to be allowed to try again rather
    than inheriting a cooldown from a fetch that produced nothing.
    """
    auth_server.keys_status = 500

    keys = KeySet(auth_server.jwks_uri)
    with pytest.raises(Exception):  # noqa: B017 - any transport failure will do
        keys.warm()

    auth_server.keys_status = 200
    assert keys.key_for("key-1") is not None


# --- The same rule one level up, where the service decides to start ----------


def test_a_service_starts_when_the_jwks_is_not_ready_yet(
    auth_server: AuthServer, monkeypatch: pytest.MonkeyPatch
):
    """Losing the race to the authorization server is ordinary, not fatal.

    `auth` and this service come up together in a compose stack, and a boot
    that could not read the JWKS has to become a log line rather than a process
    that exits 1 and never comes back. Crashing on it turns a startup ordering
    detail into a stack that is down for a reason nobody would guess, and the
    key set already knows how to recover on the first token.
    """
    from kindlast_intelligence import main

    auth_server.keys_status = 503

    monkeypatch.setenv("KINDLAST_OIDC_ISSUER", auth_server.issuer)
    monkeypatch.setenv("KINDLAST_CORE_API_URL", "http://core-api.invalid")
    monkeypatch.setenv("KINDLAST_OIDC_AUDIENCE", TEST_AUDIENCE)
    monkeypatch.setenv("KINDLAST_INTERNAL_CLIENT_ID", "intelligence")
    monkeypatch.setenv("KINDLAST_INTERNAL_CLIENT_SECRET", "not-a-real-secret")
    monkeypatch.delenv("KINDLAST_OIDC_DISCOVERY_URL", raising=False)
    monkeypatch.delenv("KINDLAST_OIDC_HOST_HEADER", raising=False)
    monkeypatch.delenv("KINDLAST_OIDC_SCOPE_CLAIM", raising=False)
    monkeypatch.delenv("KINDLAST_OIDC_AUDIENCE_FILE", raising=False)
    monkeypatch.delenv("KINDLAST_OIDC_PROJECT_ID", raising=False)
    monkeypatch.delenv("KINDLAST_INTERNAL_CLIENT_FILE", raising=False)

    assert main.build_app() is not None
