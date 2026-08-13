# Kindlast: instructions for coding agents

The single source of truth for how to work in this repository. Tool-agnostic
by design, so every agent reads the same rules. `CLAUDE.md` imports this file
rather than repeating it.

## What this is

Kindlast is an AI-native compliance workspace for GDPR and the EU AI Act. It
holds a customer's compliance record, so two properties matter more than
anything else you might optimise for:

1. **Tenant isolation is a security boundary, not a filter.** Read the row
   level security section below before touching the database.
2. **Findings cite regulation.** Anything that fabricates a citation, a
   severity or an obligation is worse than nothing, because the product's
   value is that a human can check the claim against the law.

## Writing style

**Never use em dashes (or en dashes) in prose.** This applies to product copy,
UI strings, marketing text, documentation, code comments, commit messages, PR
descriptions and Linear issues. Use a comma, a colon, parentheses, or a full
stop, and rewrite the sentence if none of those fit.

Hyphens in compound words (`plain-language`, `least-privilege`) are fine, as
are hyphens in numeric ranges (`2-4 hours`).

## Repository layout

A monorepo. The Next.js app is one workspace among several, not the root.

```
apps/web/            The Next.js app: console, marketing site, API routes
apps/core-api/       Go resource server. Owns the domain schema
libs/chassis/        Infrastructure only. No business types, ever
gen/go/              Generated proto code, committed, CI fails on drift
proto/               The contract, single source of truth for service boundaries
db/migrations/       goose migrations for the self-managed stack
db/tests/            Database isolation suite (the RLS security boundary)
deploy/              compose.yaml, Postgres role split, Zitadel, Caddy
supabase/            The legacy stack the web app still runs on
data/corpus/         GDPR, AI Act, EDPB and enforcement source data
postman/             HTTP collection for the local stack
docs/                Self-hosting, maintainer workflow, brand
```

Two backends exist at once, deliberately. `apps/web` still reads Supabase in
production while the self-managed stack in `deploy/` is built out underneath
it. Do not assume a change to one is visible to the other.

## Package manager

Always **Bun**, never npm, yarn or pnpm. Install with `bun install`, run
scripts with `bun run <script>`, reach for one-off binaries with `bunx`.

Bun is the package manager and task runner only. Next.js and the test suites
run on Node, so both toolchains have to be present.

**Never write `bun test`.** That invokes Bun's own test runner, which collects
none of this repo's Vitest suite and still exits 0, so it reads as a pass. The
command is `bun run test`.

## Commands

Run these from the repository root. The root scripts proxy into the
workspaces, so you rarely need to `cd`.

```bash
bun run dev              # the Next.js app
bun run lint             # ESLint
bun run typecheck        # tsc, through the workspace so the version is pinned
bun run test:unit        # unit and component tests, no services needed
bun run test:integration # integration tests, needs the Supabase stack
bun run test:db          # database isolation suite, needs the compose stack
```

Use `bun run typecheck`, not `bunx tsc`. `bunx` resolves a compiler from the
registry when the binary is not linked at the root, which once meant CI ran a
newer TypeScript than the lockfile pins and failed on a stricter default.

For the Go workspace:

```bash
buf lint                 # proto lint
buf generate             # regenerate gen/, which is committed
go build ./... && go test ./...   # run from within a module directory
```

`buf` is pinned (see the CI workflow for the version). Generated code is
committed and CI fails on drift, so run `buf generate` and commit the result
whenever a proto changes.

## The local stack

```bash
docker compose -f deploy/compose.yaml up -d
```

One command from a clean checkout gives a healthy stack: Postgres with the
role split applied, migrations applied by a job container that must exit zero,
Zitadel serving OIDC discovery on `localhost:8300`, Redis, and a Caddy edge.
Tear down with `down -v`.

## Row level security, which is the thing to get right

A plain Postgres container starts with a superuser, and **superusers bypass
RLS entirely**. A table owner bypasses it too, unless the table sets `FORCE
ROW LEVEL SECURITY`. The failure mode is invisible: no error, no warning,
every test green, and tenant isolation simply absent.

So:

- The application connects as `kindlast_app`: `NOSUPERUSER`, `NOBYPASSRLS`,
  owns nothing, table-level grants only. Never change this to a role that
  owns tables or bypasses RLS, however convenient it looks.
- `kindlast_migrator` owns the schema and runs migrations. The application
  never connects as it.
- Every table in `public` has RLS enabled **and forced**.
- Tenancy is two session GUCs, `app.current_org_id` and
  `app.current_user_id`, set per request. Every policy is an org equality
  plus a membership `exists`, so a middleware bug that sets an org the caller
  does not belong to still reads zero rows.

`org_id` is the tenancy key on every tenant table. `created_by` and
`approved_by` record which human did something and are never used for
isolation. Do not reintroduce a `user_id` column that means "whose data is
this".

If you add a tenant table, it needs `org_id`, `FORCE ROW LEVEL SECURITY`, and
policies in the two-GUC form. `bun run test:db` asserts all of that over
`pg_class` rather than trusting convention, and it will fail if you forget.

## Development approach

Test-driven, and it is not decorative:

1. Write the failing test first
2. Implement the minimum that makes it pass
3. Refactor while green

Vitest for unit and integration tests, React Testing Library for components,
Go's standard `testing` package for Go.

**A test that cannot fail is worse than no test**, because it reports safety
that is not there. When a test covers a security property, prove it can fail:
break the property deliberately, watch the test go red, then restore it. The
isolation suite and the scope-declaration test were both verified that way.

Three test suites, and they are not interchangeable:

| Suite | Needs | Covers |
|---|---|---|
| `test:unit` | nothing | TypeScript modules and components |
| `test:integration` | Supabase running | the legacy stack's database behaviour |
| `test:db` | the compose stack | tenant isolation and privileges |

The integration and database suites **self-skip when their stack is
unreachable**, so a green local run does not prove they ran. CI boots each
stack and fails loudly if it cannot, which is what stops coverage
disappearing silently. Keep that property if you touch CI.

## Git strategy

One branch and one PR per unit of work. Never bundle unrelated changes into a
single branch, even small ones: a pure-movement diff is reviewable in minutes,
and the same move tangled into a feature branch is not.

- Branch from `main`.
- Multiple commits per branch are fine; the PR scope stays bounded to one
  change.
- Write the commit message and PR body for someone reading it in a year with
  no other context: what changed, and why it needed changing. Prefer the
  reasoning that is not recoverable from the diff.

### Branch and PR naming

Working from a Linear issue (maintainers): follow
[`docs/maintainers.md`](./docs/maintainers.md).

Otherwise, including every external contributor:

- Branch: `type/short-description`, e.g. `fix/executor-empty-records`
- PR title: `type: short description`, e.g. `fix: refuse blank compliance records`
- Types follow Conventional Commits: `feat`, `fix`, `chore`, `docs`, `test`,
  `refactor`, `perf`

Both conventions are accepted. Neither is enforced by CI, so use judgement
rather than treating it as a gate.

### Things to do rather than ask about

- Never push to `main`, force-push a shared branch, or merge a PR unless
  asked.
- Commit and push your own work on a branch; that is expected, not
  presumptuous.
- Do not commit secrets. The compose defaults are development-only values and
  are fine in the repo; anything generated per environment is not.

## When the design and the code disagree

The backend re-architecture is designed in `scratch/core-api-surface.md`
(untracked, held by maintainers). If you are implementing from it and the
document contradicts the code, or contradicts itself, **say so rather than
picking one**. Two examples that already happened: the document routed on an
organisation slug its own schema never defined, and specified a migration
squash whose policy surface changed once functions stopped running as
`SECURITY DEFINER`. Both were worth surfacing rather than quietly resolving.

## Contributing

See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the full contributor workflow,
including how to run the suites and what a reviewable PR looks like.
