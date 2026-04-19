import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { RAGResponse } from '@/lib/api/gateway'
import type { AIActClassification } from '@/lib/ai/classify-ai-risk'

const mockQueryRAGWithSchema = vi.fn()

vi.mock('@/lib/api/gateway', () => ({
  queryRAGWithSchema: (...args: unknown[]) => mockQueryRAGWithSchema(...args),
}))

describe('classifyAIRisk', () => {
  const mockAISystems = [
    {
      name: 'Customer Chatbot',
      purpose: 'Answer customer questions',
      dataUsed: 'Customer queries',
      isAutomatedDecision: false,
    },
  ]

  const mockClassificationData: AIActClassification = {
    systems: [
      {
        name: 'Customer Chatbot',
        risk_tier: 'limited',
        reasoning: 'AI system that interacts with customers requires transparency',
        obligations: ['Transparency obligations', 'Inform users they are interacting with AI'],
        ai_act_articles: ['Art. 50'],
        deadline: '2025-08-02',
      },
    ],
    overall_summary: 'One limited-risk system identified requiring transparency obligations.',
  }

  const mockCitations = [
    {
      source: 'EU AI Act',
      title: 'Article 50 - Transparency obligations',
      url: 'https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX%3A32024R1689',
      article: 'Art. 50',
      excerpt: 'Providers shall ensure that AI systems intended to interact directly with natural persons...',
      relevance_score: 0.92,
    },
  ]

  const mockRAGResponse: RAGResponse<AIActClassification> = {
    data: mockClassificationData,
    citations: mockCitations,
    model: 'claude-sonnet',
    usage: {
      prompt_tokens: 800,
      completion_tokens: 400,
      total_tokens: 1200,
    },
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockQueryRAGWithSchema.mockResolvedValue(mockRAGResponse)
  })

  it('calls queryRAGWithSchema with correct schema and collection', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')
    await classifyAIRisk(mockAISystems)

    expect(mockQueryRAGWithSchema).toHaveBeenCalledTimes(1)
    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.schema).toBeDefined()
    expect(callArgs.systemPrompt).toBeDefined()
    expect(callArgs.query).toBeDefined()
    expect(callArgs.collection).toBe('ai_act')
  })

  it('includes AI system details in the query', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')
    await classifyAIRisk(mockAISystems)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.query).toContain('Customer Chatbot')
    expect(callArgs.query).toContain('Answer customer questions')
    expect(callArgs.query).toContain('Customer queries')
  })

  it('includes system prompt about EU AI Act expertise', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')
    await classifyAIRisk(mockAISystems)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.systemPrompt).toContain('AI Act')
    expect(callArgs.systemPrompt).toContain('risk tier')
  })

  it('returns the structured classification result with citations', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')
    const result = await classifyAIRisk(mockAISystems)

    expect(result.systems).toHaveLength(1)
    expect(result.systems[0].name).toBe('Customer Chatbot')
    expect(result.systems[0].risk_tier).toBe('limited')
    expect(result.overall_summary).toContain('limited-risk')
    expect(result.citations).toHaveLength(1)
    expect(result.citations[0].source).toBe('EU AI Act')
  })

  it('classifyAIRiskSimple returns result without citations', async () => {
    const { classifyAIRiskSimple } = await import('@/lib/ai/classify-ai-risk')
    const result = await classifyAIRiskSimple(mockAISystems)

    expect(result.systems).toHaveLength(1)
    expect(result.overall_summary).toContain('limited-risk')
    expect('citations' in result).toBe(false)
  })

  it('handles multiple AI systems in the query', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')

    const multipleSystems = [
      {
        name: 'Customer Chatbot',
        purpose: 'Answer customer questions',
        dataUsed: 'Customer queries',
        isAutomatedDecision: false,
      },
      {
        name: 'Fraud Detection',
        purpose: 'Detect fraudulent transactions',
        dataUsed: 'Transaction data',
        isAutomatedDecision: true,
      },
    ]

    await classifyAIRisk(multipleSystems)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.query).toContain('Customer Chatbot')
    expect(callArgs.query).toContain('Fraud Detection')
    expect(callArgs.query).toContain('System 1')
    expect(callArgs.query).toContain('System 2')
  })

  it('passes temperature parameter for consistent results', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')
    await classifyAIRisk(mockAISystems)

    const callArgs = mockQueryRAGWithSchema.mock.calls[0][0]
    expect(callArgs.temperature).toBe(0.3)
  })

  it('handles empty AI systems array', async () => {
    const { classifyAIRisk } = await import('@/lib/ai/classify-ai-risk')

    mockQueryRAGWithSchema.mockResolvedValue({
      data: { systems: [], overall_summary: 'No AI systems to classify.' },
      citations: [],
      model: 'claude-sonnet',
      usage: { prompt_tokens: 100, completion_tokens: 50, total_tokens: 150 },
    })

    const result = await classifyAIRisk([])

    expect(result.systems).toHaveLength(0)
    expect(result.overall_summary).toBe('No AI systems to classify.')
  })
})
