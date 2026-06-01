import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DeadlinesList } from '@/components/dashboard/deadlines-list'
import type { UpcomingDeadline } from '@/lib/dashboard/deadlines'

/**
 * ENT-79 — the upcoming-deadlines list: rows with title, date, day count and a
 * link to the finding; the 60-day empty state.
 */

function deadline(over: Partial<UpcomingDeadline> = {}): UpcomingDeadline {
  return {
    findingId: 'f1',
    title: 'EU AI Act Art. 50 transparency',
    dueAt: '2026-06-14',
    daysRemaining: 12,
    severity: 'high',
    ...over,
  }
}

describe('DeadlinesList (ENT-79)', () => {
  it('shows the 60-day empty state when there are no deadlines', () => {
    render(<DeadlinesList deadlines={[]} />)
    expect(screen.getByText('No deadlines in the next 60 days')).toBeInTheDocument()
  })

  it('renders each row with title, due date and days remaining', () => {
    render(<DeadlinesList deadlines={[deadline()]} />)
    expect(screen.getByText('EU AI Act Art. 50 transparency')).toBeInTheDocument()
    expect(screen.getByText('14 Jun 2026')).toBeInTheDocument()
    expect(screen.getByText('12 days left')).toBeInTheDocument()
  })

  it('links each row to its related finding', () => {
    render(<DeadlinesList deadlines={[deadline({ findingId: 'abc' })]} />)
    expect(screen.getByRole('link')).toHaveAttribute('href', '/feed/abc')
  })

  it('flags an overdue deadline', () => {
    render(<DeadlinesList deadlines={[deadline({ daysRemaining: -2 })]} />)
    expect(screen.getByText('2 days overdue')).toBeInTheDocument()
  })
})
