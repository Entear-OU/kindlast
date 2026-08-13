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
 * ENT-47 — Persist onboarding conversation transcript.
 *
 * Asserts the contract for two user-owned tables introduced in
 * `<ts>_onboarding_transcript.sql`:
 *
 *   - `onboarding_sessions(id, user_id, status, started_at, completed_at, ...)`
 *   - `onboarding_messages(id, session_id, user_id, role, content, ordering, ...)`
 *
 * Coverage:
 *   1. Status + role check constraints.
 *   2. `(session_id, ordering)` unique-per-session.
 *   3. RLS denies anonymous reads.
 *   4. RLS scopes each user to their own rows (read + write).
 *   5. Re-interview: a completed session and its messages are preserved verbatim
 *      when a new `in_progress` session is started by the same user.
 *
 * Suite skips when the local Supabase stack is unreachable — same pattern as
 * `rls-convention.test.ts` — so unit-only contributors stay unblocked.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('onboarding transcript persistence (ENT-47)', () => {
  let userA: TestUser
  let userB: TestUser

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    userA = await signUpTestUser(admin)
    userB = await signUpTestUser(admin)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // Cascade through onboarding_sessions / onboarding_messages on user delete.
    if (userA?.id) await deleteTestUser(admin, userA.id)
    if (userB?.id) await deleteTestUser(admin, userB.id)
  })

  it('inserts a session with default status=in_progress', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data, error } = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id, status, started_at, completed_at')
      .single()

    expect(error).toBeNull()
    expect(data?.status).toBe('in_progress')
    expect(data?.started_at).toBeTruthy()
    expect(data?.completed_at).toBeNull()
  })

  it('rejects an invalid status value', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { error } = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id, status: 'nonsense' })
    expect(error).not.toBeNull()
    expect(error?.message.toLowerCase()).toMatch(/status/)
  })

  it('rejects an invalid message role', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data: session } = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id')
      .single()

    const { error } = await client.from('onboarding_messages').insert({
      session_id: session!.id,
      user_id: userA.id,
      role: 'system',
      content: 'should fail',
      ordering: 0,
    })
    expect(error).not.toBeNull()
    expect(error?.message.toLowerCase()).toMatch(/role/)
  })

  it('enforces unique ordering within a session', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data: session } = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id')
      .single()

    const first = await client.from('onboarding_messages').insert({
      session_id: session!.id,
      user_id: userA.id,
      role: 'user',
      content: 'first',
      ordering: 0,
    })
    expect(first.error).toBeNull()

    const dup = await client.from('onboarding_messages').insert({
      session_id: session!.id,
      user_id: userA.id,
      role: 'assistant',
      content: 'second collides',
      ordering: 0,
    })
    expect(dup.error).not.toBeNull()
    expect(dup.error?.message.toLowerCase()).toMatch(/duplicate|unique/)
  })

  it('returns messages ordered by `ordering` ascending', async () => {
    const client = await createUserClient(userA.email, userA.password)
    const { data: session } = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id')
      .single()

    // Insert out of order to confirm `order(ordering)` actually sorts.
    await client.from('onboarding_messages').insert([
      { session_id: session!.id, user_id: userA.id, role: 'user', content: 'second', ordering: 1 },
      { session_id: session!.id, user_id: userA.id, role: 'assistant', content: 'first', ordering: 0 },
    ])

    const { data } = await client
      .from('onboarding_messages')
      .select('role, content, ordering')
      .eq('session_id', session!.id)
      .order('ordering', { ascending: true })

    expect(data).toEqual([
      { role: 'assistant', content: 'first', ordering: 0 },
      { role: 'user', content: 'second', ordering: 1 },
    ])
  })

  it('denies anonymous reads of sessions and messages', async () => {
    const anon = createAnonClient()

    const sessions = await anon.from('onboarding_sessions').select('*')
    expect(sessions.error).toBeNull()
    expect(sessions.data).toEqual([])

    const messages = await anon.from('onboarding_messages').select('*')
    expect(messages.error).toBeNull()
    expect(messages.data).toEqual([])
  })

  it("denies a user reading another user's session", async () => {
    const a = await createUserClient(userA.email, userA.password)
    const { data: sessionA } = await a
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id')
      .single()

    const b = await createUserClient(userB.email, userB.password)
    const { data, error } = await b
      .from('onboarding_sessions')
      .select('id')
      .eq('id', sessionA!.id)
    expect(error).toBeNull()
    expect(data).toEqual([])
  })

  it("denies a user reading another user's messages", async () => {
    const a = await createUserClient(userA.email, userA.password)
    const { data: sessionA } = await a
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id')
      .single()
    await a.from('onboarding_messages').insert({
      session_id: sessionA!.id,
      user_id: userA.id,
      role: 'user',
      content: 'private',
      ordering: 0,
    })

    const b = await createUserClient(userB.email, userB.password)
    const { data, error } = await b
      .from('onboarding_messages')
      .select('content')
      .eq('session_id', sessionA!.id)
    expect(error).toBeNull()
    expect(data).toEqual([])
  })

  it("denies a user inserting a session with another user's user_id", async () => {
    const b = await createUserClient(userB.email, userB.password)
    const { error } = await b
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
    // RLS insert policy with-check rejects the row before it lands.
    expect(error).not.toBeNull()
  })

  it('preserves a completed session and its messages when a re-interview starts', async () => {
    const client = await createUserClient(userA.email, userA.password)

    // 1. Complete a session with two messages.
    const { data: original } = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id')
      .single()

    await client.from('onboarding_messages').insert([
      {
        session_id: original!.id,
        user_id: userA.id,
        role: 'assistant',
        content: 'What does your product do?',
        ordering: 0,
      },
      {
        session_id: original!.id,
        user_id: userA.id,
        role: 'user',
        content: 'We build SME accounting tools.',
        ordering: 1,
      },
    ])

    const completed = await client
      .from('onboarding_sessions')
      .update({ status: 'completed', completed_at: new Date().toISOString() })
      .eq('id', original!.id)
      .select('id, status')
      .single()
    expect(completed.error).toBeNull()
    expect(completed.data?.status).toBe('completed')

    // 2. Start a new session (the "re-interview").
    const reinterview = await client
      .from('onboarding_sessions')
      .insert({ user_id: userA.id })
      .select('id, status')
      .single()
    expect(reinterview.error).toBeNull()
    expect(reinterview.data?.status).toBe('in_progress')
    expect(reinterview.data?.id).not.toBe(original!.id)

    // 3. The original is unchanged.
    const reread = await client
      .from('onboarding_sessions')
      .select('status')
      .eq('id', original!.id)
      .single()
    expect(reread.data?.status).toBe('completed')

    const originalMessages = await client
      .from('onboarding_messages')
      .select('role, content, ordering')
      .eq('session_id', original!.id)
      .order('ordering', { ascending: true })
    expect(originalMessages.data).toEqual([
      { role: 'assistant', content: 'What does your product do?', ordering: 0 },
      { role: 'user', content: 'We build SME accounting tools.', ordering: 1 },
    ])
  })
})
