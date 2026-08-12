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
- **[Supabase CLI](https://supabase.com/docs/guides/local-development/cli/getting-started)**,
  needed only for the integration tests and any database work.
- **Docker**, required by `supabase start`.

### Getting running

```bash
bun install
cp .env.example .env
bun run dev
```

The app boots without most environment variables. `.env.example` documents what
each one unlocks. Notably `EMAIL_PROVIDER` defaults to `console`, so local
development and CI need no email credentials.

For anything touching the database, boot the local stack:

```bash
supabase start
supabase db reset    # replay all migrations from scratch
```

## Testing

**This project follows test-driven development, and it is not decorative.**
Write the failing test first, implement the minimum that makes it pass, then
refactor while it stays green. Pull requests that change behaviour without
tests will be asked for them, so it is quicker to write them as you go.

```bash
bun run test              # everything
bun run test:unit         # unit and component tests (apps/web/__tests__/)
bun run test:integration  # integration tests (apps/web/tests/integration/), needs Supabase running
bun run test:watch        # watch mode
bun run test:coverage     # with coverage
```

[Vitest](https://vitest.dev) for unit and integration tests,
[React Testing Library](https://testing-library.com/react) for components.

The integration suites self-skip when the local Supabase stack is unreachable,
so a green `bun run test` locally does not necessarily mean the integration tests
ran. CI boots the stack and fails loudly if it is missing, so they cannot
silently disappear there.

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
