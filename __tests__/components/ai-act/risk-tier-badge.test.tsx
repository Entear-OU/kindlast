import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { RiskTierBadge } from '@/components/ai-act/risk-tier-badge'

describe('RiskTierBadge', () => {
  it('renders unacceptable tier with correct styling', () => {
    render(<RiskTierBadge tier="unacceptable" />)
    const badge = screen.getByText('Unacceptable')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('bg-red')
  })

  it('renders high tier with correct styling', () => {
    render(<RiskTierBadge tier="high" />)
    const badge = screen.getByText('High')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('bg-orange')
  })

  it('renders limited tier with correct styling', () => {
    render(<RiskTierBadge tier="limited" />)
    const badge = screen.getByText('Limited')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('bg-yellow')
  })

  it('renders minimal tier with correct styling', () => {
    render(<RiskTierBadge tier="minimal" />)
    const badge = screen.getByText('Minimal')
    expect(badge).toBeInTheDocument()
    expect(badge.className).toContain('bg-green')
  })
})
