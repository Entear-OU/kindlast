import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import LandingPage from '@/app/(public)/page'

describe('LandingPage', () => {
  it('renders the hero headline', () => {
    render(<LandingPage />)
    expect(screen.getByText(/EU compliance,/i)).toBeInTheDocument()
  })

  it('renders the open-source section', () => {
    // ENT-190 deliberately kept open source as a section on `/` rather than
    // promoting it to its own route: the full story already lives on GitHub.
    render(<LandingPage />)
    expect(screen.getByText(/build this twice\./i)).toBeInTheDocument()
  })

  it('renders the capability summary and points at the features route', () => {
    render(<LandingPage />)
    expect(
      screen.getByRole('link', { name: /capabilities in detail/i }),
    ).toHaveAttribute('href', '/features')
  })

  it('points at the how-it-works route', () => {
    render(<LandingPage />)
    const links = screen.getAllByRole('link', { name: /how it works/i })
    expect(links.length).toBeGreaterThan(0)
    for (const link of links) {
      expect(link).toHaveAttribute('href', '/how-it-works')
    }
  })

  it('has removed the waitlist entirely', () => {
    // ENT-190: there is no waitlist any more. No copy, no anchor, no Tally
    // form. The primary ask is now the public repository.
    const { container } = render(<LandingPage />)
    expect(container.textContent ?? '').not.toMatch(/waitlist/i)
    expect(container.innerHTML).not.toMatch(/#waitlist/i)
    expect(container.innerHTML).not.toMatch(/tally\.so/i)
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
   * The headline stat trio is gone entirely.
   *
   * It began as external facts (4% max fine, €20M threshold, an Annex III
   * deadline), which went stale as the dates arrived and the figures moved,
   * and which were fear-framed against the case the rest of the site makes.
   * Its replacement (Daily, Two, Zero) restated claims the hero and the
   * pipeline already make better. A number set that large has to earn it.
   */
  it('carries no headline statistic block', () => {
    const { container } = render(<LandingPage />)
    const copy = container.textContent ?? ''

    expect(copy).not.toMatch(/€\s?20M/)
    expect(copy).not.toMatch(/\b4%/)
    // Any bare "Mon 'YY" deadline goes stale the moment it arrives.
    expect(copy).not.toMatch(/\b[A-Z][a-z]{2}\s?'\d{2}\b/)
  })

  it('no longer scopes the audience to SMEs', () => {
    // The product is no longer positioned at small and medium companies
    // specifically, so copy that narrows it should not creep back.
    const { container } = render(<LandingPage />)
    expect(container.textContent ?? '').not.toMatch(/\bSMEs?\b/)
  })

  it('renders the footer', () => {
    render(<LandingPage />)
    expect(screen.getByText(/Not legal advice/i)).toBeInTheDocument()
  })
})
