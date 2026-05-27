// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { ingestObligations, type ObligationsData } from '@/lib/corpus/obligations'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createAnonClient,
  createServiceRoleClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'

/**
 * ENT-52 — End-to-end coverage for the obligations catalogue.
 *
 * The unit suite (`__tests__/lib/corpus/obligations.test.ts`) covers the
 * front gate (Zod validation, citation discriminator). Here we prove the
 * properties that actually matter for the acceptance criterion:
 *
 *   1. First ingest inserts the expected row count.
 *   2. Second ingest with the same payload leaves row count unchanged
 *      (idempotency on `slug`).
 *   3. Content changes (summary, applies_when, topic_tags) overwrite the
 *      row in place (last write wins).
 *   4. JSONB `applies_when` round-trips, and a GIN-backed @> containment
 *      query returns the right rows.
 *   5. `topic_tags` round-trip through the Postgres `text[]` column.
 *   6. The composite CHECK on `citation_kind` rejects mismatched citations
 *      (defence-in-depth — the Zod validator catches these first).
 *   7. RLS: public SELECT works; anon INSERT is denied.
 *
 * Uses the service-role client throughout, matching the production
 * ingest path (the table has no INSERT policy for non-service roles).
 */

const supabaseRunning = await isLocalSupabaseReachable()

const FIXTURE_PREFIX = '_test_ent52_'

const ROPA_SUMMARY =
  'Article 30 requires controllers (and processors) to maintain a written record of processing activities — purpose, categories of data subjects, recipients, transfers, retention. The 250-employee SME exemption is narrow and rarely useful in practice.'
const DPIA_SUMMARY =
  'Article 35 requires controllers to carry out a Data Protection Impact Assessment before processing that is likely to result in a high risk to natural persons. Triggers include automated decisions with legal effect, large-scale special-category processing, and systematic monitoring of public spaces.'
const ANNEX_SUMMARY =
  'Annex III of the EU AI Act enumerates standalone AI use cases the Regulation designates high-risk under Article 6(2). Annex III high-risk obligations (Articles 9–17 stack) apply from 2 August 2026 unless the Digital Omnibus shifts the date.'

const samplePayload = (): ObligationsData => ({
  obligations: [
    {
      slug: `${FIXTURE_PREFIX}ropa`,
      title: 'Fixture — Records of Processing Activities',
      summary: ROPA_SUMMARY,
      citation: {
        kind: 'article',
        celex: '32016R0679',
        articleNumber: 30,
      },
      appliesWhen: { role: 'controller' },
      severity: 'high',
      recurrence: 'continuous',
      effectiveDate: '2018-05-25',
      topicTags: ['ropa', 'documentation'],
    },
    {
      slug: `${FIXTURE_PREFIX}dpia`,
      title: 'Fixture — DPIA for high-risk processing',
      summary: DPIA_SUMMARY,
      citation: {
        kind: 'article',
        celex: '32016R0679',
        articleNumber: 35,
      },
      appliesWhen: { role: 'controller', thresholds: { high_risk: true } },
      severity: 'high',
      recurrence: 'ad-hoc',
      effectiveDate: '2018-05-25',
      topicTags: ['dpia', 'risk'],
    },
    {
      slug: `${FIXTURE_PREFIX}annex-iii`,
      title: 'Fixture — Annex III high-risk system obligations',
      summary: ANNEX_SUMMARY,
      citation: {
        kind: 'annex',
        celex: '32024R1689',
        annexLabel: 'III',
      },
      appliesWhen: { role: 'provider', thresholds: { high_risk: true } },
      severity: 'high',
      effectiveDate: '2026-08-02',
      topicTags: ['ai-act', 'high-risk'],
    },
  ],
})

describe.skipIf(!supabaseRunning)('ingestObligations (ENT-52)', () => {
  beforeAll(async () => {
    await applyFixtureSql(
      `delete from public.obligations where slug like '${FIXTURE_PREFIX}%';`,
    )
  })

  afterAll(async () => {
    await applyFixtureSql(
      `delete from public.obligations where slug like '${FIXTURE_PREFIX}%';`,
    )
  })

  it('inserts the expected obligation rows on first run', async () => {
    const service = createServiceRoleClient()
    const result = await ingestObligations(service, samplePayload())
    expect(result.obligationsUpserted).toBe(3)

    const counts = await querySql<{ c: number }>(
      `select count(*)::int as c from public.obligations where slug like $1`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(counts[0]!.c).toBe(3)
  })

  it('is idempotent: a second identical run does not duplicate rows', async () => {
    const service = createServiceRoleClient()
    await ingestObligations(service, samplePayload())
    await ingestObligations(service, samplePayload())

    const counts = await querySql<{ c: number }>(
      `select count(*)::int as c from public.obligations where slug like $1`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(counts[0]!.c).toBe(3)
  })

  it('overwrites summary + applies_when + topic_tags in place (last write wins)', async () => {
    const service = createServiceRoleClient()
    await ingestObligations(service, samplePayload())

    const REVISED_SUMMARY =
      'REVISED fixture — Article 30 ROPA. Curators updated this summary to reflect a refined SME-exemption interpretation; the row must overwrite in place rather than spawning a duplicate.'

    const changed = samplePayload()
    changed.obligations[0]!.summary = REVISED_SUMMARY
    changed.obligations[0]!.appliesWhen = {
      role: 'controller',
      thresholds: { employees_min: 250 },
    }
    changed.obligations[0]!.topicTags = ['ropa', 'documentation', 'sme']

    await ingestObligations(service, changed)

    const rows = await querySql<{
      summary: string
      applies_when: Record<string, unknown>
      topic_tags: string[]
    }>(
      `select summary, applies_when, topic_tags
         from public.obligations where slug = $1`,
      [`${FIXTURE_PREFIX}ropa`],
    )
    expect(rows).toHaveLength(1)
    expect(rows[0]!.summary).toBe(REVISED_SUMMARY)
    expect(rows[0]!.applies_when).toEqual({
      role: 'controller',
      thresholds: { employees_min: 250 },
    })
    expect(rows[0]!.topic_tags).toEqual(['ropa', 'documentation', 'sme'])
  })

  it('rounds JSONB applies_when through a containment query', async () => {
    const service = createServiceRoleClient()
    await ingestObligations(service, samplePayload())

    // The Watcher's trigger predicate will look like
    // `applies_when @> '{"role":"provider"}'`. Verify the GIN index path.
    const rows = await querySql<{ slug: string }>(
      `select slug from public.obligations
        where slug like $1 and applies_when @> $2::jsonb`,
      [`${FIXTURE_PREFIX}%`, JSON.stringify({ role: 'provider' })],
    )
    expect(rows.map((r) => r.slug)).toEqual([`${FIXTURE_PREFIX}annex-iii`])
  })

  it('rounds topic_tags through the Postgres text[] column (GIN containment)', async () => {
    const service = createServiceRoleClient()
    await ingestObligations(service, samplePayload())

    const rows = await querySql<{ slug: string }>(
      `select slug from public.obligations
        where slug like $1 and topic_tags @> array['dpia']::text[]`,
      [`${FIXTURE_PREFIX}%`],
    )
    expect(rows.map((r) => r.slug)).toEqual([`${FIXTURE_PREFIX}dpia`])
  })

  it('stores the recital citation kind correctly when used', async () => {
    const service = createServiceRoleClient()
    const payload: ObligationsData = {
      obligations: [
        {
          slug: `${FIXTURE_PREFIX}recital`,
          title: 'Fixture — Recital citation',
          summary:
            'A recital-grain obligation fixture. Real obligations are almost always articles, but the schema permits recital-grain citations for cases where the binding language sits in a recital — useful for argumentation, not deadlines.',
          citation: {
            kind: 'recital',
            celex: '32016R0679',
            recitalNumber: 39,
          },
          appliesWhen: {},
          severity: 'low',
          topicTags: ['transparency'],
        },
      ],
    }
    await ingestObligations(service, payload)

    const rows = await querySql<{
      citation_kind: string
      citation_recital: number | null
      citation_article: number | null
      citation_annex: string | null
    }>(
      `select citation_kind, citation_recital, citation_article, citation_annex
         from public.obligations where slug = $1`,
      [`${FIXTURE_PREFIX}recital`],
    )
    expect(rows[0]!.citation_kind).toBe('recital')
    expect(rows[0]!.citation_recital).toBe(39)
    expect(rows[0]!.citation_article).toBeNull()
    expect(rows[0]!.citation_annex).toBeNull()

    await applyFixtureSql(
      `delete from public.obligations where slug = '${FIXTURE_PREFIX}recital';`,
    )
  })

  describe('database CHECK — citation matches kind (defence-in-depth)', () => {
    // The Zod validator catches kind/column mismatches first; this proves
    // the DB rejects them too if anything bypassed the validator.
    it('rejects citation_kind=article with citation_recital set', async () => {
      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.obligations
            (slug, title, summary, citation_celex, citation_kind,
             citation_article, citation_recital, applies_when)
          values (
            '${FIXTURE_PREFIX}bad_kind',
            'bad',
            'a summary that satisfies the 100-character floor so the only thing failing this insert is the citation kind/column mismatch we want to assert here.',
            '32016R0679', 'article', 30, 1, '{}'::jsonb
          );
        `),
      ).rejects.toThrow(/citation/i)
    })

    it('rejects citation_kind=annex without citation_annex', async () => {
      await expect(
        applyFixtureSql(/* sql */ `
          insert into public.obligations
            (slug, title, summary, citation_celex, citation_kind, applies_when)
          values (
            '${FIXTURE_PREFIX}bad_kind2',
            'bad',
            'a summary that satisfies the 100-character floor so the only thing failing this insert is the citation kind/column mismatch we want to assert here.',
            '32024R1689', 'annex', '{}'::jsonb
          );
        `),
      ).rejects.toThrow(/citation/i)
    })
  })

  describe('row-level security', () => {
    it('allows anon read', async () => {
      const service = createServiceRoleClient()
      await ingestObligations(service, samplePayload())

      const anon = createAnonClient()
      const { data, error } = await anon
        .from('obligations')
        .select('slug, citation_kind')
        .like('slug', `${FIXTURE_PREFIX}%`)
        .order('slug')
      expect(error).toBeNull()
      expect(data).toHaveLength(3)
    })

    it('denies anon insert', async () => {
      const anon = createAnonClient()
      const { error } = await anon.from('obligations').insert({
        slug: `${FIXTURE_PREFIX}anon_should_not_write`,
        title: 'nope',
        summary:
          'A long-enough fixture summary that clears the CHECK so the only reason this insert is rejected is the row-level security policy, not the length constraint.',
        citation_celex: '32016R0679',
        citation_kind: 'article',
        citation_article: 30,
      })
      expect(error).not.toBeNull()
      expect(error?.message.toLowerCase()).toMatch(/row-level security|policy|permission/)
    })
  })
})
