// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  createAnonClient,
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-81 — New signups receive a Free subscription on creation.
 *
 * Exercises `<ts>_subscriptions.sql` against the local Supabase stack:
 *
 *   1. The `auth.users` insert trigger drops a Free/active subscription row for
 *      every new user — so every tier check has a row to read.
 *   2. The row defaults to plan='free', status='active'.
 *   3. RLS: a user reads only their own subscription; anon reads nothing.
 *   4. RLS: users cannot write their subscription (insert/update/delete) — only
 *      the service role can (the trigger + Stripe webhook run as service role).
 *   5. `(user_id)` is unique — at most one subscription per user.
 *
 * Skips when the local Supabase stack is unreachable — same pattern as sibling
 * integration suites.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('subscriptions on signup (ENT-81)', () => {
  let userA: TestUser
  let userB: TestUser

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    userA = await signUpTestUser(admin)
    userB = await signUpTestUser(admin)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (userA?.id) await deleteTestUser(admin, userA.id)
    if (userB?.id) await deleteTestUser(admin, userB.id)
  })

  it('creates a Free/active subscription row for a newly-created user', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data, error } = await client
      .from('subscriptions')
      .select('user_id, plan, status')
      .single()

    expect(error).toBeNull()
    expect(data?.user_id).toBe(userA.id)
    expect(data?.plan).toBe('free')
    expect(data?.status).toBe('active')
  })

  it('scopes each user to their own subscription (RLS select-own)', async () => {
    const clientA = await createUserClient(userA.email, userA.password)
    const { data } = await clientA.from('subscriptions').select('user_id')
    // A user sees exactly one row — their own — never another tenant's.
    expect(data).toHaveLength(1)
    expect(data?.[0]?.user_id).toBe(userA.id)
  })

  it('denies anonymous reads of subscriptions', async () => {
    const anon = createAnonClient()
    const { data, error } = await anon.from('subscriptions').select('*')
    expect(error).toBeNull()
    expect(data).toEqual([])
  })

  it('denies a user updating their own subscription (only service role writes)', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { error } = await client
      .from('subscriptions')
      .update({ plan: 'pro' })
      .eq('user_id', userA.id)
      .select()

    // No update policy → the row is invisible to the writer; the update affects
    // zero rows. Re-read via service role confirms the plan never changed.
    expect(error).toBeNull()
    const admin = createServiceRoleClient()
    const { data } = await admin
      .from('subscriptions')
      .select('plan')
      .eq('user_id', userA.id)
      .single()
    expect(data?.plan).toBe('free')
  })

  it('denies a user inserting a second subscription for themselves', async () => {
    const client = await createUserClient(userB.email, userB.password)
    const { error } = await client
      .from('subscriptions')
      .insert({ user_id: userB.id, plan: 'pro', status: 'active' })
    // Blocked by RLS (no insert policy for users).
    expect(error).not.toBeNull()
  })

  it('enforces one subscription per user (unique user_id)', async () => {
    const admin = createServiceRoleClient()
    const { error } = await admin
      .from('subscriptions')
      .insert({ user_id: userA.id, plan: 'pro', status: 'active' })
    expect(error).not.toBeNull()
    expect(error?.message.toLowerCase()).toMatch(/duplicate|unique|user_id/)
  })

  it('rejects an invalid plan value at the DB layer', async () => {
    const admin = createServiceRoleClient()
    const newUser = await signUpTestUser(admin)
    try {
      const { error } = await admin
        .from('subscriptions')
        .update({ plan: 'enterprise' })
        .eq('user_id', newUser.id)
      expect(error).not.toBeNull()
      expect(error?.message.toLowerCase()).toMatch(/plan|check/)
    } finally {
      await deleteTestUser(admin, newUser.id)
    }
  })
})
