"""The golden set: adversarial cases with recorded model responses (ENT-229).

# THE RESPONSES ARE RECORDED, WHICH IS WHAT MAKES THIS A GATE

A case holds what a model said, not a prompt to send. Sending prompts would make
every CI run depend on a 2B's mood, and the suite would then fail for the one
reason it must never fail for: the model being wrong. The model being wrong is
the premise here, not the regression.

Recorded responses also mean the gate runs in milliseconds, in the `intelligence`
job, with no weights to download and no endpoint to be unreachable.

`kindlast_intelligence.evals.record` captures new ones from a running
`llama-server` in this exact schema, through the real Analyst prompt, so
refreshing the set against a model tier is a command rather than a transcription
exercise.

# TIERS

A case declares `weak` and `strong` responses, or the single key `any`.

`any` is for guardrails the model's quality cannot affect: a spent token budget
refuses whatever the model would have said, so giving it two identical responses
would pad both tiers' denominators with a case that cannot distinguish them and
would quietly shrink every rate below.

# THE REGISTRY IS CLOSED ON PURPOSE

`GUARDRAILS` lists every control the suite covers, and `test_evals.py` asserts
the two sets are equal in both directions. Without it, deleting a guardrail and
its case together leaves a green suite; with it, that deletion has to be made
twice, in two files, deliberately. A typo in a case's guardrail name would
otherwise cover something that does not exist while leaving the real control
uncovered.
"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from ..harness.run import Outcome

# Every control the golden set covers, by the name a case refers to it by.
#
# The tool allow-list is deliberately absent. It is a real guardrail and it is
# covered by `test_tool_dispatch.py`, but it is unreachable from this suite: the
# Analyst declares no tools, so a case here could only exercise it by inventing
# a skill that does not exist. A row that tested a fiction would be worse than an
# honest gap, because it would read as coverage.
GUARDRAILS: tuple[str, ...] = (
    # A citation the run was never given, which is the measured failure mode.
    "citation_validator",
    # A citation to an obligation that genuinely exists but was not offered to
    # this run. Still a fabrication: it came from somewhere other than the
    # context, which is the thing being caught.
    "offered_set",
    # Output that is not the contract at all.
    "typed_output",
    # Output cut off mid-answer, which parses cleanly and reads as brief.
    "truncation",
    "token_budget",
    "queue_wait",
    "wall_clock",
    # Instructions hidden in the text an organisation typed about itself.
    "prompt_injection",
    # An em dash or en dash in the narrative, which the system prompt has asked
    # against since ENT-160 and models kept producing anyway (ENT-163). The
    # case is here rather than only in the unit suite because the interesting
    # property is a tier one: asking works often enough on a strong model to
    # look like a control and not often enough on a weak one to be one.
    "house_style",
)


class TierResponse(BaseModel):
    """What a model of this tier said, and what the harness must do with it."""

    model_config = ConfigDict(extra="forbid")

    # The raw body, exactly as the endpoint returned it. A string rather than a
    # parsed object, because half the cases here are about output that does not
    # parse, and storing them parsed would make those cases unrepresentable.
    response: str
    finish_reason: str = "stop"
    output_tokens: int = Field(default=50, ge=0)
    input_tokens: int = Field(default=100, ge=0)
    # How long the generation took. Zero everywhere except the wall-clock case,
    # which is the one guardrail that cannot be provoked by the CONTENT of a
    # response: a run blows its wall clock by taking too long, and nothing about
    # what the model said can express that.
    delay_seconds: float = Field(default=0.0, ge=0)

    expect: Outcome
    # A substring the outcome detail must contain. Optional in the schema and
    # present on every non-successful case in practice, because "refused" alone
    # is a weak assertion: a run refused by the wrong guardrail would pass it,
    # and a suite that cannot tell which control fired cannot tell when one
    # stops firing.
    expect_detail: str = ""


class GoldenCase(BaseModel):
    """One adversarial situation, replayed through the real run."""

    model_config = ConfigDict(extra="forbid")

    id: str
    # None for a control case. A suite made only of adversarial cases reports a
    # weak tier that never succeeds and a utility delta of 100 percent, which
    # measures the golden set rather than the harness, so a few cases where both
    # tiers simply answer correctly are load-bearing rather than filler.
    guardrail: str | None = None
    # OWASP LLM Top 10 (2026) rows this case holds the line on. Kept on the case
    # rather than in a table elsewhere, so the mapping moves when the case does.
    owasp: list[str] = Field(min_length=1)
    why: str

    signal: str
    obligations: list[dict[str, str]] = Field(min_length=1)

    # Limit overrides for the cases whose whole subject is a limit. Empty for
    # everything else, which then runs on the same defaults production does.
    budget: dict[str, float] = Field(default_factory=dict)
    # Simulates a run that sat in the queue. Wall-clock rather than monotonic
    # for the reason `Budget.admit` gives: the enqueue happens elsewhere.
    queued_seconds_ago: float = Field(default=0.0, ge=0)

    tiers: dict[str, TierResponse]

    @property
    def offered(self) -> set[str]:
        """The slugs this run was given, which is what a citation is checked
        against. Derived from the obligations rather than declared beside them,
        so the two cannot disagree."""
        return {o["slug"] for o in self.obligations}


def load_cases(directory: Path) -> list[GoldenCase]:
    """Read every case in a directory, refusing anything malformed.

    Sorted by id so the report is stable between runs: a gate whose output
    reorders itself makes a diff of two CI logs useless.
    """
    directory = Path(directory)
    if not directory.is_dir():
        raise FileNotFoundError(f"no golden set at {directory}")

    cases: list[GoldenCase] = []
    for path in sorted(directory.glob("*.json")):
        payload: Any = json.loads(path.read_text())
        for raw in payload if isinstance(payload, list) else [payload]:
            case = GoldenCase.model_validate(raw)
            _check(case, path)
            cases.append(case)

    if not cases:
        # An empty golden set would make every rate below a division by zero and
        # every assertion vacuous, which is the failure mode where a gate is
        # green because it is measuring nothing.
        raise ValueError(f"the golden set at {directory} is empty")

    return sorted(cases, key=lambda c: c.id)


def _check(case: GoldenCase, path: Path) -> None:
    if case.guardrail is not None and case.guardrail not in GUARDRAILS:
        raise ValueError(
            f"{path.name}: case {case.id!r} names guardrail {case.guardrail!r}, "
            f"which is not in the registry {list(GUARDRAILS)}"
        )

    keys = set(case.tiers)
    if keys == {"any"}:
        return
    if not keys or not keys <= {"weak", "strong"}:
        raise ValueError(
            f"{path.name}: case {case.id!r} declares tiers {sorted(keys)}; "
            "expected 'weak' and 'strong', or 'any' alone"
        )


def default_golden_dir() -> Path:
    """The committed golden set, relative to this source tree.

    Repo data rather than package data, and deliberately so: these are fixtures
    a maintainer edits and diffs, not something the service loads at runtime.
    The service image has no reason to carry them and this function has no
    reason to resolve inside it, which is why the gate also accepts an explicit
    path on the command line.
    """
    return _repo_relative("evals/golden")


def default_baseline_path() -> Path:
    return _repo_relative("evals/baseline.json")


def _repo_relative(relative: str) -> Path:
    # src/kindlast_intelligence/evals/cases.py -> apps/intelligence
    return Path(__file__).resolve().parents[3] / relative
