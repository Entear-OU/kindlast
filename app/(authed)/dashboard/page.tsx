import { redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/console-shell'
import { DeadlinesList } from '@/components/dashboard/deadlines-list'
import { PostureIndicator } from '@/components/dashboard/posture-indicator'
import { SeverityCounters } from '@/components/dashboard/severity-counters'
import { loadPostureInputs, loadUpcomingDeadlines } from '@/lib/dashboard/data'
import { computePosture } from '@/lib/dashboard/posture'
import { countOpenBySeverity } from '@/lib/dashboard/severity'
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

  const [inputs, deadlines] = await Promise.all([
    loadPostureInputs(supabase, user.id),
    loadUpcomingDeadlines(supabase, user.id),
  ])
  const posture = computePosture(inputs)
  const counts = countOpenBySeverity(inputs.openSeverities)

  return (
    <ConsoleShell activeRail="dashboard" title="Dashboard">
      <div className="space-y-6">
        <PostureIndicator posture={posture} />
        <SeverityCounters counts={counts} />
        <DeadlinesList deadlines={deadlines} />
      </div>
    </ConsoleShell>
  )
}
