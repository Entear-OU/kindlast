import type { SupabaseClient } from '@supabase/supabase-js'

/**
 * Data access + presentation helpers for the Agent feed (ENT-62).
 *
 * Reads go through an authenticated Supabase client so the `findings`
 * select-own RLS policy (ENT-58) is the source of truth. The feed is read-only
 * here — Approve / Reject / Snooze land in ENT-63.
 */

export type FindingSeverity = 'low' | 'medium' | 'high' | 'critical'
export type FindingStatus = 'pending' | 'approved' | 'rejected' | 'snoozed'

export interface Finding {
  id: string
  detected: string
  severity: FindingSeverity
  proposed_action: string
  regulatory_obligation: string | null
  citation_url: string | null
  obligation_slug: string | null
  effort_estimate: 'minutes' | 'hours' | 'days'
  status: FindingStatus
  created_at: string
}

const COLUMNS =
  'id,detected,severity,proposed_action,regulatory_obligation,citation_url,obligation_slug,effort_estimate,status,created_at'

/**
 * Every finding for the user, newest first (AC: reverse-chronological by
 * created_at). RLS scopes to the owner; passing user_id keeps the query
 * explicit and indexed.
 */
export async function loadFindings(
  supabase: SupabaseClient,
  userId: string,
): Promise<Finding[]> {
  const { data, error } = await supabase
    .from('findings')
    .select(COLUMNS)
    .eq('user_id', userId)
    .order('created_at', { ascending: false })

  if (error) {
    throw new Error(`loadFindings: ${error.message}`)
  }
  return (data ?? []) as Finding[]
}

// The four statuses a finding can hold today. The AC also lists 'completed', but
// no finding ever carries it yet (the Executor sets 'approved'); add it here
// once a completion state exists in the schema.
export const FEED_STATUSES: FindingStatus[] = ['pending', 'approved', 'rejected', 'snoozed']
export const FEED_SEVERITIES: FindingSeverity[] = ['critical', 'high', 'medium', 'low']

/**
 * Free-tier cap on visible pending findings (PRD §11). The seam only — ENT-62
 * shows everything; enforcement (blurred lock + upgrade prompt) is ENT-82, which
 * needs the `subscriptions` table from ENT-81 to know the user's plan.
 */
export const FREE_PENDING_LIMIT = 3

export interface FeedFilter {
  status?: FindingStatus | 'all'
  severity?: FindingSeverity | 'all'
}

/** Client-side narrowing over the already-loaded list. Order is preserved. */
export function filterFindings(findings: Finding[], filter: FeedFilter = {}): Finding[] {
  const { status = 'all', severity = 'all' } = filter
  return findings.filter(
    (f) =>
      (status === 'all' || f.status === status) &&
      (severity === 'all' || f.severity === severity),
  )
}

const SEVERITY_CHIP: Record<FindingSeverity, { label: string; className: string }> = {
  critical: { label: 'Critical', className: 'bg-rose-500/15 text-rose-300 ring-1 ring-rose-500/30' },
  high: { label: 'High', className: 'bg-orange-500/15 text-orange-300 ring-1 ring-orange-500/30' },
  medium: { label: 'Medium', className: 'bg-amber-500/15 text-amber-300 ring-1 ring-amber-500/30' },
  low: { label: 'Low', className: 'bg-zinc-500/15 text-zinc-300 ring-1 ring-zinc-500/30' },
}

/** Severity → founder-facing label + a chip class readable on the dark console. */
export function severityChip(severity: FindingSeverity): { label: string; className: string } {
  return SEVERITY_CHIP[severity]
}

const STATUS_LABEL: Record<FindingStatus, string> = {
  pending: 'Pending',
  approved: 'Approved',
  rejected: 'Rejected',
  snoozed: 'Snoozed',
}

export function statusLabel(status: FindingStatus): string {
  return STATUS_LABEL[status] ?? status
}
