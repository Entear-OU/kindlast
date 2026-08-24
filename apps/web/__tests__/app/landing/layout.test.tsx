import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import PublicLayout from '@/app/(public)/layout'

/**
 * ENT-190 turned the single-page site into three routes, so the header nav is
 * now real navigation rather than in-page anchors. Anchors would only resolve
 * on `/`, which makes them dead links from the other two routes.
 */
describe('PublicLayout', () => {
  it('renders its children', () => {
    render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    expect(screen.getByText('page body')).toBeInTheDocument()
  })

  it('links the nav at the real routes', () => {
    render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    const nav = screen.getByRole('navigation')
    expect(
      screen.getByRole('link', { name: /^How it works$/i }),
    ).toHaveAttribute('href', '/how-it-works')
    expect(screen.getByRole('link', { name: /^Features$/i })).toHaveAttribute(
      'href',
      '/features',
    )
    expect(nav).toBeInTheDocument()
  })

  it('uses no in-page anchors in the nav', () => {
    const { container } = render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    expect(container.innerHTML).not.toMatch(/href="#/)
  })

  it('makes signing up the header call to action', () => {
    // ENT-190 put the repository in this slot because the site had nothing to
    // do but read. ENT-189 gave it an assessment that needed no account.
    // ENT-254 moved the assessment inside the product, so the pill points at
    // the sign-up it now sits behind.
    render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    const cta = screen.getByRole('link', { name: /get started/i })
    expect(cta).toHaveAttribute('href', '/auth/signup')
  })

  it('links nowhere that no longer resolves', () => {
    // `/readiness` is gone, and a header linking to it would put a 404 in the
    // one place every marketing page shares.
    const { container } = render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    expect(container.innerHTML).not.toMatch(/href="\/readiness"/)
  })

  it('still reaches the repository, as the icon beside it', () => {
    render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    const repo = screen.getByRole('link', { name: /Kindlast on GitHub/i })
    expect(repo).toHaveAttribute(
      'href',
      'https://github.com/Entear-OU/kindlast',
    )
    expect(repo).toHaveAttribute('target', '_blank')
    expect(repo).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('does not link to sign-in', () => {
    // The marketing site no longer advertises an account. `/login` still
    // exists and still works for anyone who has one, it is simply not
    // something the public pages promote.
    render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    expect(screen.queryByRole('link', { name: /sign in/i })).toBeNull()
    expect(
      screen
        .queryAllByRole('link')
        .filter((el) => el.getAttribute('href') === '/login'),
    ).toHaveLength(0)
  })

  it('has no trace of the waitlist', () => {
    const { container } = render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    expect(container.textContent ?? '').not.toMatch(/waitlist/i)
    expect(container.innerHTML).not.toMatch(/tally\.so/i)
  })

  it('keeps the home link on the wordmark', () => {
    render(
      <PublicLayout>
        <p>page body</p>
      </PublicLayout>,
    )
    // Exact name match: the header's icon-only repo link is labelled
    // "Kindlast on GitHub", so a fuzzy match would find two links.
    expect(screen.getByRole('link', { name: 'kindlast' })).toHaveAttribute(
      'href',
      '/',
    )
  })
})
