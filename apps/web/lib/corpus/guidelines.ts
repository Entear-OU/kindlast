import type { SupabaseClient } from '@supabase/supabase-js'
import { z } from 'zod'

/**
 * Idempotent ingestion of EDPB / WP29 guidelines into the regulatory corpus
 * (ENT-50).
 *
 * Acts on the schema introduced in `<ts>_regulatory_guidelines.sql`:
 *
 *   regulatory_guidelines
 *     ↑ `slug` is the natural key — same guideline re-ingested is an upsert,
 *       not a duplicate. Re-runs with edited topic_tags or version overwrite
 *       in place; the row identity is the slug.
 *
 * Unlike the primary-corpus ingest (ENT-48), this path does NOT store full
 * prose text. Guidelines get revised by their publishers and the URL is the
 * citable artifact; mirroring text into our DB would create a sync trap.
 * If on-demand text fetch is ever needed, the schema grows a nullable
 * `full_text` column then.
 *
 * The caller supplies a Supabase client. In practice that is the service-role
 * client (the corpus tables have no INSERT/UPDATE/DELETE RLS policies for
 * non-service roles — see migration `<ts>_regulatory_guidelines.sql`).
 *
 * Validation runs before any DB call: `parseGuidelinesData` rejects malformed
 * JSON, duplicate slugs within the payload, non-kebab slugs / tags, and
 * publishers outside the curated whitelist. That keeps malformed snapshots
 * from producing half-written corpus state.
 */

// Slug + tag strict kebab-case keeps diffs reviewable and routes to retrieval
// safely. A lax pattern would let curators introduce
// "Consent" vs "consent" tag drift silently.
const SLUG_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

// Curated publisher whitelist. The MVP corpus is EDPB-led; WP29 (the
// pre-GDPR Article 29 Working Party) guidelines are included where the
// EDPB explicitly endorsed them on its first plenary in May 2018.
// Extending this list is a deliberate scope decision, not a curator's
// drive-by edit — that's why it's enumerated.
const PUBLISHERS = ['EDPB', 'WP29'] as const

const GuidelineSchema = z.object({
  slug: z
    .string()
    .min(1)
    .regex(SLUG_RE, 'slug must be kebab-case (lowercase letters, digits, hyphens)'),
  publisher: z.enum(PUBLISHERS, {
    message: `publisher must be one of: ${PUBLISHERS.join(', ')}`,
  }),
  title: z.string().min(1),
  adoptedDate: z
    .string()
    .regex(/^\d{4}-\d{2}-\d{2}$/, 'adoptedDate must be ISO date (YYYY-MM-DD)'),
  version: z.string().min(1).optional(),
  sourceUrl: z.string().url('sourceUrl must be a valid URL'),
  topicTags: z
    .array(
      z
        .string()
        .min(1)
        .regex(SLUG_RE, 'topicTags entries must be kebab-case'),
    )
    .min(1, 'topicTags must contain at least one entry (every guideline must be tagged)'),
})

const BaseSchema = z.object({
  guidelines: z.array(GuidelineSchema).min(1, 'guidelines must contain at least one entry'),
})

/**
 * Cross-field rules — leaf schemas can't see the full payload, so duplicate
 * detection lives here. Two checks:
 *
 *   1. Slugs are unique within the payload — without this, two rows with
 *      the same slug would silently collide on upsert.
 *   2. Topic tags within a single guideline are unique — defensive: the
 *      Postgres `text[]` column tolerates duplicates, but a duplicated
 *      tag in a curated JSON file is always a typo.
 */
export const GuidelinesDataSchema = BaseSchema.superRefine((data, ctx) => {
  const slugs = new Set<string>()
  for (const g of data.guidelines) {
    if (slugs.has(g.slug)) {
      ctx.addIssue({
        code: 'custom',
        path: ['guidelines'],
        message: `duplicate slug ${g.slug}`,
      })
    }
    slugs.add(g.slug)

    const seenTags = new Set<string>()
    for (const tag of g.topicTags) {
      if (seenTags.has(tag)) {
        ctx.addIssue({
          code: 'custom',
          path: ['guidelines'],
          message: `${g.slug}: duplicate topicTags entry ${tag}`,
        })
      }
      seenTags.add(tag)
    }
  }
})

export type GuidelinesData = z.infer<typeof GuidelinesDataSchema>
export type Guideline = GuidelinesData['guidelines'][number]

export type IngestGuidelinesResult = {
  guidelinesUpserted: number
}

/**
 * Throws a `ZodError` with all collected issues if `raw` is malformed.
 * The ingest script entry point pretty-prints these so a curator can fix
 * the source JSON without spelunking through stack traces.
 */
export function parseGuidelinesData(raw: unknown): GuidelinesData {
  return GuidelinesDataSchema.parse(raw)
}

/**
 * Ingest a batch of guidelines. Idempotent on `slug`: re-running with the
 * same payload produces the same row state. Re-running with a changed
 * payload updates content in place (no duplicate rows, no orphans). The
 * function never deletes existing rows it didn't touch — curated entries
 * removed from a later JSON snapshot stay in the DB. Same non-destructive
 * policy as the article-recital junction in ENT-48.
 */
export async function ingestGuidelines(
  supabase: SupabaseClient,
  data: GuidelinesData,
): Promise<IngestGuidelinesResult> {
  const rows = data.guidelines.map((g) => ({
    slug: g.slug,
    publisher: g.publisher,
    title: g.title,
    adopted_date: g.adoptedDate,
    version: g.version ?? null,
    source_url: g.sourceUrl,
    topic_tags: g.topicTags,
  }))

  const { data: returned, error } = await supabase
    .from('regulatory_guidelines')
    .upsert(rows, { onConflict: 'slug' })
    .select('slug')

  if (error || !returned) {
    throw new Error(
      `ingestGuidelines: upsert failed: ${error?.message ?? 'no rows returned'}`,
    )
  }

  return { guidelinesUpserted: returned.length }
}
