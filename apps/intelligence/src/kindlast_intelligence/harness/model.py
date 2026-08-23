"""The model clients: what the harness calls to get one completion.

Two implementations of one shape, `complete(messages, schema, max_tokens,
temperature) -> Completion`, and the harness cannot tell them apart:

`ProxiedModelClient` is the one the service runs (ENT-256, part five). It asks
core-api's CompletionService, naming the organisation and nothing else; core-api
resolves the organisation's model choice, opens the key only it holds, makes the
call, and returns the content and what it cost. This process therefore holds no
model endpoint and no credential, by construction: the thing ENT-236 relaxed
for one field of one request is not relaxed any more, because the field is not
read any more.

`ModelClient` is the direct OpenAI-compatible HTTP client, pointed at whatever
serves the wire format (ENT-235: llama.cpp's `llama-server` by default). It is
kept for the evals recorder, a maintainer's tool that is pointed at a model by
argument, and for tests; the service builds none.
"""

from __future__ import annotations

import json
from typing import Any, Protocol

import httpx
from connectrpc.errors import ConnectError
from kindlast.platform.v1 import completion_connect, completion_pb2
from pydantic import BaseModel, ConfigDict, Field


class Completion(BaseModel):
    """One model response, with what it cost.

    Validated on construction rather than trusted, because these numbers are
    what the budget is charged against and what `agent_runs` reports as cost. A
    negative token count from a misbehaving endpoint would otherwise quietly
    buy the run more budget than it is entitled to.
    """

    model_config = ConfigDict(frozen=True, extra="forbid")

    content: str
    input_tokens: int = Field(ge=0)
    cached_input_tokens: int = Field(ge=0)
    output_tokens: int = Field(ge=0)
    finish_reason: str

    @property
    def total_tokens(self) -> int:
        return self.input_tokens + self.output_tokens


class ModelError(Exception):
    """The model could not be reached, or answered something unusable."""


class Completer(Protocol):
    """Anything that can answer one completion: both clients below, and the
    tests' fakes."""

    def complete(
        self,
        messages: list[dict[str, str]],
        schema: dict[str, Any] | None = None,
        max_tokens: int = 800,
        temperature: float = 0.7,
    ) -> Completion: ...


class TokenSource(Protocol):
    def get(self) -> str: ...


class ProxiedModelClient:
    """One organisation's completions, through core-api (ENT-256, part five).

    Bound to an organisation at construction, because that is the one thing
    core-api needs to know to decide whose model, and whose key, answers. It
    holds this service's own OAuth token source for calling core-api as
    itself, which is the one credential this process legitimately has, and
    nothing about any model.
    """

    def __init__(
        self,
        core_api_url: str,
        tokens: TokenSource,
        org_id: str,
        timeout: float = 300.0,
    ) -> None:
        if not org_id:
            raise ValueError("a proxied model client is bound to an organisation")
        self._org_id = org_id
        self._tokens = tokens
        self._timeout_ms = int(timeout * 1000)
        self._client = completion_connect.CompletionServiceClientSync(core_api_url)
        # What served the last completion, for the run record: core-api says,
        # because core-api knows; this process does not.
        self.provider: str = ""
        self.model: str = ""

    def complete(
        self,
        messages: list[dict[str, str]],
        schema: dict[str, Any] | None = None,
        max_tokens: int = 800,
        temperature: float = 0.7,
    ) -> Completion:
        request = completion_pb2.CompleteRequest(
            org_id=self._org_id,
            messages=[
                completion_pb2.ChatMessage(role=m["role"], content=m["content"])
                for m in messages
            ],
            response_schema_json=json.dumps(schema) if schema is not None else "",
            max_tokens=max_tokens,
            temperature=temperature,
            temperature_set=True,
        )
        try:
            response = self._client.complete(
                request,
                headers={"Authorization": f"Bearer {self._tokens.get()}"},
                timeout_ms=self._timeout_ms,
            )
        except ConnectError as exc:
            # Every code core-api uses here is "the model could not be
            # reached, or would not serve this organisation": the harness
            # records it as a failed run and says why. The message carries
            # no key, because core-api never puts one in an error.
            raise ModelError(f"core-api could not complete the call: {exc}") from exc
        except Exception as exc:  # noqa: BLE001 (anything else is the transport)
            raise ModelError(f"core-api did not answer: {exc}") from exc
        self.provider = response.provider
        self.model = response.model
        return Completion(
            content=response.content,
            input_tokens=int(response.input_tokens),
            cached_input_tokens=int(response.cached_input_tokens),
            output_tokens=int(response.output_tokens),
            finish_reason=response.finish_reason,
        )


class ModelClient:
    def __init__(
        self,
        base_url: str,
        api_key: str | None = None,
        model: str | None = None,
        timeout: float = 300.0,
        client: httpx.Client | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._api_key = api_key
        # WHICH MODEL TO ASK FOR, AND WHY IT IS OPTIONAL.
        #
        # `llama-server` serves exactly one file and ignores the field, so the
        # bundled path sends none and the request is one key shorter. Every
        # hosted provider requires it, and sending nothing there is a 400 that
        # reads as an authentication problem. So it is set for an organisation
        # that chose a provider and absent otherwise, rather than defaulted to
        # a name that would be a guess in both directions (ENT-236).
        self._model = model
        self._client = client or httpx.Client(timeout=timeout)

    def complete(
        self,
        messages: list[dict[str, str]],
        schema: dict[str, Any] | None = None,
        max_tokens: int = 800,
        temperature: float = 0.7,
    ) -> Completion:
        """One call. No streaming, no tools on the wire.

        Tools are dispatched by the loop rather than through the model's own
        tool-calling protocol, because §26.3 requires a per-skill allow-list
        with unknown tools refused rather than retried, and that decision has
        to sit in code we own rather than in a field the model fills in.
        """
        body: dict[str, Any] = {
            "messages": messages,
            "max_tokens": max_tokens,
            "temperature": temperature,
        }

        if self._model is not None:
            body["model"] = self._model

        if schema is not None:
            body["response_format"] = {
                "type": "json_schema",
                "json_schema": {"name": "output", "schema": schema},
            }

        headers = {"Content-Type": "application/json"}
        # Absent for a local model, and that absence is the product property
        # rather than a missing configuration (§18.1).
        #
        # PRESENT ONLY FOR AN ORGANISATION THAT CHOSE A HOSTED PROVIDER
        # (ENT-236), in which case it arrived in that run's request and is held
        # for the life of this client and no longer. It is in a header rather
        # than the URL deliberately: a key in a URL is a key in every log line
        # that URL appears in, including the ones this deployment writes.
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"

        try:
            response = self._client.post(
                f"{self._base_url}/v1/chat/completions", json=body, headers=headers
            )
            response.raise_for_status()
            payload = response.json()
        except httpx.HTTPError as exc:
            raise ModelError(f"the model endpoint failed: {exc}") from exc
        except json.JSONDecodeError as exc:
            raise ModelError(f"the model returned no JSON envelope: {exc}") from exc

        try:
            choice = payload["choices"][0]
            message = choice["message"]
        except (KeyError, IndexError) as exc:
            raise ModelError(f"unexpected response shape: {payload!r}") from exc

        # A REASONING TRACE HERE IS A CONFIGURATION FAULT, NOT A CURIOSITY.
        #
        # ENT-235 measured that this build enables thinking by default, against
        # its own documentation, and that a thinking run exhausted its token
        # limit before answering at all. The server is started with
        # `--reasoning off` for that reason, so a trace arriving means the flag
        # is not in force and every budget is being spent on tokens nobody will
        # read.
        if message.get("reasoning_content"):
            raise ModelError(
                "the model returned a reasoning trace, so --reasoning off is not "
                "in effect; the per-run token budget assumes it is"
            )

        usage = payload.get("usage", {})
        details = usage.get("prompt_tokens_details", {}) or {}

        return Completion(
            content=message.get("content") or "",
            input_tokens=int(usage.get("prompt_tokens", 0)),
            cached_input_tokens=int(details.get("cached_tokens", 0)),
            output_tokens=int(usage.get("completion_tokens", 0)),
            finish_reason=choice.get("finish_reason", ""),
        )
