import type { SupabaseClient } from '@supabase/supabase-js'

import {
  generateFindingNarrative,
  type FindingNarrative,
  type FindingNarrativeContext,
  type GenerateNarrativeOptions,
  type NarrativeResult,
} from './narrative'

/**
 * Analyst narrative persistence (ENT-60).
 *
 * Glue between a stored finding and the generator: assemble the context the
 * model needs, generate a critic-approved narrative, and write it back to
 * `findings.detected` / `proposed_action`. `findings` is RLS write-locked (only
 * the Analyst writes), so callers pass a service-role client
 * (`lib/supabase/service-role.ts`) — the same inject-the-client convention as
 * `lib/onboarding/persistence.ts`.
 *
 * The conversion stores the signal kind and the originating signal's metadata on
 * `findings.metadata` (ENT-58), and the obligation summary on
 * `supporting_context` (ENT-59), so the context load is mostly a read of the
 * finding plus the obligation title and a little profile colour.
 */

interface FindingRow {
  id: string
  profile_id: string
  obligation_id: string
  regulatory_obligation: string | null
  supporting_context: string | null
  metadata: {
    signal_kind?: string
    signal_metadata?: Record<string, unknown>
  } | null
}

export async function loadFindingContext(
  supabase: SupabaseClient,
  findingId: string,
): Promise<FindingNarrativeContext> {
  const { data: finding, error } = await supabase
    .from('findings')
    .select('id, profile_id, obligation_id, regulatory_obligation, supporting_context, metadata')
    .eq('id', findingId)
    .single<FindingRow>()
  if (error || !finding) {
    throw new Error(`loadFindingContext(${findingId}): ${error?.message ?? 'not found'}`)
  }

  const [{ data: obligation }, { data: profile }] = await Promise.all([
    supabase.from('obligations').select('title, summary').eq('id', finding.obligation_id).single<{
      title: string
      summary: string
    }>(),
    supabase
      .from('compliance_profiles')
      .select('industry, vendor_list, ai_systems')
      .eq('id', finding.profile_id)
      .single<{ industry: string | null; vendor_list: string | null; ai_systems: string[] | null }>(),
  ])

  const sigMeta = finding.metadata?.signal_metadata ?? {}
  const asString = (v: unknown): string | null => (typeof v === 'string' ? v : null)
  const asStringArray = (v: unknown): string[] | null =>
    Array.isArray(v) && v.every((x) => typeof x === 'string') ? (v as string[]) : null

  return {
    signalKind: finding.metadata?.signal_kind ?? 'regulatory_update',
    obligationTitle: obligation?.title ?? '',
    obligationSummary: obligation?.summary ?? finding.supporting_context ?? '',
    citationLabel: finding.regulatory_obligation ?? '',
    industry: profile?.industry ?? null,
    vendors: profile?.vendor_list ?? null,
    aiSystems: profile?.ai_systems ?? null,
    deadlineDate: asString(sigMeta['effective_date']) ?? asString(sigMeta['response_due_at']),
    missingControls: asStringArray(sigMeta['missing']),
  }
}

/**
 * Write a generated narrative to the finding. Caller has already confirmed the
 * critic passed (see `generateAndPersistFinding`); `narrative_generated_at`
 * stamps it so a later run knows the baseline has been replaced.
 */
export async function persistFindingNarrative(
  supabase: SupabaseClient,
  findingId: string,
  narrative: FindingNarrative,
  generatedAt: string = new Date().toISOString(),
): Promise<void> {
  const { error } = await supabase
    .from('findings')
    .update({
      detected: narrative.description,
      proposed_action: narrative.proposedAction,
      narrative_generated_at: generatedAt,
    })
    .eq('id', findingId)
  if (error) {
    throw new Error(`persistFindingNarrative(${findingId}): ${error.message}`)
  }
}

/**
 * Generate and persist a narrative for one finding. Persists ONLY when the
 * critic passes (the AC's "rejected before persistence"); otherwise the finding
 * keeps its ENT-58 baseline and the rejection reasons are returned for logging.
 */
export async function generateAndPersistFinding(
  supabase: SupabaseClient,
  findingId: string,
  options: GenerateNarrativeOptions = {},
): Promise<NarrativeResult> {
  const context = await loadFindingContext(supabase, findingId)
  const result = await generateFindingNarrative(context, options)
  if (result.ok && result.narrative) {
    await persistFindingNarrative(supabase, findingId, result.narrative)
  }
  return result
}
