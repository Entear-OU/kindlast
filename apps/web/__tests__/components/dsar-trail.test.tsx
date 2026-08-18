import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { DsarTable } from '@/components/records/registers'
import { Trail } from '@/components/records/trail'
import type { Dsar, DsarTrailEntry } from '@/lib/records/client'

/**
 * The DSAR trail as a customer reads it (ENT-226).
 *
 * The property under test in every case below is the same one: that the surface
 * shows the DIFFERENCE between a claim and the evidence for it. A response date
 * with nothing behind it has to look different from a response date with a trail
 * behind it, or the page is decoration.
 */

// `next/link` resolves to a plain anchor in the test env (no Next runtime).
vi.mock('next/link', () => ({
  default: ({
    href,
    children,
    ...rest
  }: {
    href: string
    children: React.ReactNode
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}))

describe('the trail on a request', () => {
  const entry: DsarTrailEntry = {
    entryId: 'e-1',
    dsarId: 'd-1',
    source: 'Salesforce',
    action: 'found',
    detail: 'Contact record, opened 2019',
    occurredAt: '2026-08-05T09:00:00Z',
    recordedAt: '2026-08-09T16:30:00Z',
    createdBy: 'u-1',
  }

  // The empty state is the one that matters most. It is the state a request
  // marked responded sits in when nobody recorded anything, and saying so is
  // the whole point of the issue: `respondedAt` on its own is an assertion.
  it('says the response date stands on nothing when there is no trail', () => {
    render(<Trail entries={[]} />)

    expect(screen.getByTestId('trail-empty')).toHaveTextContent(
      /the organisation.s word for it/i,
    )
  })

  it('shows when a step happened and when it was written up, as two facts', () => {
    render(<Trail entries={[entry]} />)

    expect(screen.getByText('Salesforce')).toBeInTheDocument()
    expect(screen.getByText('Found data')).toBeInTheDocument()
    expect(screen.getByText('Contact record, opened 2019')).toBeInTheDocument()

    // Both labels present, because the gap between them is informative: a trail
    // written up entirely on the last day looks different from one kept as the
    // work was done.
    expect(screen.getByText('Happened')).toBeInTheDocument()
    expect(screen.getByText('Recorded')).toBeInTheDocument()
  })

  // "We looked in the CRM and there was nothing" and "nobody has looked in the
  // CRM" are different facts, and the words have to differ or the distinction
  // the schema draws is lost at the last step.
  it('renders a store that was searched and held nothing as its own outcome', () => {
    render(
      <Trail
        entries={[
          { ...entry, entryId: 'e-2', source: 'CRM', action: 'none_found' },
        ]}
      />,
    )

    expect(screen.getByText('Nothing found')).toBeInTheDocument()
  })

  // An action this build does not know renders as itself rather than as a
  // default, the same rule the badges follow. A trail that silently showed
  // "Searched" for a value it did not recognise would be making a claim nobody
  // recorded.
  it('renders an unknown action as itself', () => {
    render(
      <Trail
        entries={[{ ...entry, entryId: 'e-3', action: 'exported_to_subject' }]}
      />,
    )

    expect(screen.getByText('exported_to_subject')).toBeInTheDocument()
  })

  it('names the agent run behind an entry a run produced', () => {
    render(
      <Trail entries={[{ ...entry, entryId: 'e-4', agentRunId: 'run-42' }]} />,
    )

    expect(screen.getByText('run-42')).toBeInTheDocument()
  })

  it('says nothing about a run for an entry a person wrote', () => {
    render(<Trail entries={[entry]} />)
    expect(screen.queryByText(/agent run/i)).not.toBeInTheDocument()
  })
})

describe('the trail column on the register', () => {
  const answered: Dsar = {
    dsarId: 'd-1',
    requestType: 'access',
    status: 'responded',
    receivedAt: '2026-08-01T12:00:00Z',
    responseDueAt: '2026-08-31T12:00:00Z',
    respondedAt: '2026-08-20T12:00:00Z',
    urgency: 'answered',
  }

  // The count is on the list because that is where somebody scanning the
  // register decides whether the response date means anything. Zero is words
  // rather than a digit, because a bare 0 in a column of numbers is easy to
  // read past and this is the case worth noticing.
  it('says nothing is recorded rather than showing a zero', () => {
    render(<DsarTable items={[answered]} slug="alpha" />)

    expect(screen.getByTestId('dsar-trail-link')).toHaveTextContent(
      'Nothing recorded',
    )
  })

  it('counts the steps behind a request and links to them', () => {
    render(
      <DsarTable items={[{ ...answered, trailEntryCount: 3 }]} slug="alpha" />,
    )

    const link = screen.getByTestId('dsar-trail-link')
    expect(link).toHaveTextContent('3 steps')
    expect(link).toHaveAttribute('href', '/o/alpha/records/dsars/d-1')
  })

  it('says one step in the singular', () => {
    render(
      <DsarTable items={[{ ...answered, trailEntryCount: 1 }]} slug="alpha" />,
    )
    expect(screen.getByTestId('dsar-trail-link')).toHaveTextContent('1 step')
  })
})
