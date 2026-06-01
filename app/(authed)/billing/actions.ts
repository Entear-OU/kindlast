'use server'

import { headers } from 'next/headers'

import { getBillingProvider } from '@/lib/billing/provider'
import { createClient } from '@/lib/supabase/server'
import { createServiceRoleClient } from '@/lib/supabase/service-role'

/**
 * Checkout server action for the Pro plan (ENT-85).
 *
 * Creates a hosted checkout session through the billing-provider seam and
 * returns its URL for the client to redirect to. The provider customer id is
 * recorded on the subscription row via the service role (users can't write their
 * own subscription), so the webhook (ENT-86) can map events back to the user.
 *
 * `returnTo` is the path the user was on when they hit the paywall — it becomes
 * the success URL so they land back where they were trying to act. It's
 * validated to a same-origin relative path to avoid an open redirect. The cancel
 * URL returns to /billing with no state change (the plan flips only on the
 * webhook).
 */

export type CheckoutResult = { ok: true; url: string } | { ok: false; error: string }

const DEFAULT_RETURN = '/feed'

/** Only same-origin relative paths are allowed as a return destination. */
function safeReturnTo(returnTo?: string): string {
  if (!returnTo || !returnTo.startsWith('/') || returnTo.startsWith('//')) {
    return DEFAULT_RETURN
  }
  return returnTo
}

async function resolveOrigin(): Promise<string> {
  const configured = process.env.NEXT_PUBLIC_APP_URL
  if (configured) return configured.replace(/\/$/, '')
  const h = await headers()
  const proto = h.get('x-forwarded-proto') ?? 'https'
  const host = h.get('host') ?? 'localhost:3000'
  return `${proto}://${host}`
}

export async function startCheckout(returnTo?: string): Promise<CheckoutResult> {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) return { ok: false, error: 'Not authenticated' }

  // Read the caller's own subscription (RLS select-own).
  const { data: sub } = await supabase
    .from('subscriptions')
    .select('plan, provider_customer_id')
    .eq('user_id', user.id)
    .maybeSingle()
  if (sub?.plan === 'pro') return { ok: false, error: 'You are already on Pro.' }

  const origin = await resolveOrigin()
  const dest = safeReturnTo(returnTo)

  try {
    const provider = getBillingProvider()
    const session = await provider.createCheckoutSession({
      userId: user.id,
      email: user.email ?? null,
      customerId: sub?.provider_customer_id ?? null,
      successUrl: `${origin}${dest}`,
      cancelUrl: `${origin}/billing`,
    })

    // Persist the customer id for the webhook to resolve later (service role —
    // the subscription table is service-role-write only).
    const admin = createServiceRoleClient()
    await admin
      .from('subscriptions')
      .update({ provider: provider.name, provider_customer_id: session.customerId })
      .eq('user_id', user.id)

    return { ok: true, url: session.url }
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : 'Checkout failed' }
  }
}
