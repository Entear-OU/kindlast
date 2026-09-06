import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { DeadlineClocks } from '@/components/console/deadline-clocks'
import type { Dsar } from '@/lib/records/client'

const dsar = (over: Partial<Dsar> & { dsarId: string }): Dsar => ({
  subjectName: 'A. Person',
  requestType: 'access',
  status: 'open',
  urgency: 'on_track',
  daysUntilDue: 20,
  ...over,
})

describe('DeadlineClocks', () => {
  it('shows the clocks that are still running', () => {
    render(
      <DeadlineClocks
        slug="acme"
        dsars={[
          dsar({
            dsarId: '1',
            subjectName: 'Ada Lovelace',
            daysUntilDue: -2,
            urgency: 'overdue',
          }),
          dsar({
            dsarId: '2',
            subjectName: 'Grace Hopper',
            daysUntilDue: 3,
            urgency: 'due_soon',
          }),
        ]}
      />,
    )

    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText('2 days overdue')).toBeInTheDocument()
    expect(screen.getByText('Grace Hopper')).toBeInTheDocument()
    expect(screen.getByText('Due in 3 days')).toBeInTheDocument()
  })

  it('drops the requests that have been answered', () => {
    // A clock that has stopped is not a deadline. Leaving answered requests
    // here would fill the section with work already done and bury the one
    // request that still needs somebody.
    render(
      <DeadlineClocks
        slug="acme"
        dsars={[
          dsar({
            dsarId: '1',
            subjectName: 'Answered Already',
            urgency: 'answered',
          }),
          dsar({ dsarId: '2', subjectName: 'Still Waiting', daysUntilDue: 5 }),
        ]}
      />,
    )

    expect(screen.queryByText('Answered Already')).toBeNull()
    expect(screen.getByText('Still Waiting')).toBeInTheDocument()
  })

  it('keeps the order it was given', () => {
    // The server sorts by `response_due_at` ascending. Re-sorting here would be
    // a second implementation of the same ordering, and the two would disagree
    // the first time a request had no due date.
    render(
      <DeadlineClocks
        slug="acme"
        dsars={[
          dsar({ dsarId: '1', subjectName: 'First', daysUntilDue: 1 }),
          dsar({ dsarId: '2', subjectName: 'Second', daysUntilDue: 9 }),
          dsar({ dsarId: '3', subjectName: 'Third', daysUntilDue: 40 }),
        ]}
      />,
    )

    const names = screen
      .getAllByTestId('deadline-subject')
      .map((n) => n.textContent)
    expect(names).toEqual(['First', 'Second', 'Third'])
  })

  it('draws its own heading, so the block disappears whole', () => {
    // The heading lives here rather than on the page: with it on the page, an
    // organisation with no running clock kept a title with nothing under it.
    const { rerender } = render(
      <DeadlineClocks
        slug="acme"
        dsars={[dsar({ dsarId: '1', daysUntilDue: 4 })]}
      />,
    )
    expect(
      screen.getByRole('heading', { name: /on the clock/i }),
    ).toBeInTheDocument()

    rerender(
      <DeadlineClocks
        slug="acme"
        dsars={[dsar({ dsarId: '1', urgency: 'answered' })]}
      />,
    )
    expect(screen.queryByRole('heading', { name: /on the clock/i })).toBeNull()
  })

  it('renders nothing at all when no clock is running', () => {
    // Not an empty state with a reassuring sentence. A heading over "you are
    // all clear" is a claim, and this section is only ever a claim about the
    // requests the register happens to hold.
    const { container } = render(
      <DeadlineClocks
        slug="acme"
        dsars={[dsar({ dsarId: '1', urgency: 'answered' })]}
      />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('takes the countdown from the server rather than the date', () => {
    // `responseDueAt` here is years away and `daysUntilDue` says tomorrow. The
    // server's number wins, because it is the one the register and the
    // regulator's deadline agree on.
    render(
      <DeadlineClocks
        slug="acme"
        dsars={[
          dsar({
            dsarId: '1',
            responseDueAt: '2099-01-01T00:00:00Z',
            daysUntilDue: 1,
            urgency: 'due_soon',
          }),
        ]}
      />,
    )
    expect(screen.getByText('Due tomorrow')).toBeInTheDocument()
    expect(screen.queryByText(/2099/)).toBeNull()
  })

  it('links each clock to the request it is counting', () => {
    render(
      <DeadlineClocks
        slug="acme"
        dsars={[dsar({ dsarId: 'req-7', subjectName: 'Ada Lovelace' })]}
      />,
    )
    expect(screen.getByRole('link', { name: /Ada Lovelace/ })).toHaveAttribute(
      'href',
      '/o/acme/records/dsars/req-7',
    )
  })
})
