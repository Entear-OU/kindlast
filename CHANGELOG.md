# Changelog

Notable changes to Kindlast, in the format of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning follows
[Semantic Versioning](https://semver.org), and
[`docs/versioning.md`](./docs/versioning.md) says what a breaking change means
for this product.

Entries are written rather than generated. The audience is somebody upgrading a
system that holds their compliance record, so an entry says what changed and
what they have to do about it, which no commit subject knows.

## [Unreleased]

### Changed

- The local stack can run once per checkout instead of once per machine.
  `deploy/compose.yaml` still defaults to the project name `kindlast` and to
  the ports every instruction here names, so a self-hoster with one clone sees
  no change at all. What is new is that `COMPOSE_PROJECT_NAME`, the
  `KINDLAST_*_PORT` variables and `KINDLAST_MODEL_DIR` now reach every part of
  the stack, including container names, so a second copy of the repository can
  bring up a second stack that shares nothing with the first.
  `scripts/stack-env.sh` derives a consistent set of those values, and
  `docs/maintainers.md` explains it. Relevant to anyone running two
  environments from one machine, and to anyone whose tooling assumed a
  container was called `kindlast-postgres-app`: it still is, unless a project
  name is set.

## [0.1.0]

The version the repository has carried in its manifests since the beginning,
recorded here so the history starts somewhere rather than appearing to begin
mid-sentence. No tag was cut for it, and this file does not attempt to
reconstruct the changes that led to it: the git history is the record for
everything before the first release.

[Unreleased]: https://github.com/Entear-OU/kindlast/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Entear-OU/kindlast/releases/tag/v0.1.0
