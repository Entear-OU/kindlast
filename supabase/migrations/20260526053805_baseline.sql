-- Baseline migration (ENT-42)
--
-- Establishes shared infrastructure every subsequent feature migration leans
-- on. Intentionally narrow: extensions + the `updated_at` trigger helper.
-- The RLS convention (see below) is enforced by code review, not a helper
-- function — explicit per-table policies stay auditable.
--
-- Idempotent: every statement uses `if not exists` or `or replace`, so the
-- migration can be re-applied during local development without error.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Extensions ──────────────────────────────────────────────────────────────
--
-- Supabase convention: install extensions in the `extensions` schema, which
-- the project's API search_path already includes (see supabase/config.toml).

create extension if not exists "vector"   with schema extensions;
create extension if not exists "pgcrypto" with schema extensions;

-- 2. updated_at trigger helper ───────────────────────────────────────────────
--
-- Every user-facing table that tracks `updated_at` attaches this trigger:
--
--     create trigger set_updated_at
--       before update on public.<table_name>
--       for each row execute function public.set_updated_at();
--
-- Keeping the function in `public` (not `extensions`) so feature migrations
-- can reference it without qualifying the schema.

create or replace function public.set_updated_at()
returns trigger
language plpgsql
as $$
begin
  new.updated_at = now();
  return new;
end;
$$;

-- 3. RLS convention (documented, not codified) ───────────────────────────────
--
-- Every user-owned table MUST:
--
--   a. Include `user_id uuid not null references auth.users(id) on delete cascade`.
--   b. `alter table <name> enable row level security;`
--   c. Define explicit per-operation policies scoped to `auth.uid() = user_id`:
--
--        create policy "<name>_select_own" on public.<name>
--          for select using (auth.uid() = user_id);
--        create policy "<name>_insert_own" on public.<name>
--          for insert with check (auth.uid() = user_id);
--        create policy "<name>_update_own" on public.<name>
--          for update using (auth.uid() = user_id)
--                     with check (auth.uid() = user_id);
--        create policy "<name>_delete_own" on public.<name>
--          for delete using (auth.uid() = user_id);
--
-- A wrapper function (`apply_user_owned_rls(table_name)`) was considered and
-- rejected: dynamic SQL-generated policies are harder to audit and hide the
-- security surface. Tests/integration/rls-convention.test.ts asserts the
-- convention end-to-end against a tests-only fixture table.
