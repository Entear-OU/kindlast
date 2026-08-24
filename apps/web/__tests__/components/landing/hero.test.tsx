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

  it('makes signing up the primary call to action', () => {
    // ENT-190 removed the waitlist and left the repository as the only ask,
    // because there was nothing to sign up for. ENT-189 added an assessment
    // needing no account and it took the slot. ENT-254 moved that assessment
    // inside the product, so the slot leads where the assessment now is.
    render(<Hero />)
    const cta = screen.getByRole('link', {
      name: /find out what applies to you/i,
    })
    expect(cta).toHaveAttribute('href', '/auth/signup')
  })

  it('promises nothing this button cannot deliver', () => {
    // THE FAILURE THIS CATCHES HAS A NAME. The line under the button used to
    // say "no account, and your answers never leave the page", which was true
    // of `/readiness` and is the opposite of true here: the assessment is
    // behind a sign-up and every answer is written down. A call to action that
    // promises an account-free assessment now sends somebody to a registration
    // form they did not agree to.
    const { container } = render(<Hero />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/no account/i)
    expect(copy).not.toMatch(/never leave the page/i)
  })

  it('does not link to the assessment that is no longer public', () => {
    const { container } = render(<Hero />)
    expect(container.innerHTML).not.toMatch(/href="\/readiness"/)
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
