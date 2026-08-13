import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Footer } from '@/components/landing/footer'

describe('Footer', () => {
  it('renders the brand blurb', () => {
    render(<Footer />)
    expect(
      screen.getByText(/GDPR & EU AI Act compliance for companies building in Europe/i)
    ).toBeInTheDocument()
  })

  it('links to the GitHub repository', () => {
    render(<Footer />)
    const link = screen.getByRole('link', { name: /^GitHub$/i })
    expect(link).toHaveAttribute('href', 'https://github.com/Entear-OU/kindlast')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('links to the licence file', () => {
    render(<Footer />)
    const link = screen.getByRole('link', { name: /Licence/i })
    expect(link).toHaveAttribute(
      'href',
      'https://github.com/Entear-OU/kindlast/blob/main/LICENSE'
    )
  })

  it('names the licence in the bottom bar', () => {
    render(<Footer />)
    expect(screen.getByText(/AGPL-3\.0/i)).toBeInTheDocument()
  })

  it('keeps the not-legal-advice disclaimer', () => {
    render(<Footer />)
    expect(screen.getByText(/Not legal advice/i)).toBeInTheDocument()
  })

  it('links the product columns at the new routes, not in-page anchors', () => {
    // ENT-190 split the single-page site into `/`, `/how-it-works` and
    // `/features`. An `#features` anchor in the footer only resolves on the
    // home page, so it would be a dead affordance from the other two routes.
    render(<Footer />)
    expect(screen.getByRole('link', { name: /^Features$/i })).toHaveAttribute(
      'href',
      '/features'
    )
    expect(screen.getByRole('link', { name: /^How it works$/i })).toHaveAttribute(
      'href',
      '/how-it-works'
    )
  })

  it('does not link to sign-in', () => {
    // The "Account" column is gone with it: sign-in was its only entry, so
    // keeping the heading would leave an empty column in the footer grid.
    render(<Footer />)
    expect(screen.queryByRole('link', { name: /sign in/i })).toBeNull()
    expect(
      screen.queryAllByRole('link').filter((el) => el.getAttribute('href') === '/login')
    ).toHaveLength(0)
    expect(screen.queryByText(/^Account$/i)).toBeNull()
  })

  it('has no trace of the waitlist', () => {
    const { container } = render(<Footer />)
    expect(container.textContent ?? '').not.toMatch(/waitlist/i)
    expect(container.innerHTML).not.toMatch(/#waitlist/i)
    expect(container.innerHTML).not.toMatch(/tally\.so/i)
  })
})
