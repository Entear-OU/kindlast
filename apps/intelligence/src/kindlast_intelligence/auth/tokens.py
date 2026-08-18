"""Minting this service's own token to call core-api back (ENT-218, §1.2).

# WHY THIS EXISTS RATHER THAN AN ENVIRONMENT VARIABLE

The first draft took a `KINDLAST_INTERNAL_TOKEN` from the environment. That
cannot work, and the reason is §1.2: access tokens live ten minutes. A static
token in a compose file is a service that works for ten minutes after every
deploy and then reports that core-api refused it, which is the kind of failure
that gets diagnosed as a network problem three times before somebody checks an
expiry.

So the service holds client credentials and mints tokens, the same grant the
corpus loader uses.

# TWO FACTS THAT ARE NOT GUESSABLE AND COST AN AFTERNOON EACH

Recorded here as well as in the Postman collection, because a reader debugging
a refused token will be in one of the two places.

Zitadel's `client_id` for a service user is its USERNAME, not its id.

The audience is the PROJECT ID, requested through the reserved
`urn:zitadel:iam:org:project:id:<project>:aud` scope, and the granted roles
only reach the token if `urn:zitadel:iam:org:projects:roles` is also requested.
The plural in that second one is not a typo. Without it the caller
authenticates perfectly and then holds no authority at all, which presents as
a permission error rather than an authentication one and sends you looking at
grants that are already correct.
"""

from __future__ import annotations

import threading
import time

import httpx

# Refresh this long before expiry. A token that expires mid-request is a
# retry a caller should never have to write, and thirty seconds against a
# ten-minute token costs 5% of its life to remove that class of failure
# entirely.
REFRESH_MARGIN_SECONDS = 30.0


class TokenError(Exception):
    """The authorization server would not issue a token."""


class ClientCredentialsToken:
    """Mints and caches this service's own access token."""

    def __init__(
        self,
        token_endpoint: str,
        client_id: str,
        client_secret: str,
        project_id: str,
        client: httpx.Client | None = None,
        host_header: str = "",
    ) -> None:
        # THE THIRD PLACE THE SPLIT HORIZON APPLIES, after discovery and the
        # JWKS. The token endpoint in the discovery document names the issuer's
        # public address, which a container cannot reach, and the failure is a
        # bare "Connection refused" that says nothing about which of several
        # URLs was wrong. Rewritten by the caller; the Host header carries the
        # original so Zitadel routes to the right virtual server.
        self._endpoint = token_endpoint
        self._client_id = client_id
        self._client_secret = client_secret
        self._project_id = project_id
        self._http = client or httpx.Client(timeout=15.0)
        self._host_header = host_header
        self._token = ""
        self._expires_at = 0.0
        # One lock, so a burst of requests arriving on an expired token mints
        # once rather than once each. The authorization server would survive
        # the herd; the point is that it should not have to.
        self._lock = threading.Lock()

    def get(self) -> str:
        with self._lock:
            if self._token and time.monotonic() < self._expires_at:
                return self._token
            self._mint_locked()
            return self._token

    def _headers(self) -> dict[str, str]:
        headers = {"Content-Type": "application/x-www-form-urlencoded"}
        if self._host_header:
            headers["Host"] = self._host_header
        return headers

    def _mint_locked(self) -> None:
        scopes = " ".join(
            (
                "openid",
                # Without this the roles never reach the token. Plural, and not
                # a typo. See the module docstring.
                "urn:zitadel:iam:org:projects:roles",
                f"urn:zitadel:iam:org:project:id:{self._project_id}:aud",
            )
        )

        try:
            response = self._http.post(
                self._endpoint,
                data={
                    "grant_type": "client_credentials",
                    # The USERNAME, not the id. See the module docstring.
                    "client_id": self._client_id,
                    "client_secret": self._client_secret,
                    "scope": scopes,
                },
                headers=self._headers(),
            )
            response.raise_for_status()
            payload = response.json()
        except httpx.HTTPError as exc:
            raise TokenError(f"minting a token: {exc}") from exc

        token = payload.get("access_token")
        if not token:
            raise TokenError(f"no access_token in the response: {payload!r}")

        # `expires_in` is advisory and a server may omit it. Defaulting short
        # rather than long: minting more often than necessary costs one request,
        # where trusting an absent expiry costs every request after it.
        expires_in = float(payload.get("expires_in", 300))
        self._token = token
        self._expires_at = time.monotonic() + max(expires_in - REFRESH_MARGIN_SECONDS, 5.0)
