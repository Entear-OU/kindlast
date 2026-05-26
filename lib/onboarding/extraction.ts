import { openai } from '@ai-sdk/openai'
import { generateObject, type LanguageModel } from 'ai'
import { z } from 'zod'

/**
 * Compliance profile extraction (ENT-45).
 *
 * Bridges the free-text onboarding transcript (ENT-44) and the structured
 * `compliance_profiles` row (ENT-31 epic). Production wires the AI SDK's
 * `generateObject` against a real model; the unit test mocks `generateObject`
 * so the schema/prompt wiring stays asserted without an LLM round-trip.
 *
 * Why a second LLM pass instead of asking the interviewer model to fill the
 * fields inline: keeping interview and extraction decoupled means we can
 * (a) reword the schema without retuning the interviewer's voice and
 * (b) run extraction against the entire transcript at once with a tighter,
 * extraction-only system prompt — which produces more reliable JSON than
 * piggybacking on the chat turn.
 */

const yesNoUnsure = z.enum(['yes', 'no', 'unsure'])

/**
 * Schema matches the `public.compliance_profiles` row, with the exception of
 * `staff_count` which is nullable in the DB (founder may skip or be vague).
 *
 * Keys are camelCase here; the persister maps to snake_case at the boundary.
 */
export const complianceProfileSchema = z.object({
  industry: z
    .string()
    .min(1)
    .describe('Short, plain-English description of what the company does.'),
  euJurisdictions: z
    .array(z.string().min(1))
    .describe(
      'EU/EEA countries where the company has users or data subjects, as named by the founder.',
    ),
  dataCategories: z
    .array(z.string().min(1))
    .describe('Kinds of personal data collected (e.g. "email addresses", "bank details").'),
  dataSubjects: z
    .array(z.string().min(1))
    .describe('Whose data is collected (e.g. "customers", "staff", "prospects").'),
  aiSystems: z
    .array(z.string().min(1))
    .describe(
      'AI tools in use. Annotate each with whether it is internal or product-facing where the founder said so, e.g. "ChatGPT (internal)".',
    ),
  hasDpo: yesNoUnsure.describe(
    'Whether the company has a Data Protection Officer. Use "unsure" when the founder did not give a clear yes/no.',
  ),
  hasRopa: yesNoUnsure.describe(
    'Whether the company has a Record of Processing Activities. Use "unsure" when the founder did not give a clear yes/no.',
  ),
  transfersOutsideEu: yesNoUnsure.describe(
    'Whether any personal data leaves the EU/EEA. Use "unsure" when the founder did not give a clear yes/no.',
  ),
  transferDestinations: z
    .array(z.string().min(1))
    .describe(
      'Destinations of cross-border transfers, paired with the vendor where mentioned (e.g. "United States (Stripe)"). Empty when transfersOutsideEu is "no".',
    ),
  vendorList: z
    .string()
    .describe(
      'Comma-separated free-text list of third-party vendors mentioned (analytics, payment, hosting, etc.). Empty string if none mentioned.',
    ),
  staffCount: z
    .number()
    .int()
    .positive()
    .nullable()
    .describe('Best integer estimate of headcount, or null if the founder did not give a number.'),
})

export type ComplianceProfile = z.infer<typeof complianceProfileSchema>

export interface TranscriptTurn {
  role: 'user' | 'assistant'
  content: string
}

export const COMPLIANCE_PROFILE_EXTRACTION_PROMPT = `You are extracting a compliance profile from a recorded onboarding interview between an EU-SME founder and an intake agent.

Read the entire transcript and produce one structured profile object. Rules:

- Use only facts the founder stated. Do not infer policy or risk from absence of mention.
- When the founder was ambiguous about DPO, ROPA, or cross-border transfers, set the corresponding field to "unsure" rather than guessing "yes" or "no".
- Preserve the founder's vocabulary in list items (e.g. "bank details" rather than "financial data") so a reviewer can trace each field back to the transcript.
- For each AI system, annotate "(internal)" or "(product)" in parentheses when the founder distinguished them.
- For each cross-border transfer destination, pair the country with the vendor in parentheses where the founder named one (e.g. "United States (Stripe)").
- staffCount: extract the best integer estimate. If the founder gave a range, take the midpoint. If they gave no number, leave it null.
- Do not invent vendors, jurisdictions, or data categories that were not mentioned.`

const DEFAULT_MODEL_ID =
  process.env.ONBOARDING_EXTRACTION_MODEL ?? 'gpt-5.4-mini'

export interface ExtractOptions {
  model?: LanguageModel
}

export async function extractComplianceProfile(
  transcript: ReadonlyArray<TranscriptTurn>,
  options: ExtractOptions = {},
): Promise<ComplianceProfile> {
  const model = options.model ?? openai(DEFAULT_MODEL_ID)

  const { object } = await generateObject({
    model,
    schema: complianceProfileSchema,
    system: COMPLIANCE_PROFILE_EXTRACTION_PROMPT,
    messages: transcript.map((turn) => ({ role: turn.role, content: turn.content })),
  })

  return object
}
