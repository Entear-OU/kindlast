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

// Fixture summaries ≥100 chars so they clear the Zod validator floor.
const ART_6_SUMMARY =
  'Article 6 (fixture) — Classification rules. Numbered paragraphs with letter sub-points; mirrors the AI Act Article 6 shape used by the paragraph ingest idempotency suite.'
const ART_99_SUMMARY =
  'Article 99 (fixture) — No-paragraph article. Used to exercise the "article has no paragraphs[] field" branch of the paragraph ingest path.'
const PARA_1_SUMMARY =
  'Paragraph 1 (fixture) — Lead-in sentence introducing the classification rule structure used by the paragraph ingest idempotency suite.'
const PARA_1A_SUMMARY =
  'Paragraph 1(a) (fixture) — sub-point a content describing the first carve-out under the paragraph ingest fixture for ENT-95.'
const PARA_1B_SUMMARY =
  'Paragraph 1(b) (fixture) — sub-point b content describing the second carve-out under the paragraph ingest fixture for ENT-95.'
const PARA_2_SUMMARY =
  'Paragraph 2 (fixture) — second numbered paragraph for the paragraph ingest fixture used by the ingest idempotency suite.'
const REC_1_SUMMARY =
  'Recital 1 (fixture) — recital row used to satisfy the at-least-one-recital path in the paragraph ingest test fixture.'

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
      summary: ART_6_SUMMARY,
      paragraphs: [
        { label: '1', summary: PARA_1_SUMMARY, ordering: 1 },
        { label: '1(a)', summary: PARA_1A_SUMMARY, ordering: 2 },
        { label: '1(b)', summary: PARA_1B_SUMMARY, ordering: 3 },
        { label: '2', summary: PARA_2_SUMMARY, ordering: 4 },
      ],
    },
    {
      articleNumber: 99,
      heading: 'No-paragraph article',
      summary: ART_99_SUMMARY,
    },
  ],
  recitals: [{ recitalNumber: 1, summary: REC_1_SUMMARY }],
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

  it('overwrites paragraph summary in place when content changes (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const REVISED_SUMMARY =
      'Paragraph 1(a) (fixture) — REVISED sub-point a summary, updated by a curator to reflect new guidance. Row must overwrite in place.'
    const changed = samplePayload()
    changed.articles[0]!.paragraphs![1]!.summary = REVISED_SUMMARY
    await ingestRegulation(service, changed)

    const rows = await querySql<{ summary: string }>(
      `select p.summary
         from public.regulatory_article_paragraphs p
         join public.regulatory_articles a on a.id = p.article_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1
           and a.article_number = 6
           and p.paragraph_label = $2`,
      [FIXTURE_CELEX, '1(a)'],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.summary).toBe(REVISED_SUMMARY)
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
