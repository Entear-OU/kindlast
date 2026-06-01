import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { FindingsFeed } from '@/components/feed/findings-feed'
import type { Finding } from '@/lib/feed/findings'

/**
 * ENT-62 (list) + ENT-63 (actions) — RTL coverage for the Agent feed.
 *
 *   * Renders each finding's detected text, obligation reference, severity +
 *     status chips, and proposed action; status/severity filters narrow it.
 *   * Pending cards carry one-tap Approve / Reject / Snooze; decided cards don't.
 *   * Actions are optimistic: the row flips immediately, a failure rolls it back
 *     and raises a toast.
 *   * Reject reveals an optional reason textarea that is passed through.
 *   * Free users get the Pro upgrade prompt on Approve instead of an Executor run.
 */

const { approveMock, rejectMock, snoozeMock, toastSuccess, toastError, shownMock, convertedMock } =
  vi.hoisted(() => ({
    approveMock: vi.fn(),
    rejectMock: vi.fn(),
    snoozeMock: vi.fn(),
    toastSuccess: vi.fn(),
    toastError: vi.fn(),
    shownMock: vi.fn(),
    convertedMock: vi.fn(),
  }))

vi.mock('@/app/(authed)/feed/actions', () => ({
  approveFinding: approveMock,
  rejectFinding: rejectMock,
  snoozeFinding: snoozeMock,
}))

vi.mock('@/lib/analytics/track', () => ({
  trackUpgradePromptShown: shownMock,
  trackUpgradeConverted: convertedMock,
}))

vi.mock('sonner', () => ({
  toast: { success: toastSuccess, error: toastError },
}))

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

beforeEach(() => {
  vi.clearAllMocks()
  approveMock.mockResolvedValue({ ok: true })
  rejectMock.mockResolvedValue({ ok: true })
  snoozeMock.mockResolvedValue({ ok: true })
})

describe('FindingsFeed — list (ENT-62)', () => {
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

describe('FindingsFeed — actions (ENT-63)', () => {
  it('shows Approve / Reject / Snooze only on pending cards', () => {
    render(
      <FindingsFeed
        findings={[
          finding({ id: 'a', detected: 'Pending one', status: 'pending' }),
          finding({ id: 'b', detected: 'Approved one', status: 'approved' }),
        ]}
      />,
    )
    // One pending card → one set of action controls.
    expect(screen.getByRole('button', { name: 'Approve' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Snooze' })).toBeInTheDocument()
  })

  it('approves optimistically and fires the server action', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed findings={[finding({ id: 'a', detected: 'Approve me' })]} />)

    await user.click(screen.getByRole('button', { name: 'Approve' }))

    expect(approveMock).toHaveBeenCalledWith('a')
    // Optimistic: status flips, action buttons disappear.
    expect(screen.queryByRole('button', { name: 'Approve' })).not.toBeInTheDocument()
    expect(screen.getAllByText('Approved', { selector: 'span' }).length).toBeGreaterThan(0)
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled())
  })

  it('rolls back and toasts when the action fails', async () => {
    const user = userEvent.setup()
    approveMock.mockResolvedValue({ ok: false, error: 'boom' })
    render(<FindingsFeed findings={[finding({ id: 'a', detected: 'Approve me' })]} />)

    await user.click(screen.getByRole('button', { name: 'Approve' }))

    // Reverts to a pending card with its action buttons back.
    await waitFor(() => expect(toastError).toHaveBeenCalled())
    expect(screen.getByRole('button', { name: 'Approve' })).toBeInTheDocument()
    expect(screen.getAllByText('Pending', { selector: 'span' }).length).toBeGreaterThan(0)
  })

  it('reject reveals an optional reason and passes it through', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed findings={[finding({ id: 'a', detected: 'Reject me' })]} />)

    await user.click(screen.getByRole('button', { name: 'Reject' }))
    const textarea = screen.getByLabelText(/Rejection reason/i)
    await user.type(textarea, 'False positive')
    await user.click(screen.getByRole('button', { name: 'Confirm reject' }))

    expect(rejectMock).toHaveBeenCalledWith('a', 'False positive')
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled())
  })

  it('snoozes for the default 7 days', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed findings={[finding({ id: 'a', detected: 'Snooze me' })]} />)

    await user.click(screen.getByRole('button', { name: 'Snooze' }))

    expect(snoozeMock).toHaveBeenCalledWith('a', 7)
    await waitFor(() => expect(toastSuccess).toHaveBeenCalled())
  })

  it('snoozes for a chosen duration', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed findings={[finding({ id: 'a', detected: 'Snooze me' })]} />)

    await user.selectOptions(screen.getByLabelText('Snooze duration'), '30')
    await user.click(screen.getByRole('button', { name: 'Snooze' }))

    expect(snoozeMock).toHaveBeenCalledWith('a', 30)
  })

  it('opens the "Upgrade to act" modal on Approve for Free users without firing the action', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed plan="free" findings={[finding({ id: 'a', detected: 'Approve me' })]} />)

    // Reject and Snooze are available on Free (not gated) before any modal opens.
    expect(screen.getByRole('button', { name: 'Reject' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Snooze' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Approve' }))

    // Executor is never called…
    expect(approveMock).not.toHaveBeenCalled()
    // …and the upgrade modal explains why, with the AC copy.
    expect(
      await screen.findByText(
        'Pro unlocks one-tap actions. Your finding is still here, waiting.',
      ),
    ).toBeInTheDocument()
    expect(shownMock).toHaveBeenCalledWith({ source: 'executor_approve' })
  })
})

describe('FindingsFeed — free-tier 3-finding cap (ENT-82)', () => {
  const five = Array.from({ length: 5 }, (_, i) =>
    finding({ id: `f${i}`, detected: `Finding ${i}` }),
  )

  it('locks findings beyond the 3 most-recent for Free and shows the upgrade prompt', () => {
    render(<FindingsFeed plan="free" findings={five} />)

    // The 3 most-recent are interactive (each has an Approve button).
    expect(screen.getAllByRole('button', { name: 'Approve' })).toHaveLength(3)
    // The upgrade prompt carries the trigger context — all 5 are waiting.
    expect(
      screen.getByText('You have 5 findings waiting — upgrade to act on them'),
    ).toBeInTheDocument()
    const cta = screen.getByRole('link', { name: 'Upgrade to Pro' })
    expect(cta).toHaveAttribute('href', '/billing')
  })

  it('fires the shown tracking event when the prompt renders', () => {
    render(<FindingsFeed plan="free" findings={five} />)
    expect(shownMock).toHaveBeenCalledWith({
      source: 'finding_cap',
      lockedCount: 2,
      totalCount: 5,
    })
  })

  it('fires the converted tracking event when the CTA is tapped', async () => {
    const user = userEvent.setup()
    render(<FindingsFeed plan="free" findings={five} />)

    await user.click(screen.getByRole('link', { name: 'Upgrade to Pro' }))
    expect(convertedMock).toHaveBeenCalledWith({
      source: 'finding_cap',
      lockedCount: 2,
      totalCount: 5,
    })
  })

  it('does not lock or prompt for Free at or under the cap', () => {
    render(<FindingsFeed plan="free" findings={five.slice(0, 3)} />)
    expect(screen.queryByRole('link', { name: 'Upgrade to Pro' })).not.toBeInTheDocument()
    expect(shownMock).not.toHaveBeenCalled()
  })

  it('shows everything to Pro with no upgrade prompt', () => {
    render(<FindingsFeed plan="pro" findings={five} />)
    expect(screen.getAllByRole('button', { name: 'Approve' })).toHaveLength(5)
    expect(screen.queryByRole('link', { name: 'Upgrade to Pro' })).not.toBeInTheDocument()
    expect(shownMock).not.toHaveBeenCalled()
  })
})
