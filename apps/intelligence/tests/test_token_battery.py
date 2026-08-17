"""The token battery of §13.2, ported to pytest (ENT-218).

Each case spoils exactly one property of a token that would otherwise be
accepted, and asserts both that it was refused and WHICH check refused it.

Asserting only "denied" would let a broken audience check hide behind a working
expiry check, which is the specific way a battery like this rots into
decoration. Every assertion below names the error class.
"""

from __future__ import annotations

import time

import jwt
import pytest
from conftest import OTHER_AUDIENCE, TEST_AUDIENCE, AuthServer, _rsa_key

from kindlast_intelligence.auth.errors import (
    AudienceMismatch,
    IssuerMismatch,
    ScopeMissing,
    TokenExpired,
    TokenInvalid,
)
from kindlast_intelligence.auth.verifier import INTELLIGENCE_SCOPE, Verifier


def test_valid_token_allowed(auth_server: AuthServer, verifier: Verifier):
    claims = verifier.verify_internal(auth_server.mint(auth_server.claims()))

    assert claims.subject == "service-user-1"
    assert claims.token_id == "token-id-1"
    assert claims.issuer == auth_server.issuer
    assert claims.has_scope(INTELLIGENCE_SCOPE)


def test_wrong_audience_denied(auth_server: AuthServer, verifier: Verifier):
    """The §1.4 property: a token for core-api must not replay against this."""
    token = auth_server.mint(auth_server.claims(aud=OTHER_AUDIENCE))

    with pytest.raises(AudienceMismatch):
        verifier.verify(token)


def test_wrong_issuer_denied(auth_server: AuthServer, verifier: Verifier):
    token = auth_server.mint(auth_server.claims(iss="https://issuer.example.invalid"))

    with pytest.raises(IssuerMismatch):
        verifier.verify(token)


def test_alg_none_denied(auth_server: AuthServer, verifier: Verifier):
    """A plausible header and an empty signature."""
    token = jwt.encode(
        auth_server.claims(), key="", algorithm="none", headers={"kid": "key-1"}
    )

    with pytest.raises(TokenInvalid):
        verifier.verify(token)


def test_hs256_signed_with_the_public_key_denied(
    auth_server: AuthServer, verifier: Verifier
):
    """The confusion attack that matters, because anyone can mount it.

    The public key is published at the JWKS endpoint. If the library is allowed
    to take the algorithm from the token, an attacker signs an HMAC with that
    published key and the verifier checks it against the same bytes. Everyone
    who can read the JWKS can mint tokens.

    THE FORGERY IS ASSEMBLED BY HAND, AND IT HAS TO BE. `jwt.encode` refuses
    to mint this: PyJWT checks on the ENCODE path that an HMAC secret is not a
    PEM key. That guard protects nobody here, because an attacker is not using
    our library to mint their token. Using `jwt.encode` would have produced a
    test that passes because the forgery failed rather than because the
    verifier refused it, which is a test asserting nothing.
    """
    import base64
    import hashlib
    import hmac
    import json

    from cryptography.hazmat.primitives import serialization

    public_pem = auth_server.signing_key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )

    def b64(raw: bytes) -> bytes:
        return base64.urlsafe_b64encode(raw).rstrip(b"=")

    header = b64(json.dumps({"alg": "HS256", "typ": "JWT", "kid": "key-1"}).encode())
    payload = b64(json.dumps(auth_server.claims()).encode())
    signing_input = header + b"." + payload
    signature = b64(hmac.new(public_pem, signing_input, hashlib.sha256).digest())
    forged = (signing_input + b"." + signature).decode()

    # Sanity: the forgery really is a correctly signed HS256 token, so the
    # refusal below is the allow-list holding rather than the token being
    # garbage. Checked by recomputing the MAC rather than by asking PyJWT,
    # because PyJWT refuses PEM-as-HMAC on the decode path as well and would
    # report this valid forgery as invalid for its own reasons.
    expected = b64(hmac.new(public_pem, signing_input, hashlib.sha256).digest())
    assert hmac.compare_digest(signature, expected)
    assert json.loads(base64.urlsafe_b64decode(payload + b"=="))["sub"] == (
        "service-user-1"
    )

    with pytest.raises(TokenInvalid):
        verifier.verify(forged)


def test_the_allow_list_holds_only_asymmetric_algorithms():
    """Asserted directly, because the test above cannot prove it.

    THIS IS THE INTERESTING ONE, and it exists because of what happened when
    the battery was checked against deliberate sabotage.

    Adding HS256 to `SIGNING_ALGORITHMS` and re-running left all fourteen tests
    green. The forgery was still refused, but by a different mechanism: the
    verifier hands PyJWK's key OBJECT to the decoder, and PyJWT's HMAC path
    raises rather than accepting an RSA public key object as a secret. So there
    genuinely are two independent defences, which is the belt-and-braces the Go
    side describes.

    The consequence for testing is uncomfortable and worth writing down: the
    HS256 test above passes whether or not the allow-list is correct, so it
    cannot be the thing that protects the allow-list. A test that stays green
    while the property it names is broken is the exact failure `AGENTS.md`
    warns about.

    Hence this: a direct assertion on the constant, which does go red the
    moment somebody widens it. Cheap, unglamorous, and the only one of the two
    that actually guards the line it claims to.
    """
    from kindlast_intelligence.auth.verifier import SIGNING_ALGORITHMS

    # `none` is unsigned. `HS*` are symmetric, which is what makes the public
    # key usable as a forging secret. Neither may ever appear here.
    forbidden = {"none", "HS256", "HS384", "HS512"}
    assert not forbidden.intersection(SIGNING_ALGORITHMS)
    assert all(alg.startswith(("RS", "ES", "PS")) for alg in SIGNING_ALGORITHMS)


def test_expired_denied(auth_server: AuthServer, verifier: Verifier):
    token = auth_server.mint(auth_server.claims(exp=int(time.time()) - 3600))

    with pytest.raises(TokenExpired):
        verifier.verify(token)


def test_token_with_no_expiry_denied(auth_server: AuthServer, verifier: Verifier):
    claims = auth_server.claims()
    del claims["exp"]

    with pytest.raises(TokenInvalid):
        verifier.verify(auth_server.mint(claims))


def test_signed_by_a_key_the_server_does_not_serve_denied(
    auth_server: AuthServer, verifier: Verifier
):
    """Right kid, wrong key. The signature check has to be what refuses this,
    not the kid lookup, which is why the kid is one the server does serve."""
    stranger = _rsa_key()
    forged = jwt.encode(
        auth_server.claims(), stranger, algorithm="RS256", headers={"kid": "key-1"}
    )

    with pytest.raises(TokenInvalid):
        verifier.verify(forged)


def test_unknown_kid_denied(auth_server: AuthServer, verifier: Verifier):
    forged = auth_server.mint(auth_server.claims(), kid="key-nobody-has")

    with pytest.raises(TokenInvalid):
        verifier.verify(forged)


# --- The two properties this service adds on top of the shared battery -------


def test_a_user_token_is_refused(auth_server: AuthServer, verifier: Verifier):
    """ENT-218 asks for this asserted rather than assumed.

    A human's token from the console: real, correctly signed, unexpired, and
    carrying the scopes a person actually holds. It must not reach this service
    on any path, because Intelligence holds no database credential and no
    tenancy GUCs and therefore cannot check whether this human should see what
    it is about to process. It never needs to, because this refusal exists.
    """
    human = auth_server.claims(
        sub="user-subject-1",
        scope="findings:read findings:write records:read",
        client_id="web",
    )

    with pytest.raises(ScopeMissing):
        verifier.verify_internal(auth_server.mint(human))


def test_a_token_without_the_intelligence_scope_is_refused(
    auth_server: AuthServer, verifier: Verifier
):
    """Another internal service's token. Correct audience, wrong authority."""
    other_service = auth_server.claims(scope="internal:ingest")

    with pytest.raises(ScopeMissing):
        verifier.verify_internal(auth_server.mint(other_service))


def test_scope_matching_is_exact(auth_server: AuthServer, verifier: Verifier):
    """A prefix must not satisfy the requirement.

    `internal:intelligence:read` is a different authority from
    `internal:intelligence`, and a prefix comparison would silently grant one
    for the other.
    """
    near_miss = auth_server.claims(scope="internal:intelligence:read internal:intel")

    with pytest.raises(ScopeMissing):
        verifier.verify_internal(auth_server.mint(near_miss))


def test_scopes_are_read_from_a_vendor_claim_when_configured(
    auth_server: AuthServer,
):
    """Zitadel emits neither `scope` nor `scp`, measured rather than assumed.

    It asserts grants under a URN-shaped claim whose value is an object keyed
    by grant name. A verifier that only read RFC 9068's `scope` would find no
    authority at all on this stack and refuse every genuine caller.
    """
    from kindlast_intelligence.auth.jwks import KeySet, discover

    vendor_claim = "urn:zitadel:iam:org:project:1234:roles"
    document = discover(auth_server.issuer)
    keys = KeySet(document["jwks_uri"])
    keys.warm()
    verifier = Verifier(
        keys,
        issuer=document["issuer"],
        audience=TEST_AUDIENCE,
        scope_claims=(vendor_claim,),
    )

    claims = auth_server.claims()
    del claims["scope"]
    claims[vendor_claim] = {INTELLIGENCE_SCOPE: {"org-1": "kindlast.localhost"}}

    verified = verifier.verify_internal(auth_server.mint(claims))
    assert verified.has_scope(INTELLIGENCE_SCOPE)


def test_the_verifier_refuses_to_be_built_without_an_audience(
    auth_server: AuthServer,
):
    """There is no accept-any mode, and it must not be reachable by omission."""
    from kindlast_intelligence.auth.jwks import KeySet

    keys = KeySet(auth_server.jwks_uri)

    with pytest.raises(ValueError):
        Verifier(keys, issuer=auth_server.issuer, audience="")
