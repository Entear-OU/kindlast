import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ScoreCard } from '@/components/dashboard/score-card'

describe('ScoreCard', () => {
  it('renders the compliance score', () => {
    render(<ScoreCard score={67} riskLevel="medium" />)
    expect(screen.getByText('67')).toBeInTheDocument()
  })

  it('shows critical risk for scores 0-29', () => {
    render(<ScoreCard score={25} riskLevel="critical" />)
    expect(screen.getByText('25')).toBeInTheDocument()
    expect(screen.getByText('Critical Risk')).toBeInTheDocument()
  })

  it('shows high risk for scores 30-49', () => {
    render(<ScoreCard score={40} riskLevel="high" />)
    expect(screen.getByText('40')).toBeInTheDocument()
    expect(screen.getByText('High Risk')).toBeInTheDocument()
  })

  it('shows medium risk for scores 50-69', () => {
    render(<ScoreCard score={55} riskLevel="medium" />)
    expect(screen.getByText('55')).toBeInTheDocument()
    expect(screen.getByText('Medium Risk')).toBeInTheDocument()
  })

  it('shows mostly compliant for scores 70-89', () => {
    render(<ScoreCard score={80} riskLevel="low" />)
    expect(screen.getByText('80')).toBeInTheDocument()
  })

  it('shows compliant for scores 90-100', () => {
    render(<ScoreCard score={95} riskLevel="low" />)
    expect(screen.getByText('95')).toBeInTheDocument()
  })

  it('applies correct color class for critical score', () => {
    const { container } = render(<ScoreCard score={20} riskLevel="critical" />)
    const scoreEl = container.querySelector('[data-score-color]')
    expect(scoreEl?.getAttribute('data-score-color')).toBe('critical')
  })

  it('applies correct color class for high score', () => {
    const { container } = render(<ScoreCard score={35} riskLevel="high" />)
    const scoreEl = container.querySelector('[data-score-color]')
    expect(scoreEl?.getAttribute('data-score-color')).toBe('high')
  })

  it('applies correct color class for medium score', () => {
    const { container } = render(<ScoreCard score={55} riskLevel="medium" />)
    const scoreEl = container.querySelector('[data-score-color]')
    expect(scoreEl?.getAttribute('data-score-color')).toBe('medium')
  })

  it('applies correct color class for compliant score', () => {
    const { container } = render(<ScoreCard score={95} riskLevel="low" />)
    const scoreEl = container.querySelector('[data-score-color]')
    expect(scoreEl?.getAttribute('data-score-color')).toBe('compliant')
  })
})
