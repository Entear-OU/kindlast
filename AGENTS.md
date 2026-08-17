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
data/corpus/         GDPR, AI Act, EDPB and enforcement source data
postman/             HTTP collection for the local stack
docs/                Self-hosting, maintainer workflow, brand
```

**Supabase is gone** (ENT-200). There is one backend now: the self-managed
stack in `deploy/`, with the domain schema in `db/migrations/` and the service
in `apps/core-api/`.

What that cost is worth knowing before you go looking for a page that is not
there. The console (dashboard, feed, compliance records, settings, billing,
onboarding) was removed with it, because its tenancy was Supabase's
`auth.uid()` row level security and the OIDC auth path produces no Supabase
session: every one of those pages redirected the visitor straight back out.
Each returns as its surface is rebuilt on core-api, and ENT-200 lists them in
build order. The authenticated surface today is `/o/{slug}/`, where the slug
names an organisation (ENT-198). `/workspace` still resolves and redirects
into the caller's organisation, because it is in bookmarks and is where a
sign-in with no destination lands.

Every authenticated route lives under `/o/{slug}/`, and that is a tenancy
boundary rather than a URL style. The organisation comes from the path on
every request and is never remembered between them: with it held in a cookie,
a consultant with three tabs open switches in one and silently changes what
the other two are showing. A slug the caller does not belong to is **404, not
403**, and never a redirect into one they do.

The legacy schema those pages read is not lost. Its 38 migrations are in git
history at `supabase/migrations/`, last present in commit `db0bf83`, and they
are the reference for what each rebuilt surface has to carry.

## Package manager

Always **Bun**, never npm, yarn or pnpm. Install with `bun install`, run
scripts with `bun run <script>`, reach for one-off binaries with `bunx`.

Bun is the package manager and task runner only. Next.js and the test suites
run on Node, so both toolchains have to be present.

**Never write `bun test`.** That invokes Bun's own test runner, which collects
none of this repo's Vitest suite and still exits 0, so it reads as a pass. The
command is `bun run test`.

## Adding a dependency

**Always add packages with the tool, never by hand-editing a manifest.**

```bash
bun add <pkg>              # or bun add -d <pkg> for a dev dependency
go get <module>            # then commit the go.mod and go.sum it writes
uv add <pkg>
cargo add <crate>
```

The reason is narrow and it applies to agents more than to people: **a
version written from memory is a guess.** Your knowledge has a cutoff, the
registry does not. Hand-writing gets you a version that is stale, or that
never existed, or that resolves but skips a transitive requirement the
manifest needed. The tool asks the registry, writes the correct constraint,
and updates the lockfile in the same step.

This is not hypothetical. `gen/go/go.mod` was hand-written in ENT-194 and
pinned `google.golang.org/protobuf` a patch behind and `connectrpc.com/connect`
a full minor behind, for no reason other than that those were the versions
the author remembered.

**When a sibling workspace already pins a version, still use the tool, and
pass that version explicitly:**

```bash
bun add pg@8.21.0
```

Matching the sibling matters more than being newest: two versions of the same
package in one lockfile is a bug waiting for a confusing afternoon. Passing
the version keeps that property without hand-editing anything.

**Lockfiles are committed.** Commit the manifest and the lockfile in the same
commit, or the next person's install resolves something different from yours.

**Generated code pins two things that must move together.** The codegen
plugin version in `buf.gen.yaml` and the runtime library version in
`gen/go/go.mod` are a matched pair: generated output expects a compatible
runtime. Bumping one means bumping both, re-running `buf generate`, and
committing the regenerated output. The CI drift check will catch you if you
forget the last step, but not if you forget the first.

## Commands

Run these from the repository root. The root scripts proxy into the
workspaces, so you rarely need to `cd`.

```bash
bun run dev              # the Next.js app
bun run format           # Prettier, writes
bun run format:check     # Prettier, what CI runs
bun run lint             # ESLint
bun run typecheck        # tsc, through the workspace so the version is pinned
bun run test:unit        # unit and component tests, no services needed
bun run test:e2e         # the sign-in round trip, needs the compose stack
bun run test:db          # database isolation suite, needs the compose stack
```

Use `bun run typecheck`, not `bunx tsc`. `bunx` resolves a compiler from the
registry when the binary is not linked at the root, which once meant CI ran a
newer TypeScript than the lockfile pins and failed on a stricter default.

For the Go workspace:

```bash
buf lint                 # proto lint
buf generate             # regenerate gen/, which is committed
golangci-lint run ./...  # lint and formatting, from within a module directory
golangci-lint fmt ./...  # gofmt and goimports, writes
go build ./... && go test ./...   # run from within a module directory
```

`buf` is pinned (see the CI workflow for the version). Generated code is
committed and CI fails on drift, so run `buf generate` and commit the result
whenever a proto changes.

`golangci-lint` is pinned too, and CI installs the same version. One
`.golangci.yml` at the root serves all three modules; the reasoning for each
enabled linter is in that file rather than here, because that is where
somebody disagreeing with one will be looking.

Both formatters are checked by CI and neither is checked by review. If a diff
is nothing but layout, run `bun run format` or `golangci-lint fmt` rather than
arguing about it. Markdown and YAML are deliberately outside Prettier's reach:
the prose here is hand-wrapped, and a formatter would flatten emphasis that
was put there on purpose.

## The local stack

```bash
docker compose -f deploy/compose.yaml up -d
```

One command from a clean checkout gives a healthy stack: Postgres with the
role split applied, migrations applied by a job container that must exit zero,
Zitadel serving OIDC discovery on `localhost:8300`, Redis, and a Caddy edge.
Tear down with `down -v`.

Mail is delivered to Mailpit, readable at `localhost:8025`, so registration and
verification complete rather than silently going nowhere. The seed configures
it, and the reason it sets credentials Mailpit does not want is written down in
`deploy/seed/seed.sh`: Zitadel refuses a provider that has none and reports it
as a config that does not exist.

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

## Working with LLMs: the OWASP Top 10 applies to code you write

Kindlast is AI-native, so the harness around the models is most of the
engineering, and the
[OWASP Top 10 for LLM Applications (2026)](https://github.com/GenAI-Security-Project/GenAI-LLM-Top10)
is a build constraint rather than a reading list. The README's
[Working with LLMs in this repository](./README.md#working-with-llms-in-this-repository)
section states what each entry means here and where the control lives. The
rules that bite most often when writing code:

- **The model may ask; only code refuses.** Authority is the scope
  interceptor, RLS and database constraints. Never make a prompt the thing
  that prevents an action.
- **Anything retrieved, fetched from a customer's tool, or typed by a user is
  data, never instruction.** Do not concatenate it into a system prompt.
- **An agent's tools are `core-api` RPCs and nothing else.** No filesystem
  writes, no shell, no database handle, no third-party credential in the
  Python service. Third-party data enters through the workers gateway or not
  at all.
- **A citation must resolve to a stored obligation or be refused.** Validate
  model output against a typed schema before it reaches `IngestService`.
- **Every run has budgets** (tokens, model calls, tool calls, recursion) and
  **leaves a record a customer can read** (what was asked, which skill and
  model, every tool call, every citation, cost, outcome).
- **Pin everything**: model, provider, framework, skill versions. Nothing is
  fetched at runtime.

If a change would weaken one of these, stop and say so in the PR rather than
working around it.

## When the API surface changes, update the Postman collection

**Any change to the API surface updates `postman/` in the same PR.** Not a
follow-up, not an issue for later: the same PR, so the collection is reviewed
against the change it describes.

The surface means anything a caller has to know:

- A proto change: a new RPC, a changed request or response, a changed
  `required_scope`.
- A new or renamed header a caller must send, such as the active-organisation
  header.
- A change to how a caller authenticates: the audience, the scopes, the grant,
  where a credential comes from.
- Any endpoint outside the proto surface, which is most of what the collection
  is for: the authorization server's routes, `web`'s redirect endpoints, and
  the webhook paths.

The reason is narrow and it is not tidiness. **The collection is the only
executable description of the halves that will never appear in a proto file**,
so for those it is the source of truth rather than a convenience. And it is
what someone reaches for at two in the morning during an incident, which is
exactly when a request that quietly stopped matching reality does the most
damage. A collection that is merely stale is worse than no collection, because
somebody will believe it.

Three things to get right, all of which have already gone wrong once:

1. **Update the description, not only the request.** A request whose body is
   right and whose description still says "does not work yet, see ENT-195"
   sends the next person to read a closed issue. Descriptions carry the
   reasoning that is not recoverable from a URL and a method, so they are the
   part that rots hardest.
2. **Record what you measured, not what the specification implies.** The
   client-credentials request carries two facts that cost an afternoon each:
   Zitadel's `client_id` for a service user is its username rather than its
   id, and the audience is the project id. Neither is guessable and neither is
   in a document.
3. **Do not reformat the JSON.** Editing it by loading and re-dumping through
   a library expands the compact arrays and escapes the section signs, and
   turns a six-line change into a two-hundred-line diff nobody can review.
   Edit the fields you mean to edit and leave the rest byte-identical.

Mark an endpoint that does not exist yet with the issue that will deliver it,
and keep it in the collection. Requests that document the plan are deliberate:
they are how the collection tracks where the build order is going rather than
only where it has been. When `buf` emits OpenAPI (design doc §23.2), the Core
API requests should be generated from the spec instead, and this rule then
applies to regenerating them.

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
| `test:e2e` | the compose stack | the sign-in round trip, in a real browser |
| `test:db` | the compose stack | tenant isolation and privileges |

The database suite **self-skips when its stack is unreachable**, so a green
local run does not prove it ran. CI boots the stack and fails loudly if it
cannot, which is what stops coverage disappearing silently. Keep that property
if you touch CI, and give any suite you add the same treatment: the Supabase
integration job was removed with its suite (ENT-200), and the property it
protected outlives it.

`test:e2e` is not yet a CI gate. It needs the compose stack and a browser, and
wiring that is its own piece of work.

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
