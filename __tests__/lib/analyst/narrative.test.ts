import { beforeEach, describe, expect, it, vi } from 'vitest'

const { generateObjectMock } = vi.hoisted(() => ({ generateObjectMock: vi.fn() }))

vi.mock('ai', async (importOriginal) => {
  const actual = await importOriginal<typeof import('ai')>()
  return { ...actual, generateObject: generateObjectMock }
})

vi.mock('@ai-sdk/openai', () => ({
  openai: (id: string) => ({ provider: 'mock-openai', modelId: id }),
}))

import {
  ANALYST_NARRATIVE_PROMPT,
  findingNarrativeSchema,
  generateFindingNarrative,
  type FindingNarrativeContext,
} from '@/lib/analyst/narrative'

/**
 * ENT-60 — narrative generation wiring + critic gate.
 *
 * Production wires `generateObject` to a real model; these tests mock it so we
 * can assert (a) the schema/system/context wiring and (b) that the deterministic
 * critic gates persistence — a generic generated action is regenerated, and a
 * never-good action fails closed (ok:false) so the caller keeps the baseline
 * rather than persisting junk.
 */

const CONTEXT: FindingNarrativeContext = {
  signalKind: 'profile_gap',
  obligationTitle: 'Processor contracts (Art. 28)',
  obligationSummary: 'Controllers must have a data processing agreement with each processor.',
  citationLabel: 'GDPR Art. 28',
  industry: 'SaaS payroll',
  vendors: 'Stripe, AWS',
}

const GOOD = {
  description:
    'You use Stripe to process payments but have no data processing agreement on file. EU rules require one before a vendor handles personal data for you.',
  proposedAction: 'Draft a Data Processing Agreement with Stripe.',
}
const GENERIC = {
  description: 'You should look at your vendors.',
  proposedAction: 'Review your vendor agreements.',
}

beforeEach(() => generateObjectMock.mockReset())

describe('generateFindingNarrative (ENT-60)', () => {
  it('returns a critic-approved narrative and wires schema/system/context', async () => {
    generateObjectMock.mockResolvedValueOnce({ object: GOOD })

    const result = await generateFindingNarrative(CONTEXT)

    expect(result.ok).toBe(true)
    expect(result.narrative).toEqual(GOOD)
    expect(result.attempts).toBe(1)

    expect(generateObjectMock).toHaveBeenCalledTimes(1)
    const call = generateObjectMock.mock.calls[0][0]
    expect(call.schema).toBe(findingNarrativeSchema)
    expect(call.system).toBe(ANALYST_NARRATIVE_PROMPT)
    expect(call.prompt).toContain('Stripe') // concrete context reaches the model
    expect(call.prompt).toContain('GDPR Art. 28')
  })

  it('regenerates when the critic rejects, feeding the reasons back', async () => {
    generateObjectMock.mockResolvedValueOnce({ object: GENERIC })
    generateObjectMock.mockResolvedValueOnce({ object: GOOD })

    const result = await generateFindingNarrative(CONTEXT, { maxAttempts: 2 })

    expect(result.ok).toBe(true)
    expect(result.narrative).toEqual(GOOD)
    expect(result.attempts).toBe(2)
    expect(generateObjectMock).toHaveBeenCalledTimes(2)
    // The retry prompt names what to fix.
    const retryPrompt = generateObjectMock.mock.calls[1][0].prompt as string
    expect(retryPrompt).toMatch(/rejected for:/i)
    expect(retryPrompt).toContain('generic_verb')
  })

  it('fails closed when no attempt passes the critic', async () => {
    generateObjectMock.mockResolvedValue({ object: GENERIC })

    const result = await generateFindingNarrative(CONTEXT, { maxAttempts: 2 })

    expect(result.ok).toBe(false)
    expect(result.narrative).toBeUndefined()
    expect(result.reasons).toContain('generic_verb')
    expect(generateObjectMock).toHaveBeenCalledTimes(2)
  })
})
