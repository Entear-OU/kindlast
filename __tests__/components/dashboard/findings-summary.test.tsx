import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { FindingsSummary } from '@/components/dashboard/findings-summary'
import type { Finding } from '@/lib/types/database'

const makeFinding = (severity: Finding['severity']): Finding => ({
  id: `finding-${Math.random()}`,
  assessment_id: 'assessment-1',
  user_id: 'user-1',
  category: 'lawful_basis',
  severity,
  title: `${severity} finding`,
  description: 'A description',
  recommendation: 'A recommendation',
  gdpr_article: 'Art. 6',
  ai_act_article: null,
  is_resolved: false,
  resolved_at: null,
  created_at: '2024-01-01T00:00:00Z',
})

describe('FindingsSummary', () => {
  it('renders correct counts by severity', () => {
    const findings: Finding[] = [
      makeFinding('critical'),
      makeFinding('critical'),
      makeFinding('high'),
      makeFinding('high'),
      makeFinding('high'),
      makeFinding('medium'),
      makeFinding('low'),
      makeFinding('pass'),
      makeFinding('pass'),
    ]

    render(<FindingsSummary findings={findings} />)

    expect(screen.getByTestId('count-critical')).toHaveTextContent('2')
    expect(screen.getByTestId('count-high')).toHaveTextContent('3')
    expect(screen.getByTestId('count-medium')).toHaveTextContent('1')
    expect(screen.getByTestId('count-low')).toHaveTextContent('1')
    expect(screen.getByTestId('count-pass')).toHaveTextContent('2')
  })

  it('renders zero counts when no findings', () => {
    render(<FindingsSummary findings={[]} />)

    expect(screen.getByTestId('count-critical')).toHaveTextContent('0')
    expect(screen.getByTestId('count-high')).toHaveTextContent('0')
    expect(screen.getByTestId('count-medium')).toHaveTextContent('0')
    expect(screen.getByTestId('count-low')).toHaveTextContent('0')
    expect(screen.getByTestId('count-pass')).toHaveTextContent('0')
  })

  it('displays severity labels', () => {
    render(<FindingsSummary findings={[]} />)

    expect(screen.getByText(/critical/i)).toBeInTheDocument()
    expect(screen.getByText(/high/i)).toBeInTheDocument()
    expect(screen.getByText(/medium/i)).toBeInTheDocument()
    expect(screen.getByText(/low/i)).toBeInTheDocument()
    expect(screen.getByText(/pass/i)).toBeInTheDocument()
  })
})
