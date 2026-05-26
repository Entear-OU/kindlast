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
`

const DROP_SQL = /* sql */ `drop table if exists public._test_rls_fixture;`

describe.skipIf(!supabaseRunning)('RLS convention (sample scaffold test)', () => {
  let userA: { id: string; email: string; password: string }
  let userB: { id: string; email: string; password: string }
  let userARowId: string

  beforeAll(async () => {
    await applyFixtureSql(FIXTURE_SQL)

    const admin = createServiceRoleClient()
    userA = await signUpTestUser(admin)
    userB = await signUpTestUser(admin)

    const { data, error } = await admin
      .from('_test_rls_fixture')
      .insert([
        { user_id: userA.id, note: 'belongs to A' },
        { user_id: userB.id, note: 'belongs to B' },
      ])
      .select('id, user_id')
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
