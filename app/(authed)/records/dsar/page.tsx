import { redirect } from 'next/navigation'

import { ComplianceRecordsShell } from '@/components/records/compliance-records-shell'
import { DsarLog } from '@/components/records/dsar-log'
import { loadDsars } from '@/lib/records/dsar'
import { createClient } from '@/lib/supabase/server'
import { hasComplianceProfile } from '@/lib/console/require-profile'

/**
 * The DSAR Log (ENT-71) — every data-subject request with its status and
 * response deadline, inside the Compliance records console.
 *
 * `canComplete` gates the Pro-only "Mark as responded" Executor write. There is
 * no billing/plan store yet, so it defaults to enabled here — the seam that
 * becomes plan-aware once billing lands.
 */
export default async function DsarPage() {
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

  const dsars = await loadDsars(supabase, user.id)

  return (
    <ComplianceRecordsShell activeTab="dsar-log">
      <DsarLog dsars={dsars} canComplete />
    </ComplianceRecordsShell>
  )
}
