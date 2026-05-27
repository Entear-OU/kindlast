import type { SupabaseClient } from '@supabase/supabase-js'
import { z } from 'zod'

/**
 * Idempotent ingestion of structured obligations into the catalogue (ENT-52).
 *
 * Acts on the schema introduced in `<ts>_obligations_catalogue.sql`:
 *
 *   obligations
 *     ↑ `slug` is the natural key — same obligation re-ingested is an upsert,
 *       not a duplicate. Re-runs with edited summaries, topic_tags, or
 *       trigger metadata overwrite in place; the row identity is the slug.
 *
 * Catalogue rows reference the regulatory corpus by NATURAL KEY (CELEX +
 * article number, recital number, or annex label + item paragraph) — NOT
 * by surrogate UUID. The Analyst resolves the natural key to a source URL
 * via the corpus's `regulatory_documents.official_url` at runtime and
 * fetches verbatim normative text via the websearch tool from ENT-98.
 * Natural keys stay stable across corpus re-ingests; UUIDs don't.
 *
 * Validation runs before any DB call: `parseObligationsData` rejects
 * malformed JSON, duplicate slugs, citations missing required fields for
 * their declared kind, and out-of-range values (summary length, negative
 * deadlines, unknown severity). That keeps malformed snapshots from
 * producing half-written catalogue state.
 *
 * The caller supplies a Supabase client. In practice that is the
 * service-role client (the obligations table has no INSERT/UPDATE/DELETE
 * RLS policies for non-service roles — see migration
 * `<ts>_obligations_catalogue.sql`).
 */

// Slug + tag strict kebab-case keeps diffs reviewable and routes to
// retrieval safely. Matches the regex used in
// `lib/corpus/guidelines.ts` for consistency across the catalogue surface.
const SLUG_RE = /^[a-z0-9]+(?:-[a-z0-9]+)*$/

// CELEX numbers are 11+ alphanumeric chars (sector digit, year, descriptor
// letter, document number). This is a sanity guard — not the full BNF —
// to catch typos like "GDPR" or "EU AI Act" in the snapshot.
// Examples: 32016R0679 (GDPR), 32024R1689 (EU AI Act).
const CELEX_RE = /^[0-9A-Z]{8,}$/

const SUMMARY_MIN = 100
const SUMMARY_MAX = 2000

const SEVERITIES = ['low', 'medium', 'high'] as const

const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}$/

// Discriminated union over citation shape — `kind` selects which natural-key
// columns are required. Mirrors the `obligations_citation_matches_kind`
// composite CHECK in the migration. We model the validator with z.discriminatedUnion
// so error messages name the failing branch precisely.
const ArticleCitationSchema = z.object({
  kind: z.literal('article'),
  celex: z.string().regex(CELEX_RE, 'citation.celex must look like a CELEX number'),
  articleNumber: z.number().int().positive(),
  paragraph: z.string().min(1).optional(),
})

const RecitalCitationSchema = z.object({
  kind: z.literal('recital'),
  celex: z.string().regex(CELEX_RE, 'citation.celex must look like a CELEX number'),
  recitalNumber: z.number().int().positive(),
})

const AnnexCitationSchema = z.object({
  kind: z.literal('annex'),
  celex: z.string().regex(CELEX_RE, 'citation.celex must look like a CELEX number'),
  annexLabel: z.string().min(1, 'annex citation requires an annexLabel'),
  paragraph: z.string().min(1).optional(),
})

const CitationSchema = z.discriminatedUnion('kind', [
  ArticleCitationSchema,
  RecitalCitationSchema,
  AnnexCitationSchema,
])

const ObligationSchema = z.object({
  slug: z
    .string()
    .min(1)
    .regex(SLUG_RE, 'slug must be kebab-case (lowercase letters, digits, hyphens)'),
  title: z.string().min(1),
  summary: z
    .string()
    .min(SUMMARY_MIN, `summary must be at least ${SUMMARY_MIN} characters`)
    .max(SUMMARY_MAX, `summary must be at most ${SUMMARY_MAX} characters`),
  citation: CitationSchema,
  // appliesWhen is a JSONB blob the Watcher will evaluate at alert time.
  // We accept any JSON-shaped object here; downstream evaluators own the
  // strict schema. Empty `{}` (the default) means "always applies".
  appliesWhen: z.record(z.string(), z.unknown()).default({}),
  severity: z.enum(SEVERITIES).default('medium'),
  // dueWithinDays: NULL = no scheduled deadline (continuous obligations
  // like ROPA), 0 = immediate / on-event (e.g. 72-hour breach notification),
  // positive int = days until due once triggered.
  dueWithinDays: z
    .number()
    .int()
    .min(0, 'dueWithinDays cannot be negative — a deadline cannot be in the past')
    .optional(),
  recurrence: z.string().min(1).optional(),
  effectiveDate: z
    .string()
    .regex(ISO_DATE_RE, 'effectiveDate must be ISO date (YYYY-MM-DD)')
    .optional(),
  topicTags: z
    .array(
      z
        .string()
        .min(1)
        .regex(SLUG_RE, 'topicTags entries must be kebab-case'),
    )
    .min(1, 'topicTags must contain at least one entry (every obligation must be tagged)'),
})

const BaseSchema = z.object({
  obligations: z
    .array(ObligationSchema)
    .min(1, 'obligations must contain at least one entry'),
})

/**
 * Cross-field rules — leaf schemas can't see the full payload, so duplicate
 * detection lives here. Two checks:
 *
 *   1. Slugs are unique within the payload — without this, two rows with
 *      the same slug would silently collide on upsert.
 *   2. Topic tags within a single obligation are unique — defensive: the
 *      Postgres `text[]` column tolerates duplicates, but a duplicated
 *      tag in a curated JSON file is always a typo.
 */
export const ObligationsDataSchema = BaseSchema.superRefine((data, ctx) => {
  const slugs = new Set<string>()
  for (const o of data.obligations) {
    if (slugs.has(o.slug)) {
      ctx.addIssue({
        code: 'custom',
        path: ['obligations'],
        message: `duplicate slug ${o.slug}`,
      })
    }
    slugs.add(o.slug)

    const seenTags = new Set<string>()
    for (const tag of o.topicTags) {
      if (seenTags.has(tag)) {
        ctx.addIssue({
          code: 'custom',
          path: ['obligations'],
          message: `${o.slug}: duplicate topicTags entry ${tag}`,
        })
      }
      seenTags.add(tag)
    }
  }
})

export type ObligationsData = z.infer<typeof ObligationsDataSchema>
export type Obligation = ObligationsData['obligations'][number]

export type IngestObligationsResult = {
  obligationsUpserted: number
}

/**
 * Throws a `ZodError` with all collected issues if `raw` is malformed.
 * The ingest script entry point pretty-prints these so a curator can fix
 * the source JSON without spelunking through stack traces.
 */
export function parseObligationsData(raw: unknown): ObligationsData {
  return ObligationsDataSchema.parse(raw)
}

/**
 * Ingest a batch of obligations. Idempotent on `slug`: re-running with the
 * same payload produces the same row state. Re-running with a changed
 * payload updates content in place (no duplicate rows, no orphans). The
 * function never deletes existing rows it didn't touch — curated entries
 * removed from a later JSON snapshot stay in the DB. Same non-destructive
 * policy as the article-recital junction in ENT-48 and the guidelines
 * ingest in ENT-50.
 */
export async function ingestObligations(
  supabase: SupabaseClient,
  data: ObligationsData,
): Promise<IngestObligationsResult> {
  const rows = data.obligations.map((o) => toRow(o))

  const { data: returned, error } = await supabase
    .from('obligations')
    .upsert(rows, { onConflict: 'slug' })
    .select('slug')

  if (error || !returned) {
    throw new Error(
      `ingestObligations: upsert failed: ${error?.message ?? 'no rows returned'}`,
    )
  }

  return { obligationsUpserted: returned.length }
}

/**
 * Map a validated `Obligation` to the column shape of `public.obligations`.
 * The flattening of `citation` into `citation_celex`, `citation_kind`, and
 * the per-kind nullable columns mirrors the composite CHECK in the
 * migration — exporting this also makes it unit-testable in isolation.
 */
export function toRow(o: Obligation): {
  slug: string
  title: string
  summary: string
  citation_celex: string
  citation_kind: 'article' | 'recital' | 'annex'
  citation_article: number | null
  citation_recital: number | null
  citation_annex: string | null
  citation_paragraph: string | null
  applies_when: Record<string, unknown>
  severity: 'low' | 'medium' | 'high'
  due_within_days: number | null
  recurrence: string | null
  effective_date: string | null
  topic_tags: ReadonlyArray<string>
} {
  const base = {
    slug: o.slug,
    title: o.title,
    summary: o.summary,
    citation_celex: o.citation.celex,
    citation_kind: o.citation.kind,
    citation_article: null as number | null,
    citation_recital: null as number | null,
    citation_annex: null as string | null,
    citation_paragraph: null as string | null,
    applies_when: o.appliesWhen,
    severity: o.severity,
    due_within_days: o.dueWithinDays ?? null,
    recurrence: o.recurrence ?? null,
    effective_date: o.effectiveDate ?? null,
    topic_tags: o.topicTags,
  }

  switch (o.citation.kind) {
    case 'article':
      base.citation_article = o.citation.articleNumber
      base.citation_paragraph = o.citation.paragraph ?? null
      break
    case 'recital':
      base.citation_recital = o.citation.recitalNumber
      break
    case 'annex':
      base.citation_annex = o.citation.annexLabel
      base.citation_paragraph = o.citation.paragraph ?? null
      break
  }

  return base
}
