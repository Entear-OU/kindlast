import { describe, it, expect, vi } from 'vitest'

const mockGenerateObject = vi.fn()

vi.mock('ai', () => ({
  generateObject: (...args: unknown[]) => mockGenerateObject(...args),
}))

vi.mock('@ai-sdk/google', () => ({
  google: vi.fn((model: string) => `google-${model}`),
}))

vi.stubEnv('GOOGLE_GENERATIVE_AI_API_KEY', 'test-key')

import { classifyAIRisk } from '@/lib/ai/classify-ai-risk'

describe('classifyAIRisk', () => {
  it('calls generateObject with correct model and schema', async () => {
    const mockResult = {
      systems: [
        {
          name: 'Customer Chatbot',
          risk_tier: 'limited',
          reasoning: 'AI system that interacts with customers',
          obligations: ['Transparency obligations'],
          ai_act_articles: ['Art. 52'],
          deadline: '2025-08-02',
        },
      ],
      overall_summary: 'One limited-risk system identified.',
    }

    mockGenerateObject.mockResolvedValue({ object: mockResult })

    const aiSystems = [
      {
        name: 'Customer Chatbot',
        purpose: 'Answer customer questions',
        dataUsed: 'Customer queries',
        isAutomatedDecision: false,
      },
    ]

    const result = await classifyAIRisk(aiSystems)

    expect(mockGenerateObject).toHaveBeenCalledWith(
      expect.objectContaining({
        model: 'google-gemini-2.5-flash',
        prompt: expect.stringContaining('Customer Chatbot'),
      })
    )
    expect(result).toEqual(mockResult)
  })

  it('includes system prompt about EU AI Act expertise', async () => {
    mockGenerateObject.mockResolvedValue({
      object: { systems: [], overall_summary: 'No systems.' },
    })

    await classifyAIRisk([])

    expect(mockGenerateObject).toHaveBeenCalledWith(
      expect.objectContaining({
        system: expect.stringContaining('AI Act'),
      })
    )
  })
})
