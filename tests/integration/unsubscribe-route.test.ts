// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { performUnsubscribe } from '@/lib/notifications/unsubscribe'
import { signUnsubscribeToken } from '@/lib/notifications/unsubscribe-token'

import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-74 — weekly-briefing unsubscribe handler against the live stack.
 *
 * A valid signed token flips weekly_briefing_enabled to false (upserting the row
 * if absent); a tampered or expired token is refused and leaves the flag intact.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const SECRET = 'unsub-route-secret'
const NOW = 1_700_000_000

async function flag(userId: string): Promise<boolean | null> {
  const admin = createServiceRoleClient()
  const { data } = await admin
    .from('notification_preferences')
    .select('weekly_briefing_enabled')
    .eq('user_id', userId)
    .maybeSingle()
  return (data?.weekly_briefing_enabled as boolean | undefined) ?? null
}

describe.skipIf(!supabaseRunning)('briefing unsubscribe (ENT-74)', () => {
  let user: TestUser

  beforeAll(async () => {
    user = await signUpTestUser(createServiceRoleClient())
  })

  afterAll(async () => {
    if (user?.id) await deleteTestUser(createServiceRoleClient(), user.id)
  })

  it('flips weekly_briefing_enabled to false from a valid token (upserts the row)', async () => {
    const token = signUnsubscribeToken({ userId: user.id, scope: 'weekly_briefing', nowSeconds: NOW }, SECRET)
    const kind = await performUnsubscribe({
      supabase: createServiceRoleClient(),
      token,
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(kind).toBe('ok')
    expect(await flag(user.id)).toBe(false)
  })

  it('refuses an expired token and leaves the flag unchanged', async () => {
    // Re-enable first.
    await createServiceRoleClient()
      .from('notification_preferences')
      .upsert({ user_id: user.id, weekly_briefing_enabled: true })

    const token = signUnsubscribeToken(
      { userId: user.id, scope: 'weekly_briefing', nowSeconds: NOW, ttlSeconds: 60 },
      SECRET,
    )
    const kind = await performUnsubscribe({
      supabase: createServiceRoleClient(),
      token,
      secret: SECRET,
      nowSeconds: NOW + 120,
    })
    expect(kind).toBe('expired')
    expect(await flag(user.id)).toBe(true)
  })

  it('refuses a tampered token', async () => {
    const good = signUnsubscribeToken({ userId: user.id, scope: 'weekly_briefing', nowSeconds: NOW }, SECRET)
    const [payload] = good.split('.')
    const kind = await performUnsubscribe({
      supabase: createServiceRoleClient(),
      token: `${payload}.deadbeef`,
      secret: SECRET,
      nowSeconds: NOW + 5,
    })
    expect(kind).toBe('invalid')
  })
})
