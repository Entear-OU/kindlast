import Stripe from 'stripe'

import {
  BillingProviderError,
  type BillingProvider,
  type CheckoutSession,
  type CreateCheckoutParams,
  type SubscriptionStateChange,
  type SubscriptionStatus,
} from './types'
import type { Plan } from '@/lib/billing/plan'

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
  webhooks: {
    constructEvent(payload: string, header: string, secret: string): StripeEvent
  }
}

/** The minimal shape of a verified Stripe event we read. */
export interface StripeEvent {
  id: string
  type: string
  data: { object: Record<string, unknown> }
}

export interface StripeProviderConfig {
  secretKey: string
  /** The €49/mo recurring price id. */
  priceId: string
  /** Webhook signing secret (ENT-86); required only for parseWebhook. */
  webhookSecret?: string
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

    async parseWebhook(rawBody, headers): Promise<SubscriptionStateChange | null> {
      if (!config.webhookSecret) {
        throw new BillingProviderError('stripe', 'STRIPE_WEBHOOK_SECRET is required')
      }
      const signature = headers.get('stripe-signature')
      if (!signature) {
        throw new BillingProviderError('stripe', 'missing stripe-signature header')
      }

      let event: StripeEvent
      try {
        event = client.webhooks.constructEvent(rawBody, signature, config.webhookSecret)
      } catch {
        throw new BillingProviderError('stripe', 'signature verification failed')
      }

      return mapStripeEvent(event)
    },
  }
}

// ── Event mapping ──────────────────────────────────────────────────────────
// Collapse the four Stripe events we subscribe to into our normalized change.
// Anything else returns null (the route 200s and ignores it).

function str(v: unknown): string | null {
  return typeof v === 'string' ? v : null
}

function metadataUserId(obj: Record<string, unknown>): string | undefined {
  const meta = obj.metadata as Record<string, unknown> | undefined
  const id = meta?.userId
  return typeof id === 'string' && id.length > 0 ? id : undefined
}

function isoFromUnix(v: unknown): string | null {
  return typeof v === 'number' ? new Date(v * 1000).toISOString() : null
}

/** Map a Stripe subscription `status` to our (plan, status) pair. */
function fromStripeStatus(status: unknown): { plan: Plan; status: SubscriptionStatus } {
  switch (status) {
    case 'active':
    case 'trialing':
      return { plan: 'pro', status: 'active' }
    case 'past_due':
      return { plan: 'pro', status: 'past_due' }
    default:
      // canceled, unpaid, incomplete(_expired), paused → no Pro access.
      return { plan: 'free', status: 'canceled' }
  }
}

export function mapStripeEvent(event: StripeEvent): SubscriptionStateChange | null {
  const obj = event.data.object
  const base = { eventId: event.id, userId: metadataUserId(obj) }

  switch (event.type) {
    case 'checkout.session.completed': {
      const customerId = str(obj.customer)
      if (!customerId) return null
      // Period end arrives on the subscription.* events; not on the session.
      return { ...base, customerId, plan: 'pro', status: 'active', currentPeriodEnd: null }
    }
    case 'customer.subscription.updated': {
      const customerId = str(obj.customer)
      if (!customerId) return null
      return {
        ...base,
        customerId,
        ...fromStripeStatus(obj.status),
        currentPeriodEnd: isoFromUnix(obj.current_period_end),
      }
    }
    case 'customer.subscription.deleted': {
      const customerId = str(obj.customer)
      if (!customerId) return null
      return {
        ...base,
        customerId,
        plan: 'free',
        status: 'canceled',
        currentPeriodEnd: isoFromUnix(obj.current_period_end),
      }
    }
    case 'invoice.payment_failed': {
      const customerId = str(obj.customer)
      if (!customerId) return null
      // Keep Pro but flag past_due — Stripe's dunning may still recover it.
      return { ...base, customerId, plan: 'pro', status: 'past_due', currentPeriodEnd: null }
    }
    default:
      return null
  }
}
