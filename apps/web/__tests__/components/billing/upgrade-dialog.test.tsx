import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { UpgradeDialog } from '@/components/billing/upgrade-dialog'

/**
 * ENT-83 — the "Upgrade to act" modal. Pins the AC copy, the checkout CTA, and
 * the funnel tracking (shown on open, converted on CTA tap, keyed by source).
 */

const { shownMock, convertedMock } = vi.hoisted(() => ({
  shownMock: vi.fn(),
  convertedMock: vi.fn(),
}))

vi.mock('@/lib/analytics/track', () => ({
  trackUpgradePromptShown: shownMock,
  trackUpgradeConverted: convertedMock,
}))

beforeEach(() => vi.clearAllMocks())

describe('UpgradeDialog (ENT-83)', () => {
  it('explains what Pro unlocks with the AC copy when open', () => {
    render(<UpgradeDialog open onOpenChange={() => {}} source="executor_approve" />)

    expect(screen.getByText('Upgrade to act')).toBeInTheDocument()
    expect(
      screen.getByText('Pro unlocks one-tap actions. Your finding is still here, waiting.'),
    ).toBeInTheDocument()
  })

  it('points the CTA at checkout and fires the conversion event on tap', async () => {
    const user = userEvent.setup()
    render(<UpgradeDialog open onOpenChange={() => {}} source="executor_approve" />)

    const cta = screen.getByRole('link', { name: 'Upgrade to Pro' })
    expect(cta).toHaveAttribute('href', '/billing')

    await user.click(cta)
    expect(convertedMock).toHaveBeenCalledWith({ source: 'executor_approve' })
  })

  it('fires the shown event when it opens, keyed by source', () => {
    const { rerender } = render(
      <UpgradeDialog open={false} onOpenChange={() => {}} source="executor_approve" />,
    )
    expect(shownMock).not.toHaveBeenCalled()

    rerender(<UpgradeDialog open onOpenChange={() => {}} source="executor_approve" />)
    expect(shownMock).toHaveBeenCalledWith({ source: 'executor_approve' })
  })

  it('accepts overridden copy for other conversion moments', () => {
    render(
      <UpgradeDialog
        open
        onOpenChange={() => {}}
        source="ropa_cap"
        title="Custom title"
        description="Custom description"
      />,
    )
    expect(screen.getByText('Custom title')).toBeInTheDocument()
    expect(screen.getByText('Custom description')).toBeInTheDocument()
  })
})
