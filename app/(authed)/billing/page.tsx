import { redirect } from 'next/navigation'

import { BillingPlans } from '@/components/billing/billing-plans'
import { ConsoleShell } from '@/components/console/console-shell'
import { getPlan } from '@/lib/billing/plan'
import { createClient } from '@/lib/supabase/server'
import { hasComplianceProfile } from '@/lib/console/require-profile'

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

  // ENT-166: every console surface is a view over a compliance profile. Without
  // one there is nothing to show and nothing that can be written, so send them
  // to finish onboarding rather than render an empty console with dead actions.
  if (!(await hasComplianceProfile(supabase, user.id))) {
    redirect('/onboarding')
  }

  const [{ returnTo }, plan] = await Promise.all([searchParams, getPlan(supabase, user.id)])

  return (
    <ConsoleShell activeRail="billing" title="Billing">
      <div className="mx-auto w-full max-w-3xl">
        <BillingPlans plan={plan} returnTo={returnTo} />
      </div>
    </ConsoleShell>
  )
}
