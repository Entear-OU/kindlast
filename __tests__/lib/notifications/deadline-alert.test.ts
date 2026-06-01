import { describe, expect, it } from 'vitest'

import { activeThreshold } from '@/lib/notifications/deadline-alert'

/**
 * ENT-75 — deadline threshold bucketing. The crossing logic lives entirely here,
 * so test every boundary.
 */

describe('activeThreshold (ENT-75)', () => {
  it('returns null beyond 30 days', () => {
    expect(activeThreshold(31)).toBeNull()
    expect(activeThreshold(100)).toBeNull()
  })

  it('buckets the 30-day window', () => {
    expect(activeThreshold(30)).toBe(30)
    expect(activeThreshold(15)).toBe(30)
  })

  it('buckets the 14-day window', () => {
    expect(activeThreshold(14)).toBe(14)
    expect(activeThreshold(8)).toBe(14)
  })

  it('buckets the 7-day window', () => {
    expect(activeThreshold(7)).toBe(7)
    expect(activeThreshold(2)).toBe(7)
  })

  it('buckets the 1-day window and overdue', () => {
    expect(activeThreshold(1)).toBe(1)
    expect(activeThreshold(0)).toBe(1)
    expect(activeThreshold(-3)).toBe(1)
  })
})
