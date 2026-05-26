# `supabase/`

Versioned schema for the Kindlast Supabase project (ref `kstdusioclkgffyfpesu`).
All DDL flows through this directory — never apply migrations ad-hoc via MCP
`apply_migration` or the Studio SQL editor on the remote.

## Layout

```
supabase/
├── config.toml             # Supabase CLI project config (postgres v17, extensions schema in search_path)
├── .gitignore              # Ignores .branches, .temp, dotenvx-style env files
├── migrations/             # Timestamped, versioned schema changes
│   └── 20260526053805_baseline.sql
└── README.md               # ← you are here
```

## Baseline migration (`20260526053805_baseline.sql`)

Authored in [ENT-42](https://linear.app/entear/issue/ENT-42/author-baseline-migration-extensions-rls-conventions-helper-triggers). Defines the shared infrastructure every subsequent feature migration leans on:

### 1. Extensions

| Extension  | Schema       | Why |
|------------|--------------|-----|
| `vector`   | `extensions` | Embeddings for the RAG / compliance Q&A surface (PRD §05) |
| `pgcrypto` | `extensions` | `gen_random_uuid()` default for primary keys |

Both live in the `extensions` schema, which `config.toml`'s `extra_search_path` already includes, so feature migrations reference them unqualified.

### 2. `public.set_updated_at()` trigger function

Reusable trigger that sets `updated_at = now()` on every row update. Attach to any user-facing table that tracks `updated_at`:

```sql
create trigger set_updated_at
  before update on public.<table_name>
  for each row execute function public.set_updated_at();
```

### 3. RLS convention

Every user-owned table **must**:

- Include `user_id uuid not null references auth.users(id) on delete cascade`.
- `alter table <name> enable row level security;`
- Define explicit per-operation policies scoped to `auth.uid() = user_id`:

```sql
create policy "<name>_select_own" on public.<name>
  for select using (auth.uid() = user_id);
create policy "<name>_insert_own" on public.<name>
  for insert with check (auth.uid() = user_id);
create policy "<name>_update_own" on public.<name>
  for update using (auth.uid() = user_id) with check (auth.uid() = user_id);
create policy "<name>_delete_own" on public.<name>
  for delete using (auth.uid() = user_id);
```

This convention is enforced by code review and by [`tests/integration/rls-convention.test.ts`](../tests/integration/rls-convention.test.ts), which exercises the pattern against a tests-only fixture table. A wrapper function (`apply_user_owned_rls(table_name)`) was considered and rejected — dynamic SQL-generated policies are harder to audit and obscure the security surface.

## Authoring a new migration

```bash
# Generate a timestamped file
supabase migration new <slug>

# Edit supabase/migrations/<timestamp>_<slug>.sql

# Apply locally to verify (nukes + replays all migrations)
supabase db reset

# Run the integration test suite to confirm the new migration plus the
# RLS convention still hold
pnpm test

# Once reviewed & merged, push to remote
supabase db push --dry-run    # always preview first
supabase db push
```

## Local stack lifecycle

```bash
supabase start    # boot Postgres + Studio + Auth + PostgREST (Docker required)
supabase stop     # tear down
supabase status   # show URLs + keys for the running stack
```

Local Studio: <http://localhost:54323> · API: <http://localhost:54321> · Postgres: `127.0.0.1:54322`
