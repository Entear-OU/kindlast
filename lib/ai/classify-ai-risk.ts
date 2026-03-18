import { generateObject } from 'ai'
import { google } from '@ai-sdk/google'
import { z } from 'zod'

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

interface AISystem {
  name: string
  purpose: string
  dataUsed: string
  isAutomatedDecision: boolean
}

export async function classifyAIRisk(aiSystems: AISystem[]) {
  const { object } = await generateObject({
    model: google('gemini-2.5-flash'),
    schema: AIActClassificationSchema,
    system: `You are an expert in the EU AI Act (Regulation (EU) 2024/1689). You classify AI systems
by their risk tier according to the AI Act framework.

Your classification must be:
- Accurate — based on the actual provisions of the AI Act
- Specific — cite the relevant AI Act articles for each system
- Actionable — list the concrete compliance obligations for each risk tier
- Include compliance deadlines based on the AI Act implementation timeline

Risk Tiers:
- Unacceptable (Art. 5): Banned practices (social scoring, real-time biometric identification, etc.)
- High (Art. 6, Annex III): Systems in critical areas requiring conformity assessment
- Limited (Art. 50): Transparency obligations (chatbots, deepfakes, emotion recognition)
- Minimal: No specific obligations, voluntary codes of conduct`,

    prompt: `Classify the following AI systems according to the EU AI Act risk tiers:

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
cite relevant AI Act articles, and provide the compliance deadline.`,
  })

  return object
}
