"""The signing keys, fetched from the authorization server and cached.

Verification is local and in-process, never an introspection call (§1.4). That
is what makes `auth` survivable as a dependency: when it goes down, existing
tokens keep verifying until they expire, because nothing on this path talks to
it per request.
"""

from __future__ import annotations

import threading
import time
from typing import Any

import httpx
from jwt import PyJWK

from .errors import TokenInvalid

# How long to wait before refetching after an unknown `kid`.
#
# An unknown kid is the signal that the authorization server has rotated, and
# refetching is the correct response. It is also what an attacker sends to make
# this service hammer the authorization server: mint garbage with a random kid,
# repeat. The cooldown makes rotation cost one fetch and an attack cost one
# fetch, which is the same thing, which is the point.
REFETCH_COOLDOWN_SECONDS = 60.0


class KeySet:
    """A cached JWKS, refetched when a token names a key it does not hold."""

    def __init__(self, jwks_uri: str, client: httpx.Client | None = None) -> None:
        if not jwks_uri:
            raise ValueError("a key set needs a jwks_uri")
        self._jwks_uri = jwks_uri
        self._client = client or httpx.Client(timeout=10.0)
        self._keys: dict[str, PyJWK] = {}
        self._last_fetch = 0.0
        # Fetching and swapping the map happen under one lock. Without it two
        # requests arriving on an unknown kid both fetch, which is the
        # thundering herd the cooldown above exists to prevent.
        self._lock = threading.Lock()

    def warm(self) -> None:
        """Fetch once at boot.

        Failure here is deliberately not fatal to the caller's startup: an
        authorization server that is slow to come up in a compose stack should
        not permanently break a service that would have recovered on its first
        request. `key_for` refetches on demand.
        """
        with self._lock:
            self._fetch_locked()

    def key_for(self, kid: str | None) -> PyJWK:
        with self._lock:
            key = self._keys.get(kid) if kid else self._single_key_locked()
            if key is not None:
                return key

            # Unknown kid. Refetch, but at most once per cooldown.
            if time.monotonic() - self._last_fetch < REFETCH_COOLDOWN_SECONDS:
                raise TokenInvalid(f"no signing key for kid {kid!r}")

            self._fetch_locked()
            key = self._keys.get(kid) if kid else self._single_key_locked()
            if key is None:
                raise TokenInvalid(f"no signing key for kid {kid!r}")
            return key

    def _single_key_locked(self) -> PyJWK | None:
        """The only key, when a token names none.

        A `kid` is not required by RFC 7515, and a server serving exactly one
        key produces tokens that omit it. Serving several and omitting the kid
        is ambiguous, and guessing is how a verifier accepts a token signed by
        a key that was never meant for it, so that case is a refusal.
        """
        if len(self._keys) == 1:
            return next(iter(self._keys.values()))
        return None

    def _fetch_locked(self) -> None:
        self._last_fetch = time.monotonic()
        response = self._client.get(self._jwks_uri)
        response.raise_for_status()
        document: dict[str, Any] = response.json()

        keys: dict[str, PyJWK] = {}
        for entry in document.get("keys", []):
            # `use: enc` is an encryption key and cannot verify a signature.
            # Skipping rather than failing: a document legitimately carries
            # both, and refusing the whole set because one key is for another
            # purpose would break a conformant server.
            if entry.get("use") == "enc":
                continue
            try:
                key = PyJWK.from_dict(entry)
            except Exception:
                # One malformed key must not drop the others. A server midway
                # through a rotation can serve something this library cannot
                # parse, and taking the whole set down over it turns a cosmetic
                # problem into an outage.
                continue
            kid = entry.get("kid")
            if kid:
                keys[kid] = key
            elif len(document.get("keys", [])) == 1:
                keys[""] = key

        # Swapped wholesale rather than merged. Merging would mean a key the
        # authorization server has retired stays valid here forever, which
        # defeats rotation.
        self._keys = keys


def discover(issuer: str, client: httpx.Client | None = None) -> dict[str, Any]:
    """Read the OpenID configuration and check it names the issuer we asked for.

    The `jwks_uri` comes from the document rather than from an assumed path
    (§18.2), so a self-hoster can point at their own provider without a code
    change.

    The issuer check is not a formality. A document that names a different
    issuer than the one requested is either a misconfiguration or a redirect to
    somebody else's authorization server, and accepting its keys would mean
    trusting them to mint tokens for this deployment.
    """
    http = client or httpx.Client(timeout=10.0)
    url = issuer.rstrip("/") + "/.well-known/openid-configuration"
    response = http.get(url)
    response.raise_for_status()
    document: dict[str, Any] = response.json()

    if document.get("issuer") != issuer:
        raise ValueError(
            f"discovery document names issuer {document.get('issuer')!r}, "
            f"expected {issuer!r}"
        )
    if not document.get("jwks_uri"):
        raise ValueError("discovery document has no jwks_uri")
    return document
