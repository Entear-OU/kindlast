/**
 * Billing, from web's side (ENT-210).
 *
 * Read only. There is no call here that changes a plan, because a plan changes
 * when the payment provider's signed webhook says so. A console function that
 * could change one would be a way to grant an entitlement with a request.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

export interface Billing {
  /** The entitlement in force: `free` or `pro`. A cancelled or past_due `pro`
   *  subscription reads as `free` here, because entitlement is what the
   *  customer currently has rather than what they last bought. */
  plan?: string
  /** The raw subscription status: `active`, `past_due`, `canceled`. Empty when
   *  the organisation has never bought anything.
   *
   *  Separate from `plan` so the page can say "your payment failed" rather than
   *  silently showing a downgrade nobody asked for. */
  status?: string
  currentPeriodEnd?: string
  /** Whether this deployment sells anything at all. False on a self-hosted
   *  stack, and then no upgrade path is rendered. */
  billingConfigured?: boolean
  /** Whether anything is withheld for being on the free plan. */
  gatingEnabled?: boolean
}

export function getBilling(accessToken: string, orgId: string) {
  return call<Billing>('kindlast.core.v1.BillingService/GetBilling', {
    accessToken,
    orgId,
  })
}
