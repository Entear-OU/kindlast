#!/usr/bin/env bash
#
# Air-gapped operation, proved rather than asserted (ENT-240).
#
#     ./scripts/airgap-check.sh              # ~3 minutes, needs docker
#     ./scripts/airgap-check.sh --keep       # leave the stack up afterwards
#
# WHAT THIS ANSWERS
#
# `docs/self-hosting.md` tells an operator that a default install, once built
# and running, makes no outbound request at all, and that a deployment holding
# a compliance record can run with no outbound internet. That was an audit of
# the source until this script existed: somebody had read the code and believed
# it. An audit is true on the day it is written and silent every day after, and
# the property it covers is one the next feature can take away by accident.
#
# So: bring the stack up on a network with no route out, and check that the
# product still answers.
#
# HOW IT RUNS
#
#   1. Bring the stack up normally. Images are pulled and `web` is built here,
#      which is install-time egress and is in the documented table.
#   2. NEGATIVE CONTROL. From a container on the stack's own network, reach a
#      public address. This must SUCCEED. If it does not, this machine has no
#      internet to block and the run proves nothing, so the script skips rather
#      than reporting a pass it did not earn.
#   3. Recreate the same stack with `deploy/compose.airgap.yaml`, which marks
#      the network `internal: true` and so removes its route out.
#   4. From a container on that network, reach the same public address. This
#      must FAIL.
#   5. From the same container, fetch the console through the edge. This must
#      SUCCEED, because "no egress" has to mean the product still works, not
#      that everything is broken equally.
#
# WHY STEP 2 IS THE PART THAT MAKES THIS A TEST
#
# A test that cannot fail is worse than no test. The assertion in step 4 passes
# trivially on a machine with no internet, on a runner behind a proxy that
# refuses, and on a laptop with the wifi off, and in each of those it would be
# reporting a property of the environment while claiming a property of the
# stack. Step 2 is the same probe on the same image against the same address
# over the network WITHOUT the override, so a run reaches step 4 only after
# demonstrating that the probe can succeed and therefore that step 4's failure
# is caused by the thing under test. Delete the `-f compose.airgap.yaml` and
# step 4 goes red, which is the fastest way to watch this test fail on purpose.
#
# ONE STACK PER WORKTREE. This drives the compose project this worktree owns
# (`scripts/stack-env.sh`), recreates it, and tears it down at the end unless
# you pass `--keep`. It is not gentle with a stack you had running: bring your
# own back up afterwards.
#
# SKIPPING. Without docker, or without internet, the script exits 0 and says
# which. `KINDLAST_REQUIRE_AIRGAP=1` turns every skip into a failure, and CI
# sets it, for the same reason the database suite is booted rather than allowed
# to self-skip: a green run that quietly checked nothing is the failure mode
# worth engineering against.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/deploy/compose.yaml"
AIRGAP_FILE="${REPO_ROOT}/deploy/compose.airgap.yaml"

# A tiny image with curl in it, pinned like everything else. Same image on both
# sides of the comparison, so the only thing that differs between step 2 and
# step 4 is the network.
PROBE_IMAGE="curlimages/curl:8.11.1"

# Two targets, and the second one is the point.
#
# A name proves the ordinary case. It is also the WEAKER assertion, because a
# container on an internal network fails to resolve anything external, so a
# refusal by name says only that DNS has nowhere to forward to. A service that
# had an address written into it would sail past that and still reach the
# internet, which is exactly the leak worth catching.
#
# So the second target is a literal address, and a refusal there is a refusal
# at the network layer rather than at the resolver. Both are well-known hosts
# that answer HTTPS, chosen for being boring rather than for being ours: a
# probe at our own infrastructure would fail for reasons that have nothing to
# do with the network under test.
PROBE_TARGETS="https://example.com https://1.1.1.1"

KEEP=0
for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=1 ;;
    -h | --help)
      sed -n '2,57p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

REQUIRE="${KINDLAST_REQUIRE_AIRGAP:-0}"

skip_or_fail() {
  if [ "$REQUIRE" = "1" ]; then
    echo "FAIL: $1 (KINDLAST_REQUIRE_AIRGAP=1 turns this skip into a failure)" >&2
    exit 1
  fi
  echo "SKIP: $1"
  exit 0
}

step() { echo; echo "== $1"; }

command -v docker >/dev/null 2>&1 || skip_or_fail "docker is not installed"
docker info >/dev/null 2>&1 || skip_or_fail "docker is installed but not running"

# The project name and ports this worktree owns (ENT-250). In a single checkout
# this is the default project and today's ports, so nothing changes for anyone
# with one clone.
eval "$("${REPO_ROOT}/scripts/stack-env.sh")"
PROJECT="${COMPOSE_PROJECT_NAME:-kindlast}"
NETWORK="${PROJECT}_default"

compose() { docker compose -f "$COMPOSE_FILE" "$@"; }
compose_airgap() { docker compose -f "$COMPOSE_FILE" -f "$AIRGAP_FILE" "$@"; }

cleanup() {
  if [ "$KEEP" = "1" ]; then
    echo
    echo "Leaving the stack up on the air-gapped network, as asked."
    echo "Tear it down with:"
    echo
    # The project name is spelled out rather than left to the environment, on
    # purpose. Without it, the same line pasted into a shell that has not run
    # `eval "$(./scripts/stack-env.sh)"` addresses the DEFAULT `kindlast`
    # project and takes down somebody else's stack and its volumes. That has
    # already happened once, to the person writing this.
    echo "  COMPOSE_PROJECT_NAME=${PROJECT} docker compose \\"
    echo "    -f deploy/compose.yaml -f deploy/compose.airgap.yaml down -v"
    return
  fi
  echo
  echo "== Tearing down ${PROJECT}"
  compose_airgap down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_for_seed() {
  for _ in $(seq 1 120); do
    status="$(docker inspect "${PROJECT}-seed" --format '{{.State.Status}}' 2>/dev/null || echo missing)"
    if [ "$status" = "exited" ]; then
      code="$(docker inspect "${PROJECT}-seed" --format '{{.State.ExitCode}}')"
      if [ "$code" != "0" ]; then
        echo "seed exited ${code}" >&2
        compose logs seed >&2 || true
        return 1
      fi
      return 0
    fi
    sleep 2
  done
  echo "seed did not finish in time" >&2
  return 1
}

# Runs curl inside a throwaway container attached to the stack's network.
# `--network` is how the probe sees exactly what the services see, which a curl
# on the host would not: the host has its own route out either way.
probe() {
  docker run --rm --network "$NETWORK" "$PROBE_IMAGE" \
    --silent --show-error --fail --max-time 15 "$@"
}

wait_for_edge() {
  for _ in $(seq 1 120); do
    if probe --output /dev/null "http://edge/healthz" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  echo "the edge never answered /healthz" >&2
  return 1
}

step "Clearing whatever ${PROJECT} was left in"
# A run that ended badly can leave containers attached to the internal network
# from the previous attempt, and compose will not move a running container
# between two networks that differ only in a flag. Then the first `up` below
# comes back with `lookup postgres-app: no such host`, which reads as a broken
# stack rather than as leftovers. Volumes are kept: this removes containers and
# the network, not the database.
compose_airgap down >/dev/null 2>&1 || true
echo "cleared"

step "Bringing up ${PROJECT} normally, so images are pulled and web is built"
compose up -d >/dev/null || {
  compose logs --tail 50 >&2 || true
  echo "the stack did not come up before the air-gap was even applied" >&2
  exit 1
}
wait_for_seed || exit 1
wait_for_edge || {
  compose logs --tail 50 edge web >&2 || true
  exit 1
}
echo "up"

step "Negative control: the probes reach the internet on the normal network"
for target in $PROBE_TARGETS; do
  if probe --output /dev/null "$target"; then
    echo "${target}: reachable, so a refusal later means something"
  else
    skip_or_fail "this machine cannot reach ${target} anyway, so blocking egress would prove nothing"
  fi
done

step "Recreating the same stack with no route out"
# `down` and not `down -v`: the volumes carry the migrated database and
# Zitadel's state, so the stack comes back as the one that was just proved
# healthy rather than as a fresh install. The network is what is being
# replaced, and a network cannot be reconfigured underneath running containers.
compose down >/dev/null
compose_airgap up -d >/dev/null || {
  compose_airgap logs --tail 50 >&2 || true
  echo "the stack did not come up with egress blocked, which is the finding" >&2
  exit 1
}
wait_for_seed || exit 1
wait_for_edge || {
  compose_airgap logs --tail 50 edge web auth >&2 || true
  echo "the stack came up with egress blocked but never served, which is the finding" >&2
  exit 1
}
echo "up, on an internal network"

step "The air-gap holds: the internet is unreachable from inside"
for target in $PROBE_TARGETS; do
  if probe --output /dev/null "$target"; then
    echo "FAIL: a container on the stack's network reached ${target}." >&2
    echo "Either the override did not apply or docker no longer honours" >&2
    echo "internal networks. Air-gapped operation is a documented property," >&2
    echo "so this is a product failure and not a test problem." >&2
    exit 1
  fi
  echo "${target}: refused, as it must be"
done

step "And the product still answers"
probe --output /dev/null "http://edge/healthz"
echo "edge healthy"

body="$(docker run --rm --network "$NETWORK" "$PROBE_IMAGE" \
  --silent --show-error --fail --max-time 30 "http://edge/")"
case "$body" in
  *"<html"* | *"<!DOCTYPE"* | *"<!doctype"*)
    echo "the console served a page with no internet available"
    ;;
  *)
    echo "FAIL: the edge answered but did not serve a page:" >&2
    echo "$body" | head -20 >&2
    exit 1
    ;;
esac

echo
echo "PASS: ${PROJECT} came up and served the console with egress blocked."
