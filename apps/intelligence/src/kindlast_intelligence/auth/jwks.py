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
    """A cached JWKS, refetched when a token names a key it does not hold.

    The whole subtlety of this class is when it goes back to the network, and
    it is worth stating plainly because getting it wrong produces an outage
    that looks like a signature bug.

    A FRESHLY SEEDED ZITADEL SERVES `{"keys": []}`. It generates its signing key
    lazily, on the first token it issues, so an empty set at boot is correct
    rather than broken. A cache populated once at boot would therefore hold
    nothing for the entire life of the process and reject every token that
    followed, reporting each one as an unknown key rather than as an empty
    cache. Hence the rule this class exists to enforce: **the boot fetch must
    never be the last fetch.** `warm` is explicitly not counted as a refetch,
    so the first token to arrive still gets one.

    Not hypothetical, and not cheap: the first version counted it, and the
    `Intelligence end to end` CI job failed on every run it ever executed while
    passing on a laptop every time (ENT-253). CI's stack was seconds old and
    the laptop's was a day old, so on the laptop the cooldown had long lapsed
    and the first token drove the refetch that found the key. The Go side is
    the same rule and the same reasoning, in `libs/chassis/oidc/jwks.go`.
    """

    def __init__(
        self,
        jwks_uri: str,
        client: httpx.Client | None = None,
        reachable_at: str = "",
        host_header: str = "",
    ) -> None:
        """`reachable_at` and `host_header` are the split-horizon pair.

        Discovery returns a `jwks_uri` naming the issuer's PUBLIC address,
        which in compose is not where this container can reach it. So the
        address is rewritten and the Host header carries the original.

        That changes how we get there and never who we trust: the document
        still had to declare the issuer we expected before we got this far.
        """
        if not jwks_uri:
            raise ValueError("a key set needs a jwks_uri")
        self._jwks_uri = _rewrite_host(jwks_uri, reachable_at)
        self._host_header = host_header
        self._client = client or httpx.Client(timeout=10.0)
        self._keys: dict[str, PyJWK] = {}
        # `_refetched` is separate from `_last_fetch` on purpose, and the
        # separation is the fix for ENT-253. A boot fetch updates neither, so
        # the first token to name an unknown kid always reaches the network
        # whatever `warm` did or failed to do. See the class docstring.
        self._refetched = False
        self._last_fetch = 0.0
        # Fetching and swapping the map happen under one lock. Without it two
        # requests arriving on an unknown kid both fetch, which is the
        # thundering herd the cooldown above exists to prevent.
        self._lock = threading.Lock()

    def warm(self) -> None:
        """Fetch once at boot.

        Raising here is deliberately not fatal to the caller's startup, and the
        caller is what has to honour that: an authorization server that is slow
        to come up in a compose stack should not permanently break a service
        that would have recovered on its first request. `main.build_app` logs
        and carries on for exactly that reason.

        A warm neither starts the refetch cooldown nor counts as the one
        refetch it permits. That single line is the difference between a
        working stack and one that rejects every token it is ever shown; the
        class docstring has the whole argument.
        """
        with self._lock:
            self._fetch_locked()

    def key_for(self, kid: str | None) -> PyJWK:
        with self._lock:
            key = self._keys.get(kid) if kid else self._single_key_locked()
            if key is not None:
                return key

            # Unknown kid. Refetch, but at most once per cooldown, and always
            # at least once whatever `warm` did.
            if not self._may_refetch_locked():
                raise TokenInvalid(
                    f"no signing key for kid {kid!r} "
                    f"(refetched within the last {REFETCH_COOLDOWN_SECONDS:g}s)"
                )

            # The outcome of the fetch does not change the bookkeeping. A
            # failed or fruitless refetch still starts the cooldown, or a down
            # authorization server would mean one outbound request per inbound
            # request, which is the amplification this cache exists to prevent.
            self._refetched = True
            self._last_fetch = time.monotonic()
            try:
                self._fetch_locked()
            except Exception as exc:  # noqa: BLE001 - a refusal, not a 500
                # A transport failure escaping here would leave the verifier,
                # which turns everything it catches into a refusal, and reach
                # the handler as an unhandled exception: a 500 that tells a
                # caller Intelligence is broken when the authorization server
                # is the thing that is down.
                raise TokenInvalid(
                    f"no signing key for kid {kid!r}, and the refetch failed: {exc}"
                ) from exc

            key = self._keys.get(kid) if kid else self._single_key_locked()
            if key is None:
                raise TokenInvalid(f"no signing key for kid {kid!r}")
            return key

    def _may_refetch_locked(self) -> bool:
        """The one question this cache exists to get right.

        The first miss always goes to the network, whatever `warm` did, because
        `warm` may have cached an empty set from an authorization server that
        had not generated its key yet, or may have failed outright. After that
        the cooldown applies.
        """
        if not self._refetched:
            return True
        return time.monotonic() - self._last_fetch >= REFETCH_COOLDOWN_SECONDS

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
        # Deliberately does NOT touch `_refetched` or `_last_fetch`. Only
        # `key_for` records a refetch, so a boot warm cannot start the
        # cooldown. See the class docstring.
        headers = {"Host": self._host_header} if self._host_header else None
        response = self._client.get(self._jwks_uri, headers=headers)
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


def _rewrite_host(url: str, reachable_at: str) -> str:
    """Point a URL at where it can actually be reached.

    ZITADEL ROUTES BY Host, AND WITHOUT THIS THE REQUEST REACHES THE WRONG
    VIRTUAL SERVER RATHER THAN FAILING CLEANLY. Measured, and the same fact
    core-api's configuration already carries: in compose, tokens name
    `http://localhost:8300` while the container reaches `http://auth:8080`, and
    asking the second for the first's documents returns a plain 404 that
    suggests the path is wrong when the path is fine.
    """
    if not reachable_at:
        return url

    from urllib.parse import urlparse, urlunparse

    target = urlparse(reachable_at)
    if not target.netloc:
        return url
    return urlunparse(urlparse(url)._replace(scheme=target.scheme, netloc=target.netloc))


def discover(
    issuer: str,
    client: httpx.Client | None = None,
    reachable_at: str = "",
    host_header: str = "",
) -> dict[str, Any]:
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

    # ACCEPTS AN ISSUER OR A FULL DOCUMENT URL, and appending blindly is how
    # this first failed: core-api's KINDLAST_OIDC_DISCOVERY_URL convention is
    # the complete document URL, so the container asked for
    # `/.well-known/openid-configuration/.well-known/openid-configuration` and
    # got a 404 that says nothing about the real mistake.
    #
    # Tolerating both is right rather than merely convenient: one setting names
    # the issuer and another names where to fetch its document, and a caller
    # should not have to know which half of that this function wanted.
    suffix = "/.well-known/openid-configuration"
    base = issuer.rstrip("/")
    url = base if base.endswith(suffix) else base + suffix
    expected_issuer = base[: -len(suffix)] if base.endswith(suffix) else base

    url = _rewrite_host(url, reachable_at)
    headers = {"Host": host_header} if host_header else None
    response = http.get(url, headers=headers)
    response.raise_for_status()
    document: dict[str, Any] = response.json()

    if document.get("issuer") not in (issuer, expected_issuer):
        raise ValueError(
            f"discovery document names issuer {document.get('issuer')!r}, "
            f"expected {expected_issuer!r}"
        )
    if not document.get("jwks_uri"):
        raise ValueError("discovery document has no jwks_uri")
    return document
