import type { SupabaseClient } from '@supabase/supabase-js'
import { z } from 'zod'

/**
 * Idempotent ingestion of landmark DPA enforcement decisions into the
 * regulatory corpus (ENT-99).
 *
 * Acts on the schema introduced in
 * `<ts>_regulatory_enforcement_decisions.sql`:
 *
 *   regulatory_enforcement_decisions
 *     ↑ `slug` is the natural key — same decision re-ingested is an upsert,
 *       not a duplicate. Re-runs with edited tags / articles overwrite in
 *       place; the row identity is the slug.
 *
 * Progressive disclosure (ENT-32): this path does NOT store full decision
 * prose. The DPA owns the canonical document; the `sourceUrl` is the citable
 * artifact, and the Analyst fetches verbatim text at citation time via the
 * websearch tool (ENT-98). Each row carries only a curated `summary`
 * (100–2000 chars) used to route the LLM to the right decision.
 *
 * The caller supplies a Supabase client. In practice that is the service-role
 * client (the corpus tables have no INSERT/UPDATE/DELETE RLS policies for
 * non-service roles).
 *
 * Validation runs before any DB call: `parseEnforcementData` rejects malformed
 * JSON, duplicate slugs, non-kebab slugs / tags, and out-of-range summaries.
 * That keeps malformed snapshots from producing half-written corpus state.
 */

// Strict kebab-case for slug + tags keeps diffs reviewable and routes to
// retrieval safely (no "Biometrics" vs "biometrics" tag drift).
const SLUG_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

const SUMMARY_MIN = 100
const SUMMARY_MAX = 2000

const DecisionSchema = z.object({
  slug: z
    .string()
    .min(1)
    .regex(SLUG_RE, 'slug must be kebab-case (lowercase letters, digits, hyphens)'),
  dpa: z.string().min(1, 'dpa is required'),
  title: z.string().min(1),
  decisionDate: z
    .string()
    .regex(/^\d{4}-\d{2}-\d{2}$/, 'decisionDate must be ISO date (YYYY-MM-DD)'),
  // Nullable in the DB; optional in the snapshot. Whole euros, non-negative.
  fineEur: z.number().int().nonnegative('fineEur must be a non-negative integer').optional(),
  summary: z
    .string()
    .min(SUMMARY_MIN, `summary must be at least ${SUMMARY_MIN} characters`)
    .max(SUMMARY_MAX, `summary must be at most ${SUMMARY_MAX} characters`),
  sourceUrl: z.string().url('sourceUrl must be a valid URL'),
  // Articles the decision turned on. Empty array allowed for non-GDPR or
  // cross-cutting decisions.
  gdprArticles: z.array(z.number().int().positive('gdprArticles entries must be positive integers')),
  topicTags: z
    .array(z.string().min(1).regex(SLUG_RE, 'topicTags entries must be kebab-case'))
    .min(1, 'topicTags must contain at least one entry (every decision must be tagged)'),
})

const BaseSchema = z.object({
  decisions: z.array(DecisionSchema).min(1, 'decisions must contain at least one entry'),
})

/**
 * Cross-field rules — leaf schemas can't see the full payload, so duplicate
 * detection lives here:
 *
 *   1. Slugs are unique within the payload — without this, two rows with the
 *      same slug would silently collide on upsert.
 *   2. Topic tags within a single decision are unique — a duplicated tag in a
 *      curated JSON file is always a typo.
 *   3. GDPR article numbers within a single decision are unique — same.
 */
export const EnforcementDataSchema = BaseSchema.superRefine((data, ctx) => {
  const slugs = new Set<string>()
  for (const d of data.decisions) {
    if (slugs.has(d.slug)) {
      ctx.addIssue({ code: 'custom', path: ['decisions'], message: `duplicate slug ${d.slug}` })
    }
    slugs.add(d.slug)

    const seenTags = new Set<string>()
    for (const tag of d.topicTags) {
      if (seenTags.has(tag)) {
        ctx.addIssue({
          code: 'custom',
          path: ['decisions'],
          message: `${d.slug}: duplicate topicTags entry ${tag}`,
        })
      }
      seenTags.add(tag)
    }

    const seenArticles = new Set<number>()
    for (const article of d.gdprArticles) {
      if (seenArticles.has(article)) {
        ctx.addIssue({
          code: 'custom',
          path: ['decisions'],
          message: `${d.slug}: duplicate gdprArticles entry ${article}`,
        })
      }
      seenArticles.add(article)
    }
  }
})

export type EnforcementData = z.infer<typeof EnforcementDataSchema>
export type EnforcementDecision = EnforcementData['decisions'][number]

export type IngestEnforcementResult = {
  decisionsUpserted: number
}

/**
 * Throws a `ZodError` with all collected issues if `raw` is malformed.
 * The ingest script entry point pretty-prints these so a curator can fix
 * the source JSON without spelunking through stack traces.
 */
export function parseEnforcementData(raw: unknown): EnforcementData {
  return EnforcementDataSchema.parse(raw)
}

/**
 * Ingest a batch of enforcement decisions. Idempotent on `slug`: re-running
 * with the same payload produces the same row state; a changed payload
 * updates content in place (no duplicate rows, no orphans). The function
 * never deletes existing rows it didn't touch — same non-destructive policy
 * as the rest of the corpus.
 */
export async function ingestEnforcementDecisions(
  supabase: SupabaseClient,
  data: EnforcementData,
): Promise<IngestEnforcementResult> {
  const rows = data.decisions.map((d) => ({
    slug: d.slug,
    dpa: d.dpa,
    title: d.title,
    decision_date: d.decisionDate,
    fine_eur: d.fineEur ?? null,
    summary: d.summary,
    source_url: d.sourceUrl,
    gdpr_articles: d.gdprArticles,
    topic_tags: d.topicTags,
  }))

  const { data: returned, error } = await supabase
    .from('regulatory_enforcement_decisions')
    .upsert(rows, { onConflict: 'slug' })
    .select('slug')

  if (error || !returned) {
    throw new Error(
      `ingestEnforcementDecisions: upsert failed: ${error?.message ?? 'no rows returned'}`,
    )
  }

  return { decisionsUpserted: returned.length }
}
