import type { SupabaseClient } from '@supabase/supabase-js'
import { z } from 'zod'

/**
 * Idempotent ingestion of a regulatory document into the corpus (ENT-48).
 *
 * Acts on the schema introduced in `<ts>_regulatory_corpus.sql`:
 *
 *   regulatory_documents
 *     ↑ celex_number is the natural key — same regulation re-ingested is an
 *       upsert, not a duplicate.
 *
 *   regulatory_articles
 *     ↑ (document_id, article_number) is the natural key — re-ingest merges
 *       by article number; body changes overwrite (last write wins).
 *
 *   regulatory_recitals
 *     ↑ same shape as articles.
 *
 *   regulatory_article_recitals (junction)
 *     ↑ many-to-many between articles and recitals. The acceptance criterion
 *       calls for recitals "linkable from articles" — populated links are
 *       optional, the data structure is the deliverable.
 *
 * The caller supplies a Supabase client. In practice that is the service-role
 * client (the corpus tables have no INSERT/UPDATE/DELETE RLS policies for
 * non-service roles — see migration `<ts>_regulatory_corpus.sql`).
 *
 * Validation runs before any DB call: `parseRegulationData` rejects malformed
 * JSON, duplicate article/recital numbers within the payload, and junction
 * rows that reference articles or recitals not present in the same payload.
 * That keeps malformed snapshots from producing half-written corpus state.
 */

const DocumentSchema = z.object({
  title: z.string().min(1),
  shortTitle: z.string().min(1),
  celexNumber: z.string().min(1),
  versionDate: z
    .string()
    .regex(/^\d{4}-\d{2}-\d{2}$/, 'document.versionDate must be ISO date (YYYY-MM-DD)'),
  officialUrl: z.string().url('document.officialUrl must be a valid URL'),
})

const ArticleParagraphSchema = z.object({
  label: z.string().min(1),
  body: z.string().min(1),
  ordering: z.number().int().nonnegative(),
})

const ArticleSchema = z.object({
  articleNumber: z.number().int().positive(),
  heading: z.string().min(1),
  body: z.string().min(1),
  // Optional sub-paragraph rows for citation-grain Analyst output (ENT-95).
  // Articles without sub-paragraphs (most of GDPR + most AI Act articles)
  // simply omit the field; the body alone is the row.
  paragraphs: z.array(ArticleParagraphSchema).optional(),
})

const RecitalSchema = z.object({
  recitalNumber: z.number().int().positive(),
  body: z.string().min(1),
})

const ArticleRecitalLinkSchema = z.object({
  articleNumber: z.number().int().positive(),
  recitalNumber: z.number().int().positive(),
})

const BaseSchema = z.object({
  document: DocumentSchema,
  articles: z.array(ArticleSchema).min(1, 'at least one article is required'),
  recitals: z.array(RecitalSchema),
  articleRecitals: z.array(ArticleRecitalLinkSchema).optional(),
})

/**
 * Cross-field rules can't be expressed in the leaf schemas above (Zod's
 * .refine sees the full object). Three checks:
 *
 *   1. Article numbers are unique within the payload — without this, two
 *      rows with the same article_number would silently collide on upsert.
 *   2. Recital numbers are unique — same reasoning.
 *   3. Every junction entry references an article + recital that exists in
 *      the same payload — otherwise the DB insert would fail with an FK
 *      violation, but later, after partial work is done.
 */
export const RegulationDataSchema = BaseSchema.superRefine((data, ctx) => {
  const articleNumbers = new Set<number>()
  for (const article of data.articles) {
    if (articleNumbers.has(article.articleNumber)) {
      ctx.addIssue({
        code: 'custom',
        path: ['articles'],
        message: `duplicate articleNumber ${article.articleNumber}`,
      })
    }
    articleNumbers.add(article.articleNumber)

    // Paragraph labels must be unique within their parent article — the DB's
    // unique (article_id, paragraph_label) would catch this, but failing
    // here keeps half-written corpus state out of the picture.
    if (article.paragraphs) {
      const paragraphLabels = new Set<string>()
      for (const paragraph of article.paragraphs) {
        if (paragraphLabels.has(paragraph.label)) {
          ctx.addIssue({
            code: 'custom',
            path: ['articles'],
            message: `article ${article.articleNumber}: duplicate paragraph label ${paragraph.label}`,
          })
        }
        paragraphLabels.add(paragraph.label)
      }
    }
  }

  const recitalNumbers = new Set<number>()
  for (const recital of data.recitals) {
    if (recitalNumbers.has(recital.recitalNumber)) {
      ctx.addIssue({
        code: 'custom',
        path: ['recitals'],
        message: `duplicate recitalNumber ${recital.recitalNumber}`,
      })
    }
    recitalNumbers.add(recital.recitalNumber)
  }

  for (const link of data.articleRecitals ?? []) {
    if (!articleNumbers.has(link.articleNumber)) {
      ctx.addIssue({
        code: 'custom',
        path: ['articleRecitals'],
        message: `articleRecitals references unknown articleNumber ${link.articleNumber}`,
      })
    }
    if (!recitalNumbers.has(link.recitalNumber)) {
      ctx.addIssue({
        code: 'custom',
        path: ['articleRecitals'],
        message: `articleRecitals references unknown recitalNumber ${link.recitalNumber}`,
      })
    }
  }
})

export type RegulationData = z.infer<typeof RegulationDataSchema>

export type IngestResult = {
  documentId: string
  articlesUpserted: number
  recitalsUpserted: number
  linksUpserted: number
  paragraphsUpserted: number
}

/**
 * Throws a `ZodError` with all collected issues if `raw` is malformed.
 * The script entry point pretty-prints these so a curator can fix the
 * source JSON without spelunking through stack traces.
 */
export function parseRegulationData(raw: unknown): RegulationData {
  return RegulationDataSchema.parse(raw)
}

/**
 * Ingest a regulation. Idempotent: re-running with the same payload produces
 * the same row state. Re-running with a changed payload updates content in
 * place (no duplicate rows, no orphans). The function never deletes existing
 * rows it didn't touch — curated junctions, future article amendments live
 * alongside it untouched.
 */
export async function ingestRegulation(
  supabase: SupabaseClient,
  data: RegulationData,
): Promise<IngestResult> {
  const documentId = await upsertDocument(supabase, data.document)
  const articleIdByNumber = await upsertArticles(supabase, documentId, data.articles)
  const recitalIdByNumber = await upsertRecitals(supabase, documentId, data.recitals)
  const paragraphsUpserted = await upsertArticleParagraphs(
    supabase,
    data.articles,
    articleIdByNumber,
  )

  let linksUpserted = 0
  if (data.articleRecitals && data.articleRecitals.length > 0) {
    linksUpserted = await upsertArticleRecitals(
      supabase,
      data.articleRecitals,
      articleIdByNumber,
      recitalIdByNumber,
    )
  }

  return {
    documentId,
    articlesUpserted: articleIdByNumber.size,
    recitalsUpserted: recitalIdByNumber.size,
    linksUpserted,
    paragraphsUpserted,
  }
}

async function upsertDocument(
  supabase: SupabaseClient,
  doc: RegulationData['document'],
): Promise<string> {
  const { data, error } = await supabase
    .from('regulatory_documents')
    .upsert(
      {
        celex_number: doc.celexNumber,
        title: doc.title,
        short_title: doc.shortTitle,
        version_date: doc.versionDate,
        official_url: doc.officialUrl,
      },
      { onConflict: 'celex_number' },
    )
    .select('id')
    .single()

  if (error || !data) {
    throw new Error(
      `ingestRegulation: document upsert failed: ${error?.message ?? 'no row returned'}`,
    )
  }
  return data.id as string
}

async function upsertArticles(
  supabase: SupabaseClient,
  documentId: string,
  articles: RegulationData['articles'],
): Promise<Map<number, string>> {
  const rows = articles.map((a) => ({
    document_id: documentId,
    article_number: a.articleNumber,
    heading: a.heading,
    body: a.body,
  }))

  const { data, error } = await supabase
    .from('regulatory_articles')
    .upsert(rows, { onConflict: 'document_id,article_number' })
    .select('id, article_number')

  if (error || !data) {
    throw new Error(
      `ingestRegulation: article upsert failed: ${error?.message ?? 'no rows returned'}`,
    )
  }

  const idByNumber = new Map<number, string>()
  for (const row of data as Array<{ id: string; article_number: number }>) {
    idByNumber.set(row.article_number, row.id)
  }
  return idByNumber
}

async function upsertRecitals(
  supabase: SupabaseClient,
  documentId: string,
  recitals: RegulationData['recitals'],
): Promise<Map<number, string>> {
  if (recitals.length === 0) return new Map()

  const rows = recitals.map((r) => ({
    document_id: documentId,
    recital_number: r.recitalNumber,
    body: r.body,
  }))

  const { data, error } = await supabase
    .from('regulatory_recitals')
    .upsert(rows, { onConflict: 'document_id,recital_number' })
    .select('id, recital_number')

  if (error || !data) {
    throw new Error(
      `ingestRegulation: recital upsert failed: ${error?.message ?? 'no rows returned'}`,
    )
  }

  const idByNumber = new Map<number, string>()
  for (const row of data as Array<{ id: string; recital_number: number }>) {
    idByNumber.set(row.recital_number, row.id)
  }
  return idByNumber
}

async function upsertArticleParagraphs(
  supabase: SupabaseClient,
  articles: RegulationData['articles'],
  articleIdByNumber: ReadonlyMap<number, string>,
): Promise<number> {
  // Flatten all (article, paragraphs[]) pairs into a single upsert. One round
  // trip instead of one-per-article keeps the ingest cheap even at AI-Act
  // scale where ~13 articles get expanded.
  const rows: Array<{
    article_id: string
    paragraph_label: string
    body: string
    ordering: number
  }> = []
  for (const article of articles) {
    if (!article.paragraphs || article.paragraphs.length === 0) continue
    const articleId = articleIdByNumber.get(article.articleNumber)
    if (!articleId) {
      throw new Error(
        `ingestRegulation: cannot upsert paragraphs for article ${article.articleNumber} — id not in map`,
      )
    }
    for (const paragraph of article.paragraphs) {
      rows.push({
        article_id: articleId,
        paragraph_label: paragraph.label,
        body: paragraph.body,
        ordering: paragraph.ordering,
      })
    }
  }

  if (rows.length === 0) return 0

  const { data, error } = await supabase
    .from('regulatory_article_paragraphs')
    .upsert(rows, { onConflict: 'article_id,paragraph_label' })
    .select('article_id')

  if (error) {
    throw new Error(`ingestRegulation: paragraph upsert failed: ${error.message}`)
  }
  return data?.length ?? 0
}

async function upsertArticleRecitals(
  supabase: SupabaseClient,
  links: ReadonlyArray<{ articleNumber: number; recitalNumber: number }>,
  articleIdByNumber: ReadonlyMap<number, string>,
  recitalIdByNumber: ReadonlyMap<number, string>,
): Promise<number> {
  const rows = links.map((link) => {
    const articleId = articleIdByNumber.get(link.articleNumber)
    const recitalId = recitalIdByNumber.get(link.recitalNumber)
    if (!articleId || !recitalId) {
      // parseRegulationData should have caught this; defence in depth.
      throw new Error(
        `ingestRegulation: junction references missing id (article ${link.articleNumber}, recital ${link.recitalNumber})`,
      )
    }
    return { article_id: articleId, recital_id: recitalId }
  })

  const { data, error } = await supabase
    .from('regulatory_article_recitals')
    .upsert(rows, { onConflict: 'article_id,recital_id' })
    .select('article_id')

  if (error) {
    throw new Error(`ingestRegulation: junction upsert failed: ${error.message}`)
  }
  return data?.length ?? 0
}
