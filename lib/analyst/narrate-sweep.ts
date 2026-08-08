import type { SupabaseClient } from '@supabase/supabase-js'

import { generateAndPersistFinding } from './persistence'
import type { GenerateNarrativeOptions } from './narrative'

/**
 * The Analyst narrative sweep (ENT-162).
 *
 * `analyst_convert_signal` writes a per-kind baseline sentence into every new
 * finding ("Put the missing control in place to satisfy this obligation."). That
 * baseline is meant to be replaced by the LLM narrative layer (ENT-60), which
 * turns it into something specific enough to act on. The Analyst itself runs as
 * pg_cron SQL and cannot reach TypeScript, so without this sweep the narrative
 * layer never runs and every finding ships the same generic sentence.
 *
 * This is the missing caller. It picks up findings still carrying the baseline
 * (`narrative_generated_at is null`) and hands each to `generateAndPersistFinding`.
 *
 * Three properties matter, and each is a counter in the summary:
 *
 *   * `narrated` — the critic passed and the finding now reads specifically.
 *   * `skipped`  — the critic rejected every attempt. The finding KEEPS its
 *     baseline, which is the intended fallback: generic beats wrong. Because
 *     `narrative_generated_at` stays null, a later run retries it.
 *   * `failed`   — the finding threw (model outage, bad context). One bad
 *     finding must not abort the sweep, so failures are counted and skipped
 *     over rather than propagated.
 *
 * Runs under the service role: `findings` is RLS write-locked to the Analyst.
 */

/** Findings per invocation. Bounds model spend and keeps the request short. */
export const DEFAULT_NARRATE_LIMIT = 25

export interface NarrateSweepOptions {
  supabase: SupabaseClient
  /** Generator seam (model injection in tests). */
  narrativeOptions?: GenerateNarrativeOptions
  /** Max findings to narrate in one run. */
  limit?: number
  /** Restrict to one user (hermetic tests). Unset sweeps everyone. */
  userId?: string
}

export interface NarrateSweepSummary {
  processed: number
  narrated: number
  skipped: number
  failed: number
}

export async function narratePendingFindings(
  options: NarrateSweepOptions,
): Promise<NarrateSweepSummary> {
  const { supabase, narrativeOptions, userId } = options
  const limit = options.limit ?? DEFAULT_NARRATE_LIMIT
  const summary: NarrateSweepSummary = { processed: 0, narrated: 0, skipped: 0, failed: 0 }

  let query = supabase
    .from('findings')
    .select('id')
    .eq('status', 'pending')
    .is('narrative_generated_at', null)
  if (userId) query = query.eq('user_id', userId)

  const { data, error } = await query.order('created_at', { ascending: true }).limit(limit)

  if (error) {
    throw new Error(`narratePendingFindings: ${error.message}`)
  }

  for (const row of (data ?? []) as { id: string }[]) {
    summary.processed += 1
    try {
      const result = await generateAndPersistFinding(supabase, row.id, narrativeOptions ?? {})
      if (result.ok) {
        summary.narrated += 1
      } else {
        summary.skipped += 1
      }
    } catch {
      // A single bad finding must not take the sweep down with it. The baseline
      // survives and `narrative_generated_at` stays null, so the next run retries.
      summary.failed += 1
    }
  }

  return summary
}
