import { redirect } from 'next/navigation'

import { AiSystemsRegister } from '@/components/records/ai-systems-register'
import { ComplianceRecordsShell } from '@/components/records/compliance-records-shell'
import { loadAiSystems } from '@/lib/records/ai-system'
import { createClient } from '@/lib/supabase/server'
import { hasComplianceProfile } from '@/lib/console/require-profile'

/**
 * The AI Systems Register (ENT-72) — every AI system in use with its EU AI Act
 * risk classification and documentation posture, inside the Compliance records
 * console. Annex III compliance posture in one screen.
 */
export default async function AiSystemsPage() {
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

  const systems = await loadAiSystems(supabase, user.id)

  return (
    <ComplianceRecordsShell activeTab="ai-systems">
      <AiSystemsRegister systems={systems} />
    </ComplianceRecordsShell>
  )
}
