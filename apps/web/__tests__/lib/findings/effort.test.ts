import { describe, expect, it } from 'vitest'

import { effortSentence } from '@/lib/findings/effort'

/**
 * The effort hint on a finding, which used to read "Roughly days of work."
 *
 * `effort_estimate` holds the `effort_level` enum from 00002, an order of
 * magnitude rather than a quantity, and the page interpolated it into a
 * sentence that only works with a number in front of it.
 *
 * Every bucket the enum defines is pinned here rather than one example,
 * because the bug was not that one value read badly: all three did, and a test
 * covering only the one that happened to be on screen would have passed
 * against the broken version for the other two.
 */
describe('the effort hint reads as a sentence', () => {
  it.each([
    ['minutes', 'Minutes of work, roughly.'],
    ['hours', 'Hours of work, roughly.'],
    ['days', 'Days of work, roughly.'],
  ])('renders %s as a whole sentence', (bucket, expected) => {
    expect(effortSentence(bucket)).toBe(expected)
  })

  it('never leaves the bucket bare in the old phrasing', () => {
    // The specific regression: the value must not come back as something a
    // caller would drop into "Roughly ... of work." and get a missing word.
    for (const bucket of ['minutes', 'hours', 'days']) {
      expect(effortSentence(bucket)).not.toBe(bucket)
    }
  })

  it('says nothing when the Analyst recorded no estimate', () => {
    expect(effortSentence(undefined)).toBeNull()
    expect(effortSentence('')).toBeNull()
  })

  it('says nothing for a bucket it does not recognise', () => {
    // A fourth value added to effort_level without a line in the lookup should
    // cost the page a hint, not print a sentence with a hole in it.
    expect(effortSentence('weeks')).toBeNull()
  })
})
