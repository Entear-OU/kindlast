import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { UpgradePrompt } from '@/components/premium/upgrade-prompt'

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

describe('UpgradePrompt', () => {
  it('renders upgrade CTA card', () => {
    render(<UpgradePrompt />)

    expect(screen.getByText(/upgrade to premium/i)).toBeInTheDocument()
    expect(screen.getByText(/49/)).toBeInTheDocument()
  })

  it('contains a link to upgrade', () => {
    render(<UpgradePrompt />)

    const link = screen.getByRole('link')
    expect(link).toHaveAttribute('href', expect.stringContaining('/pricing'))
  })

  it('lists premium features', () => {
    render(<UpgradePrompt />)

    expect(screen.getByText(/full findings/i)).toBeInTheDocument()
    expect(screen.getByText(/ai act/i)).toBeInTheDocument()
    expect(screen.getByText(/pdf/i)).toBeInTheDocument()
  })
})
