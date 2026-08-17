import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { UnsubscribeForm } from '@/components/notifications/unsubscribe-form'

/**
 * The unsubscribe button (ENT-209).
 *
 * The property worth pinning is that this is a form and not a link. Corporate
 * mail gateways and link scanners follow every URL in a message before a human
 * sees it, so a one-click GET would unsubscribe people by the act of delivering
 * the email to them. The symptom is a customer who quietly stops receiving
 * compliance notifications for a reason nobody can reconstruct.
 */

vi.mock('@/app/unsubscribe/[token]/actions', () => ({
  unsubscribeAction: vi.fn(),
}))

describe('the unsubscribe control', () => {
  it('is a form with the token in the body, not a link', () => {
    const { container } = render(<UnsubscribeForm token="tok-123" />)

    expect(container.querySelector('form')).not.toBeNull()
    expect(container.querySelector('a')).toBeNull()

    const hidden = container.querySelector('input[name="token"]')
    expect(hidden).not.toBeNull()
    expect((hidden as HTMLInputElement).value).toBe('tok-123')
    expect((hidden as HTMLInputElement).type).toBe('hidden')
  })

  it('submits rather than navigating', () => {
    render(<UnsubscribeForm token="tok-123" />)

    const button = screen.getByRole('button', {
      name: /stop sending me these/i,
    })
    expect(button).toHaveAttribute('type', 'submit')
  })

  it('says nothing before it has been used', () => {
    render(<UnsubscribeForm token="tok-123" />)

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})
