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
      screen.getByRole('link', { name: /capabilities in detail/i })
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
   * The headline trio used to be external facts (max fine, fine threshold,
   * Annex III deadline). They went stale on two axes: the dates arrive, and
   * the figures move with each amendment. They were also fear-framed, which
   * argues against the case the rest of the site makes.
   *
   * These replacements are invariants of the system itself, so they cannot
   * expire, and this test fails if a perishable figure creeps back in.
   */
  it('states system invariants rather than perishable regulatory figures', () => {
    render(<LandingPage />)
    expect(screen.getByText('Daily')).toBeInTheDocument()
    expect(screen.getByText('Two')).toBeInTheDocument()
    expect(screen.getByText('Zero')).toBeInTheDocument()
  })

  it('carries no dated regulatory claims in the headline stats', () => {
    const { container } = render(<LandingPage />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/€\s?20M/)
    expect(copy).not.toMatch(/\b4%/)
    // Any bare "Mon 'YY" deadline will go stale the moment it arrives.
    expect(copy).not.toMatch(/\b[A-Z][a-z]{2}\s?'\d{2}\b/)
  })

  it('renders the footer', () => {
    render(<LandingPage />)
    expect(screen.getByText(/Not legal advice/i)).toBeInTheDocument()
  })
})
