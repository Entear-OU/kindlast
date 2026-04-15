import { describe, it, expect, vi, beforeEach } from 'vitest'
import { NextRequest } from 'next/server'

const mockGetUser = vi.fn()
const mockFrom = vi.fn()
const mockAssessGDPRCompliance = vi.fn()

vi.mock('@/lib/supabase/server', () => ({
  createClient: vi.fn().mockResolvedValue({
    auth: {
      getUser: () => mockGetUser(),
    },
    from: (...args: unknown[]) => mockFrom(...args),
  }),
}))

vi.mock('@/lib/ai/assess-gdpr', () => ({
  assessGDPRCompliance: (...args: unknown[]) => mockAssessGDPRCompliance(...args),
}))

describe('POST /api/assess', () => {
  const mockProfile = {
    id: 'profile-1',
    user_id: 'user-1',
    company_name: 'Test Corp',
    country: 'Estonia',
  }

  const mockAssessment = {
    id: 'assessment-1',
    user_id: 'user-1',
    profile_id: 'profile-1',
    type: 'gdpr',
    status: 'processing',
  }

  const mockAIResult = {
    overall_score: 67,
    risk_level: 'medium',
    summary: 'Some gaps found.',
    findings: [
      {
        category: 'lawful_basis',
        severity: 'critical',
        title: 'No lawful basis',
        description: 'Missing lawful basis.',
        recommendation: 'Document lawful basis.',
        gdpr_article: 'Art. 6',
      },
    ],
  }

  beforeEach(() => {
    vi.clearAllMocks()

    mockGetUser.mockResolvedValue({
      data: { user: { id: 'user-1' } },
    })

    // Chain: from('business_profiles').select().eq().single()
    const profileChain = {
      select: vi.fn().mockReturnValue({
        eq: vi.fn().mockReturnValue({
          single: vi.fn().mockResolvedValue({ data: mockProfile, error: null }),
        }),
      }),
    }

    // Chain: from('assessments').insert().select().single()
    const assessmentInsertChain = {
      insert: vi.fn().mockReturnValue({
        select: vi.fn().mockReturnValue({
          single: vi.fn().mockResolvedValue({ data: mockAssessment, error: null }),
        }),
      }),
      update: vi.fn().mockReturnValue({
        eq: vi.fn().mockResolvedValue({ error: null }),
      }),
    }

    // Chain: from('findings').insert()
    const findingsChain = {
      insert: vi.fn().mockResolvedValue({ error: null }),
    }

    mockFrom.mockImplementation((table: string) => {
      if (table === 'business_profiles') return profileChain
      if (table === 'assessments') return assessmentInsertChain
      if (table === 'findings') return findingsChain
      return {}
    })

    mockAssessGDPRCompliance.mockResolvedValue(mockAIResult)
  })

  it('returns 401 when not authenticated', async () => {
    mockGetUser.mockResolvedValue({ data: { user: null } })

    const { POST } = await import('@/app/api/assess/route')
    const request = new NextRequest('http://localhost:3000/api/assess', {
      method: 'POST',
      body: JSON.stringify({ profileId: 'profile-1' }),
    })

    const response = await POST(request)
    expect(response.status).toBe(401)
  })

  it('returns 400 when profileId is missing', async () => {
    const { POST } = await import('@/app/api/assess/route')
    const request = new NextRequest('http://localhost:3000/api/assess', {
      method: 'POST',
      body: JSON.stringify({}),
    })

    const response = await POST(request)
    expect(response.status).toBe(400)
  })

  it('creates assessment, runs AI, and returns assessment ID', async () => {
    const { POST } = await import('@/app/api/assess/route')
    const request = new NextRequest('http://localhost:3000/api/assess', {
      method: 'POST',
      body: JSON.stringify({ profileId: 'profile-1' }),
    })

    const response = await POST(request)
    const data = await response.json()

    expect(response.status).toBe(200)
    expect(data.assessmentId).toBe('assessment-1')
    expect(mockAssessGDPRCompliance).toHaveBeenCalledWith(mockProfile)
  })

  it('calls supabase to save findings', async () => {
    const { POST } = await import('@/app/api/assess/route')
    const request = new NextRequest('http://localhost:3000/api/assess', {
      method: 'POST',
      body: JSON.stringify({ profileId: 'profile-1' }),
    })

    await POST(request)

    expect(mockFrom).toHaveBeenCalledWith('findings')
  })
})
