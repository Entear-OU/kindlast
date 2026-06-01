import { redirect } from 'next/navigation'

import { ConsoleShell } from '@/components/console/console-shell'
import { FindingsFeed } from '@/components/feed/findings-feed'
import { loadFindings } from '@/lib/feed/findings'
import { createClient } from '@/lib/supabase/server'

/**
 * The Agent feed (ENT-62) — the founder's reverse-chronological list of every
 * finding the agents have produced, inside the shared console frame.
 */
export default async function FeedPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const findings = await loadFindings(supabase, user.id)

  return (
    <ConsoleShell activeRail="alerts" title="Agent feed">
      <FindingsFeed findings={findings} />
    </ConsoleShell>
  )
}
