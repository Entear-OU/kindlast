import { describe, expect, it } from 'vitest'

import {
  FEED_SEVERITIES,
  FEED_STATUSES,
  filterFindings,
  gateFindings,
  severityChip,
  statusLabel,
  upgradeWaitingMessage,
  type Finding,
} from '@/lib/feed/findings'

/**
 * ENT-62 — pure helpers behind the Agent feed: client-side filtering and the
 * severity/status presentation. The Supabase loader (loadFindings) is covered
 * by the integration suite.
 */

function finding(over: Partial<Finding> = {}): Finding {
  return {
    id: 'f1',
    detected: 'No DPA on file for Stripe',
    severity: 'high',
    proposed_action: 'Draft a Data Processing Agreement with Stripe.',
    regulatory_obligation: 'GDPR Art. 28',
    citation_url: 'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_28',
    obligation_slug: 'gdpr-art-28-processor-contracts',
    effort_estimate: 'hours',
    status: 'pending',
    rejection_reason: null,
    snoozed_until: null,
    created_at: '2026-06-01T10:00:00.000Z',
    ...over,
  }
}

describe('filterFindings (ENT-62)', () => {
  const rows = [
    finding({ id: 'a', status: 'pending', severity: 'critical' }),
    finding({ id: 'b', status: 'approved', severity: 'high' }),
    finding({ id: 'c', status: 'pending', severity: 'low' }),
  ]

  it('returns all rows (order preserved) with no filter or "all"', () => {
    expect(filterFindings(rows).map((f) => f.id)).toEqual(['a', 'b', 'c'])
    expect(filterFindings(rows, { status: 'all', severity: 'all' }).map((f) => f.id)).toEqual([
      'a',
      'b',
      'c',
    ])
  })

  it('filters by status', () => {
    expect(filterFindings(rows, { status: 'pending' }).map((f) => f.id)).toEqual(['a', 'c'])
    expect(filterFindings(rows, { status: 'approved' }).map((f) => f.id)).toEqual(['b'])
  })

  it('filters by severity', () => {
    expect(filterFindings(rows, { severity: 'critical' }).map((f) => f.id)).toEqual(['a'])
  })

  it('combines status and severity (AND)', () => {
    expect(filterFindings(rows, { status: 'pending', severity: 'low' }).map((f) => f.id)).toEqual([
      'c',
    ])
    expect(filterFindings(rows, { status: 'approved', severity: 'low' })).toHaveLength(0)
  })
})

describe('presentation helpers (ENT-62)', () => {
  it('maps every severity to a label + chip class', () => {
    for (const s of FEED_SEVERITIES) {
      const chip = severityChip(s)
      expect(chip.label.toLowerCase()).toBe(s)
      expect(chip.className).toBeTruthy()
    }
  })

  it('labels every status', () => {
    for (const s of FEED_STATUSES) {
      expect(statusLabel(s)).toMatch(/^[A-Z]/)
    }
  })
})

describe('gateFindings (free-tier cap, ENT-82)', () => {
  const five = [
    finding({ id: 'a' }),
    finding({ id: 'b' }),
    finding({ id: 'c' }),
    finding({ id: 'd' }),
    finding({ id: 'e' }),
  ]

  it('shows everything to Pro, locking nothing', () => {
    const g = gateFindings(five, 'pro')
    expect(g.visible.map((f) => f.id)).toEqual(['a', 'b', 'c', 'd', 'e'])
    expect(g.locked).toEqual([])
    expect(g.lockedCount).toBe(0)
    expect(g.totalCount).toBe(5)
  })

  it('shows Free the 3 most-recent and locks the rest', () => {
    const g = gateFindings(five, 'free')
    expect(g.visible.map((f) => f.id)).toEqual(['a', 'b', 'c'])
    expect(g.locked.map((f) => f.id)).toEqual(['d', 'e'])
    expect(g.lockedCount).toBe(2)
    expect(g.totalCount).toBe(5)
  })

  it('locks nothing for Free at or under the limit', () => {
    const g = gateFindings(five.slice(0, 3), 'free')
    expect(g.visible).toHaveLength(3)
    expect(g.lockedCount).toBe(0)
  })

  it('handles an empty list', () => {
    expect(gateFindings([], 'free')).toEqual({
      visible: [],
      locked: [],
      lockedCount: 0,
      totalCount: 0,
    })
  })
})

describe('upgradeWaitingMessage (ENT-82)', () => {
  it('carries the count as trigger context', () => {
    expect(upgradeWaitingMessage(5)).toBe(
      'You have 5 findings waiting. Upgrade to act on them',
    )
  })

  it('uses the singular noun for one finding', () => {
    expect(upgradeWaitingMessage(1)).toBe(
      'You have 1 finding waiting. Upgrade to act on them',
    )
  })
})
