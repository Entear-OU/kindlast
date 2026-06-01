import { describe, expect, it } from 'vitest'

import {
  countOpenBySeverity,
  feedSeverityHref,
  parseSeverityParam,
} from '@/lib/dashboard/severity'

/**
 * ENT-78 — the four open-items-by-severity counters and the pre-filtered feed
 * links they point at.
 */
describe('countOpenBySeverity (ENT-78)', () => {
  it('returns all four bands in Critical → Low order, even at zero', () => {
    const counts = countOpenBySeverity([])
    expect(counts.map((c) => c.severity)).toEqual(['critical', 'high', 'medium', 'low'])
    expect(counts.map((c) => c.count)).toEqual([0, 0, 0, 0])
  })

  it('tallies each severity', () => {
    const counts = countOpenBySeverity(['critical', 'high', 'high', 'low', 'low', 'low'])
    const byKey = Object.fromEntries(counts.map((c) => [c.severity, c.count]))
    expect(byKey).toEqual({ critical: 1, high: 2, medium: 0, low: 3 })
  })

  it('links each counter to the feed pre-filtered by that severity', () => {
    for (const c of countOpenBySeverity(['high'])) {
      expect(c.href).toBe(`/feed?severity=${c.severity}`)
    }
  })
})

describe('feedSeverityHref (ENT-78)', () => {
  it('builds the pre-filtered feed URL', () => {
    expect(feedSeverityHref('critical')).toBe('/feed?severity=critical')
  })
})

describe('parseSeverityParam (ENT-78)', () => {
  it('accepts a real band', () => {
    expect(parseSeverityParam('high')).toBe('high')
  })

  it('takes the first value of an array param', () => {
    expect(parseSeverityParam(['critical', 'low'])).toBe('critical')
  })

  it('rejects anything that is not a band', () => {
    expect(parseSeverityParam('nonsense')).toBeNull()
    expect(parseSeverityParam(undefined)).toBeNull()
    expect(parseSeverityParam('all')).toBeNull()
  })
})
