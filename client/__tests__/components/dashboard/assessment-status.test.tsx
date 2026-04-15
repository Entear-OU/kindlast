import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { AssessmentStatus } from '@/components/dashboard/assessment-status'

describe('AssessmentStatus', () => {
  it('shows pending status', () => {
    render(<AssessmentStatus status="pending" />)
    expect(screen.getByText(/pending/i)).toBeInTheDocument()
  })

  it('shows processing status', () => {
    render(<AssessmentStatus status="processing" />)
    expect(screen.getByText(/processing|analyzing/i)).toBeInTheDocument()
  })

  it('shows complete status', () => {
    render(<AssessmentStatus status="complete" />)
    expect(screen.getByText(/complete/i)).toBeInTheDocument()
  })

  it('shows error status', () => {
    render(<AssessmentStatus status="error" />)
    expect(screen.getByText(/error/i)).toBeInTheDocument()
  })

  it('renders with correct data attribute for styling', () => {
    const { container } = render(<AssessmentStatus status="processing" />)
    const statusEl = container.querySelector('[data-status]')
    expect(statusEl?.getAttribute('data-status')).toBe('processing')
  })
})
