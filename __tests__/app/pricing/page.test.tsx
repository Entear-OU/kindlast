import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import PricingPage from '@/app/(public)/pricing/page'

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

describe('Pricing Page', () => {
  it('renders Free and Premium tiers', () => {
    render(<PricingPage />)

    expect(screen.getAllByText('Free').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('Premium').length).toBeGreaterThanOrEqual(1)
  })

  it('shows premium price', () => {
    render(<PricingPage />)

    expect(screen.getByText(/49/)).toBeInTheDocument()
  })

  it('lists features for both tiers', () => {
    render(<PricingPage />)

    // Free features
    expect(screen.getAllByText(/compliance score/i).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText(/top 3 findings/i).length).toBeGreaterThanOrEqual(1)

    // Premium features
    expect(screen.getAllByText(/full findings/i).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText(/ai act/i).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText(/pdf/i).length).toBeGreaterThanOrEqual(1)
  })

  it('has CTA buttons', () => {
    render(<PricingPage />)

    const buttons = screen.getAllByRole('link')
    expect(buttons.length).toBeGreaterThanOrEqual(2)
  })
})
