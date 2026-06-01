/**
 * Billing provider seam (ENT-85).
 *
 * A narrow, processor-agnostic interface so Stripe can be swapped for another
 * payment provider without touching the checkout flow. Mirrors the websearch
 * provider pattern (lib/websearch): the factory (`./provider`) is the only place
 * that reads env and picks an implementation; callers pass the resulting
 * `BillingProvider` around as a value.
 *
 * ENT-86 adds webhook handling (`parseWebhook`).
 */

import type { Plan } from '@/lib/billing/plan'

export type BillingProviderName = 'stripe'

/** Normalized subscription lifecycle states, independent of any processor. */
export type SubscriptionStatus = 'active' | 'past_due' | 'canceled'

/**
 * A normalized subscription state change parsed from a verified webhook event
 * (ENT-86). The processor's vocabulary (Stripe event types, statuses) is
 * collapsed into our own here, so the rest of the system never sees a Stripe
 * type.
 */
export interface SubscriptionStateChange {
  /** Provider event id — recorded so replays are no-ops (idempotency). */
  eventId: string
  /** Provider customer id; the fallback key for resolving the local user. */
  customerId: string
  /** Our user id when the event carries it in metadata (preferred key). */
  userId?: string
  plan: Plan
  status: SubscriptionStatus
  /** End of the paid period (ISO), when the event reports it; else null. */
  currentPeriodEnd: string | null
}

export interface CreateCheckoutParams {
  /** Our user id — round-tripped through provider metadata so the webhook can map back. */
  userId: string
  email: string | null
  /** Existing provider customer id from the subscription row, if any (reused to avoid dupes). */
  customerId: string | null
  /** Where the provider returns the user on success / cancel (absolute URLs). */
  successUrl: string
  cancelUrl: string
}

export interface CheckoutSession {
  /** The hosted-checkout URL to redirect the user to. */
  url: string
  /** The provider customer id, created or reused — persisted on the subscription row. */
  customerId: string
}

export interface BillingProvider {
  readonly name: BillingProviderName
  /** Create a hosted checkout session for the recurring Pro plan. */
  createCheckoutSession(params: CreateCheckoutParams): Promise<CheckoutSession>
  /**
   * Verify a webhook request and translate it into a normalized state change, or
   * null when the event isn't one we act on. The provider reads its own
   * signature header from `headers` (keeping the route processor-agnostic) and
   * throws on a missing/invalid signature.
   */
  parseWebhook(rawBody: string, headers: Headers): Promise<SubscriptionStateChange | null>
}

export class BillingProviderError extends Error {
  constructor(
    public readonly provider: string,
    message: string,
  ) {
    super(`[billing:${provider}] ${message}`)
    this.name = 'BillingProviderError'
  }
}
