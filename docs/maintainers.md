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
connect for anyone without workspace access. Add it to your personal config
instead, either in `~/.claude.json` or in a git-ignored `.mcp.local.json`:

```json
{
  "mcpServers": {
    "linear-server": {
      "type": "http",
      "url": "https://mcp.linear.app/mcp"
    }
  }
}
```

## Supabase

Migrations are pushed to the hosted project after a PR merges, never before,
and never applied ad-hoc through Studio or MCP. See
[`supabase/README.md`](../supabase/README.md).

You will need the project ref from the Supabase dashboard to run
`supabase link`. It is deliberately not committed to the repo.
