import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { RecentFindings } from '@/components/dashboard/recent-findings'
import type { Finding } from '@/lib/types/database'

const makeFinding = (overrides: Partial<Finding> = {}): Finding => ({
  id: `finding-${Math.random()}`,
  assessment_id: 'assessment-1',
  user_id: 'user-1',
  category: 'lawful_basis',
  severity: 'high',
  title: 'Test finding title',
  description: 'Test description',
  recommendation: 'Test recommendation',
  gdpr_article: 'Art. 6',
  ai_act_article: null,
  is_resolved: false,
  resolved_at: null,
  created_at: '2024-01-01T00:00:00Z',
  ...overrides,
})

describe('RecentFindings', () => {
  it('renders up to 5 findings', () => {
    const findings = Array.from({ length: 7 }, (_, i) =>
      makeFinding({ title: `Finding ${i + 1}` })
    )

    render(<RecentFindings findings={findings} />)

    expect(screen.getByText('Finding 1')).toBeInTheDocument()
    expect(screen.getByText('Finding 5')).toBeInTheDocument()
    expect(screen.queryByText('Finding 6')).not.toBeInTheDocument()
  })

  it('renders finding titles', () => {
    const findings = [
      makeFinding({ title: 'No lawful basis documented' }),
      makeFinding({ title: 'Missing privacy policy' }),
    ]

    render(<RecentFindings findings={findings} />)

    expect(screen.getByText('No lawful basis documented')).toBeInTheDocument()
    expect(screen.getByText('Missing privacy policy')).toBeInTheDocument()
  })

  it('shows severity badges', () => {
    const findings = [
      makeFinding({ severity: 'critical', title: 'Missing DPO appointment' }),
    ]

    render(<RecentFindings findings={findings} />)

    expect(screen.getByText('critical')).toBeInTheDocument()
  })

  it('renders empty state when no findings', () => {
    render(<RecentFindings findings={[]} />)

    expect(screen.getByText(/no findings/i)).toBeInTheDocument()
  })
})
