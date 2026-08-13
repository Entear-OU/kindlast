import Stripe from 'stripe'
import { describe, expect, it } from 'vitest'

import { createStripeProvider } from '@/lib/billing/stripe'
import { BillingProviderError } from '@/lib/billing/types'

/**
 * ENT-86 — Stripe webhook parsing, exercised with REAL signatures via the SDK's
 * crypto helpers (generateTestHeaderString / constructEvent), so signature
 * verification is genuinely tested without any network or Stripe account.
 *
 * Covers each subscribed event type's mapping plus the missing/invalid signature
 * rejections (AC: "verifies signature and rejects unsigned requests" + "covers
 * each event type with fixture payloads").
 */

const SECRET = 'whsec_test_secret'

// A real Stripe client — constructEvent is pure crypto, no network on construct.
const stripe = new Stripe('sk_test_dummy')
const provider = createStripeProvider({
  secretKey: 'sk_test_dummy',
  priceId: 'price_pro',
  webhookSecret: SECRET,
  client: stripe as unknown as Parameters<typeof createStripeProvider>[0]['client'],
})

function signedHeaders(event: object): { body: string; headers: Headers } {
  const body = JSON.stringify(event)
  const sig = Stripe.webhooks.generateTestHeaderString({ payload: body, secret: SECRET })
  return { body, headers: new Headers({ 'stripe-signature': sig }) }
}

// 2026-07-01T00:00:00Z in unix seconds.
const PERIOD_END_UNIX = 1782518400
const PERIOD_END_ISO = new Date(PERIOD_END_UNIX * 1000).toISOString()

describe('Stripe parseWebhook (ENT-86)', () => {
  it('maps checkout.session.completed to pro/active', async () => {
    const { body, headers } = signedHeaders({
      id: 'evt_checkout',
      type: 'checkout.session.completed',
      data: { object: { customer: 'cus_1', metadata: { userId: 'u1' } } },
    })
    const change = await provider.parseWebhook(body, headers)
    expect(change).toEqual({
      eventId: 'evt_checkout',
      customerId: 'cus_1',
      userId: 'u1',
      plan: 'pro',
      status: 'active',
      currentPeriodEnd: null,
    })
  })

  it('maps customer.subscription.updated (active) to pro/active with period end', async () => {
    const { body, headers } = signedHeaders({
      id: 'evt_updated',
      type: 'customer.subscription.updated',
      data: {
        object: {
          customer: 'cus_1',
          status: 'active',
          current_period_end: PERIOD_END_UNIX,
          metadata: { userId: 'u1' },
        },
      },
    })
    const change = await provider.parseWebhook(body, headers)
    expect(change).toMatchObject({ plan: 'pro', status: 'active', currentPeriodEnd: PERIOD_END_ISO })
  })

  it('maps customer.subscription.updated (past_due) to pro/past_due', async () => {
    const { body, headers } = signedHeaders({
      id: 'evt_pastdue',
      type: 'customer.subscription.updated',
      data: { object: { customer: 'cus_1', status: 'past_due' } },
    })
    const change = await provider.parseWebhook(body, headers)
    expect(change).toMatchObject({ plan: 'pro', status: 'past_due' })
  })

  it('maps customer.subscription.deleted to free/canceled', async () => {
    const { body, headers } = signedHeaders({
      id: 'evt_deleted',
      type: 'customer.subscription.deleted',
      data: { object: { customer: 'cus_1', current_period_end: PERIOD_END_UNIX } },
    })
    const change = await provider.parseWebhook(body, headers)
    expect(change).toMatchObject({
      plan: 'free',
      status: 'canceled',
      currentPeriodEnd: PERIOD_END_ISO,
    })
  })

  it('maps invoice.payment_failed to pro/past_due', async () => {
    const { body, headers } = signedHeaders({
      id: 'evt_invoice',
      type: 'invoice.payment_failed',
      data: { object: { customer: 'cus_1' } },
    })
    const change = await provider.parseWebhook(body, headers)
    expect(change).toMatchObject({ plan: 'pro', status: 'past_due' })
  })

  it('ignores an unrelated event type (returns null)', async () => {
    const { body, headers } = signedHeaders({
      id: 'evt_other',
      type: 'customer.created',
      data: { object: { id: 'cus_1' } },
    })
    expect(await provider.parseWebhook(body, headers)).toBeNull()
  })

  it('rejects a request with no signature header', async () => {
    await expect(provider.parseWebhook('{}', new Headers())).rejects.toBeInstanceOf(
      BillingProviderError,
    )
  })

  it('rejects a tampered body (signature mismatch)', async () => {
    const { headers } = signedHeaders({
      id: 'evt_x',
      type: 'checkout.session.completed',
      data: { object: { customer: 'cus_1' } },
    })
    // Same (valid) signature header, but a different body → verification fails.
    await expect(
      provider.parseWebhook('{"id":"evt_tampered"}', headers),
    ).rejects.toThrow(/signature verification failed/)
  })
})
