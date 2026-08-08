import { redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/console-shell'
import { DeadlinesList } from '@/components/dashboard/deadlines-list'
import { PostureIndicator } from '@/components/dashboard/posture-indicator'
import { RecentActivity } from '@/components/dashboard/recent-activity'
import { SeverityCounters } from '@/components/dashboard/severity-counters'
import {
  loadPostureInputs,
  loadRecentActivity,
  loadUpcomingDeadlines,
} from '@/lib/dashboard/data'
import { computePosture } from '@/lib/dashboard/posture'
import { countOpenBySeverity } from '@/lib/dashboard/severity'
import { createClient } from '@/lib/supabase/server'
import { hasComplianceProfile } from '@/lib/console/require-profile'

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

  // ENT-166: every console surface is a view over a compliance profile. Without
  // one there is nothing to show and nothing that can be written, so send them
  // to finish onboarding rather than render an empty console with dead actions.
  if (!(await hasComplianceProfile(supabase, user.id))) {
    redirect('/onboarding')
  }

  const [inputs, deadlines, activity] = await Promise.all([
    loadPostureInputs(supabase, user.id),
    loadUpcomingDeadlines(supabase, user.id),
    loadRecentActivity(supabase, user.id),
  ])
  const posture = computePosture(inputs)
  const counts = countOpenBySeverity(inputs.openSeverities)

  return (
    <ConsoleShell activeRail="dashboard" title="Dashboard">
      <div className="space-y-6">
        <PostureIndicator posture={posture} />
        <SeverityCounters counts={counts} />
        <DeadlinesList deadlines={deadlines} />
        <RecentActivity
          activity={activity}
          currentUserId={user.id}
          currentUserEmail={user.email}
        />
      </div>
    </ConsoleShell>
  )
}
