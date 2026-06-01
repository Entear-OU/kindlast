import { beforeEach, describe, expect, it, vi } from 'vitest'

import { createStripeProvider, type StripeClient } from '@/lib/billing/stripe'
import { BillingProviderError } from '@/lib/billing/types'

/**
 * ENT-85 — the Stripe checkout provider, exercised against an injected fake
 * client so the tests are hermetic (no SDK, no account). Pins: customer reuse vs
 * creation, the recurring price + return URLs on the session, userId metadata
 * for the webhook, and the no-url failure mode.
 */

function fakeClient(over: Partial<StripeClient> = {}): {
  client: StripeClient
  customerCreate: ReturnType<typeof vi.fn>
  sessionCreate: ReturnType<typeof vi.fn>
} {
  const customerCreate = vi.fn().mockResolvedValue({ id: 'cus_new' })
  const sessionCreate = vi.fn().mockResolvedValue({ url: 'https://checkout.stripe/session' })
  const client = {
    customers: { create: customerCreate },
    checkout: { sessions: { create: sessionCreate } },
    ...over,
  } as unknown as StripeClient
  return { client, customerCreate, sessionCreate }
}

const params = {
  userId: 'user-1',
  email: 'founder@acme.test',
  customerId: null,
  successUrl: 'https://app/feed',
  cancelUrl: 'https://app/billing',
}

describe('createStripeProvider (ENT-85)', () => {
  let f: ReturnType<typeof fakeClient>

  beforeEach(() => {
    f = fakeClient()
  })

  it('creates a customer tagged with the user id when none exists', async () => {
    const provider = createStripeProvider({ secretKey: 'sk', priceId: 'price_pro', client: f.client })
    const session = await provider.createCheckoutSession(params)

    expect(f.customerCreate).toHaveBeenCalledWith({
      email: 'founder@acme.test',
      metadata: { userId: 'user-1' },
    })
    expect(session.customerId).toBe('cus_new')
  })

  it('reuses an existing customer id without creating one', async () => {
    const provider = createStripeProvider({ secretKey: 'sk', priceId: 'price_pro', client: f.client })
    const session = await provider.createCheckoutSession({ ...params, customerId: 'cus_existing' })

    expect(f.customerCreate).not.toHaveBeenCalled()
    expect(session.customerId).toBe('cus_existing')
  })

  it('opens a subscription session with the Pro price, URLs, and userId metadata', async () => {
    const provider = createStripeProvider({ secretKey: 'sk', priceId: 'price_pro', client: f.client })
    const session = await provider.createCheckoutSession({ ...params, customerId: 'cus_x' })

    expect(f.sessionCreate).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: 'subscription',
        customer: 'cus_x',
        line_items: [{ price: 'price_pro', quantity: 1 }],
        success_url: 'https://app/feed',
        cancel_url: 'https://app/billing',
        metadata: { userId: 'user-1' },
        subscription_data: { metadata: { userId: 'user-1' } },
      }),
    )
    expect(session.url).toBe('https://checkout.stripe/session')
  })

  it('throws a BillingProviderError when the session has no url', async () => {
    const { client } = fakeClient({
      checkout: { sessions: { create: vi.fn().mockResolvedValue({ url: null }) } },
    } as Partial<StripeClient>)
    const provider = createStripeProvider({ secretKey: 'sk', priceId: 'price_pro', client })

    await expect(provider.createCheckoutSession({ ...params, customerId: 'c' })).rejects.toThrow(
      BillingProviderError,
    )
  })
})
