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

  it('renders the subtitle about AI-powered compliance', () => {
    render(<Hero />)
    expect(
      screen.getByText(/AI-powered GDPR and EU AI Act assessment/i)
    ).toBeInTheDocument()
  })

  it('renders the waitlist CTA link', () => {
    // The hero's CTA was previously a "Get Started Free" button linking to
    // /login. The landing page now collects emails via a Tally-hosted
    // waitlist instead, so the CTA is the waitlist Link rendered by
    // <WaitlistForm />.
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /Join the waitlist/i })
    expect(cta).toBeInTheDocument()
  })

  it('waitlist CTA points at the Tally form', () => {
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /Join the waitlist/i })
    expect(cta).toHaveAttribute('href', 'https://tally.so/r/zxZaaM')
  })
})
