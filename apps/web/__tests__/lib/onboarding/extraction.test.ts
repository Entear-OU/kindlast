import { describe, expect, it, vi, beforeEach } from 'vitest'

const { generateObjectMock } = vi.hoisted(() => ({
  generateObjectMock: vi.fn(),
}))

vi.mock('ai', async (importOriginal) => {
  const actual = await importOriginal<typeof import('ai')>()
  return {
    ...actual,
    generateObject: generateObjectMock,
  }
})

vi.mock('@ai-sdk/openai', () => ({
  openai: (id: string) => ({ provider: 'mock-openai', modelId: id }),
}))

import {
  COMPLIANCE_PROFILE_EXTRACTION_PROMPT,
  complianceProfileSchema,
  extractComplianceProfile,
  type TranscriptTurn,
} from '@/lib/onboarding/extraction'

/**
 * ENT-45 — Compliance profile extraction.
 *
 * The extractor is the bridge between the free-text onboarding transcript and
 * the structured `compliance_profiles` row. Production wires the AI SDK's
 * `generateObject` to a real model; these tests mock it so we can assert the
 * wiring (correct schema, system prompt, transcript shape) and the Zod
 * round-trip without paying for an LLM call.
 *
 * A representative transcript covering all six required topics gets paired
 * with the structured object an ideal model would return for it. We then
 * verify both: (a) the extractor passed that transcript to `generateObject`
 * correctly, and (b) the parsed object is returned unchanged.
 */

const SAMPLE_TRANSCRIPT: TranscriptTurn[] = [
  { role: 'assistant', content: 'Welcome! To start — what does your company do?' },
  {
    role: 'user',
    content:
      'We build a SaaS payroll tool for small businesses across the EU. Mostly in Germany, France, and Estonia so far.',
  },
  { role: 'assistant', content: 'Great. What kinds of personal data do you collect, and from whom?' },
  {
    role: 'user',
    content:
      'Customer emails, bank details for payouts, and staff records (names, salaries, addresses) for the businesses using us. We also collect prospect emails from the marketing site.',
  },
  { role: 'assistant', content: 'Got it. Any AI tools in use — internally or in the product?' },
  {
    role: 'user',
    content:
      'Internally we use ChatGPT and GitHub Copilot. In the product we have an in-house ML model that flags suspicious payroll runs.',
  },
  { role: 'assistant', content: 'And do you have a Data Protection Officer?' },
  { role: 'user', content: 'No, not yet — we are only 18 people.' },
  { role: 'assistant', content: 'Understood. What about a Record of Processing Activities?' },
  { role: 'user', content: 'Not really. We have a Notion doc but nothing formal.' },
  { role: 'assistant', content: 'Do you transfer any data outside the EU?' },
  {
    role: 'user',
    content:
      'Some — our analytics provider is in the US (Amplitude) and Stripe processes payment data. Our hosting is on AWS Frankfurt though.',
  },
]

const EXPECTED_PROFILE = {
  industry: 'SaaS payroll for small businesses',
  euJurisdictions: ['Germany', 'France', 'Estonia'],
  dataCategories: [
    'customer email addresses',
    'bank details',
    'staff records (names, salaries, addresses)',
    'prospect email addresses',
  ],
  dataSubjects: ['customers', 'staff of customer businesses', 'prospects'],
  aiSystems: [
    'ChatGPT (internal)',
    'GitHub Copilot (internal)',
    'in-house payroll anomaly detection model (product)',
  ],
  hasDpo: 'no' as const,
  hasRopa: 'unsure' as const,
  transfersOutsideEu: 'yes' as const,
  transferDestinations: ['United States (Amplitude)', 'United States (Stripe)'],
  vendorList: 'Amplitude, Stripe, AWS (Frankfurt)',
  staffCount: 18,
}

beforeEach(() => {
  generateObjectMock.mockReset()
})

describe('extractComplianceProfile (ENT-45)', () => {
  it('parses a representative transcript into the expected structured fields', async () => {
    generateObjectMock.mockResolvedValueOnce({ object: EXPECTED_PROFILE })

    const profile = await extractComplianceProfile(SAMPLE_TRANSCRIPT)

    expect(profile).toEqual(EXPECTED_PROFILE)
  })

  it('passes the transcript and Zod schema to generateObject', async () => {
    generateObjectMock.mockResolvedValueOnce({ object: EXPECTED_PROFILE })

    await extractComplianceProfile(SAMPLE_TRANSCRIPT)

    expect(generateObjectMock).toHaveBeenCalledTimes(1)
    const call = generateObjectMock.mock.calls[0][0] as {
      schema: unknown
      messages: Array<{ role: string; content: string }>
      system: string
    }
    expect(call.schema).toBe(complianceProfileSchema)
    expect(call.system).toBe(COMPLIANCE_PROFILE_EXTRACTION_PROMPT)
    // The transcript is passed through verbatim as model messages.
    expect(call.messages).toEqual(SAMPLE_TRANSCRIPT)
  })

  it('accepts an injected model for tests and provider swaps', async () => {
    generateObjectMock.mockResolvedValueOnce({ object: EXPECTED_PROFILE })
    const injectedModel = { provider: 'fake', modelId: 'fake-extractor' }

    await extractComplianceProfile(SAMPLE_TRANSCRIPT, { model: injectedModel as never })

    const call = generateObjectMock.mock.calls[0][0] as { model: unknown }
    expect(call.model).toBe(injectedModel)
  })

  it('Zod schema rejects payloads with an invalid yes/no/unsure value', () => {
    const bad = { ...EXPECTED_PROFILE, hasDpo: 'maybe' }
    const result = complianceProfileSchema.safeParse(bad)
    expect(result.success).toBe(false)
  })

  it('Zod schema accepts a payload with null staff_count (founder skipped)', () => {
    const partial = { ...EXPECTED_PROFILE, staffCount: null }
    const result = complianceProfileSchema.safeParse(partial)
    expect(result.success).toBe(true)
  })
})
