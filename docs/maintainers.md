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

## Supabase

Migrations are pushed to the hosted project after a PR merges, never before,
and never applied ad-hoc through Studio or MCP. See
[`supabase/README.md`](../supabase/README.md).

You will need the project ref from the Supabase dashboard to run
`supabase link`. It is deliberately not committed to the repo.
