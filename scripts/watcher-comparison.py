#!/usr/bin/env python3
"""Run the deterministic Watcher and the agentic one over the same organisation,
and compare them (ENT-258, PR 3).

    python3 scripts/watcher-comparison.py

Needs the compose stack up with the `model` profile, like
`intelligence-smoke.py`, whose credential plumbing this reuses rather than
copying. That file's comments explain why the client id is a username and why
the roles scope is plural; both facts cost an afternoon each and neither is
guessable from a specification.

# WHAT THIS GATES ON, AND WHAT IT ONLY REPORTS

The obvious gate is "the agent finds at least what the three fixed detectors
find". It is the wrong gate, and `intelligence-smoke.py` already argues the
first half of why: a local 2B on a CI runner is not a thing to build a red
build on. A gate that goes red because a small model had an off day teaches a
team to press re-run, which costs more than the gate was ever worth.

The second half matters more. It is the wrong SHAPE of question. The detectors
are not a target the agent has to reach: they keep running, they keep raising
exactly what they raised, and the agent is shown their output and told not to
repeat it. Its job is what no detector was written for, which is a connection
that went revoked, a granted tool nobody uses, a fact that changed since the
last look. Scoring it against a baseline it is explicitly instructed to skip
would measure the one thing it is designed not to do.

So the comparison is REPORTED, on every run, with the numbers a maintainer
actually wants: what the detectors raised, what the agent added, and how many
conditions it correctly recognised as already open. The gate is the set of
properties that must hold whatever the model said:

  * the watch comes back 200, and SUCCEEDED or REFUSED, never FAILED
  * a run was recorded, so the provenance exists either way
  * every signal the detectors raised is STILL THERE, untouched, afterwards:
    the agent adds, and cannot remove or alter one
  * nothing it wrote is outside the vocabulary, and nothing it cited was
    outside the set it was offered
  * NO FINDING WAS WRITTEN. There is no tool for it and no RPC for it, and
    this is the end-to-end proof of that rather than the unit-test proof

The last one is why this wants a live stack. Every other assertion here has a
unit test behind it; that one is a claim about the whole surface, and only a
running system can make it.
"""

from __future__ import annotations

import importlib.util
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

# The smoke script's plumbing, imported rather than duplicated: its filename is
# not an identifier, so it is loaded by path. Duplicating it is what ENT-250
# punished once already, when one copy of the container name was updated for
# per-worktree stacks and another was not.
_spec = importlib.util.spec_from_file_location(
    "intelligence_smoke", Path(__file__).with_name("intelligence-smoke.py")
)
smoke = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(smoke)

PROJECT = os.environ.get("COMPOSE_PROJECT_NAME", "kindlast")
# THROUGH THE EDGE, BECAUSE core-api PUBLISHES NO PORT AND THAT IS DELIBERATE.
# The Caddyfile routes `/kindlast.platform.v1.*` to it and says why the edge is
# the only way in; a script that wanted a direct port would be asking for the
# one property the compose file is arranged to prevent.
CORE_API = f"http://localhost:{os.environ.get('KINDLAST_EDGE_PORT', '8000')}"
INTELLIGENCE = smoke.INTELLIGENCE

# The permitted vocabulary, written out here on purpose. This script is the
# outermost check, so it asserts against what the SCHEMA permits rather than
# importing the service's own lists: a check that imports the thing it is
# checking agrees with it by construction and proves nothing.
KINDS = {"deadline", "profile_gap", "dsar", "regulatory_update"}
SEVERITIES = {"low", "medium", "high", "critical"}

FIXTURE_SLUG = "watcher-comparison"


def fail(message: str) -> None:
    print(f"watcher-comparison: {message}", file=sys.stderr)
    sys.exit(1)


def psql(sql: str) -> str:
    result = subprocess.run(
        [
            "docker",
            "exec",
            f"{PROJECT}-postgres-app",
            "psql",
            "-U",
            "kindlast_migrator",
            "-d",
            "kindlast",
            "-tAc",
            sql,
        ],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        fail(f"psql failed: {result.stderr.strip()}\n  {sql}")
    # THE FIRST LINE, NOT THE WHOLE OUTPUT. `psql -tAc` prints the rows and
    # then the command tag, so an `insert ... returning id` comes back as the
    # id followed by "INSERT 0 1", and passing that into the next statement is
    # an invalid uuid with a confusing message. Every query here returns one
    # value or one JSON document, so the first line is the answer.
    for line in result.stdout.splitlines():
        if line.strip():
            return line.strip()
    return ""


def seed_fixture() -> str:
    """One organisation with a profile the detectors have opinions about.

    Seeded here rather than reused from the compose seed, because what this
    compares depends on the profile's answers: a fixture that drifted with the
    seed would move the numbers in the report for reasons nobody reading it
    could see. Idempotent, so running it twice against a live stack is not an
    error.
    """
    org = psql(
        f"""
        with existing as (select id from organisations where slug = '{FIXTURE_SLUG}'),
             created as (
               insert into organisations (slug, name)
               select '{FIXTURE_SLUG}', 'Watcher comparison fixture'
                where not exists (select 1 from existing)
               returning id
             )
        select id::text from existing union all select id::text from created
        """
    )
    if not org:
        fail("the fixture organisation could not be created")

    owner = psql(f"select user_id::text from memberships where org_id = '{org}' limit 1")
    if not owner:
        owner = psql(
            f"""insert into memberships (org_id, user_id, role)
                values ('{org}', gen_random_uuid(), 'owner') returning user_id::text"""
        )

    if not psql(f"select id::text from compliance_profiles where org_id = '{org}' limit 1"):
        session = psql(
            f"""insert into onboarding_sessions (org_id, created_by)
                values ('{org}', '{owner}') returning id::text"""
        )
        psql(
            f"""insert into compliance_profiles
                  (org_id, created_by, session_id, industry, has_dpo, has_ropa,
                   transfers_outside_eu, eu_jurisdictions, staff_count)
                values ('{org}', '{owner}', '{session}', 'saas', 'no', 'no', 'no',
                        '{{DE}}', 40)"""
        )
        psql(
            f"""insert into org_profile_facts (org_id, key, value, source, recorded_by)
                values ('{org}', 'has_dpo', '"no"'::jsonb, 'onboarding', '{owner}'),
                       ('{org}', 'has_ropa', '"no"'::jsonb, 'onboarding', '{owner}')"""
        )
    return org


def signals(org: str) -> dict[str, dict[str, str]]:
    """Every open signal for the organisation, keyed by deduplication key.

    Read as one JSON document rather than as delimited lines, because a title
    is free text a model wrote and any separator picked here is a separator it
    can emit.
    """
    raw = psql(
        f"""
        select coalesce(jsonb_object_agg(f.dedup_key, jsonb_build_object(
                 'title', f.title, 'severity', f.severity, 'status', f.status,
                 'kind', f.kind, 'slug', coalesce(f.obligation_slug, ''))),
                 '{{}}'::jsonb)::text
          from watcher_findings f
          join compliance_profiles p on p.id = f.profile_id
         where p.org_id = '{org}'
        """
    )
    return json.loads(raw or "{}")


def post(
    url: str,
    token: str,
    payload: dict,
    headers: dict | None = None,
    timeout: int = 900,
) -> tuple[int, dict, str]:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode(),
        headers={
            "Content-Type": "application/json",
            "Authorization": f"Bearer {token}",
            **(headers or {}),
        },
    )
    # Generous, for the reason the smoke script gives: a watch is several model
    # calls on a CPU, and the harness's own wall-clock budget is what should
    # decide a run took too long, not this script's patience.
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, json.load(response), ""
    except urllib.error.HTTPError as error:
        return error.code, {}, smoke._body(error)


def main() -> None:
    client_id, client_secret, audience = smoke.read_credentials()
    token = smoke.mint(client_id, client_secret, audience)
    org = seed_fixture()
    print(f"fixture organisation {org}")

    # --- the deterministic half --------------------------------------------
    # `detectOnly`, so this is the Watcher alone. The Analyst turning signals
    # into findings is a separate step and would muddy what is being compared.
    status, _, body = post(
        f"{CORE_API}/kindlast.platform.v1.SweepService/RunSweep",
        token,
        {"detectOnly": True},
        headers={"Kindlast-Org-Id": org},
    )
    if status != 200:
        fail(f"the deterministic sweep returned {status}: {body}")

    baseline = signals(org)
    if not baseline:
        fail(
            "the deterministic detectors raised nothing for the fixture, so "
            "there is no baseline to compare against. Either the fixture no "
            "longer has a gap any detector looks for, or the corpus is not "
            "loaded."
        )
    findings_before = psql(f"select count(*) from findings where org_id = '{org}'")

    # --- the agentic half ---------------------------------------------------
    status, context, body = post(
        f"{CORE_API}/kindlast.platform.v1.WatcherService/WatcherContext",
        token,
        {"orgId": org},
    )
    if status != 200:
        fail(f"assembling the watch context returned {status}: {body}")
    if not context.get("hasProfile"):
        fail("the fixture has no compliance profile, so there is nothing to watch")
    if not context.get("intelligenceAvailable"):
        fail("this stack reports no Intelligence, so the comparison cannot run")

    offered = {o["slug"] for o in context.get("obligations", [])}
    status, watched, body = post(
        f"{INTELLIGENCE}/kindlast.platform.v1.IntelligenceService/Watch",
        token,
        {
            "orgId": org,
            "context": context,
            "modelEndpoint": {
                "provider": context.get("modelProvider", ""),
                "model": context.get("modelName", ""),
            },
        },
    )
    if status != 200:
        fail(f"the watch returned {status}: {body}")

    # --- what must hold, whatever the model said ---------------------------
    outcome = watched.get("outcome", "")
    if outcome not in ("WATCH_OUTCOME_SUCCEEDED", "WATCH_OUTCOME_REFUSED"):
        fail(
            f"the watch ended {outcome or 'unspecified'}. SUCCEEDED and REFUSED "
            "are both correct outcomes; FAILED means something broke that was "
            f"nobody's policy: {watched.get('outcomeDetail', '')}"
        )

    run_id = watched.get("agentRunId", "")
    if not run_id:
        fail("the watch returned no agent_runs id, so nothing can be checked")
    recorded = psql(f"select skill from agent_runs where id = '{run_id}'")
    if recorded != "watcher.sweep":
        fail(f"agent_runs has no watcher.sweep row for {run_id}: {recorded!r}")

    after = signals(org)
    for key, value in baseline.items():
        if key not in after:
            fail(
                f"the deterministic signal {key!r} is gone after the agent ran. "
                "The agent adds; it must not be able to remove one."
            )
        if after[key] != value:
            fail(
                f"the deterministic signal {key!r} changed from {value} to "
                f"{after[key]}. The agent must not be able to alter one."
            )

    for signal in watched.get("signals", []):
        if not signal.get("dedupKey"):
            fail("a signal was raised with no deduplication key")
        if signal.get("severity") not in SEVERITIES:
            fail(
                f"{signal.get('dedupKey')!r} was raised with severity "
                f"{signal.get('severity')!r}"
            )

    # Kind and citation are read from the rows rather than from the response,
    # because the response says what the harness sent and the rows are what the
    # database accepted.
    new = set(after) - set(baseline)
    for key in new:
        row = after[key]
        if row["kind"] not in KINDS:
            fail(f"the agent wrote {key!r} with kind {row['kind']!r}")
        if row["slug"] and row["slug"] not in offered:
            fail(
                f"the agent cited {row['slug']!r} for {key!r}, which it was not "
                "offered. A slug that exists and was never offered is still a "
                "fabrication."
            )

    findings_after = psql(f"select count(*) from findings where org_id = '{org}'")
    if findings_after != findings_before:
        fail(
            f"findings went from {findings_before} to {findings_after} across "
            "the watch. The Watcher has no tool that writes a finding and no "
            "RPC that would let it, so if this fires, that separation is gone."
        )

    # --- the comparison, reported rather than gated -------------------------
    repeats = [s for s in watched.get("signals", []) if not s.get("raised")]
    report = [
        "## The Watcher: fixed detectors against the agent",
        "",
        "| | |",
        "|---|---|",
        f"| Organisation | `{org}` |",
        f"| Obligations offered | {len(offered)} |",
        f"| Signals the detectors raised | {len(baseline)} |",
        f"| Signals the agent added | {len(new)} |",
        f"| Conditions it recognised as already open | {len(repeats)} |",
        f"| Outcome | `{outcome.removeprefix('WATCH_OUTCOME_')}` |",
        f"| Why | {watched.get('outcomeDetail') or '(it stopped on its own)'} |",
        "",
        "The detectors are not a target the agent has to reach. It is shown "
        "their output and told not to repeat it, so a high count in the fifth "
        "row is the agent working rather than failing. What is gated is that "
        "every one of their signals survived untouched, that nothing was "
        "written outside the vocabulary or citing an obligation the run was "
        "not offered, and that no finding was written.",
    ]
    if new:
        report += ["", "What the agent added:", ""]
        report += [
            f"- `{k}`: {after[k]['title']} ({after[k]['severity']})"
            for k in sorted(new)
        ]

    text = "\n".join(report)
    print(text)
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as handle:
            handle.write(text + "\n")


if __name__ == "__main__":
    main()
