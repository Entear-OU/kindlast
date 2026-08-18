"""Capture golden-set responses from a real model (ENT-229).

    uv run python -m kindlast_intelligence.evals.record --tier weak

Points at a running `llama-server`, replays each case's signal and obligations
through the REAL Analyst prompt, and prints the recorded responses in the golden
set's schema. Refreshing the set against a new model tier is then a command
rather than a transcription exercise, which matters because a fixture somebody
typed from memory is a fixture that describes a model nobody ran.

# WHY THE GATE DOES NOT DO THIS ITSELF

Because then CI would depend on a 2B's mood, and the suite would fail for the
one reason it must never fail for: the model being wrong. Being wrong is the
premise of the golden set, not the regression. Recording is a deliberate act by a
maintainer with a model in front of them; the gate replays what was recorded.

# EXPECTATIONS ARE NOT REGENERATED, ON PURPOSE

Only `response`, `finish_reason` and the token counts are captured. `expect` and
`expect_detail` are left exactly as they were, because a recorder that rewrote
the expected outcome to match what just happened would turn every regression
into a fixture update and the gate into a very slow way of asserting nothing.

When a freshly recorded response no longer matches its expectation, that is the
interesting event: either the new tier stopped fabricating on that case, which
is worth knowing and worth a new case, or the harness changed. Both need a
person.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Sequence

from ..harness.model import ModelClient, ModelError
from ..skills import analyst
from . import cases as case_module
from .cases import GoldenCase, load_cases


def record_one(case: GoldenCase, client: ModelClient) -> dict[str, object]:
    """One live call, through the same prompt production uses.

    `analyst.build_messages` rather than a prompt written here: a recorder with
    its own prompt would capture a model's answer to a question the service never
    asks, and the fixtures would drift from the skill without anything noticing.
    """
    completion = client.complete(
        analyst.build_messages(case.signal, case.obligations),
        schema=analyst.output_schema(),
    )

    return {
        "response": completion.content,
        "finish_reason": completion.finish_reason,
        "input_tokens": completion.input_tokens,
        "output_tokens": completion.output_tokens,
    }


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tier", default="weak", help="which tier key to record into")
    parser.add_argument("--base-url", default="http://localhost:8081")
    parser.add_argument("--case", default="", help="record only this case id")
    parser.add_argument(
        "--golden", default="", help="the golden set directory (defaults to evals/golden)"
    )
    args = parser.parse_args(list(sys.argv[1:] if argv is None else argv))

    golden = Path(args.golden) if args.golden else case_module.default_golden_dir()
    client = ModelClient(args.base_url)

    captured: dict[str, dict[str, object]] = {}
    for case in load_cases(golden):
        if args.case and case.id != args.case:
            continue
        # Cases whose subject is a limit rather than an answer are skipped: a
        # freshly recorded response cannot make a spent token budget any more
        # spent, and re-recording them would only add noise to the diff.
        if "any" in case.tiers:
            continue

        try:
            captured[case.id] = record_one(case, client)
        except ModelError as exc:
            print(f"{case.id}: {exc}", file=sys.stderr)
            return 1

        recorded = captured[case.id]["response"]
        expected = case.tiers[args.tier].expect if args.tier in case.tiers else None
        print(f"# {case.id} (expects {expected})\n{recorded}\n", file=sys.stderr)

    # To stdout so it can be redirected, with the commentary above on stderr so
    # the two do not have to be separated by hand.
    print(json.dumps(captured, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
