import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { Features } from '@/components/landing/features'

describe('Features', () => {
  it('renders the section heading', () => {
    render(<Features />)
    // Heading is split with a <br /> between "Everything you need" and
    // "for EU compliance", so match each fragment independently.
    expect(screen.getByText(/Everything you need/i)).toBeInTheDocument()
    expect(screen.getByText(/for EU compliance/i)).toBeInTheDocument()
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
    expect(screen.getByText('Compliance Score')).toBeInTheDocument()
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

  it('highlights accent features with an additional detail paragraph', () => {
    // The original "Premium" badge UI was redesigned into bento-grid accent
    // cards (GDPR + AI Act) that render an extra detail paragraph beneath
    // the description. This test pins that behaviour so a future refactor
    // can't silently flatten the visual hierarchy.
    render(<Features />)
    expect(
      screen.getByText(/article-level findings/i)
    ).toBeInTheDocument()
    expect(
      screen.getByText(/documentation requirements/i)
    ).toBeInTheDocument()
  })
})
