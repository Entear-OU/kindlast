import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'

// Mock next/navigation
const mockRedirect = vi.fn()
vi.mock('next/navigation', () => ({
  redirect: (url: string) => {
    mockRedirect(url)
    throw new Error(`NEXT_REDIRECT: ${url}`)
  },
}))

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ href, children, ...props }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...props}>{children}</a>
  ),
}))

// Mock supabase server client
const mockGetUser = vi.fn()
const mockSupabase = {
  auth: { getUser: mockGetUser },
}

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn(() => Promise.resolve(mockSupabase)),
}))

// Mock queries
const mockGetLatestAssessment = vi.fn()
const mockGetFindings = vi.fn()

vi.mock('@/lib/supabase/queries', () => ({
  getLatestAssessment: (...args: unknown[]) => mockGetLatestAssessment(...args),
  getFindings: (...args: unknown[]) => mockGetFindings(...args),
}))

// Mock dashboard components
vi.mock('@/components/dashboard/score-card', () => ({
  ScoreCard: ({ score, riskLevel }: { score: number; riskLevel: string }) => (
    <div data-testid="score-card">Score: {score}, Risk: {riskLevel}</div>
  ),
}))

vi.mock('@/components/dashboard/findings-summary', () => ({
  FindingsSummary: () => <div data-testid="findings-summary">Findings Summary</div>,
}))

vi.mock('@/components/dashboard/recent-findings', () => ({
  RecentFindings: () => <div data-testid="recent-findings">Recent Findings</div>,
}))

vi.mock('@/components/dashboard/assessment-status', () => ({
  AssessmentStatus: ({ status }: { status: string }) => (
    <div data-testid="assessment-status">Status: {status}</div>
  ),
}))

vi.mock('@/components/dashboard/legal-disclaimer', () => ({
  LegalDisclaimer: () => <div data-testid="legal-disclaimer">Legal Disclaimer</div>,
}))

import DashboardPage from '@/app/(dashboard)/dashboard/page'

describe('Dashboard Page', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('redirects to /login when user is not authenticated', async () => {
    mockGetUser.mockResolvedValue({ data: { user: null }, error: null })

    await expect(DashboardPage()).rejects.toThrow('NEXT_REDIRECT: /login')
    expect(mockRedirect).toHaveBeenCalledWith('/login')
  })

  it('shows AssessmentStatus when assessment is pending', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: { id: 'a-1', status: 'pending', overall_score: null, risk_level: null },
      error: null,
    })

    const element = await DashboardPage()
    render(element)

    expect(screen.getByTestId('assessment-status')).toBeInTheDocument()
    expect(screen.getByTestId('legal-disclaimer')).toBeInTheDocument()
    expect(screen.queryByTestId('score-card')).not.toBeInTheDocument()
  })

  it('shows AssessmentStatus when assessment is processing', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: { id: 'a-1', status: 'processing', overall_score: null, risk_level: null },
      error: null,
    })

    const element = await DashboardPage()
    render(element)

    expect(screen.getByTestId('assessment-status')).toBeInTheDocument()
    expect(screen.queryByTestId('score-card')).not.toBeInTheDocument()
  })

  it('shows "No Assessment Yet" when no assessment exists', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: null,
      error: null,
    })

    const element = await DashboardPage()
    render(element)

    expect(screen.getByText('No Assessment Yet')).toBeInTheDocument()
    expect(screen.getByTestId('legal-disclaimer')).toBeInTheDocument()
    expect(screen.queryByTestId('score-card')).not.toBeInTheDocument()
  })

  it('shows ScoreCard, FindingsSummary, RecentFindings when assessment is complete', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: { id: 'a-1', status: 'complete', overall_score: 72, risk_level: 'medium' },
      error: null,
    })
    mockGetFindings.mockResolvedValue({
      data: [
        { id: 'f-1', severity: 'high', title: 'Test finding' },
      ],
      error: null,
    })

    const element = await DashboardPage()
    render(element)

    expect(screen.getByTestId('score-card')).toBeInTheDocument()
    expect(screen.getByTestId('findings-summary')).toBeInTheDocument()
    expect(screen.getByTestId('recent-findings')).toBeInTheDocument()
    expect(screen.getByTestId('legal-disclaimer')).toBeInTheDocument()
  })

  it('shows Re-run Assessment link when assessment is complete', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: { id: 'a-1', status: 'complete', overall_score: 72, risk_level: 'medium' },
      error: null,
    })
    mockGetFindings.mockResolvedValue({
      data: [],
      error: null,
    })

    const element = await DashboardPage()
    render(element)

    expect(screen.getByText('Re-run Assessment')).toBeInTheDocument()
  })

  it('fetches findings when assessment exists and is complete', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: { id: 'a-1', status: 'complete', overall_score: 72, risk_level: 'medium' },
      error: null,
    })
    mockGetFindings.mockResolvedValue({
      data: [],
      error: null,
    })

    await DashboardPage()

    expect(mockGetFindings).toHaveBeenCalledWith(mockSupabase, 'a-1')
  })

  it('does not fetch findings when no assessment exists', async () => {
    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
      error: null,
    })
    mockGetLatestAssessment.mockResolvedValue({
      data: null,
      error: null,
    })

    await DashboardPage()

    expect(mockGetFindings).not.toHaveBeenCalled()
  })
})
