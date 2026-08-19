#!/usr/bin/env bash
#
# One compose stack per worktree (ENT-250).
#
#     eval "$(./scripts/stack-env.sh)"     # this shell now points at this
#                                          # worktree's stack
#     ./scripts/stack-env.sh --write       # deploy/.env, so plain
#                                          # `docker compose -f deploy/compose.yaml`
#                                          # does too, in any shell
#
# WHY THIS EXISTS
#
# `deploy/compose.yaml` pins `name: kindlast`, so every checkout on the machine
# brought up one Postgres, one Zitadel and one Redis and shared them. That is
# fine with one checkout and a hazard with several, because the database is the
# thing being shared. On 2026-08-18 it cost, in one day:
#
#   * a migration from an unmerged branch applied to the shared database while
#     three sibling branches ran main's Go code, and three agents independently
#     diagnosed the same "not mine" test failure;
#   * main itself going red, because main's test and the shared schema
#     disagreed over a column added by a branch that had not merged;
#   * a migration hand-applied through psql because goose refused a gap, which
#     left the objects present and `goose_db_version` unaware of them;
#   * a grant committed on the shared stack to prove a test could go red,
#     because a rolled-back transaction is invisible to other sessions.
#
# None of those are mistakes a rule held by people reliably prevents. They are
# what happens when two branches address one database.
#
# WHAT IT DERIVES
#
# A compose project name and a block of seven host ports, from a SHA-256 of the
# worktree's absolute path. Deterministic in both directions that matter: the
# same worktree gets the same ports every time (so a stack survives a shell
# closing, and `down` finds what `up` created), and two worktrees get different
# ones without coordinating.
#
# THE DEFAULT IS UNCHANGED, DELIBERATELY.
#
# In the main checkout this prints exactly today's values: project `kindlast`,
# postgres on 5433, Zitadel on 8300, the edge on 8000. Every instruction in
# `README.md`, `docs/self-hosting.md` and the Postman collection stays true for
# somebody with one clone, which is most people and every self-hoster. The
# derivation activates for LINKED worktrees only. `--derive` forces it on and
# `--default` forces it off, for anyone who wants the other behaviour.
#
# COLLISIONS
#
# A hash over 1024 slots is not a guarantee, it is a probability. `--check`
# (implied by `--write`) asks docker whether any derived port is already
# published by a container belonging to a different project, and says so. Set
# `KINDLAST_STACK_SLOT` to any number in 1..1024 to move out of the way; the
# resolved slot is in the output, so the number in use is recoverable.
set -euo pipefail

# The block sits above every default in this file and below the range Linux
# allocates ephemeral ports from (`net.ipv4.ip_local_port_range`, 32768 up), so
# a derived port is never one the kernel might hand to an outbound connection
# first. 1024 slots of 8 covers 20800-28991.
PORT_BASE=20800
PORT_STRIDE=8
SLOTS=1024

# Today's published ports, in the order they are laid out within a slot. These
# are what a single checkout keeps.
DEFAULT_PG_APP_PORT=5433
DEFAULT_AUTH_PORT=8300
DEFAULT_MAILPIT_PORT=8025
DEFAULT_REDIS_PORT=6379
DEFAULT_EDGE_PORT=8000
DEFAULT_MODEL_PORT=8081
DEFAULT_INTELLIGENCE_PORT=8090

usage() {
  cat >&2 <<'USAGE'
usage: scripts/stack-env.sh [--write] [--check] [--summary] [--default|--derive]
                            [--root PATH]

  (no flags)  print `export` lines for eval, from this worktree
  --write     also write deploy/.env, which docker compose reads on its own
  --check     warn if a derived port is already published by another project
  --summary   human-readable, for reading rather than eval
  --default   force the single-checkout values (project kindlast, port 5433...)
  --derive    force the per-worktree values even in the main checkout
  --root PATH derive from PATH instead of this worktree (for tests)
USAGE
  exit 2
}

MODE=exports
WRITE=0
CHECK=0
FORCE=auto
ROOT=""

while [ $# -gt 0 ]; do
  case "$1" in
    --write) WRITE=1; CHECK=1 ;;
    --check) CHECK=1 ;;
    --summary) MODE=summary ;;
    --default) FORCE=default ;;
    --derive) FORCE=derive ;;
    --root) shift; [ $# -gt 0 ] || usage; ROOT="$1" ;;
    -h|--help) usage ;;
    *) usage ;;
  esac
  shift
done

REPO="$(cd "$(dirname "$0")/.." && pwd -P)"
[ -n "$ROOT" ] || ROOT="$REPO"

# The main checkout is the directory holding the shared git directory. In a
# linked worktree `--git-common-dir` points back at it, which is exactly the
# question being asked: am I the checkout everybody's documentation describes?
MAIN=""
if COMMON="$(git -C "$REPO" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"; then
  MAIN="$(cd "$(dirname "$COMMON")" && pwd -P)"
fi

case "$FORCE" in
  default) DERIVE=0 ;;
  derive)  DERIVE=1 ;;
  # No git means no worktrees, so there is nothing to be isolated from.
  *) if [ -z "$MAIN" ] || [ "$ROOT" = "$MAIN" ]; then DERIVE=0; else DERIVE=1; fi ;;
esac

sha256_hex() {
  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s' "$1" | sha256sum | cut -d' ' -f1
  else
    # macOS ships shasum rather than sha256sum, and the derivation has to agree
    # across the two or one worktree gets two answers on two machines. Both are
    # the same digest over the same bytes.
    printf '%s' "$1" | shasum -a 256 | cut -d' ' -f1
  fi
}

DIGEST="$(sha256_hex "$ROOT")"

# Basename, lowered and stripped to what compose accepts in a project name, so
# `docker ps` names the worktree rather than only a hash. Truncated, because
# these become container name prefixes.
SLUG="$(basename "$ROOT" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9_-' '-' | sed 's/-\{1,\}/-/g; s/^-//; s/-$//' | cut -c1-20)"
[ -n "$SLUG" ] || SLUG="worktree"

if [ "$DERIVE" = "1" ]; then
  if [ -n "${KINDLAST_STACK_SLOT:-}" ] && [ "${KINDLAST_STACK_SLOT}" != "0" ]; then
    SLOT="$KINDLAST_STACK_SLOT"
    case "$SLOT" in
      '' | *[!0-9]*)
        echo "stack-env: KINDLAST_STACK_SLOT must be a number, got '$SLOT'" >&2
        exit 1
        ;;
    esac
    if [ "$SLOT" -lt 1 ] || [ "$SLOT" -gt "$SLOTS" ]; then
      echo "stack-env: KINDLAST_STACK_SLOT must be 1..$SLOTS, got $SLOT" >&2
      exit 1
    fi
  else
    SLOT=$(( (0x${DIGEST:0:8} % SLOTS) + 1 ))
  fi
  BASE=$(( PORT_BASE + (SLOT - 1) * PORT_STRIDE ))
  PROJECT="${KINDLAST_STACK_PROJECT:-kindlast-${SLUG}-${DIGEST:0:4}}"
  PG_APP_PORT=$(( BASE + 0 ))
  AUTH_PORT=$(( BASE + 1 ))
  MAILPIT_PORT=$(( BASE + 2 ))
  REDIS_PORT=$(( BASE + 3 ))
  EDGE_PORT=$(( BASE + 4 ))
  MODEL_PORT=$(( BASE + 5 ))
  INTELLIGENCE_PORT=$(( BASE + 6 ))
else
  SLOT=0
  PROJECT="${KINDLAST_STACK_PROJECT:-kindlast}"
  PG_APP_PORT=$DEFAULT_PG_APP_PORT
  AUTH_PORT=$DEFAULT_AUTH_PORT
  MAILPIT_PORT=$DEFAULT_MAILPIT_PORT
  REDIS_PORT=$DEFAULT_REDIS_PORT
  EDGE_PORT=$DEFAULT_EDGE_PORT
  MODEL_PORT=$DEFAULT_MODEL_PORT
  INTELLIGENCE_PORT=$DEFAULT_INTELLIGENCE_PORT
fi

PG="127.0.0.1:${PG_APP_PORT}"
SLOT_NOTE=""
[ "$SLOT" = "0" ] && SLOT_NOTE="  (the single-checkout default)"

# THE WEIGHTS ARE SHARED, THE SERVER IS NOT.
#
# `deploy/models` is a bind mount rather than a named volume so `down -v` does
# not cost a 2.7 GB re-download. A worktree is its own directory, so leaving it
# relative would hand every worktree an empty directory and a fresh download,
# which is the same mistake in a new place. Point them all at the MAIN
# checkout's copy: one download per machine, and only the llama-server process
# is per project. In the main checkout, and with no git at all, this is the
# literal `./models` compose already defaults to.
if [ "$DERIVE" = "1" ] && [ -n "$MAIN" ]; then
  MODEL_DIR="${MAIN}/deploy/models"
else
  MODEL_DIR="./models"
fi

if [ "$CHECK" = "1" ] && command -v docker >/dev/null 2>&1; then
  conflicts=""
  for port in "$PG_APP_PORT" "$AUTH_PORT" "$MAILPIT_PORT" "$REDIS_PORT" \
    "$EDGE_PORT" "$MODEL_PORT" "$INTELLIGENCE_PORT"; do
    owner="$(docker ps --filter "publish=${port}" \
      --format '{{index .Labels "com.docker.compose.project"}}' 2>/dev/null | head -1)"
    if [ -n "$owner" ] && [ "$owner" != "$PROJECT" ]; then
      conflicts="${conflicts}  ${port} is published by project '${owner}'
"
    fi
  done
  if [ -n "$conflicts" ]; then
    {
      echo "stack-env: slot ${SLOT} collides with a stack that is already up:"
      printf '%s' "$conflicts"
      echo "           pick another with KINDLAST_STACK_SLOT=<1..${SLOTS}>"
    } >&2
  fi
fi

if [ "$WRITE" = "1" ]; then
  cat > "${REPO}/deploy/.env" <<EOF
# Generated by scripts/stack-env.sh (ENT-250). Not committed.
#
# docker compose reads this file on its own, because it sits beside
# compose.yaml, so \`docker compose -f deploy/compose.yaml up -d\` from this
# worktree addresses this worktree's stack with no flags and no sourcing.
#
# The test suites do NOT read it. For those:  eval "\$(./scripts/stack-env.sh)"
#
# Worktree: ${ROOT}
# Slot:     ${SLOT}${SLOT_NOTE}
COMPOSE_PROJECT_NAME=${PROJECT}
KINDLAST_PG_APP_PORT=${PG_APP_PORT}
KINDLAST_AUTH_PORT=${AUTH_PORT}
KINDLAST_MAILPIT_PORT=${MAILPIT_PORT}
KINDLAST_REDIS_PORT=${REDIS_PORT}
KINDLAST_EDGE_PORT=${EDGE_PORT}
KINDLAST_MODEL_PORT=${MODEL_PORT}
KINDLAST_INTELLIGENCE_PORT=${INTELLIGENCE_PORT}
KINDLAST_MODEL_DIR=${MODEL_DIR}
EOF
  echo "stack-env: wrote ${REPO}/deploy/.env (project ${PROJECT}, slot ${SLOT})" >&2
fi

if [ "$MODE" = "summary" ]; then
  cat <<EOF
worktree            ${ROOT}
project             ${PROJECT}
slot                ${SLOT}${SLOT_NOTE}
postgres-app        127.0.0.1:${PG_APP_PORT}
auth (Zitadel)      http://localhost:${AUTH_PORT}
mailpit             http://localhost:${MAILPIT_PORT}
redis               127.0.0.1:${REDIS_PORT}
edge (the console)  http://localhost:${EDGE_PORT}
model               http://localhost:${MODEL_PORT}
intelligence        http://localhost:${INTELLIGENCE_PORT}
model weights       ${MODEL_DIR}
EOF
  exit 0
fi

# Everything a suite or a script needs, in one place, because the failure this
# fixes is a suite reaching a stack that is not the one its branch brought up.
# `PG_*_URL` and `REDIS_ADDR` are the names the Go and TypeScript suites
# already read; they are derived here rather than duplicated there.
#
# KINDLAST_WEB_URL is deliberately NOT exported. Playwright reads it as "a
# console is already running, do not start one", so exporting it would silently
# stop `bun run test:e2e` from bringing up the dev server. Point the suite at
# the containerised console explicitly instead:
#
#     KINDLAST_WEB_URL="$KINDLAST_EDGE_URL" bun run test:e2e
cat <<EOF
export COMPOSE_PROJECT_NAME='${PROJECT}'
export KINDLAST_STACK_SLOT='${SLOT}'
export KINDLAST_PG_APP_PORT='${PG_APP_PORT}'
export KINDLAST_AUTH_PORT='${AUTH_PORT}'
export KINDLAST_MAILPIT_PORT='${MAILPIT_PORT}'
export KINDLAST_REDIS_PORT='${REDIS_PORT}'
export KINDLAST_EDGE_PORT='${EDGE_PORT}'
export KINDLAST_MODEL_PORT='${MODEL_PORT}'
export KINDLAST_INTELLIGENCE_PORT='${INTELLIGENCE_PORT}'
export KINDLAST_MODEL_DIR='${MODEL_DIR}'
export PG_HOST='127.0.0.1'
export PG_PORT='${PG_APP_PORT}'
export PG_SUPER_URL='postgres://postgres:postgres-dev-password@${PG}/kindlast'
export PG_MIGRATOR_URL='postgres://kindlast_migrator:migrator-dev-password@${PG}/kindlast'
export PG_APP_URL='postgres://kindlast_app:app-dev-password@${PG}/kindlast'
export PG_AGENT_URL='postgres://kindlast_agent:agent-dev-password@${PG}/kindlast'
export PG_BILLING_URL='postgres://kindlast_billing:billing-dev-password@${PG}/kindlast'
export PG_INGEST_URL='postgres://kindlast_ingest:ingest-dev-password@${PG}/kindlast'
export REDIS_ADDR='127.0.0.1:${REDIS_PORT}'
export KINDLAST_REDIS_URL='redis://127.0.0.1:${REDIS_PORT}'
export KINDLAST_AUTH_URL='http://localhost:${AUTH_PORT}'
export KINDLAST_EDGE_URL='http://localhost:${EDGE_PORT}'
EOF
