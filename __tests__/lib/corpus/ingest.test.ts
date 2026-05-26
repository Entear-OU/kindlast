import { describe, expect, it } from 'vitest'

import { parseRegulationData } from '@/lib/corpus/ingest'

/**
 * Unit coverage for the pure validator in `lib/corpus/ingest.ts`.
 *
 * The actual upsert path (`ingestRegulation`) is exercised end-to-end against
 * the local Supabase stack in `tests/integration/regulatory-corpus-ingest.test.ts`
 * — RLS, unique constraints, and Postgres upsert semantics belong there.
 *
 * Here we just prove the front gate: the JSON snapshot at
 * `data/corpus/gdpr.json` (and any future regulation file) is rejected
 * early when malformed, so the ingest script never sends garbage to the DB.
 */

const validInput = () => ({
  document: {
    title: 'Regulation (EU) 2016/679',
    shortTitle: 'General Data Protection Regulation',
    celexNumber: '32016R0679',
    versionDate: '2016-05-04',
    officialUrl: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj',
  },
  articles: [
    { articleNumber: 1, heading: 'Subject-matter', body: 'This Regulation lays down rules…' },
    { articleNumber: 2, heading: 'Material scope', body: '…' },
  ],
  recitals: [{ recitalNumber: 1, body: 'The protection of natural persons…' }],
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
      { articleNumber: 1, heading: 'A', body: 'a' },
      { articleNumber: 1, heading: 'B', body: 'b' },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*article/i)
  })

  it('rejects duplicate recitalNumbers', () => {
    const bad = validInput()
    bad.recitals = [
      { recitalNumber: 1, body: 'a' },
      { recitalNumber: 1, body: 'b' },
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

  it('rejects empty article bodies', () => {
    const bad = validInput()
    bad.articles[0]!.body = ''
    expect(() => parseRegulationData(bad)).toThrow()
  })
})

describe('parseRegulationData — article paragraphs (ENT-95)', () => {
  const withParagraphs = () => ({
    ...validInput(),
    articles: [
      {
        articleNumber: 6,
        heading: 'Classification',
        body: 'Top-level body.',
        paragraphs: [
          { label: '1', body: 'Lead-in sentence.', ordering: 1 },
          { label: '1(a)', body: 'Sub-point a.', ordering: 2 },
          { label: '1(b)', body: 'Sub-point b.', ordering: 3 },
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
    // GDPR's snapshot has no paragraphs field — must still parse.
    const parsed = parseRegulationData(validInput())
    expect(parsed.articles[0]!.paragraphs).toBeUndefined()
  })

  it('rejects duplicate paragraph labels within an article (would silently merge on upsert)', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs = [
      { label: '1', body: 'first', ordering: 1 },
      { label: '1', body: 'second', ordering: 2 },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*paragraph/i)
  })

  it('rejects empty paragraph labels', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs![0]!.label = ''
    expect(() => parseRegulationData(bad)).toThrow()
  })

  it('rejects empty paragraph bodies', () => {
    const bad = withParagraphs()
    bad.articles[0]!.paragraphs![0]!.body = ''
    expect(() => parseRegulationData(bad)).toThrow()
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
          heading: 'X',
          body: '…',
          paragraphs: [{ label: '1', body: 'a', ordering: 1 }],
        },
        {
          articleNumber: 6,
          heading: 'Y',
          body: '…',
          paragraphs: [{ label: '1', body: 'b', ordering: 1 }],
        },
      ],
    })
    expect(parsed.articles).toHaveLength(2)
  })
})

describe('parseRegulationData — annexes + effective dates (ENT-96)', () => {
  const withAnnex = () => ({
    ...validInput(),
    annexes: [
      {
        label: 'III',
        heading: 'High-risk AI systems referred to in Article 6(2)',
        body: 'High-risk AI systems pursuant to Article 6(2) are listed below.',
        effectiveDate: '2026-08-02',
        items: [
          { label: '1', heading: 'Biometrics', body: 'Biometric body.', ordering: 1 },
          { label: '1(a)', body: 'Remote biometric identification systems.', ordering: 2 },
          { label: '2', heading: 'Critical infrastructure', body: 'Critical infrastructure body.', ordering: 3 },
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
      { label: '1', body: 'a', ordering: 1 },
      { label: '1', body: 'b', ordering: 2 },
    ]
    expect(() => parseRegulationData(bad)).toThrow(/duplicate.*item/i)
  })

  it('rejects a non-ISO annex effectiveDate', () => {
    const bad = withAnnex()
    bad.annexes![0]!.effectiveDate = '2026/08/02'
    expect(() => parseRegulationData(bad)).toThrow(/effectiveDate/i)
  })

  it('rejects empty item bodies', () => {
    const bad = withAnnex()
    bad.annexes![0]!.items[0]!.body = ''
    expect(() => parseRegulationData(bad)).toThrow()
  })

  it('rejects empty annex labels', () => {
    const bad = withAnnex()
    bad.annexes![0]!.label = ''
    expect(() => parseRegulationData(bad)).toThrow()
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
          body: 'Providers and deployers shall take measures.',
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
          body: '…',
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
          body: 'b',
          effectiveDate: '2026-08-02',
          items: [
            { label: '1', heading: 'X', body: 'b', ordering: 1, effectiveDate: '2027-01-01' },
          ],
        },
      ],
    })
    expect(parsed.annexes![0]!.items[0]!.effectiveDate).toBe('2027-01-01')
  })
})
