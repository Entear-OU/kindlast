import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { FindingsFeed } from '@/components/feed/findings-feed'
import type { Finding } from '@/lib/feed/findings'

/**
 * ENT-62 — RTL coverage for the Agent feed.
 *
 *   * Renders each finding's detected text, obligation reference, severity +
 *     status chips, and proposed action.
 *   * Status and severity filters narrow the visible list.
 *   * Friendly all-clear empty state for a fresh user; filtered-empty state
 *     when filters exclude everything.
 *   * The obligation reference links to its citation URL when present.
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

describe('FindingsFeed (ENT-62)', () => {
  it('renders each finding with its fields and chips', () => {
    render(
      <FindingsFeed
        findings={[
          finding({ id: 'a', detected: 'No DPA on file for Stripe', severity: 'high' }),
          finding({
            id: 'b',
            detected: 'DSAR response due in 8 days',
            severity: 'critical',
            proposed_action: 'Send a response to the data-subject request before 12 June.',
            regulatory_obligation: 'GDPR Art. 12',
          }),
        ]}
      />,
    )

    expect(screen.getByText('No DPA on file for Stripe')).toBeInTheDocument()
    expect(screen.getByText('DSAR response due in 8 days')).toBeInTheDocument()
    expect(screen.getByText('Draft a Data Processing Agreement with Stripe.')).toBeInTheDocument()
    // Severity + status chips are <span>s; the filter bar also has buttons with
    // these labels, so scope to the card chips.
    expect(screen.getByText('Critical', { selector: 'span' })).toBeInTheDocument()
    expect(screen.getAllByText('Pending', { selector: 'span' }).length).toBeGreaterThan(0)
  })

  it('links the obligation reference to its citation URL', () => {
    render(<FindingsFeed findings={[finding()]} />)
    const link = screen.getByRole('link', { name: 'GDPR Art. 28' })
    expect(link).toHaveAttribute(
      'href',
      'https://eur-lex.europa.eu/eli/reg/2016/679/oj#art_28',
    )
  })

  it('filters by status', async () => {
    const user = userEvent.setup()
    render(
      <FindingsFeed
        findings={[
          finding({ id: 'a', detected: 'Pending one', status: 'pending' }),
          finding({ id: 'b', detected: 'Approved one', status: 'approved' }),
        ]}
      />,
    )
    expect(screen.getByText('Approved one')).toBeInTheDocument()

    // Click the "Approved" status filter chip (aria-pressed toggles).
    await user.click(screen.getByRole('button', { name: 'Approved', pressed: false }))

    expect(screen.queryByText('Pending one')).not.toBeInTheDocument()
    expect(screen.getByText('Approved one')).toBeInTheDocument()
  })

  it('filters by severity', async () => {
    const user = userEvent.setup()
    render(
      <FindingsFeed
        findings={[
          finding({ id: 'a', detected: 'Critical one', severity: 'critical' }),
          finding({ id: 'b', detected: 'Low one', severity: 'low' }),
        ]}
      />,
    )
    await user.click(screen.getByRole('button', { name: 'Low', pressed: false }))
    expect(screen.queryByText('Critical one')).not.toBeInTheDocument()
    expect(screen.getByText('Low one')).toBeInTheDocument()
  })

  it('shows a filtered-empty message when filters exclude everything', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed findings={[finding({ status: 'pending' })]} />)
    await user.click(screen.getByRole('button', { name: 'Rejected', pressed: false }))
    expect(screen.getByText(/No findings match these filters/i)).toBeInTheDocument()
  })

  it('shows the friendly all-clear empty state for a fresh user', () => {
    render(<FindingsFeed findings={[]} />)
    expect(screen.getByText('All clear')).toBeInTheDocument()
    expect(screen.getByText(/Watcher will let you know/i)).toBeInTheDocument()
  })
})
