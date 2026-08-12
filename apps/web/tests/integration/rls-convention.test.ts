// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, dropFixtureSql } from './helpers/db-fixture'
import { isLocalSupabaseReachable } from './helpers/supabase'
import {
  createAnonClient,
  createServiceRoleClient,
  createUserClient,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser } from './helpers/test-user'

/**
 * Sample test for ENT-43.
 *
 * Verifies the RLS convention that ENT-42 will codify in the baseline migration:
 *   - `user_id uuid references auth.users(id) on delete cascade`
 *   - policy scoped to `auth.uid() = user_id`
 *
 * We don't depend on any product table — instead we apply a tests-only fixture
 * table (`_test_rls_fixture`) that follows the convention, then assert:
 *   1. An anonymous client cannot read any row.
 *   2. User A's authenticated client can read their own row.
 *   3. User A's authenticated client is denied User B's row.
 *
 * The whole suite skips cleanly when the local Supabase stack is not reachable
 * (so unit-only contributors aren't forced to boot Docker).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_SQL = /* sql */ `
  create table if not exists public._test_rls_fixture (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references auth.users(id) on delete cascade,
    note text not null
  );

  alter table public._test_rls_fixture enable row level security;

  drop policy if exists "_test_rls_fixture_select_own" on public._test_rls_fixture;
  create policy "_test_rls_fixture_select_own"
    on public._test_rls_fixture
    for select
    using (auth.uid() = user_id);

  -- Grant explicitly (ENT-159): the fixture is created at test time by
  -- \`postgres\`, whose default privileges in \`public\` hand anon/authenticated
  -- only Dxtm — no SELECT. Without this the policy above is dead code and the
  -- assertions fail with "permission denied for table" before RLS is consulted.
  grant select on public._test_rls_fixture to anon, authenticated;
  grant select, insert, update, delete on public._test_rls_fixture to service_role;
`

const DROP_SQL = /* sql */ `drop table if exists public._test_rls_fixture;`

/**
 * Wait for PostgREST to pick up a freshly-created table. The fixture table is
 * created over a direct pg connection, but the assertions query it through
 * PostgREST (supabase-js), which serves from a schema cache. Supabase reloads
 * that cache on DDL, but the reload is asynchronous — under parallel test load
 * the first query can still race ahead of it and get PGRST205 ("table not found
 * in schema cache"). Poll until the table resolves so the suite is deterministic.
 */
async function waitForPostgrestTable(
  admin: ReturnType<typeof createServiceRoleClient>,
  table: string,
  attempts = 50,
): Promise<void> {
  for (let i = 0; i < attempts; i++) {
    const { error } = await admin.from(table).select('*', { head: true, count: 'exact' })
    if (!error || error.code !== 'PGRST205') return
    await new Promise((resolve) => setTimeout(resolve, 100))
  }
  throw new Error(`waitForPostgrestTable: ${table} never appeared in the PostgREST schema cache`)
}

/**
 * Retry a PostgREST call while it reports PGRST205 ("table not in schema
 * cache"). The `head` probe above only proves *one* PostgREST worker has picked
 * up the schema reload; under a fast-booting stack a follow-up write can still
 * land on another worker mid-reload and 205. Polling the actual operation makes
 * the setup deterministic regardless of which worker serves it.
 */
async function withSchemaCacheRetry<T extends { error: { code?: string } | null }>(
  op: () => PromiseLike<T>,
  attempts = 30,
): Promise<T> {
  let result = await op()
  for (let i = 0; i < attempts && result.error?.code === 'PGRST205'; i++) {
    await new Promise((resolve) => setTimeout(resolve, 100))
    result = await op()
  }
  return result
}

describe.skipIf(!supabaseRunning)('RLS convention (sample scaffold test)', () => {
  let userA: { id: string; email: string; password: string }
  let userB: { id: string; email: string; password: string }
  let userARowId: string

  beforeAll(async () => {
    await applyFixtureSql(`${FIXTURE_SQL}\n  notify pgrst, 'reload schema';`)

    const admin = createServiceRoleClient()
    // Don't let the assertions race PostgREST's schema-cache reload.
    await waitForPostgrestTable(admin, '_test_rls_fixture')
    userA = await signUpTestUser(admin)
    userB = await signUpTestUser(admin)

    const { data, error } = await withSchemaCacheRetry(() =>
      admin
        .from('_test_rls_fixture')
        .insert([
          { user_id: userA.id, note: 'belongs to A' },
          { user_id: userB.id, note: 'belongs to B' },
        ])
        .select('id, user_id'),
    )
    if (error) throw error
    userARowId = data!.find((row) => row.user_id === userA.id)!.id
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (userA?.id) await deleteTestUser(admin, userA.id)
    if (userB?.id) await deleteTestUser(admin, userB.id)
    await dropFixtureSql(DROP_SQL)
  })

  it('anonymous client cannot read fixture rows', async () => {
    const anon = createAnonClient()
    const { data, error } = await anon.from('_test_rls_fixture').select('*')
    // RLS hides rows without surfacing an error; should return an empty array.
    expect(error).toBeNull()
    expect(data).toEqual([])
  })

  it('authenticated user can read their own row', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data, error } = await client
      .from('_test_rls_fixture')
      .select('id, note')
      .eq('id', userARowId)
      .single()
    expect(error).toBeNull()
    expect(data).toMatchObject({ note: 'belongs to A' })
  })

  it("authenticated user is denied another user's row", async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data, error } = await client
      .from('_test_rls_fixture')
      .select('id, user_id')
      .eq('user_id', userB.id)
    expect(error).toBeNull()
    expect(data).toEqual([])
  })
})
