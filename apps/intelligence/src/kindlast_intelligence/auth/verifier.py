"""Access token verification for the Intelligence service (§1.4, §1.6, §13.2).

This is the whole of the service's authority. Everything it will ever be asked
to do sits behind these checks, so the file is deliberately small and
deliberately paranoid.

# THIS SERVICE ACCEPTS NO USER TOKENS, EVER

Not "does not expect". Cannot accept. Two independent things enforce it:

  * the audience is `kindlast-intelligence`, and a token minted for a human
    signing into the console carries the console's audience, not this one; and
  * `internal:intelligence` is required, and it is granted only to the service
    principal.

Either alone would do. Both are here because this is the property the whole
service rests on: Intelligence has no database credential and no tenancy GUCs,
so it has no way to check whether a human should see what it is about to
process. It never needs one, because a human can never reach it.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

import jwt

from .errors import (
    AudienceMismatch,
    IssuerMismatch,
    ScopeMissing,
    TokenExpired,
    TokenInvalid,
)
from .jwks import KeySet

# The allow-list, and the single most important line in this file.
#
# Both entries are asymmetric, which is what makes the two classic
# algorithm-confusion attacks impossible rather than merely unlikely:
#
#   * `alg: none`, a token with a plausible header and an empty signature.
#   * `alg: HS256` signed with the authorization server's PUBLIC key as the
#     HMAC secret, which verifies if the library takes the algorithm from the
#     token and the key material is symmetric-capable. The public key is
#     published at the JWKS endpoint, so anyone at all can mint one.
#
# The rule generalises past these two: the verifier decides the algorithm,
# never the token.
SIGNING_ALGORITHMS = ("RS256", "ES256")

# Ordinary disagreement between two machines' clocks. Thirty seconds against a
# ten-minute access token (§1.2) is a 5% extension of its life, which is a
# better trade than intermittent rejection of freshly minted tokens on a stack
# whose containers have drifted apart.
CLOCK_SKEW_LEEWAY_SECONDS = 30

# The one scope this service accepts. Not a prefix, not a family.
INTELLIGENCE_SCOPE = "internal:intelligence"


@dataclass(frozen=True)
class Claims:
    """The verified identity a handler is allowed to trust.

    Standard claims only. Nothing here knows what an organisation is: the
    active organisation travels in the request rather than the token, so there
    is exactly one source of truth for membership and switching organisation
    needs no re-minting (§20.1).
    """

    issuer: str
    subject: str
    scopes: tuple[str, ...] = field(default_factory=tuple)
    client_id: str = ""
    token_id: str = ""
    expires_at: datetime | None = None

    def has_scope(self, scope: str) -> bool:
        """Exact match, never a prefix and never a wildcard.

        `internal:ingest` must not satisfy a requirement for
        `internal:intelligence`, and a prefix comparison would let it.
        """
        return scope in self.scopes


def _scopes_from(value: Any) -> list[str]:
    """Read scopes from whichever shape the claim carries.

    Measured rather than assumed. RFC 9068 says a space-delimited `scope`
    string, which is what a conformant server emits. Zitadel emits neither
    `scope` nor `scp` and asserts project roles under a URN-shaped claim whose
    value is an OBJECT keyed by role name. Keycloak nests an array. All three
    appear in real deployments, so all three are read.
    """
    if isinstance(value, str):
        return value.split()
    if isinstance(value, list):
        return [str(v) for v in value]
    if isinstance(value, dict):
        return [str(k) for k in value]
    return []


class Verifier:
    """Checks tokens against one issuer and one audience."""

    def __init__(
        self,
        keys: KeySet,
        issuer: str,
        audience: str,
        scope_claims: tuple[str, ...] = (),
    ) -> None:
        # None of these is optional and there is no "accept any" mode. The
        # audience especially: see the module docstring.
        if keys is None:
            raise ValueError("a verifier needs a key set")
        if not issuer:
            raise ValueError("a verifier needs an issuer")
        if not audience:
            raise ValueError("a verifier needs an audience")

        self._keys = keys
        self._issuer = issuer
        self._audience = audience
        self._scope_claims = ("scope", "scp", *scope_claims)

    def verify(self, raw: str) -> Claims:
        """Verify a bearer token and return the claims it carries.

        Raises one of the typed errors. The caller turns every one of them into
        the same response: telling a client whether their token was expired,
        forged or minted for another audience is a distinction only somebody
        probing the boundary has a use for.
        """
        try:
            header = jwt.get_unverified_header(raw)
        except jwt.PyJWTError as exc:
            raise TokenInvalid(f"unreadable token header: {exc}") from exc

        # Read the kid to select a key, and nothing else. In particular the
        # `alg` in the header is NOT consulted: `algorithms` below is what
        # decides, which is the whole defence against confusion attacks.
        key = self._keys.key_for(header.get("kid"))

        try:
            payload = jwt.decode(
                raw,
                key.key,
                algorithms=list(SIGNING_ALGORITHMS),
                issuer=self._issuer,
                audience=self._audience,
                leeway=CLOCK_SKEW_LEEWAY_SECONDS,
                options={
                    # A token with no expiry is a token that never stops being
                    # useful to whoever steals it.
                    "require": ["exp"],
                    "verify_exp": True,
                    "verify_aud": True,
                    "verify_iss": True,
                    "verify_signature": True,
                },
            )
        except jwt.ExpiredSignatureError as exc:
            raise TokenExpired(str(exc)) from exc
        except jwt.InvalidAudienceError as exc:
            raise AudienceMismatch(str(exc)) from exc
        except jwt.InvalidIssuerError as exc:
            raise IssuerMismatch(str(exc)) from exc
        except jwt.MissingRequiredClaimError as exc:
            # `exp` absent. Reported as invalid rather than expired, because
            # the token is malformed rather than stale, and the battery asserts
            # which one fired.
            raise TokenInvalid(str(exc)) from exc
        except jwt.PyJWTError as exc:
            raise TokenInvalid(str(exc)) from exc
        except Exception as exc:  # noqa: BLE001 - fail closed, see below
            # FAIL CLOSED ON ANYTHING THE LIBRARY DID NOT CLASSIFY.
            #
            # Broad on purpose, which is usually the wrong instinct and is the
            # right one here: this is a security boundary, and the only two
            # outcomes it may have are "verified" and "refused". An exception
            # nobody anticipated must not become a 500 that a caller can tell
            # apart from a refusal, and must never fall through to code that
            # assumes the token was good.
            #
            # Not hypothetical. Widening the algorithm allow-list to include
            # HS256, as a deliberate sabotage to prove this battery can fail,
            # made PyJWT raise a bare TypeError ("Expected a string value")
            # rather than a PyJWTError, because it was handed an RSA public key
            # object where an HMAC secret was expected. Without this clause
            # that propagated out of the verifier uncaught.
            raise TokenInvalid(f"token verification failed: {exc}") from exc

        scopes: list[str] = []
        for claim in self._scope_claims:
            scopes.extend(_scopes_from(payload.get(claim)))

        expires_at = None
        if (exp := payload.get("exp")) is not None:
            expires_at = datetime.fromtimestamp(exp, tz=timezone.utc)

        return Claims(
            issuer=str(payload.get("iss", "")),
            subject=str(payload.get("sub", "")),
            scopes=tuple(scopes),
            # `client_id`, not `azp`. Measured: on this stack Zitadel emits
            # `client_id` on both authorization-code and client-credentials
            # tokens and emits no `azp` at all, so a reader written against
            # `azp` would match nothing and look like it worked (ENT-221).
            client_id=str(payload.get("client_id", "")),
            token_id=str(payload.get("jti", "")),
            expires_at=expires_at,
        )

    def verify_internal(self, raw: str) -> Claims:
        """Verify, and additionally require `internal:intelligence`.

        The entry point every handler uses. `verify` alone is not sufficient
        authority for anything in this service.
        """
        claims = self.verify(raw)
        if not claims.has_scope(INTELLIGENCE_SCOPE):
            raise ScopeMissing(
                f"token does not carry {INTELLIGENCE_SCOPE!r}; "
                "this service is reachable only by the platform"
            )
        return claims
