// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { ingestGuidelines, type GuidelinesData } from '@/lib/corpus/guidelines'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'

/**
 * ENT-50 — End-to-end idempotency for `ingestGuidelines`.
 *
 * The unit suite (`__tests__/lib/corpus/guidelines.test.ts`) covers the
 * front gate (Zod validation). Here we prove the property that actually
 * matters for the acceptance criterion: re-running ingestion against the
 * same source data does not duplicate rows, content changes overwrite in
 * place, and `topic_tags` round-trip through the `text[]` column.
 *
 * Coverage:
 *   1. First ingest inserts the expected row count and returns matching stats.
 *   2. Second ingest with the same payload leaves row counts unchanged.
 *   3. Re-ingest with a changed title / topic_tags updates the row in place.
 *   4. `topic_tags` round-trips through the Postgres `text[]` column.
 *
 * Uses the service-role client throughout, matching the production
 * ingest path (corpus tables have no INSERT policy for non-service roles).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_PREFIX = '_test_ent50_'

const samplePayload = (): GuidelinesData => ({
  guidelines: [
    {
      slug: `${FIXTURE_PREFIX}consent`,
      publisher: 'EDPB',
      title: 'Test fixture — consent guideline',
      adoptedDate: '2020-05-04',
      version: '1.0',
      sourceUrl: 'https://example.invalid/consent',
      topicTags: ['consent', 'lawful-basis'],
    },
    {
      slug: `${FIXTURE_PREFIX}transfers`,
      publisher: 'EDPB',
      title: 'Test fixture — transfers guideline',
      adoptedDate: '2023-02-14',
      sourceUrl: 'https://example.invalid/transfers',
      topicTags: ['transfers'],
    },
    {
      slug: `${FIXTURE_PREFIX}dpo`,
      publisher: 'WP29',
      title: 'Test fixture — DPO guideline',
      adoptedDate: '2017-04-05',
      version: 'rev.01',
      sourceUrl: 'https://example.invalid/dpo',
      topicTags: ['dpo'],
    },
  ],
})

describe.skipIf(!supabaseRunning)('ingestGuidelines (ENT-50)', () => {
  beforeAll(async () => {
    await applyFixtureSql(
      `delete from public.regulatory_guidelines where slug like '${FIXTURE_PREFIX}%';`,
    )
  })

  afterAll(async () => {
    await applyFixtureSql(
      `delete from public.regulatory_guidelines where slug like '${FIXTURE_PREFIX}%';`,
    )
  })

  it('inserts the expected guideline rows on first run', async () => {
    const service = createServiceRoleClient()
    const result = await ingestGuidelines(service, samplePayload())

    expect(result.guidelinesUpserted).toBe(3)

    const counts = await querySql<{ c: number }>(
      `select count(*)::int as c from public.regulatory_guidelines where slug like $1`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(counts[0]!.c).toBe(3)
  })

  it('is idempotent: a second identical run does not duplicate rows', async () => {
    const service = createServiceRoleClient()
    await ingestGuidelines(service, samplePayload())
    await ingestGuidelines(service, samplePayload())

    const counts = await querySql<{ c: number }>(
      `select count(*)::int as c from public.regulatory_guidelines where slug like $1`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(counts[0]!.c).toBe(3)
  })

  it('overwrites title + topic_tags in place when content changes (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestGuidelines(service, samplePayload())

    const changed = samplePayload()
    changed.guidelines[0]!.title = 'Test fixture — consent guideline (REVISED)'
    changed.guidelines[0]!.topicTags = ['consent', 'lawful-basis', 'cookies']
    await ingestGuidelines(service, changed)

    const rows = await querySql<{ title: string; topic_tags: string[] }>(
      `select title, topic_tags from public.regulatory_guidelines where slug = $1`,
      [`${FIXTURE_PREFIX}consent`],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.title).toBe('Test fixture — consent guideline (REVISED)')
    expect(rows[0]!.topic_tags).toEqual(['consent', 'lawful-basis', 'cookies'])
  })

  it('round-trips topic_tags through the Postgres text[] column', async () => {
    const service = createServiceRoleClient()
    await ingestGuidelines(service, samplePayload())

    // GIN index on topic_tags should let `@>` containment queries work.
    const rows = await querySql<{ slug: string }>(
      `select slug from public.regulatory_guidelines
        where slug like $1 and topic_tags @> array['transfers']::text[]`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(rows.map((r) => r.slug)).toEqual([`${FIXTURE_PREFIX}transfers`])
  })

  it('stores publisher faithfully (EDPB vs WP29)', async () => {
    const service = createServiceRoleClient()
    await ingestGuidelines(service, samplePayload())

    const rows = await querySql<{ slug: string; publisher: string }>(
      `select slug, publisher from public.regulatory_guidelines
        where slug like $1 order by slug`,
      [`${FIXTURE_PREFIX}%`],
    )
    const bySlug = new Map(rows.map((r) => [r.slug, r.publisher]))
    expect(bySlug.get(`${FIXTURE_PREFIX}consent`)).toBe('EDPB')
    expect(bySlug.get(`${FIXTURE_PREFIX}dpo`)).toBe('WP29')
  })
})
