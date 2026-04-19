import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { BusinessProfile } from '@/lib/types/database'
import type { RAGResponse } from '@/lib/api/gateway'
import type { AssessmentResult } from '@/lib/ai/schemas'

const mockQueryRAGWithSchema = vi.fn()

vi.mock('@/lib/api/gateway', () => ({
  queryRAGWithSchema: (...args: unknown[]) => mockQueryRAGWithSchema(...args),
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

  const mockAssessmentData: AssessmentResult = {
    overall_score: 45,
    risk_level: 'high',
    summary: 'Significant compliance gaps found.',
    findings: [
      {
        category: 'cookie_compliance',
        severity: 'high',
        title: 'No cookie consent mechanism',
        description: 'The business lacks a cookie consent mechanism.',
        recommendation: 'Implement a cookie consent banner.',
        gdpr_article: 'Art. 7',
      },
    ],
  }

  const mockCitations = [
    {
      source: 'GDPR',
      title: 'Article 7 - Conditions for consent',
      url: 'https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX%3A32016R0679',
      article: 'Art. 7',
      excerpt: 'Where processing is based on consent, the controller shall be able to demonstrate...',
      relevance_score: 0.95,
    },
  ]

  const mockRAGResponse: RAGResponse<AssessmentResult> = {
    data: mockAssessmentData,
    citations: mockCitations,
    model: 'claude-sonnet',
    usage: {
      prompt_tokens: 1000,
      completion_tokens: 500,
      total_tokens: 1500,
    },
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockQueryRAGWithSchema.mockResolvedValue(mockRAGResponse)
  })

  it('calls queryRAG with correct schema and collection', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    expect(mockQueryRAGWithSchema).toHaveBeenCalledTimes(1)
    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.schema).toBeDefined()
    expect(callArgs.systemPrompt).toBeDefined()
    expect(callArgs.query).toBeDefined()
    expect(callArgs.collection).toBe('gdpr')
    expect(callArgs.topK).toBe(10)
  })

  it('includes business profile data in the query', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.query).toContain('Test Corp')
    expect(callArgs.query).toContain('Estonia')
    expect(callArgs.query).toContain('SaaS')
    expect(callArgs.query).toContain('12')
    expect(callArgs.query).toContain('email')
    expect(callArgs.query).toContain('Stripe')
  })

  it('includes system prompt about GDPR expertise', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.systemPrompt).toContain('expert EU data protection consultant')
    expect(callArgs.systemPrompt).toContain('GDPR')
  })

  it('returns the structured assessment result with citations', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    const result = await assessGDPRCompliance(mockProfile)

    expect(result.overall_score).toBe(45)
    expect(result.risk_level).toBe('high')
    expect(result.findings).toHaveLength(1)
    expect(result.citations).toHaveLength(1)
    expect(result.citations[0].source).toBe('GDPR')
  })

  it('assessGDPRComplianceSimple returns result without citations', async () => {
    const { assessGDPRComplianceSimple } = await import('@/lib/ai/assess-gdpr')
    const result = await assessGDPRComplianceSimple(mockProfile)

    expect(result.overall_score).toBe(45)
    expect(result.risk_level).toBe('high')
    expect(result.findings).toHaveLength(1)
    expect('citations' in result).toBe(false)
  })

  it('passes temperature parameter for consistent results', async () => {
    const { assessGDPRCompliance } = await import('@/lib/ai/assess-gdpr')
    await assessGDPRCompliance(mockProfile)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.temperature).toBe(0.3)
  })
})
