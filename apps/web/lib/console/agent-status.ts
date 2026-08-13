import type { SupabaseClient } from '@supabase/supabase-js'

import { agentStatusLabel, type AgentStatusPill } from '@/lib/dashboard/activity'

/**
 * The console header's live agent status (ENT-155).
 *
 * Replaces the hard-coded "Agent running · last scan 4 min ago" pill with the
 * real last Watcher run, and drives the Alerts rail badge from whether any
 * findings are actually pending. Both reads are RLS-scoped to the owner. Shared
 * by every authed surface through {@link ConsoleShell}.
 */
export interface ConsoleAgentStatus {
  agentStatus: AgentStatusPill
  /** Whether the Alerts rail should show its unread dot. */
  hasPendingFindings: boolean
}

export async function loadConsoleAgentStatus(
  supabase: SupabaseClient,
  userId: string,
): Promise<ConsoleAgentStatus> {
  const [profileRes, pendingRes] = await Promise.all([
    supabase
      .from('compliance_profiles')
      .select('watcher_last_run_at')
      .eq('user_id', userId)
      .order('watcher_last_run_at', { ascending: false, nullsFirst: false })
      .limit(1)
      .maybeSingle(),
    supabase
      .from('findings')
      .select('id', { count: 'exact', head: true })
      .eq('user_id', userId)
      .eq('status', 'pending'),
  ])

  const watcherLastRunAt = (profileRes.data?.watcher_last_run_at as string | null) ?? null
  return {
    agentStatus: agentStatusLabel(watcherLastRunAt),
    hasPendingFindings: (pendingRes.count ?? 0) > 0,
  }
}
