import { describe, expect, it } from 'vitest'

import type { ComplianceProfile } from '@/lib/onboarding/extraction'
import { computePostureSummary } from '@/lib/onboarding/posture-summary'

/**
 * ENT-46 — Posture summary projection.
 *
 * `computePostureSummary` is the deterministic bridge between the structured
 * `ComplianceProfile` (ENT-45) and the inline card the founder sees at the
 * end of onboarding. Deterministic (not LLM-generated) because:
 *
 *   * The summary needs to load in <10s end-to-end and we already paid for
 *     one `generateObject` pass during extraction.
 *   * The card has structured slots (covered list, missing list, draft
 *     finding with severity + regulation reference) that are tedious to
 *     coax out of a free-form model response and easy to assert here.
 *
 * Priority rule for `topAction` (first match wins, highest-risk first):
 *
 *   1. ROPA missing → Article 30 GDPR. Baseline document every controller
 *      that isn't exempt under the <250-staff carve-out needs.
 *   2. AI literacy missing whenever any AI system is in use → Article 4
 *      EU AI Act. In force since Feb 2025; easiest gap to remediate.
 *   3. Cross-border transfer safeguards if data leaves the EU → Chapter V
 *      GDPR. Surfaces only when transfersOutsideEu === 'yes'.
 *   4. DPO designation when the profile claims none and we have a non-
 *      trivial headcount (staffCount >= 50) or unsure transfers → Article 37.
 *   5. Fallback: a generic privacy-policy review nudge so the card always
 *      has something concrete to offer.
 */

function baseProfile(overrides: Partial<ComplianceProfile> = {}): ComplianceProfile {
  return {
    industry: 'SaaS payroll',
    euJurisdictions: ['Germany'],
    dataCategories: ['email addresses'],
    dataSubjects: ['customers'],
    aiSystems: [],
    hasDpo: 'no',
    hasRopa: 'no',
    transfersOutsideEu: 'no',
    transferDestinations: [],
    vendorList: '',
    staffCount: 10,
    ...overrides,
  }
}

describe('computePostureSummary (ENT-46)', () => {
  it('lists business mapping and data inventory as covered for any answered profile', () => {
    const summary = computePostureSummary(baseProfile())

    const coveredKeys = summary.covered.map((item) => item.key)
    expect(coveredKeys).toContain('business_mapped')
    expect(coveredKeys).toContain('data_inventory')
  })

  it('marks DPO covered when hasDpo is "yes" and missing otherwise', () => {
    const withDpo = computePostureSummary(baseProfile({ hasDpo: 'yes' }))
    expect(withDpo.covered.map((c) => c.key)).toContain('dpo')
    expect(withDpo.missing.map((c) => c.key)).not.toContain('dpo')

    const withoutDpo = computePostureSummary(baseProfile({ hasDpo: 'no' }))
    expect(withoutDpo.missing.map((c) => c.key)).toContain('dpo')
    expect(withoutDpo.covered.map((c) => c.key)).not.toContain('dpo')

    const unsure = computePostureSummary(baseProfile({ hasDpo: 'unsure' }))
    expect(unsure.missing.map((c) => c.key)).toContain('dpo')
  })

  it('marks ROPA covered when hasRopa is "yes" and missing otherwise', () => {
    const withRopa = computePostureSummary(baseProfile({ hasRopa: 'yes' }))
    expect(withRopa.covered.map((c) => c.key)).toContain('ropa')

    const withoutRopa = computePostureSummary(baseProfile({ hasRopa: 'no' }))
    expect(withoutRopa.missing.map((c) => c.key)).toContain('ropa')
  })

  it('surfaces AI literacy as missing whenever any AI system is in use', () => {
    const withAi = computePostureSummary(
      baseProfile({ aiSystems: ['ChatGPT (internal)'] }),
    )
    expect(withAi.missing.map((c) => c.key)).toContain('ai_literacy')

    const withoutAi = computePostureSummary(baseProfile({ aiSystems: [] }))
    expect(withoutAi.missing.map((c) => c.key)).not.toContain('ai_literacy')
  })

  it('flags cross-border transfer safeguards only when transfersOutsideEu is "yes"', () => {
    const transfers = computePostureSummary(
      baseProfile({
        transfersOutsideEu: 'yes',
        transferDestinations: ['United States (Stripe)'],
      }),
    )
    expect(transfers.missing.map((c) => c.key)).toContain('transfer_safeguards')

    const noTransfers = computePostureSummary(
      baseProfile({ transfersOutsideEu: 'no' }),
    )
    expect(noTransfers.missing.map((c) => c.key)).not.toContain('transfer_safeguards')
  })

  it('picks ROPA as the top action when it is missing', () => {
    const summary = computePostureSummary(
      baseProfile({
        hasRopa: 'no',
        hasDpo: 'no',
        aiSystems: ['ChatGPT (internal)'],
        transfersOutsideEu: 'yes',
      }),
    )

    expect(summary.topAction.key).toBe('ropa')
    expect(summary.topAction.regulation).toMatch(/Article 30/i)
    expect(summary.topAction.severity).toBe('high')
  })

  it('picks AI literacy as the top action when ROPA is in place but AI tools are in use', () => {
    const summary = computePostureSummary(
      baseProfile({
        hasRopa: 'yes',
        aiSystems: ['ChatGPT (internal)', 'GitHub Copilot (internal)'],
      }),
    )

    expect(summary.topAction.key).toBe('ai_literacy')
    expect(summary.topAction.regulation).toMatch(/Article 4/i)
  })

  it('picks cross-border safeguards when ROPA + AI literacy are not the gap', () => {
    const summary = computePostureSummary(
      baseProfile({
        hasRopa: 'yes',
        aiSystems: [],
        transfersOutsideEu: 'yes',
        transferDestinations: ['United States (Stripe)'],
      }),
    )

    expect(summary.topAction.key).toBe('transfer_safeguards')
    expect(summary.topAction.regulation).toMatch(/Chapter V|GDPR/i)
  })

  it('picks DPO designation when nothing higher-priority is missing', () => {
    const summary = computePostureSummary(
      baseProfile({
        hasRopa: 'yes',
        aiSystems: [],
        transfersOutsideEu: 'no',
        hasDpo: 'no',
        staffCount: 80,
      }),
    )

    expect(summary.topAction.key).toBe('dpo')
    expect(summary.topAction.regulation).toMatch(/Article 37/i)
  })

  it('falls back to a privacy-policy nudge when every higher-priority gap is closed', () => {
    const summary = computePostureSummary(
      baseProfile({
        hasRopa: 'yes',
        hasDpo: 'yes',
        aiSystems: [],
        transfersOutsideEu: 'no',
      }),
    )

    expect(summary.topAction.key).toBe('privacy_policy_review')
    expect(summary.topAction.severity).toBe('low')
  })

  it('produces a draft finding with a stable id derived from the action key', () => {
    const first = computePostureSummary(baseProfile())
    const second = computePostureSummary(baseProfile())
    // Two invocations on the same profile shape should yield the same id so
    // a refresh of the page doesn't appear to show "a different finding".
    expect(first.topAction.id).toBe(second.topAction.id)
    expect(first.topAction.id).toContain(first.topAction.key)
  })
})
