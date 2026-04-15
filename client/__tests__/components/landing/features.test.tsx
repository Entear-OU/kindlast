import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Features } from '@/components/landing/features'

describe('Features', () => {
  it('renders the section heading', () => {
    render(<Features />)
    expect(
      screen.getByText(/Everything you need for EU compliance/i)
    ).toBeInTheDocument()
  })

  it('renders the GDPR assessment feature', () => {
    render(<Features />)
    expect(screen.getByText('GDPR Gap Analysis')).toBeInTheDocument()
    expect(
      screen.getByText(/article-level findings/i)
    ).toBeInTheDocument()
  })

  it('renders the AI Act classification feature', () => {
    render(<Features />)
    expect(screen.getByText('EU AI Act Classification')).toBeInTheDocument()
  })

  it('renders the compliance score feature', () => {
    render(<Features />)
    expect(screen.getByText('Compliance Score & Dashboard')).toBeInTheDocument()
  })

  it('renders the PDF export feature', () => {
    render(<Features />)
    expect(screen.getByText('Audit-Ready PDF Reports')).toBeInTheDocument()
  })

  it('renders the actionable recommendations feature', () => {
    render(<Features />)
    expect(screen.getByText('Actionable Recommendations')).toBeInTheDocument()
  })

  it('renders the data security feature', () => {
    render(<Features />)
    expect(screen.getByText('Privacy-First Architecture')).toBeInTheDocument()
  })

  it('renders six feature cards total', () => {
    render(<Features />)
    const cards = screen.getAllByRole('heading', { level: 3 })
    expect(cards).toHaveLength(6)
  })

  it('renders highlight labels for premium features', () => {
    render(<Features />)
    const premiumBadges = screen.getAllByText('Premium')
    expect(premiumBadges.length).toBeGreaterThanOrEqual(2)
  })
})
