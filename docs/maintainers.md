# Maintainer workflow

Internal process notes for Kindlast maintainers at Entear OÜ. Everything here
depends on access to the Linear workspace, so it does not apply to external
contributors. If you are contributing from outside, see
[`CONTRIBUTING.md`](../CONTRIBUTING.md) instead. Nothing in this document is a
requirement for having a PR accepted.

## Issue tracking

Work is tracked in Linear under the Entear team (`ENT`).

- One branch and one PR per Linear sub-issue. Never bundle multiple sub-issues
  into a single branch.
- Branch from `main` using Linear's generated `gitBranchName`, e.g.
  `dev/ent-40-delete-legacy-assessment-flow-code`. Copying it verbatim is what
  makes Linear attach the branch and PR to the issue automatically.
- One PR per sub-issue, merged sequentially once its acceptance criteria pass.
- Epic-level issues (e.g. ENT-30) do not get their own branch. They exist only
  to group sub-issues.
- PR titles carry the Linear issue ID as a bracketed prefix, e.g.
  `[ENT-40] chore: remove legacy assessment-flow code`. The body links back to
  the issue URL so Linear auto-links the PR.

Referring to `ENT-XX` in code comments is fine and often useful: it records why
a piece of code exists. Keep those references out of the README and other
front-door documentation, where they are noise to anyone who cannot open them.

## Auto-merge

The `auto-merge` label opts a PR into being squash-merged automatically once CI
passes, which is what makes stacked branch chains practical. See
`.github/workflows/auto-merge.yml`.

It never merges a PR from a fork, and applying a label needs triage access, so
the label cannot be self-applied by an outside contributor.

The header of that workflow says native auto-merge was unavailable because
"this repo's plan doesn't offer branch protection". **That is no longer true**:
the repository is public, so rulesets are free, and two are active (below).
Replacing the custom workflow with `gh pr merge --auto` is now possible and is
worth doing once required status checks exist, since native auto-merge needs a
required check to gate on.

## Branch protection

Two rulesets apply to the default branch. Read them with:

```bash
gh api repos/Entear-OU/kindlast/rules/branches/main --jq '[.[] | .type]'
```

| Ruleset | Rules | Why |
|---|---|---|
| `Copilot review for default branch` | `deletion`, `non_fast_forward`, `copilot_code_review` | Predates this section |
| `main` | `deletion`, `non_fast_forward`, `pull_request` | Added 2026-08-19 |

The overlap on `deletion` and `non_fast_forward` is deliberate rather than
sloppy: the two rulesets have separate lifetimes, and the day somebody deletes
the Copilot one to stop the review requests, main should not silently lose its
force-push protection with it.

**The rule that changed something is `pull_request`.** Force-push and deletion
were already blocked. Direct pushes to main were not, so `AGENTS.md`'s "never
push to `main`" was a convention every agent was trusted to follow. It is now
enforced by the server. Required approvals are **zero**, which is honest for a
single-maintainer repository: the gate is that a change arrives as a pull
request with its conversations resolved, not that a second human signed it off.

**Nobody can bypass, including the repository owner** (`bypass_actors` is
empty). That is the point, and it has a cost: see below.

### Required status checks are deliberately absent

They are the obvious next rule and adding them today would freeze the
repository. Every CI job currently fails in three seconds with zero steps and
the annotation *"The job was not started because recent account payments have
failed or your spending limit needs to be increased"*. A required check that
can never pass is a branch nothing can ever merge into.

**Add them when billing is restored.** The job names, which are what the API
wants and are easy to get subtly wrong:

```
Lint, typecheck & unit tests
Proto codegen & Go tests
Compose stack & isolation tests
Intelligence (Python)
Intelligence end to end
Local model service
```

Consider whether all six should gate. `Local model service` and
`Intelligence end to end` pull a multi-gigabyte model and are the slowest by
far, so requiring them makes every merge wait on them.

### Lifting protection for a history rewrite

`non_fast_forward` blocks the force-push that ENT-174's secret purge needs, and
there is no bypass actor, so the rewrite cannot simply be pushed. The procedure:

1. Set **both** rulesets to `"enforcement": "disabled"`. One is not enough; both
   carry `non_fast_forward`.
2. Do the rewrite and force-push.
3. Set both back to `"active"` **in the same sitting**, before anything else.

```bash
gh api repos/Entear-OU/kindlast/rulesets/21024087 --method PUT -f enforcement=disabled
gh api repos/Entear-OU/kindlast/rulesets/15442181 --method PUT -f enforcement=disabled
# ... rewrite, force-push ...
gh api repos/Entear-OU/kindlast/rulesets/21024087 --method PUT -f enforcement=active
gh api repos/Entear-OU/kindlast/rulesets/15442181 --method PUT -f enforcement=active
```

Anyone who has cloned or forked will need to re-clone after a rewrite, which for
a public repository is a real cost and part of why ENT-174 is worth deciding on
rather than drifting.

## MCP servers

`.mcp.json` at the repo root is committed and auto-loaded by Claude Code. It
carries only servers that work for anyone who clones the repo: `playwright` and
`shadcn`.

The Linear MCP server is deliberately **not** in there, because it fails to
connect for anyone without workspace access. The same goes for any other server
needing private credentials. Add those to your personal config instead:

```bash
claude mcp add --scope user --transport http linear-server https://mcp.linear.app/mcp
```

Claude Code reads MCP servers from two files, and the scope decides which:

| Scope | Lives in | Applies to |
|---|---|---|
| `project` | `.mcp.json` at the repo root, committed | everyone who clones the repo |
| `user` | `~/.claude.json` | you, in every project |
| `local` | `~/.claude.json`, under this project's entry | you, in this project only |

**There is no `.mcp.local.json`.** An earlier version of this page suggested
one; Claude Code never reads that file, so a server configured there simply
never appears, with no error to explain why. `--scope user` is the safer of the
two personal scopes for an account-level server like Linear, since it applies
wherever you are rather than being tied to one project entry.

Check what is loaded, and from where, with `claude mcp list` and
`claude mcp get <name>`.

## Contributor licence agreement

External contributions require acceptance of [`CLA.md`](../CLA.md) before they
can be merged. The reason is narrow and specific: without it, contributors
retain copyright on their patches, and Entear cannot offer a commercial licence
exception to organisations that cannot comply with the AGPL's network copyleft.
Retrofitting a CLA later means tracking down every past contributor and getting
each to agree, which in practice means the option is gone.

Signatures are collected by CLA Assistant on the pull request. Setting that up
needs three things, none of which are in this repo yet:

1. A workflow at `.github/workflows/cla.yml` running
   `contributor-assistant/github-action`.
2. A `PERSONAL_ACCESS_TOKEN` repository secret with `repo` scope, used to write
   the signature file.
3. A store for signatures. Either a `signatures/` path in this repo or, tidier,
   a separate private repository so the signature file does not churn the main
   history.

Maintainers and anyone with write access are exempt from the check, so this
does not add friction to internal work.

## Run the Go suite with `-p 1`

```bash
cd apps/core-api && go test -p 1 ./...
```

Not a preference, and not slowness for its own sake. `go test ./...` runs
different test **packages** in parallel (`-p` defaults to GOMAXPROCS), and
every package in `apps/core-api` connects to the same `kindlast` database in
the compose stack. Two packages writing the same table are two processes
sharing state with no transaction between them.

That is not hypothetical: it made the corpus drift guard intermittently red
for a week (ENT-252). `internal/server/interceptor` seeds an obligation citing
the real GDPR CELEX and deletes it again, while `internal/store/postgres` is
counting obligations either side of its own ingest, and the count moves. It
cost three separate people an investigation each in one day, and every one of
them correctly concluded "not mine" without finding the mechanism.

The drift guard itself was narrowed to measure only the rows it ingested, so
that particular collision cannot come back. `-p 1` is the general case: the
next one will be between two tables nobody has thought about yet. CI passes
the flag on the stack-backed job for the same reason.

A related failure that `-p 1` does **not** fix: two worktrees running suites
against one shared compose stack. That is the next section, and the two are
siblings rather than alternatives. `-p 1` stops one worktree's packages
colliding inside one database. A stack per worktree stops two worktrees
colliding at all. Neither helps with the other's problem, and a machine running
several branches at once wants both.

## One compose stack per worktree

```bash
./scripts/stack-env.sh --write      # once per worktree
docker compose -f deploy/compose.yaml up -d

eval "$(./scripts/stack-env.sh)"    # once per shell, for the suites
bun run test:db
cd apps/core-api && go test -p 1 ./...
```

`--write` produces `deploy/.env`, which docker compose reads on its own because
it sits beside the compose file. From then on every `docker compose -f
deploy/compose.yaml ...` in that worktree addresses that worktree's stack, with
no flags and nothing to remember. The `eval` is the other half: the test suites
do not read `deploy/.env`, so they need the same values in the shell, plus the
`PG_*_URL` and `REDIS_ADDR` names they already look for.

`--summary` prints the block in a readable form when you want to know where
Zitadel is, and `docker compose -f deploy/compose.yaml down` still tears down
the right stack because the project name is in `deploy/.env` too.

**Nothing changes in the main checkout.** It gets project `kindlast`, postgres
on 5433, Zitadel on 8300 and the edge on 8000, exactly as `README.md` and
`docs/self-hosting.md` describe. The derivation activates for linked worktrees
only, so a self-hoster and anybody with a single clone never meets it. Running
`--write` there is harmless and writes those same defaults.

The project name and the ports come from a SHA-256 of the worktree's absolute
path, which makes them stable: the same worktree gets the same block every
time, so a stack outlives the shell that started it and `down` finds what `up`
created. A hash is a probability rather than a guarantee, so `--write` also
asks docker whether any derived port is already published by another project
and says so; `KINDLAST_STACK_SLOT=<1..1024>` moves out of the way.

The model is the one thing shared on purpose. `deploy/models` is a bind mount
rather than a named volume so `down -v` does not cost a 2.7 GB re-download, and
`scripts/stack-env.sh` points every worktree at the **main checkout's** copy
through `KINDLAST_MODEL_DIR`. The weights are downloaded once per machine; only
the llama-server process is per project, and the `model` profile is opt-in, so
a second worktree that does not need inference simply does not start it.
Sharing the server itself would mean joining an external docker network and
coupling two stacks' lifecycles, where tearing the first one down breaks the
second.

Why this is a mechanism rather than a rule people follow. In one day on
2026-08-18, five worktrees sharing one Postgres produced: a migration from an
unmerged branch applied under three sibling branches running main's Go code,
with three people separately reaching the same correct "not mine"; main going
red because main's test and the shared schema disagreed over a column only an
unmerged branch had added; a migration hand-applied through `psql` because
goose refused the gap, leaving `goose_db_version` unaware of objects that
existed; and a grant committed on the shared stack to prove a test could go
red, because a rolled-back transaction is invisible to other sessions. None of
those are careless. They are what sharing one database does.

CI is unaffected and deliberately so: a job is one checkout with one stack, it
sets no `COMPOSE_PROJECT_NAME`, and it gets the defaults.

## Migrations

Schema changes are goose migrations in `db/migrations/`, applied by the job
container in `deploy/compose.yaml`. The job must exit zero or the stack is not
considered up, which is what stops a half-migrated database from looking
healthy.

Migrations are applied to a deployed environment after a PR merges, never
before, and never by hand against a live database.

The rule that matters more than the mechanics: a new tenant table needs
`org_id`, `FORCE ROW LEVEL SECURITY`, and policies in the two-GUC form. Do not
take that on trust when reviewing. `bun run test:db` asserts it over
`pg_class`, and it will fail the PR rather than let an unprotected table
through.

Supabase was removed in ENT-200. Its 38 migrations are in git history at
`supabase/migrations/`, last present in commit `db0bf83`, and they are the
reference for the surfaces still to be rebuilt on core-api.

## Cutting a release

The policy is [`docs/versioning.md`](./versioning.md), which is written for
everyone. What is maintainer-only is that only a maintainer does this, and the
order matters:

```bash
git switch main && git pull
# 1. Move the changelog's Unreleased entries under the new heading, with a date
#    and the upgrade note if there is one, and commit that first.
bun run version:set 0.2.0        # VERSION + both manifests, checked
git commit -am "chore(release): 0.2.0"
git push
git tag -a v0.2.0 -m "v0.2.0" && git push origin v0.2.0
```

The changelog is edited before the bump rather than after, so the release
commit is the version change and nothing else, and a reader can see at a glance
that the tag and the notes were cut from the same tree.

**Nothing is published.** No registry, no image push, no GitHub release
artefact. A tag is the whole release today, because self-hosters build from the
repository. When images start being published, the push belongs in a workflow
triggered by the tag rather than in this list, so that what shipped is
reproducible from what was tagged.
