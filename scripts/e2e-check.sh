#!/usr/bin/env bash
#
# The browser suite, run so that a green result means it actually ran (ENT-264).
#
#     scripts/e2e-check.sh auth.spec.ts journey.spec.ts
#
# WHY THIS WRAPPER EXISTS RATHER THAN A BARE `bun run test:e2e`
#
# AGENTS.md states the property this file enforces: the database suite
# self-skips when its stack is unreachable, so a green local run proves
# nothing, and CI's job is to boot the stack and fail loudly instead. The
# browser suite has the same hazard in three more shapes, and none of them is
# hypothetical:
#
#   1. NOTHING RAN. A renamed spec file, a `--grep` that matches nothing, or a
#      testDir that moved leaves Playwright with an empty run. Playwright does
#      exit non-zero on that today, but only because `--pass-with-no-tests` is
#      off by default, which is one flag away from silence.
#   2. EVERYTHING SKIPPED. `test.skip` at the top of a describe is a zero exit
#      code and a green tick, and it is the normal way somebody parks a flaky
#      test on a Friday. A run of nothing but skips has to be a failure here,
#      because the whole point of the job is that these paths were exercised.
#   3. THE WRONG CONSOLE. With no KINDLAST_WEB_URL, playwright.config.ts starts
#      `bun run dev` and the suite drives the development server. That is a
#      reasonable default on a laptop and the wrong thing in CI: the two builds
#      fail differently, and it is the production build behind the edge that a
#      self-hoster runs. A job that quietly tested the dev server would report
#      a green console nobody deploys.
#
# So the run is bracketed. Before: the console named must answer. After: the
# JSON report must show every spec file that was asked for, at least one test
# in each, and no skips. Each of the three was verified able to fail by being
# induced on purpose before this landed.
#
# It is deliberately usable on a laptop, because a guard that only exists
# inside a workflow file is one nobody can debug:
#
#     eval "$(./scripts/stack-env.sh)"
#     KINDLAST_WEB_URL="$KINDLAST_EDGE_URL" scripts/e2e-check.sh auth.spec.ts
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd -P)"
WEB="${REPO}/apps/web"

if [ $# -eq 0 ]; then
  echo "usage: scripts/e2e-check.sh <spec file> [<spec file> ...]" >&2
  echo "       e.g. scripts/e2e-check.sh auth.spec.ts journey.spec.ts" >&2
  exit 2
fi

SPECS=("$@")

# (3) above. Not defaulted, on purpose: a default here would be a silent
# fallback to the dev server, which is the exact failure this refuses.
if [ -z "${KINDLAST_WEB_URL:-}" ]; then
  cat >&2 <<'MSG'
e2e-check: KINDLAST_WEB_URL is not set.

It names the console under test, and this script will not guess. Unset,
playwright.config.ts would start `bun run dev` and the suite would drive the
development server instead of the production build the stack serves.

    eval "$(./scripts/stack-env.sh)"
    KINDLAST_WEB_URL="$KINDLAST_EDGE_URL" scripts/e2e-check.sh auth.spec.ts
MSG
  exit 1
fi

# The console answers before a browser is asked to drive it, so an unreachable
# stack fails here with the URL in the message rather than as thirty browser
# timeouts nobody reads to the end of.
echo "e2e-check: console under test is ${KINDLAST_WEB_URL}"
if ! curl --fail --silent --show-error --max-time 10 \
  "${KINDLAST_WEB_URL}/healthz" >/dev/null; then
  echo "e2e-check: ${KINDLAST_WEB_URL}/healthz did not answer." >&2
  echo "           Is the compose stack up?" >&2
  echo "           docker compose -f deploy/compose.yaml up -d" >&2
  exit 1
fi

# Zitadel too. Every spec either signs in or asserts about the hand-off, so an
# identity provider that is not serving discovery yet is a run that cannot
# pass, and saying so here costs a second.
AUTH_URL="${KINDLAST_AUTH_URL:-http://localhost:${KINDLAST_AUTH_PORT:-8300}}"
echo "e2e-check: identity provider is ${AUTH_URL}"
if ! curl --fail --silent --show-error --max-time 10 \
  "${AUTH_URL}/.well-known/openid-configuration" >/dev/null; then
  echo "e2e-check: ${AUTH_URL} is not serving OIDC discovery." >&2
  exit 1
fi

REPORT="${KINDLAST_E2E_REPORT:-${WEB}/e2e-report.json}"
rm -f "$REPORT"

# `github,json` in CI so the annotations still land on the run page and the
# machine-readable copy exists for the assertions below. `list,json` locally,
# because the GitHub reporter prints almost nothing to a terminal.
if [ -n "${CI:-}" ]; then
  REPORTERS="github,json"
else
  REPORTERS="list,json"
fi

status=0
(
  cd "$WEB"
  PLAYWRIGHT_JSON_OUTPUT_NAME="$REPORT" \
    node node_modules/@playwright/test/cli.js test \
    --reporter="$REPORTERS" \
    "${SPECS[@]}"
) || status=$?

# THE ASSERTIONS BELOW RUN ONLY ON A ZERO EXIT, AND THAT IS DELIBERATE.
#
# A failing run is already red, loudly, with the assertion that failed printed
# above. The only thing left to catch is the opposite case: exit code 0 over a
# report showing nothing was exercised. Running the checks on a red run as well
# was the first version of this file, and it was worse: Playwright marks the
# remaining tests of a `mode: 'serial'` describe as SKIPPED when an earlier one
# fails, so a single genuine failure also tripped the no-skip rule and printed
# "this run proves nothing" underneath a run that had just proven something
# quite specific. Observed, not imagined, on the first run of this script.
if [ "$status" -ne 0 ]; then
  echo "e2e-check: playwright reported failures (exit ${status})"
  exit "$status"
fi

if [ ! -f "$REPORT" ]; then
  echo "e2e-check: playwright wrote no JSON report at ${REPORT}." >&2
  echo "           Nothing can be proven about this run, so it is a failure." >&2
  exit 1
fi

KINDLAST_E2E_REPORT="$REPORT" KINDLAST_E2E_SPECS="${SPECS[*]}" node - <<'NODE'
const fs = require('node:fs')

const report = JSON.parse(fs.readFileSync(process.env.KINDLAST_E2E_REPORT, 'utf8'))
const wanted = process.env.KINDLAST_E2E_SPECS.split(/\s+/).filter(Boolean)

// Every test in the report, flattened, carrying the spec file it came from.
// The reporter nests describe blocks arbitrarily deep, so this walks rather
// than assuming a shape, and a nesting change in a future Playwright does not
// silently start reporting zero tests.
const results = []
const walk = (suite) => {
  for (const spec of suite.specs ?? []) {
    for (const test of spec.tests ?? []) {
      results.push({ file: spec.file ?? suite.file, title: spec.title, status: test.status })
    }
  }
  for (const child of suite.suites ?? []) walk(child)
}
for (const suite of report.suites ?? []) walk(suite)

const problems = []

// (1): the run has to have contained tests at all.
if (results.length === 0) {
  problems.push('the report contains no tests at all')
}

// And every spec file that was asked for has to be in it. This is the guard
// against a rename: a spec file that no longer exists under that name is a
// silently narrower suite, and a narrower suite is the thing a gate must never
// become without somebody deciding to.
for (const want of wanted) {
  const ran = results.filter((r) => (r.file ?? '').endsWith(want))
  if (ran.length === 0) {
    problems.push(`no test ran from ${want}, which this job requires`)
  }
}

// (2): a suite parked behind test.skip is a green run that exercised nothing.
// Only reachable here on an otherwise-passing run, so these are real skips
// rather than the cascade a failed serial describe produces.
const skipped = results.filter((r) => r.status === 'skipped')
if (skipped.length > 0) {
  problems.push(
    `${skipped.length} test(s) were skipped, and this job does not accept a skip:\n` +
      skipped.map((r) => `    ${r.file} > ${r.title}`).join('\n'),
  )
}

const counts = results.reduce((acc, r) => {
  acc[r.status] = (acc[r.status] ?? 0) + 1
  return acc
}, {})
console.log(
  `e2e-check: ${results.length} test(s) in the report ` +
    `(${Object.entries(counts).map(([k, v]) => `${k}: ${v}`).join(', ') || 'none'})`,
)

if (problems.length > 0) {
  console.error('\ne2e-check: this run proves nothing, so it fails:')
  for (const problem of problems) console.error(`  - ${problem}`)
  process.exit(1)
}
NODE

echo "e2e-check: the suite ran and passed"
