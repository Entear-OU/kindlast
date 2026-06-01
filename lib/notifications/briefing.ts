/**
 * Weekly briefing data builder (ENT-74).
 *
 * Assembles the three sections of the Monday posture email for one user, each
 * from a deterministic query: open findings counted by severity, deadlines
 * within 30 days (the Watcher's deadline/DSAR signals, surfaced as findings),
 * and the Executor actions taken in the last 7 days (the audit log). Pure data —
 * rendering lives in `briefing-email.ts`.
 */

import type { SupabaseClient } from '@supabase/supabase-js'

import { FEED_SEVERITIES, type FindingSeverity } from '@/lib/feed/findings'

export interface BriefingDeadline {
  label: string
  daysRemaining: number
}

export interface BriefingAction {
  actionType: string
  targetTable: string
  occurredAt: string
}

export interface BriefingData {
  findingsBySeverity: Record<FindingSeverity, number>
  openTotal: number
  upcomingDeadlines: BriefingDeadline[]
  executorActions: BriefingAction[]
}

const DEADLINE_KINDS = ['deadline', 'dsar']
const DEADLINE_WINDOW_DAYS = 30
const ACTIONS_WINDOW_DAYS = 7

interface PendingFindingRow {
  severity: FindingSeverity
  detected: string
  regulatory_obligation: string | null
  metadata: {
    signal_kind?: string
    signal_metadata?: { days_remaining?: number }
  } | null
}

interface AuditRow {
  action_type: string
  target_table: string
  occurred_at: string
}

function emptyCounts(): Record<FindingSeverity, number> {
  return { critical: 0, high: 0, medium: 0, low: 0 }
}

export async function buildBriefing(
  supabase: SupabaseClient,
  userId: string,
): Promise<BriefingData> {
  const { data: pending, error: findingsError } = await supabase
    .from('findings')
    .select('severity,detected,regulatory_obligation,metadata')
    .eq('user_id', userId)
    .eq('status', 'pending')
  if (findingsError) throw new Error(`buildBriefing findings: ${findingsError.message}`)

  const findingsBySeverity = emptyCounts()
  const upcomingDeadlines: BriefingDeadline[] = []

  for (const row of (pending ?? []) as PendingFindingRow[]) {
    if (row.severity in findingsBySeverity) findingsBySeverity[row.severity] += 1

    const kind = row.metadata?.signal_kind
    const days = row.metadata?.signal_metadata?.days_remaining
    if (kind && DEADLINE_KINDS.includes(kind) && typeof days === 'number' && days <= DEADLINE_WINDOW_DAYS) {
      upcomingDeadlines.push({
        label: row.regulatory_obligation ?? row.detected,
        daysRemaining: days,
      })
    }
  }
  upcomingDeadlines.sort((a, b) => a.daysRemaining - b.daysRemaining)

  const openTotal = FEED_SEVERITIES.reduce((sum, s) => sum + findingsBySeverity[s], 0)

  const since = new Date(Date.now() - ACTIONS_WINDOW_DAYS * 24 * 60 * 60 * 1000).toISOString()
  const { data: actions, error: actionsError } = await supabase
    .from('audit_log')
    .select('action_type,target_table,occurred_at')
    .eq('user_id', userId)
    .gte('occurred_at', since)
    .order('occurred_at', { ascending: false })
  if (actionsError) throw new Error(`buildBriefing actions: ${actionsError.message}`)

  const executorActions: BriefingAction[] = ((actions ?? []) as AuditRow[]).map((a) => ({
    actionType: a.action_type,
    targetTable: a.target_table,
    occurredAt: a.occurred_at,
  }))

  return { findingsBySeverity, openTotal, upcomingDeadlines, executorActions }
}
