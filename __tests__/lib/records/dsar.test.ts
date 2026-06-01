import { describe, expect, it } from 'vitest'

import {
  daysUntilDue,
  deriveDsarStatus,
  formatDate,
  formatDueLabel,
  isOpenDsar,
  type Dsar,
} from '@/lib/records/dsar'

/**
 * ENT-71 — pure helpers behind the DSAR Log: the derived status pill, the
 * deadline countdown, and the date columns.
 */

const now = new Date('2026-05-20T12:00:00.000Z')

function dsar(over: Partial<Dsar> = {}): Dsar {
  return {
    id: 'd1',
    subject_name: 'Jane Roe',
    request_type: 'access',
    handler: 'Privacy Team',
    status: 'open',
    received_at: '2026-05-10T10:00:00.000Z',
    response_due_at: '2026-06-09T10:00:00.000Z', // 20 days out
    responded_at: null,
    finding_id: null,
    created_at: '2026-05-10T10:00:00.000Z',
    updated_at: '2026-05-10T10:00:00.000Z',
    ...over,
  }
}

describe('deriveDsarStatus (ENT-71)', () => {
  it('reads responded / closed as done', () => {
    expect(deriveDsarStatus(dsar({ status: 'responded' }), now).tone).toBe('done')
    expect(deriveDsarStatus(dsar({ status: 'closed' }), now).label).toBe('Closed')
  })

  it('flags an open request past its deadline as overdue', () => {
    const s = deriveDsarStatus(dsar({ response_due_at: '2026-05-15T10:00:00.000Z' }), now)
    expect(s).toEqual({ label: 'Overdue', tone: 'danger' })
  })

  it('flags due within the 10-day escalation window as due soon', () => {
    const s = deriveDsarStatus(dsar({ response_due_at: '2026-05-25T10:00:00.000Z' }), now)
    expect(s).toEqual({ label: 'Due soon', tone: 'warn' })
  })

  it('shows a comfortable open request as open', () => {
    expect(deriveDsarStatus(dsar(), now)).toEqual({ label: 'Open', tone: 'info' })
    expect(deriveDsarStatus(dsar({ status: 'in_progress' }), now).label).toBe('In progress')
  })
})

describe('daysUntilDue / formatDueLabel (ENT-71)', () => {
  it('counts whole days, negative when overdue', () => {
    expect(daysUntilDue(dsar({ response_due_at: '2026-05-25T00:00:00.000Z' }), now)).toBe(5)
    expect(daysUntilDue(dsar({ response_due_at: '2026-05-18T00:00:00.000Z' }), now)).toBe(-2)
  })

  it('labels the deadline cell', () => {
    expect(formatDueLabel(dsar({ response_due_at: '2026-05-25T00:00:00.000Z' }), now)).toBe(
      'Due in 5 days',
    )
    expect(formatDueLabel(dsar({ response_due_at: '2026-05-19T00:00:00.000Z' }), now)).toBe(
      '1 day overdue',
    )
    expect(formatDueLabel(dsar({ response_due_at: '2026-05-20T00:00:00.000Z' }), now)).toBe(
      'Due today',
    )
  })

  it('shows a dash for a request already answered', () => {
    expect(formatDueLabel(dsar({ status: 'responded' }), now)).toBe('—')
  })
})

describe('isOpenDsar (ENT-71)', () => {
  it('is open for open / in_progress only', () => {
    expect(isOpenDsar(dsar({ status: 'open' }))).toBe(true)
    expect(isOpenDsar(dsar({ status: 'in_progress' }))).toBe(true)
    expect(isOpenDsar(dsar({ status: 'responded' }))).toBe(false)
    expect(isOpenDsar(dsar({ status: 'closed' }))).toBe(false)
  })
})

describe('formatDate (ENT-71)', () => {
  it('formats a date and dashes nulls', () => {
    expect(formatDate('2026-05-08T10:00:00.000Z', now)).toBe('8 May')
    expect(formatDate(null, now)).toBe('—')
  })
})
