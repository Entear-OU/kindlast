import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { getBillingProvider } from '@/lib/billing/provider'
import { BillingProviderError, type BillingProvider } from '@/lib/billing/types'

/**
 * ENT-85 — the billing-provider factory. Pins env-driven selection, the
 * required-key guard, the unknown-provider rejection, and the test override.
 */

const ENV_KEYS = ['BILLING_PROVIDER', 'STRIPE_SECRET_KEY', 'STRIPE_PRICE_ID'] as const
const saved: Record<string, string | undefined> = {}

beforeEach(() => {
  for (const k of ENV_KEYS) saved[k] = process.env[k]
})
afterEach(() => {
  for (const k of ENV_KEYS) {
    if (saved[k] === undefined) delete process.env[k]
    else process.env[k] = saved[k]
  }
})

describe('getBillingProvider (ENT-85)', () => {
  it('returns the Stripe provider by default when keys are set', () => {
    delete process.env.BILLING_PROVIDER
    process.env.STRIPE_SECRET_KEY = 'sk_test'
    process.env.STRIPE_PRICE_ID = 'price_pro'
    expect(getBillingProvider().name).toBe('stripe')
  })

  it('throws when STRIPE_SECRET_KEY is missing', () => {
    process.env.BILLING_PROVIDER = 'stripe'
    delete process.env.STRIPE_SECRET_KEY
    process.env.STRIPE_PRICE_ID = 'price_pro'
    expect(() => getBillingProvider()).toThrow(BillingProviderError)
  })

  it('throws when STRIPE_PRICE_ID is missing', () => {
    process.env.BILLING_PROVIDER = 'stripe'
    process.env.STRIPE_SECRET_KEY = 'sk_test'
    delete process.env.STRIPE_PRICE_ID
    expect(() => getBillingProvider()).toThrow(/STRIPE_PRICE_ID/)
  })

  it('rejects an unknown provider name', () => {
    process.env.BILLING_PROVIDER = 'paddle'
    expect(() => getBillingProvider()).toThrow(/not a known provider/)
  })

  it('returns a provided override without touching env', () => {
    delete process.env.STRIPE_SECRET_KEY
    const override = { name: 'stripe', createCheckoutSession: vi.fn() } as unknown as BillingProvider
    expect(getBillingProvider({ override })).toBe(override)
  })
})
