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
