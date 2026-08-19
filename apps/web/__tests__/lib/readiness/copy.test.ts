import { describe, expect, it } from 'vitest'

import * as copy from '@/lib/readiness/copy'
import { assertsLaw, legalAssertions } from '@/lib/readiness/claims'

/**
 * ENT-189. The marketing copy on the readiness page, held to the same rule as
 * a model's output.
 *
 * The page is the one surface where a confident sentence about the law reaches
 * somebody with no way to check it, so "the writer will be careful" is not a
 * control. This is.
 */

const STRINGS = Object.entries(copy).filter(
  (entry): entry is [string, string] => typeof entry[1] === 'string',
)

describe('the readiness page copy', () => {
  it('has strings to check', () => {
    // Guards the guard: an `export *` that stopped exporting strings would
    // leave every assertion below iterating an empty list and passing.
    expect(STRINGS.length).toBeGreaterThan(10)
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

  it('says plainly that nothing is transmitted, because that is the deal', () => {
    expect(copy.NO_TRANSMISSION).toMatch(/never leave/i)
    expect(assertsLaw(copy.NO_TRANSMISSION)).toBe(false)
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
