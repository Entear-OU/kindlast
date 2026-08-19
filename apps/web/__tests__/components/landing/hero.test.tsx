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

  it('makes the readiness check the primary call to action', () => {
    // ENT-190 removed the waitlist and left the repository as the only ask,
    // because there was nothing to sign up for. ENT-189 added something a
    // visitor can actually do without signing up for anything, so it leads and
    // the repository stays beside it.
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /check where you stand/i })
    expect(cta).toHaveAttribute('href', '/readiness')
  })

  it('promises no account and no transmission next to that button', () => {
    // The claim is the reason somebody clicks it, and it is only true because
    // the assessment has no server side at all.
    render(<Hero />)
    expect(screen.getByText(/never leave the page/i)).toBeInTheDocument()
  })

  it('keeps the repository as the second ask', () => {
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
