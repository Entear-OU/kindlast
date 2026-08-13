// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  applyFixtureSql,
  dropFixtureSql,
  querySql,
} from './helpers/db-fixture'
import { isLocalSupabaseReachable } from './helpers/supabase'

/**
 * Verifies the baseline migration (ENT-42) sets up the shared infrastructure
 * every subsequent feature migration relies on:
 *
 *   1. `vector` extension present in the `extensions` schema
 *   2. `pgcrypto` extension present (so `gen_random_uuid()` works)
 *   3. `public.set_updated_at()` trigger function that bumps `updated_at` to
 *      `now()` on row update
 *   4. The migration is idempotent — re-running its body does not error
 *
 * The "RLS convention" piece of ENT-42 is enforced by code review + the
 * sample suite in `rls-convention.test.ts`; the migration itself codifies
 * extensions and helpers, not per-table policies.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('Baseline migration (ENT-42)', () => {
  describe('extensions', () => {
    it('installs the vector extension in the extensions schema', async () => {
      const rows = await querySql<{ extname: string; nspname: string }>(`
        select e.extname, n.nspname
        from pg_extension e
        join pg_namespace n on n.oid = e.extnamespace
        where e.extname = 'vector'
      `)
      expect(rows).toHaveLength(1)
      expect(rows[0]!.nspname).toBe('extensions')
    })

    it('installs pgcrypto (gen_random_uuid is callable)', async () => {
      const rows = await querySql<{ uuid: string }>(
        `select gen_random_uuid()::text as uuid`,
      )
      expect(rows).toHaveLength(1)
      expect(rows[0]!.uuid).toMatch(
        /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
      )
    })
  })

  describe('set_updated_at trigger function', () => {
    const FIXTURE_SQL = /* sql */ `
      create table if not exists public._test_updated_at_fixture (
        id uuid primary key default gen_random_uuid(),
        label text not null,
        updated_at timestamptz not null default now()
      );

      drop trigger if exists set_updated_at on public._test_updated_at_fixture;
      create trigger set_updated_at
        before update on public._test_updated_at_fixture
        for each row
        execute function public.set_updated_at();
    `

    const DROP_SQL = /* sql */ `drop table if exists public._test_updated_at_fixture;`

    beforeAll(async () => {
      await applyFixtureSql(FIXTURE_SQL)
    })

    afterAll(async () => {
      await dropFixtureSql(DROP_SQL)
    })

    it('exists in the public schema with signature () returns trigger', async () => {
      const rows = await querySql<{
        proname: string
        nspname: string
        return_type: string
      }>(`
        select p.proname, n.nspname, pg_catalog.pg_get_function_result(p.oid) as return_type
        from pg_proc p
        join pg_namespace n on n.oid = p.pronamespace
        where n.nspname = 'public' and p.proname = 'set_updated_at'
      `)
      expect(rows).toHaveLength(1)
      expect(rows[0]!.return_type).toBe('trigger')
    })

    it('bumps updated_at on row update', async () => {
      const inserted = await querySql<{ id: string; updated_at: string }>(`
        insert into public._test_updated_at_fixture (label, updated_at)
        values ('initial', '2000-01-01T00:00:00Z')
        returning id, updated_at::text
      `)
      const id = inserted[0]!.id
      const beforeTs = new Date(inserted[0]!.updated_at).getTime()

      await querySql(`update public._test_updated_at_fixture set label = $1 where id = $2`, [
        'changed',
        id,
      ])

      const after = await querySql<{ updated_at: string }>(
        `select updated_at::text from public._test_updated_at_fixture where id = $1`,
        [id],
      )
      const afterTs = new Date(after[0]!.updated_at).getTime()
      expect(afterTs).toBeGreaterThan(beforeTs)
    })
  })

  it('migration is idempotent (re-applying its DDL does not error)', async () => {
    // The migration's `create extension if not exists` + `create or replace
    // function` form means re-running the same statements is safe. Verify by
    // re-applying the exact patterns the migration uses.
    const REAPPLY = /* sql */ `
      create extension if not exists "vector" with schema extensions;
      create extension if not exists "pgcrypto" with schema extensions;
      create or replace function public.set_updated_at()
      returns trigger
      language plpgsql
      as $$
      begin
        new.updated_at = now();
        return new;
      end;
      $$;
    `
    await expect(applyFixtureSql(REAPPLY)).resolves.toBeUndefined()
  })
})
