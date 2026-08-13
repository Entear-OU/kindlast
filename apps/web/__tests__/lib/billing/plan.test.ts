import { describe, expect, it, vi } from 'vitest'

import type { SupabaseClient } from '@supabase/supabase-js'

import { getPlan } from '@/lib/billing/plan'

/**
 * The billing seam (ENT-63) becomes a real lookup in ENT-81: `getPlan` reads the
 * caller's own `subscriptions` row (RLS-scoped) and reports `free`/`pro`. The
 * subscription trigger guarantees a row exists, but `getPlan` stays defensive —
 * a missing row or a read error resolves to `free`, never an accidental Pro
 * unlock. The DB-backed behaviour (trigger, backfill, RLS) is covered by the
 * integration suite; here we pin the mapping + safe default.
 */

/** Minimal stub of the `from(...).select(...).eq(...).maybeSingle()` chain. */
function stubClient(result: { data: unknown; error: unknown }): SupabaseClient {
  const maybeSingle = vi.fn().mockResolvedValue(result)
  const eq = vi.fn().mockReturnValue({ maybeSingle })
  const select = vi.fn().mockReturnValue({ eq })
  const from = vi.fn().mockReturnValue({ select })
  return { from } as unknown as SupabaseClient
}

describe('getPlan (subscriptions lookup, ENT-81)', () => {
  it('returns "pro" when the subscription row is on the pro plan', async () => {
    const client = stubClient({ data: { plan: 'pro' }, error: null })
    await expect(getPlan(client, 'user-1')).resolves.toBe('pro')
  })

  it('returns "free" when the subscription row is on the free plan', async () => {
    const client = stubClient({ data: { plan: 'free' }, error: null })
    await expect(getPlan(client, 'user-1')).resolves.toBe('free')
  })

  it('defaults to "free" when no subscription row exists', async () => {
    const client = stubClient({ data: null, error: null })
    await expect(getPlan(client, 'user-1')).resolves.toBe('free')
  })

  it('defaults to "free" when the read errors (never an accidental unlock)', async () => {
    const client = stubClient({ data: null, error: { message: 'boom' } })
    await expect(getPlan(client, 'user-1')).resolves.toBe('free')
  })

  it('queries the subscriptions table scoped to the given user', async () => {
    const maybeSingle = vi.fn().mockResolvedValue({ data: { plan: 'pro' }, error: null })
    const eq = vi.fn().mockReturnValue({ maybeSingle })
    const select = vi.fn().mockReturnValue({ eq })
    const from = vi.fn().mockReturnValue({ select })
    const client = { from } as unknown as SupabaseClient

    await getPlan(client, 'user-42')

    expect(from).toHaveBeenCalledWith('subscriptions')
    expect(select).toHaveBeenCalledWith('plan')
    expect(eq).toHaveBeenCalledWith('user_id', 'user-42')
  })
})
