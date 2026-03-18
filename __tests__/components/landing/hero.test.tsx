import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Hero } from '@/components/landing/hero'

describe('Hero', () => {
  it('renders the headline', () => {
    render(<Hero />)
    expect(
      screen.getByText('Two regulations, one platform, zero guesswork')
    ).toBeInTheDocument()
  })

  it('renders the subtitle about AI-powered compliance', () => {
    render(<Hero />)
    expect(
      screen.getByText(/AI-powered GDPR & AI Act compliance/i)
    ).toBeInTheDocument()
  })

  it('renders the CTA button with correct text', () => {
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /Get Started Free/i })
    expect(cta).toBeInTheDocument()
  })

  it('CTA button links to the login page', () => {
    render(<Hero />)
    const cta = screen.getByRole('link', { name: /Get Started Free/i })
    expect(cta).toHaveAttribute('href', '/login')
  })
})
