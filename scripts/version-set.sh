#!/usr/bin/env bash
#
# Set the product's version everywhere it is written down.
#
#     bun run version:set 0.2.0
#
# One command rather than three edits, because the three drifting apart is the
# failure this and scripts/version-check.sh exist to prevent. It does not
# commit, does not tag and does not touch the changelog: those are judgement
# calls, and docs/versioning.md lists them in order.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-}"

if [ -z "$VERSION" ]; then
  echo "usage: bun run version:set <version>   (e.g. 0.2.0)" >&2
  exit 2
fi

VERSION="${VERSION#v}"

if ! printf '%s' "$VERSION" \
  | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'; then
  echo "version-set: '${VERSION}' is not a semantic version" >&2
  exit 1
fi

printf '%s\n' "$VERSION" > VERSION

# Only the first "version" key, which in both manifests is the package's own.
# A dependency happening to be named `version` would sit under `dependencies`,
# further down, and is not what this rewrites.
for manifest in package.json apps/web/package.json; do
  perl -0pi -e 's/("version"\s*:\s*")[^"]+(")/${1}'"$VERSION"'${2}/' "$manifest"
done

./scripts/version-check.sh

cat <<NEXT

Next, from docs/versioning.md:
  1. Move the changelog's Unreleased entries under a ${VERSION} heading.
  2. Commit as: chore(release): ${VERSION}
  3. git tag -a v${VERSION} -m "v${VERSION}" && git push origin v${VERSION}
NEXT
