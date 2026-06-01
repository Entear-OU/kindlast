import { redirect } from 'next/navigation'

import { BillingPlans } from '@/components/billing/billing-plans'
import { getPlan } from '@/lib/billing/plan'
import { createClient } from '@/lib/supabase/server'

/**
 * The upgrade / billing page (ENT-85) — the destination of every "Upgrade to
 * Pro" CTA across the app (feed cap, Approve modal, ROPA cap). Shows the two
 * tiers and starts checkout for the €49/mo Pro plan.
 *
 * `returnTo` carries the path the founder was on when they hit the paywall, so
 * the checkout success URL lands them back where they were trying to act.
 */
export default async function BillingPage({
  searchParams,
}: {
  searchParams: Promise<{ returnTo?: string }>
}) {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const [{ returnTo }, plan] = await Promise.all([searchParams, getPlan(supabase, user.id)])

  return (
    <div className="mx-auto w-full max-w-3xl px-4 py-10">
      <BillingPlans plan={plan} returnTo={returnTo} />
    </div>
  )
}
