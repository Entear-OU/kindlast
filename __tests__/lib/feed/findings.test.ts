import { describe, expect, it } from 'vitest'

import {
  FEED_SEVERITIES,
  FEED_STATUSES,
  filterFindings,
  severityChip,
  statusLabel,
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
