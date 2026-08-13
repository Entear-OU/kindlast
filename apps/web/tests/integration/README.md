# Integration tests

Vitest suites that exercise real Postgres + Supabase Auth against a local stack.
Scaffolded in [ENT-43](https://linear.app/entear/issue/ENT-43/establish-supabase-backed-integration-test-scaffold).

## Running

```bash
# One-time: boot the local stack (Docker required)
supabase start

# Run all tests — unit + integration. Integration suites auto-skip if the
# local stack is unreachable (so unit-only contributors don't need Docker).
bun run test
```

Local Studio runs at <http://localhost:54323>; the suite connects to the
default published ports:

- Postgres: `postgresql://postgres:postgres@127.0.0.1:54322/postgres`
- Auth / PostgREST: `http://127.0.0.1:54321`

Override with `SUPABASE_TEST_DB_URL`, `SUPABASE_TEST_URL`,
`SUPABASE_TEST_ANON_KEY`, `SUPABASE_TEST_SERVICE_ROLE_KEY` if you ever point
tests at a non-default stack (e.g. a CI sandbox).

## Layout

```
tests/integration/
├── helpers/
│   ├── supabase.ts       # anon / service-role / authenticated client factories
│   ├── test-user.ts      # ephemeral test-user lifecycle (Auth admin API)
│   └── db-fixture.ts     # direct-`pg` SQL fixture apply/drop
├── rls-convention.test.ts  # sample suite — RLS scoped to auth.uid()
└── README.md
```

## Authoring conventions

- **Tests own their fixtures.** Use `applyFixtureSql` in `beforeAll`,
  `dropFixtureSql` in `afterAll`. Idempotent DDL (`if not exists`,
  `drop ... if exists` + `create`) keeps suites re-runnable without
  `supabase db reset`.
- **Namespace test data.** Test users get `*@kindlast.test` emails;
  test-only tables get a `_test_` prefix.
- **Service-role client for setup only.** Asserting RLS requires
  `createUserClient(email, password)` or `createAnonClient()` — the
  service-role client bypasses RLS by design.
- **Skip cleanly when the stack is down.** Use
  `await isLocalSupabaseReachable()` at module scope + `describe.skipIf`
  so `bun run test` stays green on machines without Docker.
