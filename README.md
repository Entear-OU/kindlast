# Kindlast

DPO Copilot for EU SMEs — an agentic compliance workspace currently being rebuilt per the [ENT-30 backend foundation epic](https://linear.app/entear/issue/ENT-30/epic-backend-foundation-and-cleanup).

> ⚠️ The previous one-shot GDPR/AI Act assessment flow has been retired (ENT-40). The agentic surface is in active development; the sections below describe only what's currently in the repo.

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Framework | Next.js 16 (App Router) |
| UI | shadcn/ui + Tailwind CSS v4 |
| Database | Supabase (Postgres + Auth + RLS + pgvector) |
| Validation | Zod |
| Testing | Vitest + React Testing Library |

## Getting Started

### Prerequisites

- Node.js 18+
- [pnpm](https://pnpm.io)
- [Supabase CLI](https://supabase.com/docs/guides/local-development/cli/getting-started) (`brew install supabase/tap/supabase` on macOS)
- Docker Desktop (required by `supabase start` for the local stack)

### Setup

```bash
# Install JS dependencies
pnpm install

# Copy environment variables
cp .env.example .env
# Fill in SUPABASE_URL + SUPABASE_PUBLISHABLE_KEY in .env

# Start the Next.js dev server
pnpm dev
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `SUPABASE_URL` | Supabase project URL |
| `SUPABASE_PUBLISHABLE_KEY` | Supabase publishable (anon) key |
| `SUPABASE_SECRET_KEY` | Supabase secret key (server-side only) |

## Supabase Local Workflow

All schema changes flow through **versioned migrations** committed to `supabase/migrations/`. Never apply DDL directly to the remote project — go through the CLI.

### One-time setup

Only needed if you are pushing migrations to a hosted Supabase project. Local
development against `supabase start` needs neither step.

```bash
# Authenticate the CLI with your Supabase account
supabase login

# Link this checkout to your remote project (once per machine)
supabase link --project-ref <your-project-ref>
```

Your project ref is the subdomain of your Supabase project URL: for
`https://abcdefghijklmnop.supabase.co` the ref is `abcdefghijklmnop`. You can
also read it off the project's General Settings page in the dashboard.

### Day-to-day

```bash
# Boot the local Postgres + Studio + Auth stack
supabase start

# Apply pending migrations to your local database
supabase db reset                    # nuke + replay all migrations from scratch
# or
supabase migration up                # apply only new migrations

# Create a new migration
supabase migration new <slug>        # produces supabase/migrations/<timestamp>_<slug>.sql

# Push committed migrations to the remote project (after PR merge)
supabase db push --dry-run           # always preview first
supabase db push                     # actually apply

# Stop the local stack
supabase stop
```

Local Studio runs at <http://localhost:54323>; local API at <http://localhost:54321>.

## Development

```bash
pnpm dev              # Next.js dev server
pnpm build            # production build
pnpm start            # production server
pnpm lint             # ESLint
pnpm test             # Vitest (run once)
pnpm test:watch       # Vitest watch mode
pnpm test:coverage    # with coverage report
```

## Project Structure

```
app/
├── (public)/           # Public-facing pages (landing, login)
└── auth/callback/      # OAuth callback

lib/
├── auth/               # Auth server actions
├── supabase/           # Supabase client/server/middleware helpers
└── utils.ts            # Shared utilities

components/
├── landing/            # Landing page sections
└── ui/                 # shadcn/ui base components

supabase/
├── config.toml         # Supabase CLI project config (committed)
└── migrations/         # Versioned schema migrations
```

## Testing

The project follows test-driven development (TDD) per [CLAUDE.md](./CLAUDE.md). Tests use Vitest + React Testing Library; database/RLS work is exercised against a local Supabase instance (see [ENT-43](https://linear.app/entear/issue/ENT-43/establish-supabase-backed-integration-test-scaffold)).

## License

Copyright (C) 2026 Entear OÜ.

Kindlast is free software, licensed under the [GNU Affero General Public License v3.0](./LICENSE) (AGPL-3.0-only). You may use, study, modify, and redistribute it under those terms.

The AGPL is a copyleft licence with one addition that matters for a hosted product: if you run a modified version of Kindlast as a network service, section 13 requires you to offer your users the corresponding source of your modifications. Running an unmodified copy, or using Kindlast internally without offering it to others over a network, carries no such obligation.

The licence covers the code in this repository. Two things are scoped separately:

- **Regulatory corpus.** The contents of `data/corpus/` are third-party regulatory texts under their own terms. See the sourcing notes alongside them.
- **Trademarks.** The Kindlast name and the logo assets in `docs/brand/` are not covered by the AGPL grant. You may not use them to imply endorsement by, or affiliation with, Entear OÜ.
