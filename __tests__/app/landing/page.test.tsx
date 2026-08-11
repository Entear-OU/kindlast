import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import LandingPage from '@/app/(public)/page'

describe('LandingPage', () => {
  it('renders the open-source section', () => {
    render(<LandingPage />)
    expect(screen.getByText(/build this twice\./i)).toBeInTheDocument()
  })

  it('still renders the waitlist CTA', () => {
    render(<LandingPage />)
    expect(screen.getByText(/Join the waitlist\./i)).toBeInTheDocument()
  })

  /**
   * Pricing is off the public site for now: the €49/mo figure and the
   * "founding-member pricing" promise both predate the AGPL relicence and the
   * backend rebuild, and we don't want a number on the page we aren't ready to
   * stand behind. The authed billing surfaces (components/billing) are
   * deliberately untouched, this assertion guards the marketing page only.
   */
  it('makes no pricing claim', () => {
    const { container } = render(<LandingPage />)
    const copy = container.textContent ?? ''

    expect(copy).not.toMatch(/founding-member/i)
    expect(copy).not.toMatch(/\/mo\b/i)
    expect(copy).not.toMatch(/per month/i)
    expect(copy).not.toMatch(/€\s?49/)
  })

  /**
   * Guard against over-correcting: the €20M GDPR fine threshold is regulatory
   * content, not a price, and removing it would gut the problem statement.
   */
  it('keeps the regulatory fine statistics', () => {
    render(<LandingPage />)
    expect(screen.getByText('€20M')).toBeInTheDocument()
    expect(screen.getByText('4%')).toBeInTheDocument()
  })
})
