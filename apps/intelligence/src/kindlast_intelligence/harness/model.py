"""The model client: OpenAI-compatible HTTP, pointed at whatever serves it.

ENT-235 makes that llama.cpp's `llama-server` by default, local and needing no
API key. It could equally be vLLM on a GPU or a hosted provider, because the
abstraction point is the wire format rather than the runtime. Which is why
there is no llama.cpp in this file: the endpoint is configuration.
"""

from __future__ import annotations

import json
from typing import Any

import httpx
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


class ModelClient:
    def __init__(
        self,
        base_url: str,
        api_key: str | None = None,
        timeout: float = 300.0,
        client: httpx.Client | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._api_key = api_key
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

        if schema is not None:
            body["response_format"] = {
                "type": "json_schema",
                "json_schema": {"name": "output", "schema": schema},
            }

        headers = {"Content-Type": "application/json"}
        # Absent for a local model, and that absence is the product property
        # rather than a missing configuration (§18.1).
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
