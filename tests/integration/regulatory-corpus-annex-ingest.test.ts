// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { ingestRegulation, type RegulationData } from '@/lib/corpus/ingest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'

/**
 * ENT-96 — End-to-end annex + effective-date ingest.
 *
 * Coverage:
 *   1. First ingest inserts annex + items + applies article.effective_date.
 *   2. Re-ingest with the same payload leaves row counts unchanged
 *      (idempotency).
 *   3. Item.effectiveDate overrides annex.effectiveDate at the row level.
 *   4. Content change to an annex item body overwrites in place.
 *   5. Article.effectiveDate flips back to null when removed from the payload
 *      and re-ingested.
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_CELEX = '_TEST_ENT96_annex_ingest'

// Fixture summaries ≥100 chars so they clear the progressive-disclosure
// length floor enforced by both the Zod schema and the DB CHECK.
const ANNEX_SUMMARY =
  'Annex III enumerates AI use cases the Regulation designates high-risk under Article 6(2). Listed systems trigger Articles 9–17 obligations.'
const ITEM_1_SUMMARY =
  'Category 1 — Biometrics: AI systems that identify, categorise, or recognise the emotional state of natural persons (fixture).'
const ITEM_1A_SUMMARY =
  'Annex III 1(a) — Remote biometric identification systems; excludes one-to-one verification used solely to confirm identity (fixture).'
const ITEM_2_SUMMARY =
  'Annex III 2 — Critical infrastructure: AI used as a safety component in road traffic, water, gas, heating, or electricity supply (fixture).'

const samplePayload = (): RegulationData => ({
  document: {
    title: 'Annex ingest fixture',
    shortTitle: 'AI Fixture',
    celexNumber: FIXTURE_CELEX,
    versionDate: '2024-07-12',
    officialUrl: 'https://example.invalid/a',
  },
  articles: [
    {
      articleNumber: 4,
      heading: 'AI literacy',
      body: 'Providers and deployers shall take measures to ensure literacy.',
      effectiveDate: '2025-02-02',
    },
    {
      articleNumber: 5,
      heading: 'Prohibited practices',
      body: 'No effective_date — falls back to document version_date.',
    },
  ],
  recitals: [{ recitalNumber: 1, body: 'Recital body.' }],
  annexes: [
    {
      label: 'III',
      heading: 'High-risk AI systems referred to in Article 6(2)',
      summary: ANNEX_SUMMARY,
      effectiveDate: '2026-08-02',
      items: [
        { label: '1', heading: 'Biometrics', summary: ITEM_1_SUMMARY, ordering: 1 },
        { label: '1(a)', summary: ITEM_1A_SUMMARY, ordering: 2 },
        { label: '2', heading: 'Critical infrastructure', summary: ITEM_2_SUMMARY, ordering: 3 },
      ],
    },
  ],
})

describe.skipIf(!supabaseRunning)('ingestRegulation — annexes + effective dates (ENT-96)', () => {
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

  it('inserts annex + items on first run and applies article.effective_date', async () => {
    const service = createServiceRoleClient()
    const result = await ingestRegulation(service, samplePayload())
    expect(result.annexesUpserted).toBe(1)
    expect(result.annexItemsUpserted).toBe(3)

    const annexRow = await querySql<{
      annex_label: string
      heading: string
      effective_date: string
      item_count: number
    }>(
      `select
         a.annex_label, a.heading, a.effective_date::text,
         (select count(*)::int from public.regulatory_annex_items where annex_id = a.id) as item_count
       from public.regulatory_annexes a
       join public.regulatory_documents d on d.id = a.document_id
       where d.celex_number = $1`,
      [FIXTURE_CELEX],
    )
    expect(annexRow[0]!.annex_label).toBe('III')
    expect(annexRow[0]!.effective_date).toBe('2026-08-02')
    expect(annexRow[0]!.item_count).toBe(3)

    const articleRows = await querySql<{ article_number: number; effective_date: string | null }>(
      `select a.article_number, a.effective_date::text
         from public.regulatory_articles a
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1
         order by a.article_number`,
      [FIXTURE_CELEX],
    )
    expect(articleRows[0]).toEqual({ article_number: 4, effective_date: '2025-02-02' })
    expect(articleRows[1]).toEqual({ article_number: 5, effective_date: null })
  })

  it('is idempotent: a second identical ingest leaves counts unchanged', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())
    await ingestRegulation(service, samplePayload())

    const counts = await querySql<{ annexes: number; items: number }>(
      `select
         (select count(*)::int from public.regulatory_annexes a
            join public.regulatory_documents d on d.id = a.document_id
            where d.celex_number = $1) as annexes,
         (select count(*)::int from public.regulatory_annex_items i
            join public.regulatory_annexes a on a.id = i.annex_id
            join public.regulatory_documents d on d.id = a.document_id
            where d.celex_number = $1) as items`,
      [FIXTURE_CELEX],
    )
    expect(counts[0]).toEqual({ annexes: 1, items: 3 })
  })

  it('overwrites annex item summary in place when content changes (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const REVISED_SUMMARY =
      'REVISED Annex III 1(a) — Remote biometric identification systems summary, updated by a curator to reflect new guidance (≥100 chars).'
    const changed = samplePayload()
    changed.annexes![0]!.items[1]!.summary = REVISED_SUMMARY
    await ingestRegulation(service, changed)

    const rows = await querySql<{ summary: string }>(
      `select i.summary
         from public.regulatory_annex_items i
         join public.regulatory_annexes a on a.id = i.annex_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1 and i.item_label = $2`,
      [FIXTURE_CELEX, '1(a)'],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.summary).toBe(REVISED_SUMMARY)
  })

  it('item.effectiveDate overrides the annex-level effectiveDate at the row level', async () => {
    const service = createServiceRoleClient()
    const payload = samplePayload()
    payload.annexes![0]!.items[0]!.effectiveDate = '2027-01-01'
    await ingestRegulation(service, payload)

    const rows = await querySql<{ item_label: string; effective_date: string | null }>(
      `select i.item_label, i.effective_date::text
         from public.regulatory_annex_items i
         join public.regulatory_annexes a on a.id = i.annex_id
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1
         order by i.ordering`,
      [FIXTURE_CELEX],
    )
    expect(rows.find((r) => r.item_label === '1')!.effective_date).toBe('2027-01-01')
    // Other items inherit (via reader normalisation) from the annex default;
    // the row column itself stays null.
    expect(rows.find((r) => r.item_label === '1(a)')!.effective_date).toBeNull()
  })

  it('flips article.effective_date back to null when removed from the payload', async () => {
    const service = createServiceRoleClient()
    await ingestRegulation(service, samplePayload())

    const cleared = samplePayload()
    delete cleared.articles[0]!.effectiveDate
    await ingestRegulation(service, cleared)

    const rows = await querySql<{ effective_date: string | null }>(
      `select a.effective_date::text
         from public.regulatory_articles a
         join public.regulatory_documents d on d.id = a.document_id
         where d.celex_number = $1 and a.article_number = 4`,
      [FIXTURE_CELEX],
    )
    expect(rows[0]!.effective_date).toBeNull()
  })
})
