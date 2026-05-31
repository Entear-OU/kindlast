import { describe, expect, it } from 'vitest'

import { parseEnforcementData } from '@/lib/corpus/enforcement'

/**
 * Unit coverage for the validator in `lib/corpus/enforcement.ts` (ENT-99).
 *
 * The actual upsert path (`ingestEnforcementDecisions`) is exercised end-to-end
 * against the local Supabase stack in
 * `tests/integration/regulatory-enforcement-ingest.test.ts` — RLS, the
 * slug-uniqueness constraint, and the int[] / text[] array round-trips
 * belong there.
 *
 * Here we just prove the front gate: the JSON snapshot at
 * `data/corpus/enforcement-decisions.json` is rejected early when malformed,
 * so the ingest script never sends garbage to the DB.
 */

const validDecision = () => ({
  slug: 'cnil-2022-clearview-ai',
  dpa: 'CNIL',
  title: 'Clearview AI — facial recognition without lawful basis',
  decisionDate: '2022-10-17',
  fineEur: 20_000_000,
  summary:
    'The CNIL fined Clearview AI €20M for scraping biometric data of French residents without a lawful basis under Article 6 and processing special-category data under Article 9. Landmark on facial-recognition databases.',
  sourceUrl: 'https://www.cnil.fr/en/facial-recognition-cnil-fines-clearview-ai-eu20-million',
  gdprArticles: [6, 9, 12, 15, 17],
  topicTags: ['biometrics', 'lawful-basis', 'dsar'],
})

const validInput = () => ({ decisions: [validDecision()] })

describe('parseEnforcementData', () => {
  it('accepts a well-formed payload', () => {
    const parsed = parseEnforcementData(validInput())
    expect(parsed.decisions).toHaveLength(1)
    expect(parsed.decisions[0]!.slug).toBe('cnil-2022-clearview-ai')
    expect(parsed.decisions[0]!.gdprArticles).toEqual([6, 9, 12, 15, 17])
    expect(parsed.decisions[0]!.topicTags).toEqual(['biometrics', 'lawful-basis', 'dsar'])
  })

  it('accepts a decision with no fine (reprimand / corrective order)', () => {
    const noFine = validDecision() as Partial<ReturnType<typeof validDecision>>
    delete noFine.fineEur
    const parsed = parseEnforcementData({ decisions: [noFine] })
    expect(parsed.decisions[0]!.fineEur).toBeUndefined()
  })

  it('accepts a decision with an empty gdprArticles array (non-GDPR or cross-cutting)', () => {
    const parsed = parseEnforcementData({
      decisions: [{ ...validDecision(), gdprArticles: [] }],
    })
    expect(parsed.decisions[0]!.gdprArticles).toEqual([])
  })

  it('rejects an empty decisions array', () => {
    expect(() => parseEnforcementData({ decisions: [] })).toThrow(/decisions/i)
  })

  it('rejects a missing slug', () => {
    const bad = validDecision() as Record<string, unknown>
    delete bad.slug
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/slug/i)
  })

  it('rejects a non-kebab slug (slug is the natural key — strict to keep diffs reviewable)', () => {
    const bad = validDecision()
    bad.slug = 'CNIL 2022 Clearview'
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/slug/i)
  })

  it('rejects an empty dpa', () => {
    const bad = validDecision()
    bad.dpa = ''
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/dpa/i)
  })

  it('rejects a non-ISO decisionDate', () => {
    const bad = validDecision()
    bad.decisionDate = '17/10/2022'
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/decisionDate/i)
  })

  it('rejects a non-URL sourceUrl', () => {
    const bad = validDecision()
    bad.sourceUrl = 'not-a-url'
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/sourceUrl|url/i)
  })

  it('rejects a summary shorter than the progressive-disclosure floor (100 chars)', () => {
    const bad = validDecision()
    bad.summary = 'too short'
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/summary/i)
  })

  it('rejects a summary longer than the ceiling (2000 chars)', () => {
    const bad = validDecision()
    bad.summary = 'a'.repeat(2001)
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/summary/i)
  })

  it('rejects a negative fine', () => {
    const bad = validDecision()
    bad.fineEur = -100
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/fine/i)
  })

  it('rejects a non-integer or non-positive gdpr article number', () => {
    const bad = validDecision()
    bad.gdprArticles = [6, 0]
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/article/i)
  })

  it('rejects duplicate slugs (would silently merge on upsert)', () => {
    expect(() =>
      parseEnforcementData({ decisions: [validDecision(), validDecision()] }),
    ).toThrow(/duplicate.*slug/i)
  })

  it('rejects an empty topicTags array — every decision must be tagged for retrieval', () => {
    const bad = validDecision()
    bad.topicTags = []
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/topicTags|tag/i)
  })

  it('rejects a non-kebab topic tag', () => {
    const bad = validDecision()
    bad.topicTags = ['Biometrics']
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/topicTags|tag/i)
  })

  it('rejects duplicate topic tags within a single decision', () => {
    const bad = validDecision()
    bad.topicTags = ['biometrics', 'biometrics']
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/topicTags|duplicate/i)
  })

  it('rejects duplicate gdpr articles within a single decision', () => {
    const bad = validDecision()
    bad.gdprArticles = [6, 6]
    expect(() => parseEnforcementData({ decisions: [bad] })).toThrow(/duplicate|article/i)
  })
})
