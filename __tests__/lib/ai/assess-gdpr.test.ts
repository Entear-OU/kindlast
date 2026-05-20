import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { BusinessProfile } from '@/lib/types/database'

const mockGenerateObject = vi.fn()

vi.mock('ai', () => ({
  generateObject: (...args: unknown[]) => mockGenerateObject(...args),
}))

vi.mock('@ai-sdk/google', () => ({
  google: vi.fn((model: string) => ({ modelId: model })),
}))

describe('assessGDPRCompliance', () => {
  const mockProfile: BusinessProfile = {
    id: 'profile-1',
    user_id: 'user-1',
    company_name: 'Test Corp',
    country: 'Estonia',
    industry: 'SaaS',
    employee_count: 12,
    processes_personal_data: true,
    data_types: ['email', 'payment'],
    uses_ai_systems: false,
    ai_system_descriptions: null,
    third_party_processors: ['Stripe', 'Google Analytics'],
    transfers_data_outside_eu: false,
    has_dpo: false,
    has_privacy_policy: true,
    has_cookie_consent: false,
    has_breach_notification: false,
    has_dsr_process: false,
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
  }

  const mockResult = {
    overall_score: 45,
    risk_level: 'high' as const,
    summary: 'Significant compliance gaps found.',
    findings: [
      {
        category: 'cookie_compliance' as const,
        severity: 'high' as const,
        title: 'No cookie consent mechanism',
        description: 'The business lacks a cookie consent mechanism.',
        recommendation: 'Implement a cookie consent banner.',
        gdpr_article: 'Art. 7',
      },
    ],
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockGenerateObject.mockResolvedValue({ object: mockResult })
  })

  it('calls generateObject with correct schema and model', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    expect(mockGenerateObject).toHaveBeenCalledTimes(1)
    const callArgs = mockGenerateObject.mock.calls[0][0]
    expect(callArgs.model).toBeDefined()
    expect(callArgs.schema).toBeDefined()
    expect(callArgs.system).toBeDefined()
    expect(callArgs.prompt).toBeDefined()
  })

  it('includes business profile data in the prompt', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    const callArgs = mockGenerateObject.mock.calls[0][0]
    expect(callArgs.prompt).toContain('Test Corp')
    expect(callArgs.prompt).toContain('Estonia')
    expect(callArgs.prompt).toContain('SaaS')
    expect(callArgs.prompt).toContain('12')
    expect(callArgs.prompt).toContain('email')
    expect(callArgs.prompt).toContain('Stripe')
  })

  it('includes system prompt about GDPR expertise', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    const callArgs = mockGenerateObject.mock.calls[0][0]
    expect(callArgs.system).toContain('expert EU data protection consultant')
    expect(callArgs.system).toContain('GDPR')
  })

  it('returns the structured assessment result', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    const result = await assessGDPRCompliance(mockProfile)

    expect(result).toEqual(mockResult)
    expect(result.overall_score).toBe(45)
    expect(result.risk_level).toBe('high')
    expect(result.findings).toHaveLength(1)
  })
})
