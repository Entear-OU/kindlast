#!/usr/bin/env bash
#
# Assert that every place carrying the product's version agrees with VERSION.
#
# There is one version for the whole product (docs/versioning.md), and the
# manifests carry a copy because tooling reads them. A copy that nobody checks
# is a copy that drifts, and the drift is invisible: `package.json` saying
# 0.1.0 while the tag says 0.4.0 costs nothing until somebody is trying to work
# out which build produced a finding.
#
# CI runs this. `bun run version:set X.Y.Z` is what keeps it passing.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ ! -f VERSION ]; then
  echo "version-check: VERSION is missing" >&2
  exit 1
fi

VERSION="$(tr -d '[:space:]' < VERSION)"

# Semantic Versioning 2.0.0's own grammar, minus the build metadata we have no
# use for. Anchored, because a version that merely CONTAINS a valid version is
# how you end up releasing "v1.2.3 ".
if ! printf '%s' "$VERSION" \
  | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'; then
  echo "version-check: VERSION is '${VERSION}', which is not a semantic version" >&2
  exit 1
fi

status=0

check() {
  manifest="$1"
  declared="$(grep -m1 '"version"' "$manifest" | sed -E 's/.*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
  if [ "$declared" != "$VERSION" ]; then
    echo "version-check: ${manifest} says ${declared}, VERSION says ${VERSION}" >&2
    status=1
  fi
}

# Every manifest that carries a version of the product itself. Workspace
# members whose version is not the product's would be listed nowhere, because
# there are none: apps/web is the product's console, not a package anyone
# installs. db/tests is a private test workspace and declares no version.
check package.json
check apps/web/package.json

if [ "$status" -ne 0 ]; then
  echo "version-check: run 'bun run version:set ${VERSION}' to bring them into line" >&2
  exit "$status"
fi

echo "version-check: ${VERSION}, and every manifest agrees"
