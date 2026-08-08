import type { SupabaseClient } from '@supabase/supabase-js'

import type { Plan } from '@/lib/billing/plan'

/**
 * Data access + presentation helpers for the ROPA register (ENT-70).
 *
 * Reads go through an authenticated Supabase client so RLS is the source of
 * truth. The two writes (manual add, inline edit) go through the SECURITY
 * DEFINER RPCs `create_processing_activity` / `update_processing_activity`
 * (migration 20260601160000) so each change records an audit entry atomically
 * and the Free-tier cap is enforced server-side — see `./ropa-actions` server
 * actions, which call them.
 */

export interface ProcessingActivity {
  id: string
  name: string
  purpose: string | null
  legal_basis: string | null
  data_categories: string[]
  recipients: string[]
  retention_period: string | null
  /** Set when the row was ratified from an approved finding (Executor write). */
  finding_id: string | null
  created_at: string
  updated_at: string
}

/** The editable shape the register's add/edit forms submit. */
export interface ProcessingActivityInput {
  name: string
  purpose: string
  legal_basis: string
  data_categories: string[]
  recipients: string[]
  retention_period: string
}

export type RopaStatus = 'complete' | 'review_needed' | 'incomplete'

/** Free-tier cap on *manual* activities. Mirrors `ropa_manual_activity_limit()`. */
export const ROPA_MANUAL_LIMIT = 3

/**
 * Plan-aware manual-activity limit (ENT-84). Mirrors the DB's plan-aware
 * `ropa_manual_activity_limit()`: `null` means uncapped (Pro), Free is capped at
 * ROPA_MANUAL_LIMIT. The DB is the authority — this is the client mirror so the
 * UI can disable "Add activity" and lock excess rows without a round-trip.
 */
export function ropaManualLimit(plan: Plan): number | null {
  return plan === 'pro' ? null : ROPA_MANUAL_LIMIT
}

/**
 * Free-tier edge case (ENT-84): a downgrade can leave more manual activities than
 * the cap allows. The excess manual rows go read-only (with an upgrade hint)
 * rather than letting the founder keep editing an over-quota register; the
 * most-recent `limit` manual rows stay editable. Executor-ratified rows
 * (finding_id set) are never capped, so they're always editable.
 *
 * `activities` is assumed in display order (loadProcessingActivities orders by
 * updated_at desc), so the head of the manual rows is "most-recent". Returns the
 * ids that should render read-only; empty when uncapped (Pro) or under the cap.
 */
export function lockedManualActivityIds(
  activities: ProcessingActivity[],
  limit: number | null,
): Set<string> {
  if (limit === null) return new Set()
  const locked = new Set<string>()
  let manualSeen = 0
  for (const a of activities) {
    if (a.finding_id !== null) continue
    manualSeen += 1
    if (manualSeen > limit) locked.add(a.id)
  }
  return locked
}

const COLUMNS =
  'id,name,purpose,legal_basis,data_categories,recipients,retention_period,finding_id,created_at,updated_at'

export async function loadProcessingActivities(
  supabase: SupabaseClient,
  userId: string,
): Promise<ProcessingActivity[]> {
  const { data, error } = await supabase
    .from('processing_activities')
    .select(COLUMNS)
    .eq('user_id', userId)
    .order('updated_at', { ascending: false })

  if (error) {
    throw new Error(`loadProcessingActivities: ${error.message}`)
  }
  return (data ?? []) as ProcessingActivity[]
}

/** How many of these activities were added manually (count toward the cap). */
export function manualActivityCount(activities: ProcessingActivity[]): number {
  return activities.filter((a) => a.finding_id === null).length
}

/**
 * Derive the register's status pill from the row, with no extra schema:
 *
 *   * `incomplete`    — missing a mandatory GDPR Art. 30 field.
 *   * `review_needed` — an Executor pre-fill (finding_id set) the founder has
 *                       not yet edited (updated_at still equals created_at).
 *   * `complete`      — every mandatory field present, and human-touched.
 */
export function deriveRopaStatus(a: ProcessingActivity): RopaStatus {
  const filled = (s: string | null) => !!s && s.trim().length > 0
  const hasMandatory =
    filled(a.purpose) &&
    filled(a.legal_basis) &&
    filled(a.retention_period) &&
    a.data_categories.length > 0 &&
    a.recipients.length > 0

  if (!hasMandatory) return 'incomplete'

  const edited = new Date(a.updated_at).getTime() > new Date(a.created_at).getTime()
  if (a.finding_id !== null && !edited) return 'review_needed'

  return 'complete'
}

export const ROPA_STATUS_LABEL: Record<RopaStatus, string> = {
  complete: 'Complete',
  review_needed: 'Review needed',
  incomplete: 'Incomplete',
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/**
 * Compact "last updated" label, matching the register design: "Today" for the
 * current day, "8 May" within the same year, "8 May 2025" otherwise. `now` is
 * injectable so the formatting is deterministic to test.
 */
export function formatUpdatedAt(iso: string, now: Date = new Date()): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '–'
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  if (sameDay) return 'Today'
  const base = `${d.getDate()} ${MONTHS[d.getMonth()]}`
  return d.getFullYear() === now.getFullYear() ? base : `${base} ${d.getFullYear()}`
}
