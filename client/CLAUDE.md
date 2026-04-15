# Kindlast — Claude Code Instructions

## Package Manager

Always use **pnpm** — never npm or yarn.

## Development Approach

Always follow **test-driven development (TDD)**:

1. Write failing tests first
2. Implement the minimum code to make tests pass
3. Refactor while keeping tests green

Use **Vitest** for unit/integration tests and **React Testing Library** for component tests.
