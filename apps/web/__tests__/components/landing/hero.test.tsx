import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Hero } from '@/components/landing/hero'

describe('Hero', () => {
  it('renders the headline', () => {
    render(<Hero />)
    // Headline is split across two lines ("EU compliance," / "finally simple.")
    // by an explicit <br />, so match the fragments individually.
    expect(screen.getByText(/EU compliance,/i)).toBeInTheDocument()
    expect(screen.getByText(/finally simple\./i)).toBeInTheDocument()
  })

  it('renders the subtitle about the agents', () => {
    render(<Hero />)
    expect(screen.getByText(/GDPR and EU AI Act/i)).toBeInTheDocument()
  })

  it('renders "Read the source" as the primary call to action', () => {
    // ENT-190 removed the waitlist. The repository is public under AGPL-3.0,
    // so reading the source is the honest primary ask: there is nothing to
    // sign up for yet, and the code is the product claim.
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /Read the source/i })
    expect(cta).toHaveAttribute('href', 'https://github.com/Entear-OU/kindlast')
  })

  it('opens the repository in a new tab safely', () => {
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /Read the source/i })
    expect(cta).toHaveAttribute('target', '_blank')
    expect(cta).toHaveAttribute('rel', expect.stringContaining('noopener'))
    expect(cta).toHaveAttribute('rel', expect.stringContaining('noreferrer'))
  })

  it('does not link to sign-in', () => {
    // Reading the source is now the only ask in the hero. `/login` still
    // works for existing accounts, the marketing site just does not point
    // at it.
    render(<Hero />)
    expect(screen.queryByRole('link', { name: /sign in/i })).toBeNull()
    expect(
      screen
        .queryAllByRole('link')
        .filter((el) => el.getAttribute('href') === '/login'),
    ).toHaveLength(0)
  })

  it('has no trace of the waitlist', () => {
    const { container } = render(<Hero />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/waitlist/i)
    expect(copy).not.toMatch(/already on the list/i)
    expect(container.innerHTML).not.toMatch(/tally\.so/i)
  })
})
