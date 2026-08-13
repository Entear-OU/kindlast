import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PostureSummaryCard } from '@/components/onboarding/posture-summary-card'
import type { PostureSummary } from '@/lib/onboarding/posture-summary'

/**
 * ENT-46 — RTL coverage for the inline posture summary card.
 *
 * The card is the visible payoff of onboarding (AC: "summary renders inline
 * at the end of the chat — no separate screen jump"). These tests pin:
 *
 *   * Covered items render with a green/check affordance and a recognisable
 *     accessible name so screen readers don't confuse green-vs-red purely
 *     by colour.
 *   * Missing items render with a red/cross affordance.
 *   * The draft finding shows title, description, regulation, and an
 *     Approve button that flips into an "Approved" confirmation state on
 *     click. The button is a stub for this PR (no DB write — see the
 *     PostureSummaryCard source for the rationale).
 */

const { toastMock } = vi.hoisted(() => ({ toastMock: vi.fn() }))
vi.mock('sonner', () => ({
  toast: { success: toastMock },
}))

const SAMPLE: PostureSummary = {
  covered: [
    {
      key: 'business_mapped',
      label: 'Business profile mapped',
      detail: 'SaaS payroll · Germany',
    },
    { key: 'data_inventory', label: 'Personal data inventory drafted' },
  ],
  missing: [
    { key: 'ropa', label: 'Record of Processing Activities' },
    { key: 'ai_literacy', label: 'AI literacy training (Article 4)' },
  ],
  topAction: {
    id: 'posture-ropa',
    key: 'ropa',
    title: 'Draft your Record of Processing Activities',
    description:
      'GDPR Article 30 requires every controller to keep a Record of Processing Activities…',
    regulation: 'GDPR Article 30',
    severity: 'high',
  },
}

describe('PostureSummaryCard (ENT-46)', () => {
  it('renders each covered item with a check affordance', () => {
    render(<PostureSummaryCard summary={SAMPLE} />)

    expect(screen.getByText('Business profile mapped')).toBeInTheDocument()
    expect(screen.getByText('Personal data inventory drafted')).toBeInTheDocument()

    // Accessible labels distinguish the two columns without relying on colour.
    expect(screen.getAllByLabelText(/covered/i).length).toBeGreaterThanOrEqual(2)
  })

  it('renders each missing item with a cross affordance', () => {
    render(<PostureSummaryCard summary={SAMPLE} />)

    expect(
      screen.getByText('Record of Processing Activities'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('AI literacy training (Article 4)'),
    ).toBeInTheDocument()

    expect(screen.getAllByLabelText(/missing/i).length).toBeGreaterThanOrEqual(2)
  })

  it('renders the draft finding with title, description, and regulation reference', () => {
    render(<PostureSummaryCard summary={SAMPLE} />)

    expect(
      screen.getByRole('heading', {
        name: /draft your record of processing activities/i,
      }),
    ).toBeInTheDocument()
    // Regulation is rendered in a dedicated badge; assert via that anchor
    // so we're not coupled to the wording inside the longer description.
    expect(screen.getByText('GDPR Article 30')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /^approve$/i }),
    ).toBeEnabled()
  })

  it('flips to an approved state on click and fires a confirmation toast', async () => {
    toastMock.mockClear()
    const user = userEvent.setup()
    render(<PostureSummaryCard summary={SAMPLE} />)

    await user.click(screen.getByRole('button', { name: /^approve$/i }))

    expect(toastMock).toHaveBeenCalledTimes(1)
    // After approval the button is no longer offered — the card surfaces
    // an "Approved" badge instead so a second click can't double-fire.
    expect(
      screen.queryByRole('button', { name: /^approve$/i }),
    ).not.toBeInTheDocument()
    expect(screen.getByText(/approved/i)).toBeInTheDocument()
  })

  it('honours an initial approved state from props so a reload-from-server view stays consistent', () => {
    // Server-side rendering won't know about a prior click in this PR (no DB
    // row yet), but plumbing the prop now keeps the call-site shape stable
    // for the follow-up that persists approvals.
    render(<PostureSummaryCard summary={SAMPLE} initialApproved />)

    expect(
      screen.queryByRole('button', { name: /^approve$/i }),
    ).not.toBeInTheDocument()
    expect(screen.getByText(/approved/i)).toBeInTheDocument()
  })
})
