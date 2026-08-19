#!/usr/bin/env bash
set -uo pipefail
cd "$(dirname "$0")"
eval "$(./scripts/stack-env.sh)"
echo "PG_APP_URL=$PG_APP_URL"
echo "REDIS_ADDR=$REDIS_ADDR"
if [ "${1:-}" = "--in" ]; then
  shift
  dir="$1"
  shift
  cd "$dir"
fi
exec "$@"
