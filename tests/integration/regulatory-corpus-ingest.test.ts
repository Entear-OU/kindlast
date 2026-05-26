// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { ingestRegulation, type RegulationData } from '@/lib/corpus/ingest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'

/**
 * ENT-48 — End-to-end idempotency for `ingestRegulation`.
 *
 * The unit suite (`__tests__/lib/corpus/ingest.test.ts`) covers the front
 * gate (Zod validation). Here we prove the property that actually matters
 * for the acceptance criterion: re-running ingestion against the same
 * source data does not duplicate rows, and a content change overwrites
 * in place (last write wins).
 *
 * Coverage:
 *   1. First ingest inserts the expected row counts and returns matching
 *      stats.
 *   2. Second ingest with the same payload leaves row counts unchanged.
 *   3. Re-ingest with a changed article body updates the row in place
 *      (no duplicate, no orphan).
 *   4. Junction rows are populated when `articleRecitals` is supplied,
 *      and re-ingest does not duplicate them either.
 *   5. Pre-existing curated junction rows survive a content re-ingest
 *      that does not include `articleRecitals` (no destructive cleanup).
 *
 * Uses the service-role client throughout, matching the production
 * ingest path (corpus tables have no INSERT policy for non-service roles).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_CELEX = '_TEST_ENT48_ingest_celex'

const samplePayload = (): RegulationData => ({
  document: {
    title: 'Test regulation (Entear test fixture)',
    shortTitle: 'Test Reg',
    celexNumber: FIXTURE_CELEX,
    versionDate: '2016-05-04',
    officialUrl: 'https://example.invalid/test-regulation',
  },
  articles: [
    { articleNumber: 1, heading: 'Subject-matter', body: 'Article 1 body — initial.' },
    { articleNumber: 2, heading: 'Material scope', body: 'Article 2 body — initial.' },
    { articleNumber: 3, heading: 'Territorial scope', body: 'Article 3 body — initial.' },
  ],
  recitals: [
    { recitalNumber: 1, body: 'Recital 1 body.' },
    { recitalNumber: 2, body: 'Recital 2 body.' },
  ],
})

describe.skipIf(!supabaseRunning)('ingestRegulation (ENT-48)', () => {
  beforeAll(async () => {
    await applyFixtureSql(
      `delete from public.regulatory_documents where celex_number = '${FIXTURE_CELEX}';`,
    )
  })

  afterAll(async () => {
    await applyFixtureSql(
      `delete from public.regulatory_documents where celex_number = '${FIXTURE_CELEX}';`,
    )
  })

  it('inserts a document, articles, and recitals on first run', async () => {
    const service = createServiceRoleClient()
    const result = await ingestRegulation(service, samplePayload())

    expect(result.documentId).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
    )
    expect(result.articlesUpserted).toBe(3)
    expect(result.recitalsUpserted).toBe(2)
    expect(result.linksUpserted).toBe(0)

    const counts = await querySql<{ articles: number; recitals: number }>(
      `select
         (select count(*)::int from public.regulatory_articles where document_id = $1) as articles,
         (select count(*)::int from public.regulatory_recitals where document_id = $1) as recitals`,
      [result.documentId],
    )
    expect(counts[0]).toEqual({ articles: 3, recitals: 2 })
  })

  it('is idempotent: a second identical run does not duplicate rows', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const before = await querySql<{ total_articles: number; total_recitals: number }>(
      `select
         (select count(*)::int from public.regulatory_articles a
            join public.regulatory_documents d on d.id = a.document_id
            where d.celex_number = $1) as total_articles,
         (select count(*)::int from public.regulatory_recitals r
            join public.regulatory_documents d on d.id = r.document_id
            where d.celex_number = $1) as total_recitals`,
      [FIXTURE_CELEX],
    )

    await ingestRegulation(service, samplePayload())

    const after = await querySql<{ total_articles: number; total_recitals: number }>(
      `select
         (select count(*)::int from public.regulatory_articles a
            join public.regulatory_documents d on d.id = a.document_id
            where d.celex_number = $1) as total_articles,
         (select count(*)::int from public.regulatory_recitals r
            join public.regulatory_documents d on d.id = r.document_id
            where d.celex_number = $1) as total_recitals`,
      [FIXTURE_CELEX],
    )

    expect(after[0]).toEqual(before[0])
  })

  it('overwrites article body in place when content changes (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const changed = samplePayload()
    changed.articles[0]!.body = 'Article 1 body — REVISED text.'
    await ingestRegulation(service, changed)

    const rows = await querySql<{ body: string }>(
      `select a.body
         from public.regulatory_articles a
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1 and a.article_number = 1`,
      [FIXTURE_CELEX],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.body).toBe('Article 1 body — REVISED text.')

    // Sanity: still only one row for that article number.
    const cnt = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_articles a
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1 and a.article_number = 1`,
      [FIXTURE_CELEX],
    )
    expect(cnt[0]!.c).toBe(1)
  })

  it('populates the article-recital junction when articleRecitals is supplied', async () => {
    const service = createServiceRoleClient()
    const payload = samplePayload()
    payload.articleRecitals = [
      { articleNumber: 1, recitalNumber: 1 },
      { articleNumber: 1, recitalNumber: 2 },
      { articleNumber: 2, recitalNumber: 1 },
    ]
    const result = await ingestRegulation(service, payload)
    expect(result.linksUpserted).toBe(3)

    const links = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_article_recitals ar
         join public.regulatory_articles a on a.id = ar.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1`,
      [FIXTURE_CELEX],
    )
    expect(links[0]!.c).toBe(3)

    // Re-ingest the same junction set — still 3 rows.
    await ingestRegulation(service, payload)
    const linksAfter = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_article_recitals ar
         join public.regulatory_articles a on a.id = ar.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1`,
      [FIXTURE_CELEX],
    )
    expect(linksAfter[0]!.c).toBe(3)
  })

  it('does not delete existing junction rows on a content-only re-ingest', async () => {
    const service = createServiceRoleClient()
    const seeded = samplePayload()
    seeded.articleRecitals = [{ articleNumber: 1, recitalNumber: 1 }]
    await ingestRegulation(service, seeded)

    // Re-ingest the document + articles + recitals only (no articleRecitals).
    // Curated junction rows must survive — the script must not assume
    // article_recitals is "owned" by the snapshot.
    await ingestRegulation(service, samplePayload())

    const links = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_article_recitals ar
         join public.regulatory_articles a on a.id = ar.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1`,
      [FIXTURE_CELEX],
    )
    expect(links[0]!.c).toBeGreaterThanOrEqual(1)
  })
})
