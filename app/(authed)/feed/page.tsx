import { redirect } from 'next/navigation'

import { getPlan } from '@/lib/billing/plan'
import { ConsoleShell } from '@/components/console/console-shell'
import { FindingsFeed } from '@/components/feed/findings-feed'
import { parseSeverityParam } from '@/lib/dashboard/severity'
import { loadFindings } from '@/lib/feed/findings'
import { createClient } from '@/lib/supabase/server'

/**
 * The Agent feed (ENT-62 list, ENT-63 actions) — the founder's
 * reverse-chronological list of every finding the agents have produced, with
 * one-tap Approve / Reject / Snooze, inside the shared console frame.
 *
 * `?severity=` pre-filters the list — the destination of the dashboard's
 * severity counters (ENT-78). An unknown value is ignored (shows everything).
 */
export default async function FeedPage({
  searchParams,
}: {
  searchParams: Promise<{ severity?: string | string[] }>
}) {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const [findings, plan, params] = await Promise.all([
    loadFindings(supabase, user.id),
    getPlan(supabase, user.id),
    searchParams,
  ])

  return (
    <ConsoleShell activeRail="alerts" title="Agent feed">
      <FindingsFeed
        findings={findings}
        plan={plan}
        initialSeverity={parseSeverityParam(params.severity) ?? 'all'}
      />
    </ConsoleShell>
  )
}
