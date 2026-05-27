import { describe, expect, it } from 'vitest'

import { parseRegulationData } from '@/lib/corpus/ingest'

/**
 * Unit coverage for the pure validator in `lib/corpus/ingest.ts`.
 *
 * The actual upsert path (`ingestRegulation`) is exercised end-to-end against
 * the local Supabase stack in `tests/integration/regulatory-corpus-ingest.test.ts`
 * — RLS, unique constraints, and Postgres upsert semantics belong there.
 *
 * Here we just prove the front gate: malformed snapshots are rejected early
 * so the ingest script never sends garbage to the DB.
 *
 * Architecture context (ENT-32 progressive disclosure, completed in ENT-97):
 * each article / recital / paragraph / annex / annex item row carries a
 * curated `summary` (100–2000 chars) — the LLM's routing artifact — not
 * the verbatim OJ body. Sample summaries in this suite are ≥100 chars to
 * satisfy the validator floor.
 */

// Reusable summaries deliberately ≥100 chars; recognisable routing prose, not
// padded placeholders.
const ARTICLE_1_SUMMARY =
  'Article 1 of the GDPR — Subject-matter and objectives. Establishes the Regulation\'s scope: protect natural persons regarding personal data processing and free movement of such data within the Union.'
const ARTICLE_2_SUMMARY =
  'Article 2 of the GDPR — Material scope. Frames what processing falls under the Regulation and carves out exclusions (purely personal/household activity, national security, law enforcement under LED 2016/680).'
const ARTICLE_4_SUMMARY =
  'Article 4 of the EU AI Act — AI literacy. Imposes a duty on providers and deployers to ensure sufficient AI literacy among staff who operate or are affected by their AI systems. Already in force since 2 February 2025.'
const ARTICLE_6_SUMMARY =
  'Article 6 of the EU AI Act — Classification rules for high-risk AI systems. Two prongs: Annex I product-safety route and Annex III standalone-system route. Carve-out in Art 6(3) for narrow procedural tasks. Determines whether Articles 9–17 obligations apply.'
const RECITAL_1_SUMMARY =
  'Recital 1 of the GDPR. Frames data protection as a fundamental right under Article 8 of the EU Charter; foundation for the human-rights reading of the Regulation. Cited when arguing the Regulation\'s objective and proportionality.'

const validInput = () => ({
  document: {
    title: 'Regulation (EU) 2016/679',
    shortTitle: 'General Data Protection Regulation',
    celexNumber: '32016R0679',
    versionDate: '2016-05-04',
    officialUrl: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj',
  },
  articles: [
    { articleNumber: 1, heading: 'Subject-matter', summary: ARTICLE_1_SUMMARY },
    { articleNumber: 2, heading: 'Material scope', summary: ARTICLE_2_SUMMARY },
  ],
  recitals: [{ recitalNumber: 1, summary: RECITAL_1_SUMMARY }],
})

describe('parseRegulationData', () => {
  it('accepts a well-formed payload', () => {
    const parsed = parseRegulationData(validInput())
    expect(parsed.document.celexNumber).toBe('32016R0679')
    expect(parsed.articles).toHaveLength(2)
    expect(parsed.recitals).toHaveLength(1)
    expect(parsed.articleRecitals).toBeUndefined()
  })

  it('accepts an optional articleRecitals junction list', () => {
    const parsed = parseRegulationData({
      ...validInput(),
      articleRecitals: [{ articleNumber: 1, recitalNumber: 1 }],
    })
    expect(parsed.articleRecitals).toEqual([{ articleNumber: 1, recitalNumber: 1 }])
  })

  it('rejects missing document fields', () => {
    const bad = validInput() as Record<string, unknown>
    delete (bad.document as Record<string, unknown>).celexNumber
    expect(() => parseRegulationData(bad)).toThrow(/celexNumber/i)
  })

  it('rejects a non-ISO version_date', () => {
    const bad = validInput()
    bad.document.versionDate = '2016/05/04'
    expect(() => parseRegulationData(bad)).toThrow(/versionDate/i)
  })

  it('rejects a non-URL official_url', () => {
    const bad = validInput()
    bad.document.officialUrl = 'not-a-url'
    expect(() => parseRegulationData(bad)).toThrow(/officialUrl|url/i)
  })

  it('rejects an empty articles array', () => {
    const bad = validInput()
    bad.articles = []
    expect(() => parseRegulationData(bad)).toThrow(/articles/i)
  })

  it('rejects duplicate articleNumbers (would silently merge on upsert)', () => {
    const bad = validInput()
    bad.articles = [
      { articleNumber: 1, heading: 'A', summary: ARTICLE_1_SUMMARY },
      { articleNumber: 1, heading: 'B', summary: ARTICLE_2_SUMMARY },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*article/i)
  })

  it('rejects duplicate recitalNumbers', () => {
    const bad = validInput()
    bad.recitals = [
      { recitalNumber: 1, summary: RECITAL_1_SUMMARY },
      { recitalNumber: 1, summary: RECITAL_1_SUMMARY },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*recital/i)
  })

  it('rejects articleRecitals references that point at unknown articles', () => {
    const bad = {
      ...validInput(),
      articleRecitals: [{ articleNumber: 99, recitalNumber: 1 }],
    }
    expect(() => parseRegulationData(bad)).toThrow(/unknown.*article/i)
  })

  it('rejects articleRecitals references that point at unknown recitals', () => {
    const bad = {
      ...validInput(),
      articleRecitals: [{ articleNumber: 1, recitalNumber: 99 }],
    }
    expect(() => parseRegulationData(bad)).toThrow(/unknown.*recital/i)
  })

  it('rejects non-positive article numbers', () => {
    const bad = validInput()
    bad.articles[0]!.articleNumber = 0
    expect(() => parseRegulationData(bad)).toThrow()
  })

  it('rejects an article summary shorter than the progressive-disclosure floor (100 chars)', () => {
    const bad = validInput()
    bad.articles[0]!.summary = 'too short'
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })

  it('rejects a recital summary shorter than the progressive-disclosure floor', () => {
    const bad = validInput()
    bad.recitals[0]!.summary = 'too short'
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })

  it('rejects an article summary longer than the ceiling (2000 chars)', () => {
    const bad = validInput()
    bad.articles[0]!.summary = 'a'.repeat(2001)
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })
})

describe('parseRegulationData — article paragraphs (ENT-95 / ENT-97)', () => {
  const PARA_1_SUMMARY =
    'Article 6(1) of the EU AI Act — high-risk classification by Annex I or Annex III. A system is high-risk if it is a safety component of an Annex I product OR falls within an Annex III use case.'
  const PARA_1A_SUMMARY =
    'Article 6(1)(a) of the EU AI Act — Annex I product-safety prong. The system is a safety component of a regulated product covered by the Union harmonisation legislation in Annex I (machinery, medical devices, etc.).'
  const PARA_1B_SUMMARY =
    'Article 6(1)(b) of the EU AI Act — Annex III standalone-system prong. The system itself (not a safety component) falls within one of the Annex III high-risk use case categories.'

  const withParagraphs = () => ({
    ...validInput(),
    articles: [
      {
        articleNumber: 6,
        heading: 'Classification',
        summary: ARTICLE_6_SUMMARY,
        paragraphs: [
          { label: '1', summary: PARA_1_SUMMARY, ordering: 1 },
          { label: '1(a)', summary: PARA_1A_SUMMARY, ordering: 2 },
          { label: '1(b)', summary: PARA_1B_SUMMARY, ordering: 3 },
        ],
      },
    ],
  })

  it('accepts an article with paragraphs[]', () => {
    const parsed = parseRegulationData(withParagraphs())
    expect(parsed.articles[0]!.paragraphs).toHaveLength(3)
    expect(parsed.articles[0]!.paragraphs![1]!.label).toBe('1(a)')
  })

  it('accepts articles without paragraphs (back-compat for GDPR shape)', () => {
    // Articles without sub-paragraph rows just omit the field.
    const parsed = parseRegulationData(validInput())
    expect(parsed.articles[0]!.paragraphs).toBeUndefined()
  })

  it('rejects duplicate paragraph labels within an article (would silently merge on upsert)', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs = [
      { label: '1', summary: PARA_1_SUMMARY, ordering: 1 },
      { label: '1', summary: PARA_1A_SUMMARY, ordering: 2 },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*paragraph/i)
  })

  it('rejects empty paragraph labels', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs![0]!.label = ''
    expect(() => parseRegulationData(bad)).toThrow()
  })

  it('rejects a paragraph summary shorter than the progressive-disclosure floor', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs![0]!.summary = 'too short'
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })

  it('rejects negative paragraph ordering', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs![0]!.ordering = -1
    expect(() => parseRegulationData(bad)).toThrow()
  })

  it('allows the same paragraph_label across different articles', () => {
    // Natural key is (article_id, paragraph_label) — Article 4's "1" and
    // Article 6's "1" must both be accepted.
    const parsed = parseRegulationData({
      ...validInput(),
      articles: [
        {
          articleNumber: 4,
          heading: 'AI literacy',
          summary: ARTICLE_4_SUMMARY,
          paragraphs: [{ label: '1', summary: PARA_1_SUMMARY, ordering: 1 }],
        },
        {
          articleNumber: 6,
          heading: 'Classification',
          summary: ARTICLE_6_SUMMARY,
          paragraphs: [{ label: '1', summary: PARA_1A_SUMMARY, ordering: 1 }],
        },
      ],
    })
    expect(parsed.articles).toHaveLength(2)
  })
})

describe('parseRegulationData — annexes + effective dates (ENT-96)', () => {
  // Sample summaries are deliberately ≥100 chars to satisfy the
  // progressive-disclosure length constraint without padding noise.
  // Each one is a recognisable real-world routing artifact.
  const ANNEX_SUMMARY =
    'Annex III enumerates AI use cases the Regulation designates high-risk under Article 6(2). Listed systems trigger Articles 9–17 obligations: risk management, data governance, transparency, post-market monitoring, conformity assessment, and EU-database registration.'
  const ITEM_1_SUMMARY =
    'Category 1 — Biometrics: AI systems that identify, categorise, or recognise emotional state in natural persons, in so far as Union or Member State law permits the use. Covers remote biometric identification, biometric categorisation by sensitive attributes, and emotion recognition.'
  const ITEM_1A_SUMMARY =
    'Annex III 1(a) — Remote biometric identification systems. Excludes one-to-one verification used solely to confirm a natural person is who they claim to be. Risk concern: large-scale identification in public spaces, surveillance overreach. Triggers DPIA + transparency notice for deployers.'
  const ITEM_2_SUMMARY =
    'Annex III 2 — Critical infrastructure: AI used as a safety component in management/operation of critical digital infrastructure, road traffic, or water/gas/heating/electricity supply. Risk concern: cascading failures from AI-driven control of safety-critical systems.'

  const withAnnex = () => ({
    ...validInput(),
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

  it('accepts a document with annexes[]', () => {
    const parsed = parseRegulationData(withAnnex())
    expect(parsed.annexes).toHaveLength(1)
    expect(parsed.annexes![0]!.label).toBe('III')
    expect(parsed.annexes![0]!.items).toHaveLength(3)
  })

  it('accepts payloads without annexes[] (back-compat for GDPR)', () => {
    const parsed = parseRegulationData(validInput())
    expect(parsed.annexes).toBeUndefined()
  })

  it('rejects duplicate annex labels within a document', () => {
    const bad = withAnnex()
    bad.annexes = [bad.annexes![0]!, { ...bad.annexes![0]! }]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*annex/i)
  })

  it('rejects duplicate item labels within an annex', () => {
    const bad = withAnnex()
    bad.annexes![0]!.items = [
      { label: '1', summary: ITEM_1_SUMMARY, ordering: 1 },
      { label: '1', summary: ITEM_2_SUMMARY, ordering: 2 },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*item/i)
  })

  it('rejects a non-ISO annex effectiveDate', () => {
    const bad = withAnnex()
    bad.annexes![0]!.effectiveDate = '2026/08/02'
    expect(() => parseRegulationData(bad)).toThrow(/effectiveDate/i)
  })

  it('rejects empty annex labels', () => {
    const bad = withAnnex()
    bad.annexes![0]!.label = ''
    expect(() => parseRegulationData(bad)).toThrow()
  })

  it('rejects an annex summary shorter than the progressive-disclosure floor (100 chars)', () => {
    const bad = withAnnex()
    bad.annexes![0]!.summary = 'too short'
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })

  it('rejects an annex item summary shorter than the progressive-disclosure floor', () => {
    const bad = withAnnex()
    bad.annexes![0]!.items[0]!.summary = 'too short'
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })

  it('rejects an annex summary longer than the ceiling (2000 chars)', () => {
    // The ceiling keeps summaries scannable in LLM context; if a curator
    // needs more, they should split into sub-items rather than balloon a
    // single row.
    const bad = withAnnex()
    bad.annexes![0]!.summary = 'a'.repeat(2001)
    expect(() => parseRegulationData(bad)).toThrow(/summary/i)
  })

  it('accepts items with no heading (sub-items skip the field)', () => {
    const parsed = parseRegulationData(withAnnex())
    const subItem = parsed.annexes![0]!.items.find((i) => i.label === '1(a)')
    expect(subItem).toBeDefined()
    expect(subItem!.heading).toBeUndefined()
  })

  it('accepts an article with effectiveDate', () => {
    const parsed = parseRegulationData({
      ...validInput(),
      articles: [
        {
          articleNumber: 4,
          heading: 'AI literacy',
          summary: ARTICLE_4_SUMMARY,
          effectiveDate: '2025-02-02',
        },
      ],
    })
    expect(parsed.articles[0]!.effectiveDate).toBe('2025-02-02')
  })

  it('rejects a non-ISO article effectiveDate', () => {
    const bad = {
      ...validInput(),
      articles: [
        {
          articleNumber: 4,
          heading: 'AI literacy',
          summary: ARTICLE_4_SUMMARY,
          effectiveDate: '02/02/2025',
        },
      ],
    }
    expect(() => parseRegulationData(bad)).toThrow(/effectiveDate/i)
  })

  it('allows item.effectiveDate to override the annex-level effectiveDate', () => {
    // Useful for the rare case where a single Annex III item carves out a
    // different deadline. The DB column is nullable per-item; the validator
    // accepts it without requiring the override.
    const parsed = parseRegulationData({
      ...validInput(),
      annexes: [
        {
          label: 'III',
          heading: 'h',
          summary: ANNEX_SUMMARY,
          effectiveDate: '2026-08-02',
          items: [
            {
              label: '1',
              heading: 'X',
              summary: ITEM_1_SUMMARY,
              ordering: 1,
              effectiveDate: '2027-01-01',
            },
          ],
        },
      ],
    })
    expect(parsed.annexes![0]!.items[0]!.effectiveDate).toBe('2027-01-01')
  })
})
