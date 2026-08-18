#!/usr/bin/env python3
"""Prove the local model endpoint answers, and answers in the shape asked for.

ENT-235's acceptance criteria, as a script rather than as prose in an issue, so
CI runs the same thing a developer runs.

    python3 scripts/model-smoke.py [base_url]

It asserts the SHAPE and deliberately not the CONTENT. The model is free to be
wrong about which article the GDPR puts a record of processing activities in,
and it is: asked twice on a 2B tier it answered 50 and then 34, where the
answer is 30. Both were schema-valid.

That is the point rather than a caveat. The grammar guarantees the shape and
nothing guarantees the content, which is why §26.3 refuses any citation that
does not resolve to a stored obligation. A smoke test that asserted the right
article would be testing the model's knowledge, would be flaky by design, and
would go red for a reason that is somebody else's job to catch.
"""

from __future__ import annotations

import json
import sys
import urllib.request

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8081"

REQUEST = {
    "messages": [
        {
            "role": "system",
            "content": (
                "Reply with JSON having exactly two fields: "
                "regulation (string) and article (integer)."
            ),
        },
        {
            "role": "user",
            "content": "Which GDPR article requires a record of processing activities?",
        },
    ],
    "response_format": {
        "type": "json_schema",
        "json_schema": {
            "name": "citation",
            "schema": {
                "type": "object",
                "properties": {
                    "regulation": {"type": "string"},
                    "article": {"type": "integer"},
                },
                "required": ["regulation", "article"],
                "additionalProperties": False,
            },
        },
    },
    "max_tokens": 200,
}


def main() -> int:
    request = urllib.request.Request(
        f"{BASE}/v1/chat/completions",
        data=json.dumps(REQUEST).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=300) as response:
        body = json.load(response)

    choice = body["choices"][0]
    message = choice["message"]

    if choice["finish_reason"] != "stop":
        print(
            f"finish_reason was {choice['finish_reason']!r}, not 'stop'. "
            "A 'length' here usually means the model spent its budget thinking.",
            file=sys.stderr,
        )
        return 1

    # The server is started with `--reasoning off` because §26.3's per-run
    # token budget assumes the cheap path. This build enables thinking by
    # default, so the flag is load-bearing rather than belt and braces, and a
    # regression would be silent and expensive.
    if message.get("reasoning_content"):
        print(
            "the server returned a reasoning trace, so --reasoning off is not "
            "in effect; the per-run token budget assumes it is",
            file=sys.stderr,
        )
        return 1

    try:
        obj = json.loads(message["content"])
    except (TypeError, json.JSONDecodeError) as exc:
        print(f"content is not JSON: {exc}: {message.get('content')!r}", file=sys.stderr)
        return 1

    if set(obj) != {"regulation", "article"}:
        print(f"schema not honoured, got keys {sorted(obj)}", file=sys.stderr)
        return 1
    if not isinstance(obj["article"], int) or isinstance(obj["article"], bool):
        print(f"article is not an integer: {obj['article']!r}", file=sys.stderr)
        return 1

    usage = body.get("usage", {})
    print(
        f"schema-valid: {obj}  "
        f"(completion {usage.get('completion_tokens')} tokens, "
        f"cached {usage.get('prompt_tokens_details', {}).get('cached_tokens')} "
        f"of {usage.get('prompt_tokens')})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
