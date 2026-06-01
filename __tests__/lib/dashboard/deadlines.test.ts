import { describe, expect, it } from 'vitest'

import {
  buildUpcomingDeadlines,
  daysRemainingLabel,
  formatDueDate,
  type DeadlineFindingRow,
} from '@/lib/dashboard/deadlines'

/**
 * ENT-79 — the upcoming-deadlines list: derivation from deadline/DSAR findings,
 * ascending-by-due-date order, and the date / day-count formatting.
 */

function row(over: Partial<DeadlineFindingRow> = {}): DeadlineFindingRow {
  return {
    id: 'f1',
    severity: 'high',
    regulatory_obligation: 'EU AI Act Art. 50 transparency',
    detected: 'AI Act transparency obligation takes effect soon',
    metadata: {
      signal_kind: 'deadline',
      signal_metadata: { days_remaining: 12, effective_date: '2026-06-14' },
    },
    ...over,
  }
}

describe('buildUpcomingDeadlines (ENT-79)', () => {
  it('maps a deadline finding to obligation title, due date, days and link target', () => {
    const [d] = buildUpcomingDeadlines([row()])
    expect(d).toEqual({
      findingId: 'f1',
      title: 'EU AI Act Art. 50 transparency',
      dueAt: '2026-06-14',
      daysRemaining: 12,
      severity: 'high',
    })
  })

  it('falls back to the detected text when there is no obligation title', () => {
    const [d] = buildUpcomingDeadlines([row({ regulatory_obligation: null })])
    expect(d.title).toBe('AI Act transparency obligation takes effect soon')
  })

  it('sources the due date from response_due_at for a DSAR', () => {
    const [d] = buildUpcomingDeadlines([
      row({
        id: 'dsar1',
        metadata: {
          signal_kind: 'dsar',
          signal_metadata: { days_remaining: 3, response_due_at: '2026-06-05T09:00:00.000Z' },
        },
      }),
    ])
    expect(d.findingId).toBe('dsar1')
    expect(d.dueAt).toBe('2026-06-05T09:00:00.000Z')
  })

  it('sorts ascending by due date (overdue first)', () => {
    const out = buildUpcomingDeadlines([
      row({ id: 'late', metadata: { signal_kind: 'deadline', signal_metadata: { days_remaining: 40, effective_date: '2026-07-12' } } }),
      row({ id: 'soon', metadata: { signal_kind: 'deadline', signal_metadata: { days_remaining: 2, effective_date: '2026-06-04' } } }),
      row({ id: 'overdue', metadata: { signal_kind: 'dsar', signal_metadata: { days_remaining: -1, response_due_at: '2026-06-01' } } }),
    ])
    expect(out.map((d) => d.findingId)).toEqual(['overdue', 'soon', 'late'])
  })

  it('ignores non-deadline findings and rows past the 60-day window', () => {
    const out = buildUpcomingDeadlines([
      row({ id: 'gap', metadata: { signal_kind: 'profile_gap', signal_metadata: { days_remaining: 5, effective_date: '2026-06-07' } } }),
      row({ id: 'far', metadata: { signal_kind: 'deadline', signal_metadata: { days_remaining: 61, effective_date: '2026-08-02' } } }),
    ])
    expect(out).toEqual([])
  })

  it('drops a deadline with no due date', () => {
    expect(
      buildUpcomingDeadlines([
        row({ metadata: { signal_kind: 'deadline', signal_metadata: { days_remaining: 5 } } }),
      ]),
    ).toEqual([])
  })
})

describe('formatDueDate (ENT-79)', () => {
  it('formats a date-only value', () => {
    expect(formatDueDate('2026-06-14')).toBe('14 Jun 2026')
  })

  it('formats a timestamp by its date part, ignoring timezone', () => {
    expect(formatDueDate('2026-12-01T23:30:00.000Z')).toBe('1 Dec 2026')
  })
})

describe('daysRemainingLabel (ENT-79)', () => {
  it('handles today, future and overdue with correct pluralisation', () => {
    expect(daysRemainingLabel(0)).toBe('Due today')
    expect(daysRemainingLabel(1)).toBe('1 day left')
    expect(daysRemainingLabel(12)).toBe('12 days left')
    expect(daysRemainingLabel(-1)).toBe('1 day overdue')
    expect(daysRemainingLabel(-3)).toBe('3 days overdue')
  })
})
