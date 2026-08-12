import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { CapabilitySummary } from '@/components/landing/capability-summary'

/**
 * The home page keeps a short capability summary; the full detail moved to
 * `/features` in ENT-190. This component is the bridge between the two, so the
 * link out is as load-bearing as the copy.
 */
describe('CapabilitySummary', () => {
  it('renders the section heading', () => {
    render(<CapabilitySummary />)
    expect(screen.getByRole('heading', { level: 2 })).toBeInTheDocument()
  })

  it('summarises the capability areas', () => {
    const { container } = render(<CapabilitySummary />)
    const copy = container.textContent ?? ''
    expect(copy).toMatch(/GDPR/i)
    expect(copy).toMatch(/EU AI Act/i)
    expect(copy).toMatch(/DSAR/i)
    expect(copy).toMatch(/ROPA/i)
  })

  it('links to the features route', () => {
    render(<CapabilitySummary />)
    const link = screen.getByRole('link', { name: /capabilities in detail/i })
    expect(link).toHaveAttribute('href', '/features')
  })

  it('links to the how-it-works route', () => {
    render(<CapabilitySummary />)
    const link = screen.getByRole('link', { name: /how it works/i })
    expect(link).toHaveAttribute('href', '/how-it-works')
  })

  it('makes no pricing claim', () => {
    const { container } = render(<CapabilitySummary />)
    const copy = container.textContent ?? ''
    expect(copy).not.toMatch(/\/mo\b/i)
    expect(copy).not.toMatch(/per month/i)
  })
})
