import type { SupabaseClient } from '@supabase/supabase-js'

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
