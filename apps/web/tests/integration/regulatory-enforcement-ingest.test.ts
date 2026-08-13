// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  ingestEnforcementDecisions,
  type EnforcementData,
} from '@/lib/corpus/enforcement'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import { createAnonClient, createServiceRoleClient, isLocalSupabaseReachable } from './helpers/supabase'

/**
 * ENT-99 — End-to-end idempotency for `ingestEnforcementDecisions`.
 *
 * The unit suite (`__tests__/lib/corpus/enforcement.test.ts`) covers the
 * front gate (Zod validation). Here we prove the properties that matter for
 * the acceptance criterion: re-running ingestion does not duplicate rows,
 * content changes overwrite in place, the `int[]` (gdpr_articles) and
 * `text[]` (topic_tags) columns round-trip with `@>` containment, and RLS
 * lets anon read but not write.
 *
 * Uses the service-role client for writes, matching the production ingest
 * path (corpus tables have no INSERT policy for non-service roles).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_PREFIX = '_test_ent99_'

const samplePayload = (): EnforcementData => ({
  decisions: [
    {
      slug: `${FIXTURE_PREFIX}clearview`,
      dpa: 'CNIL',
      title: 'Test fixture — Clearview biometrics',
      decisionDate: '2022-10-17',
      fineEur: 20_000_000,
      summary:
        'Fixture decision covering biometric scraping without lawful basis under Articles 6 and 9, used by the enforcement-ingest idempotency suite. Long enough to clear the summary length floor.',
      sourceUrl: 'https://example.invalid/clearview',
      gdprArticles: [6, 9, 17],
      topicTags: ['biometrics', 'lawful-basis'],
    },
    {
      slug: `${FIXTURE_PREFIX}ba`,
      dpa: 'ICO',
      title: 'Test fixture — security breach',
      decisionDate: '2020-10-16',
      summary:
        'Fixture decision covering an Article 32 security failing with no monetary fine attached, used to exercise the nullable fine_eur path in the enforcement-ingest idempotency suite.',
      sourceUrl: 'https://example.invalid/ba',
      gdprArticles: [32],
      topicTags: ['security', 'breach'],
    },
  ],
})

describe.skipIf(!supabaseRunning)('ingestEnforcementDecisions (ENT-99)', () => {
  beforeAll(async () => {
    await applyFixtureSql(
      `delete from public.regulatory_enforcement_decisions where slug like '${FIXTURE_PREFIX}%';`,
    )
  })

  afterAll(async () => {
    await applyFixtureSql(
      `delete from public.regulatory_enforcement_decisions where slug like '${FIXTURE_PREFIX}%';`,
    )
  })

  it('inserts the expected decision rows on first run', async () => {
    const service = createServiceRoleClient()
    const result = await ingestEnforcementDecisions(service, samplePayload())
    expect(result.decisionsUpserted).toBe(2)

    const counts = await querySql<{ c: number }>(
      `select count(*)::int as c from public.regulatory_enforcement_decisions where slug like $1`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(counts[0]!.c).toBe(2)
  })

  it('stores a null fine for decisions without one', async () => {
    const service = createServiceRoleClient()
    await ingestEnforcementDecisions(service, samplePayload())

    const rows = await querySql<{ slug: string; fine_eur: number | null }>(
      `select slug, fine_eur from public.regulatory_enforcement_decisions
        where slug like $1 order by slug`,
      [`${FIXTURE_PREFIX}%`],
    )
    const bySlug = new Map(rows.map((r) => [r.slug, r.fine_eur]))
    expect(bySlug.get(`${FIXTURE_PREFIX}ba`)).toBeNull()
    expect(Number(bySlug.get(`${FIXTURE_PREFIX}clearview`))).toBe(20_000_000)
  })

  it('is idempotent: a second identical run does not duplicate rows', async () => {
    const service = createServiceRoleClient()
    await ingestEnforcementDecisions(service, samplePayload())
    await ingestEnforcementDecisions(service, samplePayload())

    const counts = await querySql<{ c: number }>(
      `select count(*)::int as c from public.regulatory_enforcement_decisions where slug like $1`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(counts[0]!.c).toBe(2)
  })

  it('overwrites summary + topic_tags in place when content changes (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestEnforcementDecisions(service, samplePayload())

    const changed = samplePayload()
    changed.decisions[0]!.summary =
      'REVISED fixture summary for the Clearview decision, edited by a curator to reflect updated guidance. Still long enough to clear the summary length floor constraint.'
    changed.decisions[0]!.topicTags = ['biometrics', 'lawful-basis', 'dsar']
    await ingestEnforcementDecisions(service, changed)

    const rows = await querySql<{ summary: string; topic_tags: string[] }>(
      `select summary, topic_tags from public.regulatory_enforcement_decisions where slug = $1`,
      [`${FIXTURE_PREFIX}clearview`],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.summary).toContain('REVISED')
    expect(rows[0]!.topic_tags).toEqual(['biometrics', 'lawful-basis', 'dsar'])
  })

  it('round-trips gdpr_articles through the int[] column with @> containment', async () => {
    const service = createServiceRoleClient()
    await ingestEnforcementDecisions(service, samplePayload())

    // "How has Article 32 been enforced?" — the GIN index on gdpr_articles
    // makes this the Analyst's primary routing query.
    const rows = await querySql<{ slug: string }>(
      `select slug from public.regulatory_enforcement_decisions
        where slug like $1 and gdpr_articles @> array[32]::int[]`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(rows.map((r) => r.slug)).toEqual([`${FIXTURE_PREFIX}ba`])
  })

  it('round-trips topic_tags through the text[] column with @> containment', async () => {
    const service = createServiceRoleClient()
    await ingestEnforcementDecisions(service, samplePayload())

    const rows = await querySql<{ slug: string }>(
      `select slug from public.regulatory_enforcement_decisions
        where slug like $1 and topic_tags @> array['security']::text[]`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(rows.map((r) => r.slug)).toEqual([`${FIXTURE_PREFIX}ba`])
  })

  it('allows anon read but denies anon write (corpus RLS convention)', async () => {
    const service = createServiceRoleClient()
    await ingestEnforcementDecisions(service, samplePayload())

    const anon = createAnonClient()
    const { data, error: readError } = await anon
      .from('regulatory_enforcement_decisions')
      .select('slug, dpa')
      .eq('slug', `${FIXTURE_PREFIX}clearview`)
      .single()
    expect(readError).toBeNull()
    expect(data?.dpa).toBe('CNIL')

    const { error: writeError } = await anon.from('regulatory_enforcement_decisions').insert({
      slug: `${FIXTURE_PREFIX}should_not_write`,
      dpa: 'X',
      title: 'x',
      decision_date: '2020-01-01',
      summary: 'x'.repeat(120),
      source_url: 'https://example.invalid/x',
    })
    expect(writeError).not.toBeNull()
    expect(writeError?.message.toLowerCase()).toMatch(/row-level security|policy|permission/)
  })
})
