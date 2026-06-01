import type { SupabaseClient } from '@supabase/supabase-js'

/**
 * Data access + presentation helpers for the AI Systems Register (ENT-72).
 *
 * Reads go through an authenticated Supabase client (RLS is the source of
 * truth). The two writes — manual "Add system" and inline edit — go through the
 * SECURITY DEFINER RPCs `create_ai_system_manual` / `update_ai_system`
 * (migration 20260601180000), so each change records an audit entry and a
 * classification change is gated on a reviewed approval.
 */

export type RiskClassification =
  | 'unacceptable'
  | 'high'
  | 'limited'
  | 'minimal'
  | 'unclassified'

export type DocumentationStatus = 'missing' | 'in_progress' | 'complete'

export interface AiSystem {
  id: string
  name: string
  vendor: string | null
  purpose: string | null
  risk_classification: RiskClassification
  documentation_status: DocumentationStatus
  last_reviewed_at: string | null
  finding_id: string | null
  created_at: string
  updated_at: string
}

export type PillTone = 'done' | 'danger' | 'warn' | 'info' | 'muted'

export const RISK_OPTIONS: RiskClassification[] = [
  'unclassified',
  'minimal',
  'limited',
  'high',
  'unacceptable',
]

export const DOC_OPTIONS: DocumentationStatus[] = ['missing', 'in_progress', 'complete']

export const RISK_LABEL: Record<RiskClassification, string> = {
  unacceptable: 'Unacceptable',
  high: 'High risk',
  limited: 'Limited',
  minimal: 'Minimal',
  unclassified: 'Unclassified',
}

export const RISK_TONE: Record<RiskClassification, PillTone> = {
  unacceptable: 'danger',
  high: 'danger',
  limited: 'warn',
  minimal: 'info',
  unclassified: 'muted',
}

export const DOC_LABEL: Record<DocumentationStatus, string> = {
  missing: 'Missing',
  in_progress: 'In progress',
  complete: 'Complete',
}

export const DOC_TONE: Record<DocumentationStatus, PillTone> = {
  missing: 'warn',
  in_progress: 'info',
  complete: 'done',
}

const COLUMNS =
  'id,name,vendor,purpose,risk_classification,documentation_status,last_reviewed_at,finding_id,created_at,updated_at'

export async function loadAiSystems(supabase: SupabaseClient, userId: string): Promise<AiSystem[]> {
  const { data, error } = await supabase
    .from('ai_systems')
    .select(COLUMNS)
    .eq('user_id', userId)
    .order('updated_at', { ascending: false })

  if (error) {
    throw new Error(`loadAiSystems: ${error.message}`)
  }
  return (data ?? []) as AiSystem[]
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** "Last reviewed" cell: a date, or "Never" when it has not been reviewed. */
export function formatReviewed(iso: string | null, now: Date = new Date()): string {
  if (!iso) return 'Never'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return 'Never'
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  if (sameDay) return 'Today'
  const base = `${d.getDate()} ${MONTHS[d.getMonth()]}`
  return d.getFullYear() === now.getFullYear() ? base : `${base} ${d.getFullYear()}`
}
