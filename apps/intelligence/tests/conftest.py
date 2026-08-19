"""A real authorization server, for tests that must not stub the cryptography.

§13.2 is explicit that the verifier is the code most worth testing, and that
substituting a mock means every scope and tenancy test is really testing the
mock. So this fixture is an actual HTTP server serving an actual discovery
document and an actual JWKS, and every token in the battery is really signed.

The Go side does the same thing in `libs/chassis/oidc/authserver_test.go`. Two
implementations of the same idea, because the property they protect is the same
one and neither service should be trusting the other's test suite.
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any

import jwt
import pytest
from cryptography.hazmat.primitives.asymmetric import rsa

# The audience this resource server accepts, and the only one.
#
# §1.4: core-api accepts `aud: kindlast-core-api`, intelligence accepts
# `aud: kindlast-intelligence`, and neither accepts the other's. The battery
# below mints a token for the other one and asserts it is refused, because a
# replay across resource servers is the most common OAuth misconfiguration in a
# multi-service estate.
TEST_AUDIENCE = "kindlast-intelligence"
OTHER_AUDIENCE = "kindlast-core-api"


def _rsa_key() -> rsa.RSAPrivateKey:
    return rsa.generate_private_key(public_exponent=65537, key_size=2048)


class AuthServer:
    """An OpenID provider on localhost, with two keys and a mint."""

    def __init__(self) -> None:
        self.signing_key = _rsa_key()
        # A second key the server publishes but the battery does not sign with,
        # so "unknown kid" and "known kid, wrong key" stay distinguishable.
        self.other_key = _rsa_key()
        self._server = HTTPServer(("127.0.0.1", 0), self._handler())
        self.port = self._server.server_address[1]
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()
        self.jwks_requests = 0

        # A FRESHLY SEEDED ZITADEL SERVES `{"keys": []}` (ENT-253).
        #
        # It generates its signing key lazily, on the first token it issues, so
        # an empty set is a correct answer rather than a broken server. Set
        # this to False to be that server: the cache's whole job is to notice
        # it was warmed against nothing and go back to the network when a token
        # names a key it does not hold.
        self.serves_keys = True

        # What `/keys` answers with. 500 is the other half of the same
        # question: a refetch that fails must refuse the token rather than
        # raise a transport error out of the verifier, and must still start the
        # cooldown so an unreachable authorization server does not get one
        # outbound request per inbound one.
        self.keys_status = 200

    @property
    def issuer(self) -> str:
        return f"http://127.0.0.1:{self.port}"

    @property
    def jwks_uri(self) -> str:
        return f"{self.issuer}/keys"

    def stop(self) -> None:
        self._server.shutdown()
        self._server.server_close()

    def claims(self, **overrides: Any) -> dict[str, Any]:
        """A token that should verify, before the caller spoils one property."""
        import time

        base: dict[str, Any] = {
            "iss": self.issuer,
            "sub": "service-user-1",
            "aud": TEST_AUDIENCE,
            "exp": int(time.time()) + 600,
            "iat": int(time.time()),
            "jti": "token-id-1",
            "client_id": "kindlast-intelligence-client",
            "scope": "internal:intelligence",
        }
        base.update(overrides)
        return base

    def mint(self, claims: dict[str, Any], kid: str = "key-1") -> str:
        return jwt.encode(
            claims, self.signing_key, algorithm="RS256", headers={"kid": kid}
        )

    def _jwk(self, key: rsa.RSAPrivateKey, kid: str) -> dict[str, Any]:
        return json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(key.public_key())) | {
            "kid": kid,
            "use": "sig",
            "alg": "RS256",
        }

    def _handler(self):
        server = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args: Any) -> None:  # keep pytest output clean
                pass

            def do_GET(self) -> None:  # noqa: N802 (http.server's spelling)
                if self.path == "/.well-known/openid-configuration":
                    body = {
                        "issuer": server.issuer,
                        "jwks_uri": server.jwks_uri,
                        "authorization_endpoint": f"{server.issuer}/authorize",
                        "token_endpoint": f"{server.issuer}/token",
                    }
                elif self.path == "/keys":
                    server.jwks_requests += 1
                    if server.keys_status != 200:
                        self.send_response(server.keys_status)
                        self.end_headers()
                        return
                    body = {
                        "keys": (
                            [
                                server._jwk(server.signing_key, "key-1"),
                                server._jwk(server.other_key, "key-2"),
                            ]
                            if server.serves_keys
                            else []
                        )
                    }
                else:
                    self.send_response(404)
                    self.end_headers()
                    return

                payload = json.dumps(body).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

        return Handler


@pytest.fixture
def auth_server():
    server = AuthServer()
    yield server
    server.stop()


@pytest.fixture
def verifier(auth_server: AuthServer):
    """Wired the way production wires it: discover, take the jwks_uri from the
    document rather than assuming a path, warm the cache once."""
    from kindlast_intelligence.auth.jwks import KeySet, discover
    from kindlast_intelligence.auth.verifier import Verifier

    document = discover(auth_server.issuer)
    assert document["jwks_uri"] == auth_server.jwks_uri

    keys = KeySet(document["jwks_uri"])
    keys.warm()
    return Verifier(keys, issuer=document["issuer"], audience=TEST_AUDIENCE)
