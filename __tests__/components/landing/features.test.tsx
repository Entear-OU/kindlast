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

  it('renders the GDPR assessment feature card', () => {
    render(<Features />)
    expect(screen.getByText('GDPR Assessment')).toBeInTheDocument()
  })

  it('renders the AI Act classification feature card', () => {
    render(<Features />)
    expect(screen.getByText('AI Act Classification')).toBeInTheDocument()
  })

  it('renders the PDF export feature card', () => {
    render(<Features />)
    expect(screen.getByText('PDF Export')).toBeInTheDocument()
  })

  it('renders the compliance score feature card', () => {
    render(<Features />)
    expect(screen.getByText('Compliance Score')).toBeInTheDocument()
  })

  it('renders four feature cards total', () => {
    render(<Features />)
    const cards = screen.getAllByRole('heading', { level: 3 })
    expect(cards).toHaveLength(4)
  })
})
