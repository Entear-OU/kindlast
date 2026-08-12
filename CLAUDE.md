# Kindlast - Claude Code Instructions

## Writing Style

**Never use em dashes (—) in copy.** This applies to all user-facing product
copy, UI strings, marketing text, docs, commit messages, PR descriptions, and
Linear issues. Use a comma, a colon, parentheses, or a full stop instead, and
rewrite the sentence if none of those fit. Hyphens in compound words
(`plain-language`, `least-privilege`) are fine.

## Package Manager

Always use **Bun**, never npm, yarn or pnpm. Install with `bun install`, run
scripts with `bun run <script>`, and reach for one-off binaries with `bunx`.

Bun is the package manager and task runner only. Next.js and the test suite
still run on Node, so both toolchains have to be present.

**Never write `bun test`.** That invokes Bun's own test runner, which collects
none of this repo's Vitest suite and still exits 0, so it reads as a pass. The
command is `bun run test` (or `test:unit` / `test:integration`).

## Development Approach

Always follow **test-driven development (TDD)**:

1. Write failing tests first
2. Implement the minimum code to make tests pass
3. Refactor while keeping tests green

Use **Vitest** for unit/integration tests and **React Testing Library** for component tests.

## Git Strategy

One branch and one PR per unit of work. Never bundle unrelated changes into a
single branch.

- Branch from `main`.
- Each branch's commit history can be multiple commits, but the PR scope stays
  bounded to one change.
- Write the commit message and PR body for someone who will read them in a year
  with no other context: what changed, and why it needed changing.

### Branch and PR naming

If you are working from a Linear issue (Kindlast maintainers), use the
project's issue tracking conventions, documented in
[`docs/maintainers.md`](./docs/maintainers.md).

If you are not (this includes every external contributor), use:

- Branch: `type/short-description`, e.g. `fix/executor-empty-records`.
- PR title: `type: short description`, e.g. `fix: refuse blank compliance records`.
- Types follow Conventional Commits: `feat`, `fix`, `chore`, `docs`, `test`,
  `refactor`, `perf`.

Both conventions are accepted. Neither is enforced by CI, so use judgement
rather than treating this as a gate.

## Contributing

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the full contributor workflow,
including how to run the test suites and what a reviewable PR looks like.
