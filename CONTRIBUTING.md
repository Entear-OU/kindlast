# Contributing to Kindlast

Thanks for considering a contribution. Kindlast is an AI-native compliance
workspace for EU companies, and it benefits enormously from people who have
felt the problem first hand.

This document covers the practical mechanics. For the architecture and what the
product actually does, start with the [README](./README.md).

## Before you start

**Open an issue first for anything non-trivial.** A bug fix or a typo can go
straight to a pull request. A new feature, a refactor, or anything that changes
behaviour deserves a conversation first, so you do not spend a weekend on
something that does not fit the direction.

**Contributions require accepting the [CLA](./CLA.md).** An automated check
will prompt you on your first pull request. It grants a licence, it does not
assign copyright: you keep full ownership of your work and can use it however
you like elsewhere.

## Development setup

### Prerequisites

- **[Bun](https://bun.sh) 1.3 or later.** Never npm, yarn or pnpm. The lockfile
  is `bun.lock`, and installing with anything else will produce a second
  lockfile that disagrees with it.
- **Node.js 22.13 or later.** Bun installs the dependencies and runs the
  scripts, but Next.js and Vitest both execute on Node, so you need both. CI
  runs Node 22.
- **Docker**, for the local stack: Postgres, the OIDC provider, Redis, the Go
  API and the edge. Needed for database work and the end-to-end tests.

### Getting running

```bash
bun install
docker compose -f deploy/compose.yaml up -d   # Postgres, Zitadel, Redis, core-api, edge
./scripts/web-env.sh                          # writes apps/web/.env.local from the stack
bun run dev
```

The marketing site boots without any of this. Signing in does not: the app is a
confidential OAuth client and needs the credentials the stack's seed job
created, which is what `web-env.sh` fetches. Do not write that file by hand,
and re-run the script after `docker compose down -v`, which discards the volume
holding them.

`.env.example` documents the remaining variables and what each unlocks. Notably
`EMAIL_PROVIDER` defaults to `console`, so local development and CI need no
email credentials.

## Testing

**This project follows test-driven development, and it is not decorative.**
Write the failing test first, implement the minimum that makes it pass, then
refactor while it stays green. Pull requests that change behaviour without
tests will be asked for them, so it is quicker to write them as you go.

```bash
bun run test              # everything
bun run test:unit         # unit and component tests (apps/web/__tests__/)
bun run test:e2e          # the sign-in round trip, needs the compose stack
bun run test:db           # tenant isolation (db/tests/), needs the compose stack
bun run test:watch        # watch mode
bun run test:coverage     # with coverage
```

[Vitest](https://vitest.dev) for unit tests,
[React Testing Library](https://testing-library.com/react) for components,
[Playwright](https://playwright.dev) for the browser journey, and Go's standard
`testing` package for the API.

The database suite self-skips when the local stack is unreachable, so a green
`bun run test` locally does not necessarily mean it ran. CI boots the stack and
fails loudly if it is missing, so it cannot silently disappear there. If you add
a suite that needs a service, give it the same treatment.

The end-to-end suite creates its own throwaway users through the identity
provider's admin API, so it needs the stack up but no accounts of yours.

Before pushing:

```bash
bun run lint
bunx tsc --noEmit
```

## House rules

**Never use em dashes in copy.** This applies to UI strings, product copy,
documentation, code comments, commit messages, and pull request descriptions.
Use a comma, a colon, parentheses, or a full stop, and rewrite the sentence if
none of those fit. Hyphens in compound words are fine. This is a real
convention here, enforced in review, and it is unusual enough that you would
not guess it.

**Match the surrounding code.** Comment density, naming, and idiom vary by area.
The existing file is the style guide.

**Keep pull requests bounded.** One branch, one PR, one change. A PR that fixes
a bug and refactors two unrelated modules is three PRs.

## Branch and PR naming

```
Branch:    fix/executor-empty-records
PR title:  fix: refuse blank compliance records
```

Types follow [Conventional Commits](https://www.conventionalcommits.org):
`feat`, `fix`, `chore`, `docs`, `test`, `refactor`, `perf`.

Maintainers use a Linear-derived convention instead, documented in
[`docs/maintainers.md`](./docs/maintainers.md). Both are accepted, and neither
is enforced by CI.

## Versioning and the changelog

Kindlast follows [Semantic Versioning](https://semver.org), with one version
for the whole product in `VERSION` at the root.
[`docs/versioning.md`](./docs/versioning.md) has the policy, including what
counts as a breaking change here and why a new required environment variable is
one.

**Two things a pull request may need.** If your change alters something a
self-hoster or an integrator depends on, add an entry under `Unreleased` in
[`CHANGELOG.md`](./CHANGELOG.md), written for somebody upgrading rather than
for somebody reading the diff. And if it changes behaviour but breaks nothing,
it still belongs there when they would want to know about it.

**Do not bump the version in a feature pull request.** Releases are cut
separately, and a branch that moves `VERSION` conflicts with every other branch
that does. `bun run version:check` is what CI runs, and it only asserts that
the manifests agree with `VERSION`; `bun run version:set X.Y.Z` is for cutting
a release.

## What makes a pull request easy to review

- A description explaining **why**, not just what. The diff already shows what.
- Tests that would have failed before your change.
- Green CI. Lint, typecheck, unit, and integration all have to pass.
- No unrelated formatting churn.
- Screenshots for anything that changes the UI.

## Reporting bugs and requesting features

Use the issue templates. For bugs, the single most useful thing you can provide
is a reproduction: the steps, what you expected, and what happened instead.

Security vulnerabilities do **not** go in public issues. See
[SECURITY.md](./SECURITY.md).

## Code of conduct

Participation is governed by our [Code of Conduct](./CODE_OF_CONDUCT.md).
Please read it.

## Questions

Open a [discussion](https://github.com/Entear-OU/kindlast/discussions) or ask on
the relevant issue. If something in this document is wrong or unclear, that is a
bug worth reporting too.
