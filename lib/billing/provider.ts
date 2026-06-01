/**
 * Billing provider factory (ENT-85).
 *
 * Reads `BILLING_PROVIDER` (default `stripe`) and returns the matching
 * implementation, the single place that touches billing env vars. Mirrors
 * `getWebSearchProvider`. Callers can pass an explicit provider name or a ready
 * `override` instance — the latter keeps server actions trivially mockable in
 * tests without env.
 */

import { createStripeProvider } from './stripe'
import {
  BillingProviderError,
  type BillingProvider,
  type BillingProviderName,
} from './types'

const KNOWN_PROVIDERS: ReadonlyArray<BillingProviderName> = ['stripe']

export interface GetBillingProviderOptions {
  provider?: BillingProviderName
  /** Pre-built provider, used in tests / DI — bypasses env entirely. */
  override?: BillingProvider
}

function resolveProviderName(explicit?: BillingProviderName): BillingProviderName {
  const value = explicit ?? process.env.BILLING_PROVIDER ?? 'stripe'
  if (!KNOWN_PROVIDERS.includes(value as BillingProviderName)) {
    throw new Error(
      `BILLING_PROVIDER=${value} is not a known provider (one of: ${KNOWN_PROVIDERS.join(', ')})`,
    )
  }
  return value as BillingProviderName
}

export function getBillingProvider(options?: GetBillingProviderOptions): BillingProvider {
  if (options?.override) return options.override

  const name = resolveProviderName(options?.provider)
  switch (name) {
    case 'stripe': {
      const secretKey = process.env.STRIPE_SECRET_KEY ?? ''
      const priceId = process.env.STRIPE_PRICE_ID ?? ''
      if (!secretKey) {
        throw new BillingProviderError('stripe', 'STRIPE_SECRET_KEY is required')
      }
      if (!priceId) {
        throw new BillingProviderError('stripe', 'STRIPE_PRICE_ID is required')
      }
      // webhookSecret is optional here — only parseWebhook requires it, and it
      // raises a clear error if it's missing — so checkout still works without it.
      return createStripeProvider({
        secretKey,
        priceId,
        webhookSecret: process.env.STRIPE_WEBHOOK_SECRET ?? '',
      })
    }
  }
}

export { BillingProviderError } from './types'
export type { BillingProvider, BillingProviderName } from './types'
