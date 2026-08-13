// @vitest-environment node
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'

import { applySubscriptionChange } from '@/lib/billing/apply'
import type { SubscriptionStateChange } from '@/lib/billing/types'

import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-86 — applySubscriptionChange against the local stack.
 *
 * The signup trigger (ENT-81) gives each user a Free/active row, so these tests
 * drive it through the webhook-derived state changes and assert the source of
 * truth moves correctly and idempotently:
 *
 *   1. A pro/active change flips plan + status + current_period_end.
 *   2. Replaying the same event id is a no-op (returns false, no double-apply).
 *   3. Resolution by provider_customer_id works when no userId is present.
 *   4. A canceled change drops the user back to Free.
 *
 * Skips when the local stack is unreachable — same pattern as sibling suites.
 */

const supabaseRunning = await isLocalSupabaseReachable()

function change(over: Partial<SubscriptionStateChange>): SubscriptionStateChange {
  return {
    eventId: 'evt_default',
    customerId: 'cus_default',
    plan: 'pro',
    status: 'active',
    currentPeriodEnd: '2026-07-01T00:00:00.000Z',
    ...over,
  }
}

describe.skipIf(!supabaseRunning)('applySubscriptionChange (ENT-86)', () => {
  let user: TestUser
  const admin = createServiceRoleClient()

  beforeAll(async () => {
    user = await signUpTestUser(admin)
  })

  afterAll(async () => {
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  afterEach(async () => {
    // Reset the user to Free and clear the event ledger between cases.
    await admin
      .from('subscriptions')
      .update({ plan: 'free', status: 'active', current_period_end: null, provider_customer_id: null })
      .eq('user_id', user.id)
    await admin
      .from('billing_webhook_events')
      .delete()
      .in('event_id', ['evt_a', 'evt_b', 'evt_cancel'])
  })

  async function readSub() {
    const { data } = await admin
      .from('subscriptions')
      .select('plan, status, current_period_end, provider_customer_id')
      .eq('user_id', user.id)
      .single()
    return data
  }

  it('applies a pro/active change resolved by userId', async () => {
    const applied = await applySubscriptionChange(
      admin,
      change({ eventId: 'evt_a', userId: user.id, customerId: 'cus_a' }),
    )
    expect(applied).toBe(true)

    const sub = await readSub()
    expect(sub).toMatchObject({
      plan: 'pro',
      status: 'active',
      provider_customer_id: 'cus_a',
    })
    expect(sub?.current_period_end).not.toBeNull()
  })

  it('is idempotent — replaying the same event id does not re-apply', async () => {
    const c = change({ eventId: 'evt_a', userId: user.id, customerId: 'cus_a' })
    expect(await applySubscriptionChange(admin, c)).toBe(true)
    // Replay returns false and leaves state untouched.
    expect(await applySubscriptionChange(admin, c)).toBe(false)

    const { count } = await admin
      .from('billing_webhook_events')
      .select('event_id', { count: 'exact', head: true })
      .eq('event_id', 'evt_a')
    expect(count).toBe(1)
  })

  it('resolves by provider_customer_id when no userId is present', async () => {
    // Seed the customer id (as checkout would), then send a userId-less event.
    await admin
      .from('subscriptions')
      .update({ provider_customer_id: 'cus_b' })
      .eq('user_id', user.id)

    const applied = await applySubscriptionChange(
      admin,
      change({ eventId: 'evt_b', userId: undefined, customerId: 'cus_b', status: 'past_due' }),
    )
    expect(applied).toBe(true)
    expect(await readSub()).toMatchObject({ plan: 'pro', status: 'past_due' })
  })

  it('drops the user back to Free on a canceled change', async () => {
    await applySubscriptionChange(
      admin,
      change({ eventId: 'evt_a', userId: user.id, customerId: 'cus_a' }),
    )
    await applySubscriptionChange(
      admin,
      change({
        eventId: 'evt_cancel',
        userId: user.id,
        customerId: 'cus_a',
        plan: 'free',
        status: 'canceled',
      }),
    )
    expect(await readSub()).toMatchObject({ plan: 'free', status: 'canceled' })
  })
})
