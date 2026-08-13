import { openai } from '@ai-sdk/openai'
import { generateObject, type LanguageModel } from 'ai'
import { z } from 'zod'

import { critiqueDescription, critiqueProposedAction } from './critic'

/**
 * Finding narrative generation (ENT-60).
 *
 * Turns a structurally-complete finding (ENT-58/59 — signal kind, obligation,
 * citation, profile context) into the two founder-facing fields: a
 * plain-language `description` and a specific, verb-led, singular
 * `proposedAction`. This is the epic's intended home for model nuance
 * (`lib/onboarding/posture-summary.ts` deferred it here), so it mirrors the
 * AI-SDK pattern in `lib/onboarding/extraction.ts`: `generateObject` against a
 * Zod schema, mocked in tests.
 *
 * The generator is paired with the deterministic critic (`./critic`): a
 * generated action that reads as generic ("Review your vendor agreements") is
 * regenerated with the critic's reasons fed back, and only a passing narrative
 * is returned for persistence. The model writes; the critic gates.
 */

export const findingNarrativeSchema = z.object({
  description: z
    .string()
    .min(1)
    .describe(
      'One to two sentences, plain English, no legal jargon. Say what was detected and why it matters to this specific business.',
    ),
  proposedAction: z
    .string()
    .min(1)
    .describe(
      'A single, specific, verb-led action the founder can approve in one click — e.g. "Draft a Data Processing Agreement with Stripe". Start with an imperative verb. Name the concrete vendor, system, person, or date. Exactly one step; no "and then", no lists.',
    ),
})

export type FindingNarrative = z.infer<typeof findingNarrativeSchema>

/** Everything the generator needs to be concrete about this finding. */
export interface FindingNarrativeContext {
  /** deadline | profile_gap | dsar | regulatory_update */
  signalKind: string
  obligationTitle: string
  obligationSummary: string
  /** e.g. "GDPR Art. 30" */
  citationLabel: string
  industry?: string | null
  /** Comma-separated vendor list from the profile. */
  vendors?: string | null
  aiSystems?: string[] | null
  /** Deadline / due date pulled from the signal metadata (ISO or human). */
  deadlineDate?: string | null
  /** DSAR subject name, when the signal is a DSAR. */
  subjectName?: string | null
  /** profile_gap: the unsatisfied control tokens (e.g. ["dpo"]). */
  missingControls?: string[] | null
  /**
   * Prior founder-rejection reasons for the SAME condition (same
   * profile_id + obligation_slug), newest first (ENT-65). Distinct from the
   * critic-retry `priorReasons` param of buildNarrativePrompt — these are the
   * founder's own objections, fed in so the model stops repeating a false
   * positive the founder has already dismissed.
   */
  priorRejectionReasons?: string[]
}

export const ANALYST_NARRATIVE_PROMPT = `You are the Analyst in a compliance copilot for EU small businesses. You turn a detected compliance condition into something a non-legal founder can read and act on in 30 seconds.

Produce two fields:

- description: one to two sentences, plain English, no legal jargon (no "pursuant to", "the controller shall", article-number recitation). Explain what was detected and why it matters to THIS business, using the specifics you are given (vendor names, AI systems, dates).
- proposedAction: ONE specific step, led by an imperative verb, that maps to a single action the user can approve. Name the concrete thing — "Draft a Data Processing Agreement with Stripe", not "Review your vendor agreements". Do not chain steps with "and then"; do not hedge ("consider", "as appropriate"); do not produce a list.

Be concrete over comprehensive. If you cannot be specific with the given context, pick the single most important step and name it precisely.`

const DEFAULT_MODEL_ID = process.env.ANALYST_NARRATIVE_MODEL ?? 'gpt-5.4-mini'

export interface GenerateNarrativeOptions {
  model?: LanguageModel
  /** Critic-gated regeneration attempts (default 2). */
  maxAttempts?: number
}

export interface NarrativeResult {
  ok: boolean
  narrative?: FindingNarrative
  /** Critic reasons from the final (rejected) attempt; empty when ok. */
  reasons: string[]
  attempts: number
}

/** Render the per-finding context (and any prior critic feedback) as the user turn. */
export function buildNarrativePrompt(
  context: FindingNarrativeContext,
  priorReasons: string[] = [],
): string {
  const lines = [
    `Detected condition kind: ${context.signalKind}`,
    `Obligation: ${context.obligationTitle} (${context.citationLabel})`,
    `What the obligation requires: ${context.obligationSummary}`,
  ]
  if (context.industry) lines.push(`Business: ${context.industry}`)
  if (context.vendors) lines.push(`Vendors in use: ${context.vendors}`)
  if (context.aiSystems?.length) lines.push(`AI systems: ${context.aiSystems.join(', ')}`)
  if (context.deadlineDate) lines.push(`Deadline: ${context.deadlineDate}`)
  if (context.subjectName) lines.push(`Data-subject request from: ${context.subjectName}`)
  if (context.missingControls?.length) {
    lines.push(`Missing controls: ${context.missingControls.join(', ')}`)
  }
  if (context.priorRejectionReasons?.length) {
    const quoted = context.priorRejectionReasons.map((r) => `"${r}"`).join('; ')
    lines.push(
      '',
      `The founder has previously rejected similar findings for this obligation, saying: ${quoted}. Take these objections seriously — only raise this if it still applies, and make the description and action directly address those concerns.`,
    )
  }
  if (priorReasons.length) {
    lines.push(
      '',
      `Your previous attempt was rejected for: ${priorReasons.join(', ')}. Fix these — be more specific and use a single imperative action.`,
    )
  }
  return lines.join('\n')
}

/**
 * Generate a critic-approved narrative. Regenerates up to `maxAttempts` times,
 * feeding the critic's reasons back to the model each retry. Returns
 * `{ ok: false }` (with reasons) if no attempt passes — the caller must then
 * keep the baseline rather than persist a bad narrative.
 */
export async function generateFindingNarrative(
  context: FindingNarrativeContext,
  options: GenerateNarrativeOptions = {},
): Promise<NarrativeResult> {
  const model = options.model ?? openai(DEFAULT_MODEL_ID)
  const maxAttempts = Math.max(1, options.maxAttempts ?? 2)

  let reasons: string[] = []
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const { object } = await generateObject({
      model,
      schema: findingNarrativeSchema,
      system: ANALYST_NARRATIVE_PROMPT,
      prompt: buildNarrativePrompt(context, reasons),
    })

    const action = critiqueProposedAction(object.proposedAction)
    const description = critiqueDescription(object.description)
    if (action.ok && description.ok) {
      return { ok: true, narrative: object, reasons: [], attempts: attempt }
    }
    reasons = [...new Set([...action.reasons, ...description.reasons])]
  }

  return { ok: false, reasons, attempts: maxAttempts }
}
