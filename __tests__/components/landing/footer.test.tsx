import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Footer } from '@/components/landing/footer'

describe('Footer', () => {
  it('renders the brand blurb', () => {
    render(<Footer />)
    expect(
      screen.getByText(/AI-powered GDPR & EU AI Act compliance/i)
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
})
