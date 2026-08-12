import type { SupabaseClient } from '@supabase/supabase-js'

import type { Plan } from '@/lib/billing/plan'
import type { Finding, FindingSeverity } from '@/lib/feed/findings'

/**
 * Data access + presentation helpers for the finding DETAIL view (ENT-64).
 *
 * Reads go through an authenticated Supabase client so the `findings`
 * select-own RLS policy (ENT-58) stays the source of truth. The detail view
 * adds the obligation summary (`supporting_context`) and the supporting chunks
 * (via the `finding_supporting_chunks` RPC), plus a free-tier gate.
 */

export interface SupportingChunk {
  ordinal: number
  label: string
  quoted_text: string
  source_url: string | null
}

// The feed Finding plus the column the detail view adds (the obligation summary).
export interface FindingDetail extends Finding {
  supporting_context: string | null
}

// Same columns the feed loads, plus `supporting_context` for the detail view.
const DETAIL_COLUMNS =
  'id,detected,severity,proposed_action,regulatory_obligation,citation_url,obligation_slug,effort_estimate,status,rejection_reason,snoozed_until,created_at,supporting_context'

/**
 * Single-finding fetch, RLS-scoped (select-own policy) + explicit user_id for
 * index use. Returns null when not found / not owned (caller renders notFound()).
 */
export async function loadFindingDetail(
  supabase: SupabaseClient,
  userId: string,
  id: string,
): Promise<FindingDetail | null> {
  const { data, error } = await supabase
    .from('findings')
    .select(DETAIL_COLUMNS)
    .eq('user_id', userId)
    .eq('id', id)
    .maybeSingle()

  if (error) {
    throw new Error(`loadFindingDetail: ${error.message}`)
  }
  if (!data) {
    return null
  }
  return data as FindingDetail
}

/**
 * Supporting chunks via the RPC (ordered by ordinal). Returns [] on error/empty
 * so the detail page still renders the finding even if context lookup fails.
 */
export async function loadSupportingChunks(
  supabase: SupabaseClient,
  id: string,
): Promise<SupportingChunk[]> {
  const { data, error } = await supabase.rpc('finding_supporting_chunks', {
    p_finding_id: id,
  })

  if (error) {
    return []
  }
  return (data ?? []) as SupportingChunk[]
}

// Free-tier gate: Pro sees all chunks; Free sees only the first, with the rest locked.
export interface GatedChunks {
  visible: SupportingChunk[]
  lockedCount: number
}

export function gateChunks(chunks: SupportingChunk[], plan: Plan): GatedChunks {
  if (plan === 'pro') {
    return { visible: chunks, lockedCount: 0 }
  }
  return { visible: chunks.slice(0, 1), lockedCount: Math.max(0, chunks.length - 1) }
}

// A short, honest severity rationale derived from the level (no stored rationale exists).
const SEVERITY_RATIONALE: Record<FindingSeverity, string> = {
  critical:
    'Critical: a likely breach of a hard legal obligation that exposes you to enforcement or fines if left unaddressed.',
  high: 'High: a clear compliance gap against a specific obligation that should be closed before it draws regulatory attention.',
  medium:
    'Medium: a meaningful gap worth fixing soon, though the immediate enforcement risk is limited.',
  low: 'Low: a minor or precautionary issue that improves your compliance posture but carries little near-term risk.',
}

export function severityRationale(severity: FindingSeverity): string {
  return SEVERITY_RATIONALE[severity]
}
