import { redirect } from 'next/navigation'

import { ComplianceRecordsShell } from '@/components/records/compliance-records-shell'
import { RopaRegister } from '@/components/records/ropa-register'
import { getPlan } from '@/lib/billing/plan'
import { loadProcessingActivities } from '@/lib/records/ropa'
import { createClient } from '@/lib/supabase/server'

/**
 * The ROPA register (ENT-70) — the founder's view/edit surface for their Record
 * of Processing Activities, inside the Compliance records console.
 */
export default async function RopaPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const [activities, plan] = await Promise.all([
    loadProcessingActivities(supabase, user.id),
    getPlan(supabase, user.id),
  ])

  return (
    <ComplianceRecordsShell activeTab="ropa">
      <RopaRegister activities={activities} plan={plan} />
    </ComplianceRecordsShell>
  )
}
