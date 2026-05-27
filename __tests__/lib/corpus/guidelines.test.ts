import { describe, expect, it } from 'vitest'

import { parseGuidelinesData } from '@/lib/corpus/guidelines'

/**
 * Unit coverage for the validator in `lib/corpus/guidelines.ts` (ENT-50).
 *
 * The actual upsert path (`ingestGuidelines`) is exercised end-to-end against
 * the local Supabase stack in `tests/integration/regulatory-guidelines-ingest.test.ts`
 * — RLS, the slug-uniqueness constraint, and topic_tags array round-trips
 * belong there.
 *
 * Here we just prove the front gate: the JSON snapshot at
 * `data/corpus/edpb-guidelines.json` is rejected early when malformed, so
 * the ingest script never sends garbage to the DB.
 */

const validGuideline = () => ({
  slug: 'edpb-05-2020-consent',
  publisher: 'EDPB',
  title: 'Guidelines 05/2020 on consent under Regulation 2016/679',
  adoptedDate: '2020-05-04',
  version: '1.1',
  sourceUrl: 'https://edpb.europa.eu/our-work-tools/our-documents/guidelines/guidelines-052020-consent-under-regulation-2016679_en',
  topicTags: ['consent', 'lawful-basis'],
})

const validInput = () => ({
  guidelines: [validGuideline()],
})

describe('parseGuidelinesData', () => {
  it('accepts a well-formed payload', () => {
    const parsed = parseGuidelinesData(validInput())
    expect(parsed.guidelines).toHaveLength(1)
    expect(parsed.guidelines[0]!.slug).toBe('edpb-05-2020-consent')
    expect(parsed.guidelines[0]!.topicTags).toEqual(['consent', 'lawful-basis'])
  })

  it('accepts a WP29-publisher entry (legacy guidelines endorsed by EDPB)', () => {
    const parsed = parseGuidelinesData({
      guidelines: [
        {
          ...validGuideline(),
          slug: 'wp29-wp243-dpo',
          publisher: 'WP29',
          title: 'Guidelines on Data Protection Officers (WP243 rev.01)',
          topicTags: ['dpo'],
        },
      ],
    })
    expect(parsed.guidelines[0]!.publisher).toBe('WP29')
  })

  it('allows the optional version field to be omitted', () => {
    const noVersion = validGuideline() as Partial<ReturnType<typeof validGuideline>>
    delete noVersion.version
    const parsed = parseGuidelinesData({ guidelines: [noVersion] })
    expect(parsed.guidelines[0]!.version).toBeUndefined()
  })

  it('rejects an empty guidelines array (snapshot must contain at least one entry)', () => {
    expect(() => parseGuidelinesData({ guidelines: [] })).toThrow(/guidelines/i)
  })

  it('rejects a missing slug', () => {
    const bad = validGuideline() as Record<string, unknown>
    delete bad.slug
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/slug/i)
  })

  it('rejects a non-kebab slug (slug is the natural key — strict to keep diffs reviewable)', () => {
    const bad = validGuideline()
    bad.slug = 'EDPB 05/2020 Consent'
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/slug/i)
  })

  it('rejects a non-ISO adoptedDate', () => {
    const bad = validGuideline()
    bad.adoptedDate = '04/05/2020'
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/adoptedDate/i)
  })

  it('rejects a non-URL sourceUrl', () => {
    const bad = validGuideline()
    bad.sourceUrl = 'not-a-url'
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/sourceUrl|url/i)
  })

  it('rejects an empty title', () => {
    const bad = validGuideline()
    bad.title = ''
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/title/i)
  })

  it('rejects duplicate slugs (would silently merge on upsert)', () => {
    const bad = {
      guidelines: [validGuideline(), validGuideline()],
    }
    expect(() => parseGuidelinesData(bad)).toThrow(/duplicate.*slug/i)
  })

  it('rejects an empty topicTags array — every guideline must be tagged for retrieval (AC)', () => {
    const bad = validGuideline()
    bad.topicTags = []
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/topicTags/i)
  })

  it('rejects a topic tag that is not kebab-case (tags are routed to retrieval — strict shape)', () => {
    const bad = validGuideline()
    bad.topicTags = ['Consent']
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/topicTags|tag/i)
  })

  it('rejects duplicate topic tags within a single guideline', () => {
    const bad = validGuideline()
    bad.topicTags = ['consent', 'consent']
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/topicTags|duplicate/i)
  })

  it('rejects an unknown publisher (whitelist keeps the curated set narrow)', () => {
    const bad = validGuideline() as Record<string, unknown>
    bad.publisher = 'Random Blog'
    expect(() => parseGuidelinesData({ guidelines: [bad] })).toThrow(/publisher/i)
  })
})
