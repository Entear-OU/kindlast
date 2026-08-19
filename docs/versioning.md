# Versioning

Kindlast uses [Semantic Versioning 2.0.0](https://semver.org). This page says
what that means for a self-hostable compliance workspace, because the general
rule ("bump major on a breaking change") does not on its own tell you whether a
new required environment variable is one.

## One version for the whole product

There is a single version number, in `VERSION` at the repository root. It names
the product, not a package: the web console, `core-api`, the workers gateway,
the Intelligence service, the database schema and the compose file that runs
them are released together, because that is how they are deployed. A
self-hoster runs one `docker compose up`, and "which Kindlast am I running" has
to have one answer.

The alternative, a version per workspace, is what you want when the pieces are
consumed independently: published libraries, an SDK somebody pins, a service
another team deploys on its own schedule. None of that is true here yet. When
it becomes true (a published client generated from `proto/` is the likeliest
first case), that artefact gets its own version and this page gains a section,
rather than the whole repository switching schemes.

`package.json` files carry the same number so that tooling reading them is not
lying. `scripts/version-check.sh` asserts they agree, and CI runs it, because a
rule nothing checks is a rule that drifts.

## What counts as breaking

The public surface of this product is what somebody outside the repository
depends on:

- **The proto contract** in `proto/`: an RPC removed or renamed, a field's
  meaning changed, a `required_scope` tightened.
- **The HTTP surface** an integrator reaches: routed paths at the edge, the
  webhook endpoints, the redirect endpoints in the auth flow.
- **Configuration**: an environment variable removed or renamed, a default
  changed in a way that alters behaviour, a new variable that is *required*
  rather than optional.
- **The database schema**, as far as a self-hoster's data is concerned: a
  migration that is not backward compatible with the previous release's code,
  or that cannot be applied to an existing database.
- **The deployment shape**: a service removed from `deploy/compose.yaml`, a
  published port changed, a volume whose contents are no longer read.

What is explicitly **not** breaking, however much it changes:

- Internal Go packages, TypeScript modules and Python modules. Nothing outside
  this repository imports them, and treating them as public would make every
  refactor a major release.
- UI copy, layout and page structure.
- Test suites, fixtures and development tooling.
- A migration that adds a nullable column, an index, or a table.

## Before 1.0, which is where we are

SemVer gives `0.y.z` its own rule and it is the one in force: **anything may
change in a minor bump.** So while the version starts with `0`:

- `0.MINOR.0` carries breaking changes as well as features.
- `0.y.PATCH` is for fixes that break nothing.

That is not a licence to be careless. A breaking change still goes in the
changelog under a heading that says so, with the upgrade step next to it,
because the person reading it is upgrading a system holding their compliance
record. Pre-1.0 means the version number does not shout about it; the release
notes still do.

1.0 is a statement that the schema and the API are ones we are prepared to
carry forward, rather than a statement that the product is finished.

## Releasing

1. Land everything you intend to ship on `main`.
2. Move the changelog's `Unreleased` entries under a new version heading with
   today's date, and write the upgrade note if there is one.
3. Bump `VERSION` and the `package.json` files together. `bun run version:set
   <version>` does all three and is the only supported way, because doing it by
   hand is how they drift.
4. Commit as `chore(release): X.Y.Z`.
5. Tag it `vX.Y.Z` and push the tag. The tag is the release; nothing is
   published to a registry.

There is deliberately no release automation, no changesets, and no bot that
opens a release pull request. At this cadence the ceremony would cost more than
it saves, and an automated changelog assembled from commit subjects is worse
than four sentences somebody wrote on purpose. Revisit when releases are
frequent enough that a human doing this is the bottleneck.

## Tags, branches and commits

- Tags are `vX.Y.Z`, annotated, on `main`.
- There are no release branches. Fixes go to `main` and ship in the next
  release, because supporting an old line means backporting, and nothing is
  deployed from an old line yet.
- Commit subjects follow Conventional Commits (`feat`, `fix`, `chore`, `docs`,
  `test`, `refactor`, `perf`). They are **not** wired to version bumps: the
  changelog is written, not generated, so a `feat` commit does not on its own
  decide the next number.
