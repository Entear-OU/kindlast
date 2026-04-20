import { z } from 'zod'
import { queryRAGWithSchema, type LegacyCitation } from '@/lib/api/gateway'

// Re-export Citation type for consumers
export type Citation = LegacyCitation

export const AIActClassificationSchema = z.object({
  systems: z.array(
    z.object({
      name: z.string(),
      risk_tier: z.enum(['unacceptable', 'high', 'limited', 'minimal']),
      reasoning: z.string(),
      obligations: z.array(z.string()),
      ai_act_articles: z.array(z.string()),
      deadline: z.string(),
    })
  ),
  overall_summary: z.string(),
})

export type AIActClassification = z.infer<typeof AIActClassificationSchema>

export interface AIActClassificationWithCitations extends AIActClassification {
  citations: LegacyCitation[]
}

interface AISystem {
  name: string
  purpose: string
  dataUsed: string
  isAutomatedDecision: boolean
}

const AI_ACT_SYSTEM_PROMPT = `You are an expert in the EU AI Act (Regulation (EU) 2024/1689). You classify AI systems
by their risk tier according to the AI Act framework.

Your classification must be:
- Accurate - based on the actual provisions of the AI Act
- Specific - cite the relevant AI Act articles for each system
- Actionable - list the concrete compliance obligations for each risk tier
- Include compliance deadlines based on the AI Act implementation timeline

Risk Tiers:
- Unacceptable (Art. 5): Banned practices (social scoring, real-time biometric identification, etc.)
- High (Art. 6, Annex III): Systems in critical areas requiring conformity assessment
- Limited (Art. 50): Transparency obligations (chatbots, deepfakes, emotion recognition)
- Minimal: No specific obligations, voluntary codes of conduct

Use the provided regulatory context to ground your classification in the primary AI Act text.`

function buildClassificationPrompt(aiSystems: AISystem[]): string {
  return `Classify the following AI systems according to the EU AI Act risk tiers:

${aiSystems
  .map(
    (s, i) =>
      `System ${i + 1}:
  Name: ${s.name}
  Purpose: ${s.purpose}
  Data Used: ${s.dataUsed}
  Automated Decision-Making: ${s.isAutomatedDecision}`
  )
  .join('\n\n')}

For each system, determine the risk tier, explain your reasoning, list compliance obligations,
cite relevant AI Act articles, and provide the compliance deadline.`
}

/**
 * Classifies AI systems according to EU AI Act risk tiers using the backend RAG service.
 *
 * @param aiSystems - Array of AI system descriptions to classify
 * @param token - JWT access token for authentication
 * @returns Classification result with risk tiers and citations from regulatory sources
 */
export async function classifyAIRisk(
  aiSystems: AISystem[],
  token?: string
): Promise<AIActClassificationWithCitations> {
  const response = await queryRAGWithSchema({
    query: buildClassificationPrompt(aiSystems),
    systemPrompt: AI_ACT_SYSTEM_PROMPT,
    schema: AIActClassificationSchema,
    collection: 'ai_act',
    topK: 10,
    temperature: 0.3,
    token,
  })

  return {
    ...response.data,
    citations: response.citations,
  }
}

/**
 * Classifies AI systems without citations (for backward compatibility).
 *
 * @param aiSystems - Array of AI system descriptions to classify
 * @returns Classification result with risk tiers
 */
export async function classifyAIRiskSimple(
  aiSystems: AISystem[]
): Promise<AIActClassification> {
  const result = await classifyAIRisk(aiSystems)
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { citations, ...classificationResult } = result
  return classificationResult
}
