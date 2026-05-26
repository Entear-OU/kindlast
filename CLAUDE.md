# Kindlast — Claude Code Instructions

## Package Manager

Always use **pnpm** — never npm or yarn.

## Development Approach

Always follow **test-driven development (TDD)**:

1. Write failing tests first
2. Implement the minimum code to make tests pass
3. Refactor while keeping tests green

Use **Vitest** for unit/integration tests and **React Testing Library** for component tests.

## Git Strategy

One branch + PR per Linear sub-issue — never bundle multiple sub-issues into a single branch.

- Branch from `main` using Linear's generated `gitBranchName` (e.g. `dev/ent-40-delete-legacy-assessment-flow-code`).
- One PR per sub-issue, merged sequentially when its acceptance criteria pass.
- Each sub-issue's commit history can be multiple commits, but the PR scope stays bounded to that issue.
- Epic-level issues (e.g. ENT-30) do not get their own branch; they exist only to group sub-issues.
- **PR titles must include the Linear issue ID** as a prefix in brackets, e.g. `[ENT-40] chore: remove legacy assessment-flow code`. The body should link back to the Linear issue URL so Linear auto-links the PR.
