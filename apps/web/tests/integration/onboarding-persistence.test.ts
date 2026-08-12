// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  appendMessages,
  getOrCreateActiveSession,
  loadTranscript,
  textFromUIMessage,
  uiMessageFromRow,
} from '@/lib/onboarding/persistence'

import {
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-44 — Onboarding persistence helpers.
 *
 * Exercises `lib/onboarding/persistence.ts` end-to-end against the local
 * Supabase stack and the tables created by ENT-47. Run through an
 * authenticated user client so RLS stays in the loop.
 */

const supabaseRunning = await isLocalSupabaseReachable()

describe.skipIf(!supabaseRunning)('onboarding persistence helpers (ENT-44)', () => {
  let user: TestUser

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('getOrCreateActiveSession creates a new session on first call', async () => {
    const client = await createUserClient(user.email, user.password)
    const id = await getOrCreateActiveSession(client, user.id)
    expect(id).toMatch(/^[0-9a-f-]{36}$/i)
  })

  it('getOrCreateActiveSession returns the same in_progress session on repeat calls', async () => {
    const client = await createUserClient(user.email, user.password)
    const first = await getOrCreateActiveSession(client, user.id)
    const second = await getOrCreateActiveSession(client, user.id)
    expect(second).toBe(first)
  })

  it('getOrCreateActiveSession opens a fresh session after the previous one completed', async () => {
    const client = await createUserClient(user.email, user.password)
    const previous = await getOrCreateActiveSession(client, user.id)

    const { error: completeErr } = await client
      .from('onboarding_sessions')
      .update({ status: 'completed', completed_at: new Date().toISOString() })
      .eq('id', previous)
    expect(completeErr).toBeNull()

    const next = await getOrCreateActiveSession(client, user.id)
    expect(next).not.toBe(previous)
  })

  it('appendMessages assigns sequential ordering starting from 0 for an empty session', async () => {
    const client = await createUserClient(user.email, user.password)
    const sessionId = await getOrCreateActiveSession(client, user.id)

    const rows = await appendMessages(client, {
      sessionId,
      userId: user.id,
      messages: [
        { role: 'assistant', content: 'first prompt' },
        { role: 'user', content: 'first answer' },
      ],
    })

    expect(rows.map((r) => [r.role, r.content, r.ordering])).toEqual([
      ['assistant', 'first prompt', 0],
      ['user', 'first answer', 1],
    ])
  })

  it('appendMessages continues numbering after existing messages', async () => {
    const client = await createUserClient(user.email, user.password)
    const sessionId = await getOrCreateActiveSession(client, user.id)

    await appendMessages(client, {
      sessionId,
      userId: user.id,
      messages: [{ role: 'assistant', content: 'seed' }],
    })

    const next = await appendMessages(client, {
      sessionId,
      userId: user.id,
      messages: [
        { role: 'user', content: 'answer 1' },
        { role: 'assistant', content: 'follow-up' },
      ],
    })

    const transcript = await loadTranscript(client, sessionId)
    const orderings = transcript.map((r) => r.ordering)
    // Strictly increasing, contiguous.
    expect(orderings).toEqual([...orderings].sort((a, b) => a - b))
    expect(new Set(orderings).size).toBe(orderings.length)
    // Newly inserted rows extend the tail.
    expect(next.at(-1)?.ordering).toBe(orderings.at(-1))
  })

  it('appendMessages with an empty array is a no-op', async () => {
    const client = await createUserClient(user.email, user.password)
    const sessionId = await getOrCreateActiveSession(client, user.id)
    const result = await appendMessages(client, { sessionId, userId: user.id, messages: [] })
    expect(result).toEqual([])
  })

  it('loadTranscript returns rows sorted by ordering', async () => {
    const client = await createUserClient(user.email, user.password)
    const sessionId = await getOrCreateActiveSession(client, user.id)
    const transcript = await loadTranscript(client, sessionId)
    const orderings = transcript.map((r) => r.ordering)
    expect(orderings).toEqual([...orderings].sort((a, b) => a - b))
  })

  it('textFromUIMessage flattens text parts and ignores non-text parts', () => {
    const text = textFromUIMessage({
      id: 'm1',
      role: 'assistant',
      parts: [
        { type: 'text', text: 'Hello ' },
        // A non-text part the assistant might emit (e.g. step-boundary) — ignored.
        { type: 'step-start' } as never,
        { type: 'text', text: 'world.' },
      ],
    } as never)
    expect(text).toBe('Hello world.')
  })

  it('uiMessageFromRow shapes a UIMessage with the row id and a single text part', () => {
    const message = uiMessageFromRow({
      id: 'row-1',
      session_id: 'sess-1',
      user_id: 'u-1',
      role: 'user',
      content: 'We sell SaaS to EU SMEs.',
      ordering: 0,
      created_at: '2026-05-26T06:00:00.000Z',
    })
    expect(message.id).toBe('row-1')
    expect(message.role).toBe('user')
    expect(textFromUIMessage(message)).toBe('We sell SaaS to EU SMEs.')
  })
})
