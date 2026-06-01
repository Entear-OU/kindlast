import Stripe from 'stripe'

import {
  BillingProviderError,
  type BillingProvider,
  type CheckoutSession,
  type CreateCheckoutParams,
} from './types'

/**
 * Stripe implementation of the BillingProvider seam (ENT-85).
 *
 * Only the slice of the Stripe SDK this provider uses is captured in
 * `StripeClient`, so tests can inject a fake without standing up the real SDK or
 * a Stripe account. `createStripeProvider` defaults to a real `Stripe` client
 * built from the secret key when none is injected.
 */

export interface StripeClient {
  customers: {
    create(params: {
      email?: string
      metadata?: Record<string, string>
    }): Promise<{ id: string }>
  }
  checkout: {
    sessions: {
      create(params: {
        mode: 'subscription'
        customer: string
        line_items: { price: string; quantity: number }[]
        success_url: string
        cancel_url: string
        metadata?: Record<string, string>
        subscription_data?: { metadata?: Record<string, string> }
      }): Promise<{ url: string | null }>
    }
  }
}

export interface StripeProviderConfig {
  secretKey: string
  /** The €49/mo recurring price id. */
  priceId: string
  /** Injectable for tests; defaults to a real Stripe client from `secretKey`. */
  client?: StripeClient
}

export function createStripeProvider(config: StripeProviderConfig): BillingProvider {
  const client: StripeClient =
    config.client ?? (new Stripe(config.secretKey) as unknown as StripeClient)

  return {
    name: 'stripe',

    async createCheckoutSession({
      userId,
      email,
      customerId,
      successUrl,
      cancelUrl,
    }: CreateCheckoutParams): Promise<CheckoutSession> {
      // Reuse the existing customer if we have one; otherwise create one tagged
      // with our user id so the webhook (ENT-86) can map events back.
      const customer =
        customerId ??
        (
          await client.customers.create({
            email: email ?? undefined,
            metadata: { userId },
          })
        ).id

      const session = await client.checkout.sessions.create({
        mode: 'subscription',
        customer,
        line_items: [{ price: config.priceId, quantity: 1 }],
        success_url: successUrl,
        cancel_url: cancelUrl,
        // userId on both the session and the subscription so any of the events we
        // handle (checkout / subscription.*) can resolve the local user.
        metadata: { userId },
        subscription_data: { metadata: { userId } },
      })

      if (!session.url) {
        throw new BillingProviderError('stripe', 'checkout session returned no url')
      }

      return { url: session.url, customerId: customer }
    },
  }
}
