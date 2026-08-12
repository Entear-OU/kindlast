import { describe, expect, it } from 'vitest'

import {
  STALE_AFTER_HOURS,
  actionLabel,
  agentStatusLabel,
  approverLabel,
  formatRelativeTime,
  hoursSince,
  isWatcherRunStale,
  targetLabel,
} from '@/lib/dashboard/activity'

/**
 * ENT-80 — the recent-activity presentation and the >36h Watcher staleness rule.
 */

const NOW = new Date('2026-06-02T12:00:00.000Z')

function hoursAgo(h: number): string {
  return new Date(NOW.getTime() - h * 60 * 60 * 1000).toISOString()
}

describe('actionLabel / targetLabel (ENT-80)', () => {
  it('maps the known Executor vocabulary', () => {
    expect(actionLabel('create_ropa')).toBe('Created processing record')
    expect(actionLabel('create_dsar')).toBe('Logged data-subject request')
    expect(actionLabel('create_ai_system')).toBe('Registered AI system')
    expect(targetLabel('processing_activities')).toBe('Records of processing')
    expect(targetLabel('ai_systems')).toBe('AI systems register')
  })

  it('humanises an unknown token rather than dropping it', () => {
    expect(actionLabel('mark_dsar_responded')).toBe('Mark dsar responded')
    expect(targetLabel('some_table')).toBe('Some table')
  })
})

describe('approverLabel (ENT-80)', () => {
  it('reads as the owner when it matches the current user', () => {
    expect(approverLabel('u1', 'u1', 'founder@acme.io')).toBe('founder@acme.io')
    expect(approverLabel('u1', 'u1', null)).toBe('You')
  })

  it('reads as a teammate otherwise', () => {
    expect(approverLabel('u2', 'u1', 'founder@acme.io')).toBe('A teammate')
  })
})

describe('isWatcherRunStale (ENT-80)', () => {
  it('is fresh within 36 hours', () => {
    expect(isWatcherRunStale(hoursAgo(1), NOW)).toBe(false)
    expect(isWatcherRunStale(hoursAgo(STALE_AFTER_HOURS), NOW)).toBe(false)
  })

  it('is stale past 36 hours', () => {
    expect(isWatcherRunStale(hoursAgo(37), NOW)).toBe(true)
  })

  it('treats "never run" as stale', () => {
    expect(isWatcherRunStale(null, NOW)).toBe(true)
  })
})

describe('hoursSince (ENT-80)', () => {
  it('measures whole-ish hours since a timestamp', () => {
    expect(hoursSince(hoursAgo(5), NOW)).toBeCloseTo(5)
  })
})

describe('formatRelativeTime (ENT-80)', () => {
  it('describes the recent past in human units', () => {
    expect(formatRelativeTime(NOW.toISOString(), NOW)).toBe('just now')
    expect(formatRelativeTime(new Date(NOW.getTime() - 5 * 60 * 1000).toISOString(), NOW)).toBe(
      '5 min ago',
    )
    expect(formatRelativeTime(hoursAgo(1), NOW)).toBe('1 hour ago')
    expect(formatRelativeTime(hoursAgo(5), NOW)).toBe('5 hours ago')
    expect(formatRelativeTime(hoursAgo(48), NOW)).toBe('2 days ago')
  })

  it('falls back to an absolute date past a month', () => {
    expect(formatRelativeTime('2026-01-05T09:00:00.000Z', NOW)).toBe('5 Jan')
    expect(formatRelativeTime('2024-01-05T09:00:00.000Z', NOW)).toBe('5 Jan 2024')
  })

  it('handles a null/never timestamp', () => {
    expect(formatRelativeTime(null, NOW)).toBe('never')
  })
})

describe('agentStatusLabel (ENT-155)', () => {
  it('reports an honest "not run yet" with an amber dot when the Watcher never ran', () => {
    expect(agentStatusLabel(null, NOW)).toEqual({
      running: false,
      text: "Watcher hasn't run yet",
    })
  })

  it('reports a green "running" pill with the relative scan time for a recent run', () => {
    const pill = agentStatusLabel(hoursAgo(2), NOW)
    expect(pill.running).toBe(true)
    expect(pill.text).toBe('Agent running · last scan 2 hours ago')
  })

  it('reports an amber "idle" pill once the run is stale (>36h)', () => {
    const pill = agentStatusLabel(hoursAgo(STALE_AFTER_HOURS + 12), NOW)
    expect(pill.running).toBe(false)
    expect(pill.text).toMatch(/^Agent idle · last scan /)
  })

  it('never emits the old hard-coded "4 min ago" string', () => {
    expect(agentStatusLabel(hoursAgo(1), NOW).text).not.toContain('4 min ago')
  })
})
