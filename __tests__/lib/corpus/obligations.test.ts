import { describe, expect, it } from 'vitest'

import { parseObligationsData } from '@/lib/corpus/obligations'

/**
 * Unit coverage for the validator in `lib/corpus/obligations.ts` (ENT-52).
 *
 * The actual upsert path (`ingestObligations`) is exercised end-to-end
 * against the local Supabase stack in
 * `tests/integration/obligations-ingest.test.ts` — RLS, slug uniqueness,
 * topic_tags array round-trips, JSONB round-trip belong there.
 *
 * Here we just prove the front gate: the JSON snapshot at
 * `data/corpus/obligations.json` is rejected early when malformed, so
 * the ingest script never sends garbage to the DB.
 *
 * Catalogue rows reference the corpus by NATURAL KEY (CELEX + article
 * number, recital number, or annex label + item label). The validator
 * enforces the citation-kind discriminator so a row that declares
 * `citation.kind = 'article'` must carry `citation.articleNumber` (and
 * not `recitalNumber` / `annexLabel`).
 */

const validObligation = () => ({
  slug: 'gdpr-art-30-ropa',
  title: 'Records of Processing Activities',
  summary:
    'Article 30 requires controllers (and processors) to maintain a written record of processing activities — purpose, categories of data subjects, recipients, transfers, retention. The 250-employee SME exemption is narrow and rarely useful in practice.',
  citation: {
    kind: 'article' as const,
    celex: '32016R0679',
    articleNumber: 30,
  },
  appliesWhen: { role: 'controller' },
  severity: 'high' as const,
  topicTags: ['ropa', 'documentation'],
})

const validInput = () => ({
  obligations: [validObligation()],
})

describe('parseObligationsData', () => {
  it('accepts a well-formed payload', () => {
    const parsed = parseObligationsData(validInput())
    expect(parsed.obligations).toHaveLength(1)
    expect(parsed.obligations[0]!.slug).toBe('gdpr-art-30-ropa')
    expect(parsed.obligations[0]!.citation.kind).toBe('article')
  })

  it('accepts an annex citation with both annexLabel and an item paragraph', () => {
    const parsed = parseObligationsData({
      obligations: [
        {
          ...validObligation(),
          slug: 'ai-act-annex-iii-high-risk',
          title: 'High-risk AI system obligations (Annex III)',
          citation: {
            kind: 'annex' as const,
            celex: '32024R1689',
            annexLabel: 'III',
            paragraph: '1(a)',
          },
        },
      ],
    })
    expect(parsed.obligations[0]!.citation.kind).toBe('annex')
  })

  it('accepts a recital citation', () => {
    const parsed = parseObligationsData({
      obligations: [
        {
          ...validObligation(),
          slug: 'gdpr-recital-39-transparency',
          citation: {
            kind: 'recital' as const,
            celex: '32016R0679',
            recitalNumber: 39,
          },
        },
      ],
    })
    expect(parsed.obligations[0]!.citation.kind).toBe('recital')
  })

  it('accepts optional dueWithinDays, recurrence, and effectiveDate', () => {
    const parsed = parseObligationsData({
      obligations: [
        {
          ...validObligation(),
          slug: 'gdpr-art-33-breach',
          dueWithinDays: 0,
          recurrence: 'on-event',
          effectiveDate: '2018-05-25',
        },
      ],
    })
    expect(parsed.obligations[0]!.dueWithinDays).toBe(0)
    expect(parsed.obligations[0]!.recurrence).toBe('on-event')
    expect(parsed.obligations[0]!.effectiveDate).toBe('2018-05-25')
  })

  it('defaults severity to "medium" and appliesWhen to {} when omitted', () => {
    const minimal = {
      slug: 'minimal',
      title: 'Minimal fixture',
      summary:
        'Minimal obligation fixture used to verify the validator defaults — severity defaults to medium, appliesWhen defaults to an empty object so Watcher predicates always have a JSONB to evaluate.',
      citation: {
        kind: 'article' as const,
        celex: '32016R0679',
        articleNumber: 5,
      },
      topicTags: ['principles'],
    }
    const parsed = parseObligationsData({ obligations: [minimal] })
    expect(parsed.obligations[0]!.severity).toBe('medium')
    expect(parsed.obligations[0]!.appliesWhen).toEqual({})
  })

  it('rejects an empty obligations array (snapshot must contain at least one entry)', () => {
    expect(() => parseObligationsData({ obligations: [] })).toThrow(/obligations/i)
  })

  it('rejects a non-kebab slug', () => {
    const bad = validObligation()
    bad.slug = 'GDPR Art 30 ROPA'
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/slug/i)
  })

  it('rejects a summary shorter than 100 characters', () => {
    const bad = validObligation()
    bad.summary = 'too short'
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/summary/i)
  })

  it('rejects a summary longer than 2000 characters', () => {
    const bad = validObligation()
    bad.summary = 'x'.repeat(2001)
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/summary/i)
  })

  it('rejects citation.kind=article when articleNumber is missing', () => {
    const bad = validObligation()
    // @ts-expect-error — exercising runtime validation against malformed input
    bad.citation = { kind: 'article', celex: '32016R0679' }
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/article/i)
  })

  it('rejects citation.kind=annex without an annexLabel', () => {
    const bad = validObligation()
    // @ts-expect-error — exercising runtime validation against malformed input
    bad.citation = { kind: 'annex', celex: '32024R1689' }
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/annex/i)
  })

  it('rejects citation.kind=recital without a recitalNumber', () => {
    const bad = validObligation()
    // @ts-expect-error — exercising runtime validation against malformed input
    bad.citation = { kind: 'recital', celex: '32016R0679' }
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/recital/i)
  })

  it('rejects an unknown citation.kind', () => {
    const bad = validObligation()
    // @ts-expect-error — exercising runtime validation against malformed input
    bad.citation = { kind: 'paragraph', celex: '32016R0679', articleNumber: 30 }
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/kind/i)
  })

  it('rejects a non-ISO effectiveDate', () => {
    const bad = validObligation() as ReturnType<typeof validObligation> & {
      effectiveDate?: string
    }
    bad.effectiveDate = '25/05/2018'
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/effectiveDate/i)
  })

  it('rejects a negative dueWithinDays (a deadline cannot be in the past)', () => {
    const bad = validObligation() as ReturnType<typeof validObligation> & {
      dueWithinDays?: number
    }
    bad.dueWithinDays = -1
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/dueWithinDays/i)
  })

  it('rejects an unknown severity', () => {
    const bad = validObligation() as Record<string, unknown>
    bad.severity = 'critical'
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/severity/i)
  })

  it('rejects duplicate slugs (would silently merge on upsert)', () => {
    expect(() =>
      parseObligationsData({
        obligations: [validObligation(), validObligation()],
      }),
    ).toThrow(/duplicate.*slug/i)
  })

  it('rejects an empty topicTags array (every obligation must be tagged for retrieval)', () => {
    const bad = validObligation()
    bad.topicTags = []
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/topicTags/i)
  })

  it('rejects a non-kebab topic tag', () => {
    const bad = validObligation()
    bad.topicTags = ['Documentation']
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/topicTags|tag/i)
  })

  it('rejects duplicate topic tags within a single obligation', () => {
    const bad = validObligation()
    bad.topicTags = ['ropa', 'ropa']
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/topicTags|duplicate/i)
  })

  it('rejects an empty title', () => {
    const bad = validObligation()
    bad.title = ''
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/title/i)
  })

  it('rejects a non-CELEX-shaped celex value (sanity guard)', () => {
    const bad = validObligation()
    bad.citation.celex = 'GDPR'
    expect(() => parseObligationsData({ obligations: [bad] })).toThrow(/celex/i)
  })
})
