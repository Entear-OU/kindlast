// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { ingestRegulation, type RegulationData } from '@/lib/corpus/ingest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'

/**
 * ENT-95 — End-to-end paragraph ingest idempotency.
 *
 * The sibling suite (`regulatory-corpus-ingest.test.ts`) proves the same
 * property for documents / articles / recitals. Paragraph upsert lands in
 * a different table (`regulatory_article_paragraphs`) with a different
 * natural key (`(article_id, paragraph_label)`) — exercising it directly
 * here keeps the regression surface explicit.
 *
 * Coverage:
 *   1. First ingest inserts paragraph rows and the result count matches.
 *   2. Second ingest with the same payload leaves paragraph count unchanged.
 *   3. Changing a paragraph body and re-ingesting overwrites in place
 *      (no duplicate row, no orphan).
 *   4. Articles without `paragraphs` produce zero paragraph rows.
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_CELEX = '_TEST_ENT95_paragraph_ingest'

const samplePayload = (): RegulationData => ({
  document: {
    title: 'Paragraph ingest fixture',
    shortTitle: 'PI Fixture',
    celexNumber: FIXTURE_CELEX,
    versionDate: '2024-07-12',
    officialUrl: 'https://example.invalid/p',
  },
  articles: [
    {
      articleNumber: 6,
      heading: 'Classification rules',
      body: 'Mirrors Article 6 shape: numbered paragraphs with letter sub-points.',
      paragraphs: [
        { label: '1', body: 'Lead-in sentence for paragraph 1.', ordering: 1 },
        { label: '1(a)', body: 'Sub-point a body — initial.', ordering: 2 },
        { label: '1(b)', body: 'Sub-point b body — initial.', ordering: 3 },
        { label: '2', body: 'Paragraph 2 body.', ordering: 4 },
      ],
    },
    {
      articleNumber: 99,
      heading: 'No-paragraph article',
      body: 'This article has no paragraphs field — exercises the no-paragraphs path.',
    },
  ],
  recitals: [{ recitalNumber: 1, body: 'Recital fixture body.' }],
})

describe.skipIf(!supabaseRunning)('ingestRegulation — paragraphs (ENT-95)', () => {
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

  it('inserts paragraph rows on first run and counts match', async () => {
    const service = createServiceRoleClient()
    const result = await ingestRegulation(service, samplePayload())
    expect(result.paragraphsUpserted).toBe(4)

    const rows = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_article_paragraphs p
         join public.regulatory_articles a on a.id = p.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1`,
      [FIXTURE_CELEX],
    )
    expect(rows[0]!.c).toBe(4)
  })

  it('is idempotent: a second identical ingest does not duplicate paragraph rows', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())
    await ingestRegulation(service, samplePayload())

    const rows = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_article_paragraphs p
         join public.regulatory_articles a on a.id = p.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1`,
      [FIXTURE_CELEX],
    )
    expect(rows[0]!.c).toBe(4)
  })

  it('overwrites paragraph body in place when content changes (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const changed = samplePayload()
    changed.articles[0]!.paragraphs![1]!.body = 'Sub-point a body — REVISED.'
    await ingestRegulation(service, changed)

    const rows = await querySql<{ body: string }>(
      `select p.body
         from public.regulatory_article_paragraphs p
         join public.regulatory_articles a on a.id = p.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1
           and a.article_number = 6
           and p.paragraph_label = $2`,
      [FIXTURE_CELEX, '1(a)'],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.body).toBe('Sub-point a body — REVISED.')
  })

  it('produces zero paragraph rows for articles without a paragraphs field', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const rows = await querySql<{ c: number }>(
      `select count(*)::int as c
         from public.regulatory_article_paragraphs p
         join public.regulatory_articles a on a.id = p.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1 and a.article_number = 99`,
      [FIXTURE_CELEX],
    )
    expect(rows[0]!.c).toBe(0)
  })
})
