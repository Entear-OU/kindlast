import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-85 — the checkout server action. Pins: auth guard, the already-Pro
 * short-circuit, the happy path (provider called with origin+returnTo URLs, the
 * customer id persisted via the service role), and the open-redirect guard on
 * returnTo.
 */

const {
  getUserMock,
  subSelectMaybeSingle,
  serviceUpdateEq,
  serviceUpdate,
  createCheckoutMock,
  headersGet,
} = vi.hoisted(() => ({
  getUserMock: vi.fn(),
  subSelectMaybeSingle: vi.fn(),
  serviceUpdateEq: vi.fn(),
  serviceUpdate: vi.fn(),
  createCheckoutMock: vi.fn(),
  headersGet: vi.fn(),
}))

vi.mock('next/headers', () => ({
  headers: () => ({ get: headersGet }),
}))

vi.mock('@/lib/supabase/server', () => ({
  createClient: async () => ({
    auth: { getUser: getUserMock },
    from: () => ({
      select: () => ({ eq: () => ({ maybeSingle: subSelectMaybeSingle }) }),
    }),
  }),
}))

vi.mock('@/lib/supabase/service-role', () => ({
  createServiceRoleClient: () => ({
    from: () => ({ update: serviceUpdate }),
  }),
}))

vi.mock('@/lib/billing/provider', () => ({
  getBillingProvider: () => ({ name: 'stripe', createCheckoutSession: createCheckoutMock }),
}))

import { startCheckout } from '@/app/(authed)/billing/actions'

beforeEach(() => {
  vi.clearAllMocks()
  process.env.NEXT_PUBLIC_APP_URL = 'https://app.test'
  getUserMock.mockResolvedValue({ data: { user: { id: 'u1', email: 'f@acme.test' } } })
  subSelectMaybeSingle.mockResolvedValue({ data: { plan: 'free', provider_customer_id: null } })
  createCheckoutMock.mockResolvedValue({ url: 'https://checkout/x', customerId: 'cus_1' })
  serviceUpdateEq.mockResolvedValue({ error: null })
  serviceUpdate.mockReturnValue({ eq: serviceUpdateEq })
})
afterEach(() => {
  delete process.env.NEXT_PUBLIC_APP_URL
})

describe('startCheckout (ENT-85)', () => {
  it('returns an error when not authenticated', async () => {
    getUserMock.mockResolvedValue({ data: { user: null } })
    expect(await startCheckout('/feed')).toEqual({ ok: false, error: 'Not authenticated' })
  })

  it('short-circuits when already on Pro', async () => {
    subSelectMaybeSingle.mockResolvedValue({ data: { plan: 'pro', provider_customer_id: 'cus_1' } })
    const res = await startCheckout('/feed')
    expect(res.ok).toBe(false)
    expect(createCheckoutMock).not.toHaveBeenCalled()
  })

  it('creates a session with origin+returnTo URLs and persists the customer id', async () => {
    const res = await startCheckout('/records/ropa')

    expect(createCheckoutMock).toHaveBeenCalledWith(
      expect.objectContaining({
        userId: 'u1',
        email: 'f@acme.test',
        customerId: null,
        successUrl: 'https://app.test/records/ropa',
        cancelUrl: 'https://app.test/billing',
      }),
    )
    // Customer id is persisted through the service-role client.
    expect(serviceUpdate).toHaveBeenCalledWith({ provider: 'stripe', provider_customer_id: 'cus_1' })
    expect(serviceUpdateEq).toHaveBeenCalledWith('user_id', 'u1')
    expect(res).toEqual({ ok: true, url: 'https://checkout/x' })
  })

  it('rejects an off-origin returnTo (open-redirect guard) and falls back to /feed', async () => {
    await startCheckout('https://evil.test/phish')
    expect(createCheckoutMock).toHaveBeenCalledWith(
      expect.objectContaining({ successUrl: 'https://app.test/feed' }),
    )
  })

  it('also rejects a protocol-relative returnTo', async () => {
    await startCheckout('//evil.test')
    expect(createCheckoutMock).toHaveBeenCalledWith(
      expect.objectContaining({ successUrl: 'https://app.test/feed' }),
    )
  })
})
