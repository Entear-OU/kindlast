import { describe, expect, it } from 'vitest'

import * as copy from '@/lib/onboarding/copy'
import { assertsLaw, legalAssertions } from '@/lib/onboarding/claims'

/**
 * ENT-189, ENT-254. Onboarding's copy, held to the same rule as a model's
 * output.
 *
 * This is where a customer meets the product's claims first, so a confident
 * sentence summarising the law would be read as the product speaking, beside a
 * correct citation, with nothing on screen to check it against. "The writer
 * will be careful" is not a control. This is.
 */

const STRINGS = Object.entries(copy).filter(
  (entry): entry is [string, string] => typeof entry[1] === 'string',
)

describe('the onboarding copy', () => {
  it('has strings to check', () => {
    // Guards the guard: an `export *` that stopped exporting strings would
    // leave every assertion below iterating an empty list and passing.
    expect(STRINGS.length).toBeGreaterThan(5)
  })

  it('asserts no law anywhere', () => {
    for (const [name, text] of STRINGS) {
      const breaches = legalAssertions(text)
      expect(
        breaches,
        `${name}: ${breaches.map((b) => `${b.rule} ("${b.matched}")`).join(', ')}`,
      ).toEqual([])
    }
  })

  it('uses no em dash or en dash', () => {
    // The house style rule, on the surface where it is most visible.
    for (const [name, text] of STRINGS) {
      expect(text, name).not.toMatch(/[–—]/)
    }
  })

  it('says plainly that answers are saved, because that is the deal now', () => {
    // The sentence this replaced promised the opposite: `/readiness` recorded
    // nothing and said so. Carrying that promise onto a surface that writes
    // every answer down would have been the worst kind of stale copy.
    expect(copy.ANSWERS_ARE_SAVED).toMatch(/saved/i)
    expect(copy.ANSWERS_ARE_SAVED).toMatch(/correct/i)
    expect(assertsLaw(copy.ANSWERS_ARE_SAVED)).toBe(false)
  })

  it('separates the quoted law from what we wrote', () => {
    // The two provenance lines are the design's load-bearing idea. If either
    // disappears, the result page stops telling the reader which half is which.
    expect(copy.QUOTE_PROVENANCE).toMatch(/unedited/i)
    expect(copy.WHY_PROVENANCE).toMatch(/your answers/i)
  })

  it('refuses to call itself an audit', () => {
    expect(copy.NOT_AN_AUDIT).toMatch(/not an audit/i)
    expect(copy.NOT_AN_AUDIT).toMatch(/not legal advice/i)
  })
})
