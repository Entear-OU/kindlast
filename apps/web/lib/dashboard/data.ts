import type { SupabaseClient } from '@supabase/supabase-js'

import type { AuditEntry, RecentActivity } from './activity'
import {
  buildUpcomingDeadlines,
  type DeadlineFindingRow,
  type UpcomingDeadline,
} from './deadlines'
import type { FindingSeverity } from '@/lib/feed/findings'
import type { PostureDeadline, PostureInputs } from './posture'

/**
 * Data access for the compliance dashboard (epic ENT-37).
 *
 * Reads go through an authenticated Supabase client so the `findings`
 * select-own RLS policy (ENT-58) is the source of truth. The dashboard is
 * entirely read-only over tables the agents already populate — no new writes,
 * no new migrations.
 */

/** The deadline-bearing finding kinds (set by the Watcher, carried in metadata). */
const DEADLINE_KINDS = new Set(['deadline', 'dsar'])

interface PostureFindingRow {
  severity: FindingSeverity
  metadata: {
    signal_kind?: string
    signal_metadata?: { days_remaining?: number | string } | null
  } | null
}

/**
 * The two inputs the posture rule (ENT-77) needs: the severities of every open
 * (pending) finding, and the approaching/overdue deadlines among them.
 *
 * Deadlines are derived from the deadline/DSAR findings the Analyst already
 * produced — their `metadata.signal_metadata.days_remaining` is the Watcher's
 * own day count, so the dashboard and the feed never drift.
 */
export async function loadPostureInputs(
  supabase: SupabaseClient,
  userId: string,
): Promise<PostureInputs> {
  const { data, error } = await supabase
    .from('findings')
    .select('severity,metadata')
    .eq('user_id', userId)
    .eq('status', 'pending')

  if (error) {
    throw new Error(`loadPostureInputs: ${error.message}`)
  }

  const rows = (data ?? []) as PostureFindingRow[]
  const openSeverities = rows.map((r) => r.severity)
  const deadlines: PostureDeadline[] = rows
    .filter((r) => DEADLINE_KINDS.has(r.metadata?.signal_kind ?? ''))
    .map((r) => ({
      severity: r.severity,
      daysRemaining: Number(r.metadata?.signal_metadata?.days_remaining ?? 0),
    }))

  return { openSeverities, deadlines }
}

/**
 * The upcoming-deadlines list (ENT-79): every open deadline/DSAR finding,
 * turned into a sorted, windowed list of dates the founder should plan around.
 * RLS scopes the read to the owner; the windowing/sorting is pure
 * (`buildUpcomingDeadlines`).
 */
export async function loadUpcomingDeadlines(
  supabase: SupabaseClient,
  userId: string,
): Promise<UpcomingDeadline[]> {
  const { data, error } = await supabase
    .from('findings')
    .select('id,severity,regulatory_obligation,detected,metadata')
    .eq('user_id', userId)
    .eq('status', 'pending')

  if (error) {
    throw new Error(`loadUpcomingDeadlines: ${error.message}`)
  }

  return buildUpcomingDeadlines((data ?? []) as DeadlineFindingRow[])
}

/** How many audit entries the "Recent actions" widget shows (AC: last 10). */
const RECENT_ACTIONS_LIMIT = 10

interface AuditRow {
  id: string
  action_type: string
  target_table: string
  target_id: string | null
  approving_user_id: string
  occurred_at: string
}

/**
 * The recent-activity widget's data (ENT-80): the last 10 Executor audit
 * entries (newest first, served from the `audit_log_user_recent_idx`) and the
 * profile's last Watcher run. Both reads are RLS-scoped to the owner.
 */
export async function loadRecentActivity(
  supabase: SupabaseClient,
  userId: string,
): Promise<RecentActivity> {
  const [auditRes, profileRes] = await Promise.all([
    supabase
      .from('audit_log')
      .select('id,action_type,target_table,target_id,approving_user_id,occurred_at')
      .eq('user_id', userId)
      .order('occurred_at', { ascending: false })
      .limit(RECENT_ACTIONS_LIMIT),
    supabase
      .from('compliance_profiles')
      .select('watcher_last_run_at')
      .eq('user_id', userId)
      .order('watcher_last_run_at', { ascending: false, nullsFirst: false })
      .limit(1)
      .maybeSingle(),
  ])

  if (auditRes.error) {
    throw new Error(`loadRecentActivity (audit): ${auditRes.error.message}`)
  }
  if (profileRes.error) {
    throw new Error(`loadRecentActivity (profile): ${profileRes.error.message}`)
  }

  const entries: AuditEntry[] = ((auditRes.data ?? []) as AuditRow[]).map((r) => ({
    id: r.id,
    actionType: r.action_type,
    targetTable: r.target_table,
    targetId: r.target_id,
    approvingUserId: r.approving_user_id,
    occurredAt: r.occurred_at,
  }))

  return {
    entries,
    watcherLastRunAt: (profileRes.data?.watcher_last_run_at as string | null) ?? null,
  }
}
