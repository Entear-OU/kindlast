import { redirect } from 'next/navigation'

import { ComplianceRecordsShell } from '@/components/records/compliance-records-shell'
import { DsarLog } from '@/components/records/dsar-log'
import { loadDsars } from '@/lib/records/dsar'
import { createClient } from '@/lib/supabase/server'

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

  const dsars = await loadDsars(supabase, user.id)

  return (
    <ComplianceRecordsShell activeTab="dsar-log">
      <DsarLog dsars={dsars} canComplete />
    </ComplianceRecordsShell>
  )
}
