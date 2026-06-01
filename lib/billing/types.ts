/**
 * Billing provider seam (ENT-85).
 *
 * A narrow, processor-agnostic interface so Stripe can be swapped for another
 * payment provider without touching the checkout flow. Mirrors the websearch
 * provider pattern (lib/websearch): the factory (`./provider`) is the only place
 * that reads env and picks an implementation; callers pass the resulting
 * `BillingProvider` around as a value.
 *
 * ENT-86 extends this interface with webhook handling.
 */

export type BillingProviderName = 'stripe'

/** Normalized subscription lifecycle states, independent of any processor. */
export type SubscriptionStatus = 'active' | 'past_due' | 'canceled'

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
