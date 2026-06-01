import { redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/console-shell'
import { PostureIndicator } from '@/components/dashboard/posture-indicator'
import { loadPostureInputs } from '@/lib/dashboard/data'
import { computePosture } from '@/lib/dashboard/posture'
import { createClient } from '@/lib/supabase/server'

/**
 * The compliance dashboard (epic ENT-37) — the read-only posture overview a
 * founder lands on when they actively log in. The Green / Amber / Red headline
 * (ENT-77) is the first and largest thing they see; the severity counters
 * (ENT-78), upcoming deadlines (ENT-79) and recent activity (ENT-80) stack
 * beneath it as the epic lands.
 */
export default async function DashboardPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const posture = computePosture(await loadPostureInputs(supabase, user.id))

  return (
    <ConsoleShell activeRail="dashboard" title="Dashboard">
      <div className="space-y-6">
        <PostureIndicator posture={posture} />
      </div>
    </ConsoleShell>
  )
}
