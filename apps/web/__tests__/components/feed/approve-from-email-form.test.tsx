import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { ApproveFromEmailForm } from '@/components/feed/approve-from-email-form'

/**
 * The approve-from-email control (§8, ENT-249).
 *
 * The property worth pinning is that this is a form and not a link, and it is
 * the same property the unsubscribe control carries with more at stake.
 * Corporate mail gateways, link previewers and archiving proxies follow every
 * URL in a message before a human sees it. Under a link, delivering a finding
 * notification would approve the finding: a regulatory decision written into
 * the customer's compliance record, with an audit row naming somebody who never
 * opened the message.
 *
 * PROVEN ABLE TO FAIL. Replacing the form with an anchor to the same endpoint
 * turns "is a form and not a link" red, and nothing else in the file moves.
 */

vi.mock('@/app/approve/[findingId]/[token]/actions', () => ({
  approveFromEmailAction: vi.fn(),
}))

describe('the approve-from-email control', () => {
  it('is a form with both halves in the body, not a link', () => {
    const { container } = render(
      <ApproveFromEmailForm findingId="f-123" token="deleg-123" />,
    )

    expect(container.querySelector('form')).not.toBeNull()
    expect(container.querySelector('a')).toBeNull()

    // Both halves travel, because core-api refuses a delegation whose binding
    // does not match the finding presented. Neither is enough on its own, which
    // is what makes a token recovered from a mail relay's logs worthless.
    const token = container.querySelector('input[name="token"]')
    const finding = container.querySelector('input[name="findingId"]')
    expect((token as HTMLInputElement).value).toBe('deleg-123')
    expect((token as HTMLInputElement).type).toBe('hidden')
    expect((finding as HTMLInputElement).value).toBe('f-123')
    expect((finding as HTMLInputElement).type).toBe('hidden')
  })

  it('submits rather than navigating', () => {
    render(<ApproveFromEmailForm findingId="f-123" token="deleg-123" />)

    const button = screen.getByRole('button', { name: /approve this finding/i })
    expect(button).toHaveAttribute('type', 'submit')
  })

  it('says nothing before it has been used', () => {
    render(<ApproveFromEmailForm findingId="f-123" token="deleg-123" />)

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
