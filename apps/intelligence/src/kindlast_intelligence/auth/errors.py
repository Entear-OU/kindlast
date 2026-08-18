"""The failure modes a resource server has to tell apart (§13.2).

All of these deny the request, so they are not a policy decision. They exist so
a log line says which check bit, and so the token battery can assert it was the
intended one rather than any refusal at all.

A test that only asserts "denied" passes when the token is rejected for the
wrong reason, which is how a broken audience check hides behind a working
expiry check. This mirrors `libs/chassis/oidc`'s four sentinel errors on the Go
side deliberately: the two resource servers refuse the same things for the same
reasons, and a reader moving between them should not have to learn two
vocabularies.
"""


class VerificationError(Exception):
    """Base for every refusal. Never raised directly."""


class TokenInvalid(VerificationError):
    """Malformed, unsigned, signed by an unknown key, or signed with an
    algorithm this verifier does not accept."""


class TokenExpired(VerificationError):
    """Past `exp`, allowing for clock skew. Also raised when `exp` is absent,
    because a token that never expires is worse than one that has."""


class AudienceMismatch(VerificationError):
    """Minted for a different resource server.

    The one §1.4 turns on: `core-api` accepts only `aud: kindlast-core-api` and
    this service only `aud: kindlast-intelligence`. Without the check, a token
    minted for one replays against the other, which is the most common OAuth
    misconfiguration in a multi-service estate.
    """


class IssuerMismatch(VerificationError):
    """Minted by an authorization server this deployment does not trust."""


class ScopeMissing(VerificationError):
    """Verified, and not permitted here.

    Distinct from the four above because it is the only one that is about
    authority rather than authenticity: the token is genuine and the caller is
    who they say they are. They simply may not do this.
    """
