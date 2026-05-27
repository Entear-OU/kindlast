import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

import { parseObligationsData } from '@/lib/corpus/obligations'

/**
 * Snapshot test for the curated seed `data/corpus/obligations.json` (ENT-52).
 *
 * Two roles:
 *
 *   1. Ensure the seed file passes the same Zod validator the ingest
 *      script runs — if a curator edits the JSON in a way that breaks
 *      shape (missing field, malformed citation), this test fails locally
 *      before the failure shows up at ingest time.
 *   2. Lock in the MVP coverage floor: the explicit obligations called out
 *      in the ENT-52 Linear issue description must be present. If a future
 *      cleanup deletes one of these, that's a scope decision that requires
 *      updating this test.
 *
 * The exact count is intentionally not pinned — adding new obligations
 * shouldn't break the suite. The floor is checked so the catalogue can't
 * shrink below MVP.
 */

const SNAPSHOT_PATH = resolve(__dirname, '../../../data/corpus/obligations.json')

describe('data/corpus/obligations.json', () => {
  const raw = JSON.parse(readFileSync(SNAPSHOT_PATH, 'utf8'))
  const data = parseObligationsData(raw)

  it('parses against the Zod validator', () => {
    expect(data.obligations.length).toBeGreaterThan(0)
  })

  it('contains at least the MVP minimum (10 obligations)', () => {
    expect(data.obligations.length).toBeGreaterThanOrEqual(10)
  })

  it('covers the AC-required obligations from the ENT-52 Linear description', () => {
    const slugs = new Set(data.obligations.map((o) => o.slug))
    // Each entry below corresponds to a bullet on the Linear issue.
    expect(slugs.has('gdpr-art-30-ropa')).toBe(true)
    expect(slugs.has('gdpr-art-35-dpia')).toBe(true)
    expect(slugs.has('gdpr-arts-12-22-data-subject-rights')).toBe(true)
    expect(slugs.has('ai-act-art-4-ai-literacy')).toBe(true)
    expect(slugs.has('ai-act-annex-iii-high-risk-systems')).toBe(true)
  })

  it('covers the additional MVP obligations on the agent brief', () => {
    const slugs = new Set(data.obligations.map((o) => o.slug))
    // Breach notification — 72-hour deadline → due_within_days = 0
    expect(slugs.has('gdpr-art-33-breach-notification')).toBe(true)
    // DPO appointment
    expect(slugs.has('gdpr-art-37-dpo-appointment')).toBe(true)
    // Data transfers (Chapter V)
    expect(slugs.has('gdpr-chapter-v-international-transfers')).toBe(true)
    // Lawful basis / consent
    expect(slugs.has('gdpr-art-6-lawful-basis')).toBe(true)
    expect(slugs.has('gdpr-art-7-consent-conditions')).toBe(true)
    // AI Act deployer obligations
    expect(slugs.has('ai-act-art-26-deployer-obligations')).toBe(true)
  })

  it('encodes the 72-hour breach notification as due_within_days=0', () => {
    const breach = data.obligations.find(
      (o) => o.slug === 'gdpr-art-33-breach-notification',
    )
    expect(breach).toBeDefined()
    expect(breach?.dueWithinDays).toBe(0)
    expect(breach?.recurrence).toBe('on-event')
  })

  it('encodes the AI Act Article 4 effective date (2025-02-02)', () => {
    const literacy = data.obligations.find((o) => o.slug === 'ai-act-art-4-ai-literacy')
    expect(literacy?.effectiveDate).toBe('2025-02-02')
  })

  it('encodes Annex III effective date (2026-08-02)', () => {
    const annex = data.obligations.find(
      (o) => o.slug === 'ai-act-annex-iii-high-risk-systems',
    )
    expect(annex?.citation.kind).toBe('annex')
    expect(annex?.effectiveDate).toBe('2026-08-02')
  })

  it('references only known CELEX numbers (GDPR + EU AI Act)', () => {
    const KNOWN_CELEX = new Set(['32016R0679', '32024R1689'])
    for (const o of data.obligations) {
      expect(KNOWN_CELEX.has(o.citation.celex)).toBe(true)
    }
  })

  it('tags every obligation with at least one topic (retrieval surface)', () => {
    for (const o of data.obligations) {
      expect(o.topicTags.length).toBeGreaterThan(0)
    }
  })

  it('declares no duplicate slugs (validator catches this — sanity belt-and-braces)', () => {
    const slugs = data.obligations.map((o) => o.slug)
    expect(new Set(slugs).size).toBe(slugs.length)
  })
})
