import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { FindingCard } from '@/components/findings/finding-card'
import type { Finding } from '@/lib/types/database'

const mockFinding: Finding = {
  id: 'finding-1',
  assessment_id: 'assessment-1',
  user_id: 'user-1',
  category: 'lawful_basis',
  severity: 'critical',
  title: 'No lawful basis documented',
  description: 'The business has not documented a lawful basis for processing personal data.',
  recommendation: 'Document "consent" as your lawful basis for marketing emails.',
  gdpr_article: 'Art. 6',
  ai_act_article: null,
  is_resolved: false,
  resolved_at: null,
  created_at: '2024-01-01T00:00:00Z',
}

describe('FindingCard', () => {
  it('renders the finding title', () => {
    render(<FindingCard finding={mockFinding} />)
    expect(screen.getByText('No lawful basis documented')).toBeInTheDocument()
  })

  it('renders the severity badge', () => {
    render(<FindingCard finding={mockFinding} />)
    expect(screen.getByText(/critical/i)).toBeInTheDocument()
  })

  it('renders the description', () => {
    render(<FindingCard finding={mockFinding} />)
    expect(
      screen.getByText(/has not documented a lawful basis/)
    ).toBeInTheDocument()
  })

  it('renders the recommendation', () => {
    render(<FindingCard finding={mockFinding} />)
    expect(
      screen.getByText(/Document "consent" as your lawful basis/)
    ).toBeInTheDocument()
  })

  it('renders the GDPR article', () => {
    render(<FindingCard finding={mockFinding} />)
    expect(screen.getByText('Art. 6')).toBeInTheDocument()
  })

  it('shows resolved state when is_resolved is true', () => {
    render(<FindingCard finding={{ ...mockFinding, is_resolved: true }} />)
    expect(screen.getByText(/resolved/i)).toBeInTheDocument()
  })

  it('renders category label', () => {
    render(<FindingCard finding={mockFinding} />)
    expect(screen.getByText('Lawful Basis')).toBeInTheDocument()
  })
})
