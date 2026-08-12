import { describe, expect, it } from 'vitest'

import { critiqueDescription, critiqueProposedAction } from '@/lib/analyst/critic'

/**
 * ENT-60 — the Analyst-side critic.
 *
 * The critic is the deterministic guardrail that rejects vague, generic, or
 * multi-step proposed actions before a finding is persisted, so the feed only
 * ever shows a founder something specific they can approve in one click. The
 * acceptance criteria call out exactly this: a unit test must verify that bad
 * outputs (e.g. "Review your vendor agreements") are rejected before
 * persistence. The generator (LLM) is non-deterministic; this critic is not.
 */

describe('critiqueProposedAction (ENT-60)', () => {
  const GOOD = [
    'Draft a Data Processing Agreement with Stripe.',
    'Appoint a Data Protection Officer.',
    'Publish a Record of Processing Activities.',
    'Send a response to the data-subject request before 12 June.',
    'Register your high-risk AI system in the EU database.',
  ]

  const BAD: Array<[string, string, string]> = [
    // [action, expected reason, why]
    ['Review your vendor agreements.', 'generic_verb', 'the canonical AC example'],
    ['Consider whether a DPIA is needed.', 'generic_verb', 'weak opener'],
    ['Ensure your processes are compliant.', 'generic_verb', 'non-specific imperative'],
    ['Look into your data transfers.', 'generic_verb', '"look into" is not an action'],
    ['Update your vendor agreements as appropriate.', 'generic_phrase', 'vague object + hedge'],
    ['Draft a DPA with Stripe and then notify your DPO.', 'multiple_actions', 'two Executor writes'],
    ['Draft a DPA with Stripe. Notify your DPO.', 'multiple_sentences', 'two sentences = two actions'],
    ['You should maybe update your policies.', 'not_verb_led', 'not imperative'],
    ['Do it.', 'too_short', 'no substance'],
  ]

  it('accepts specific, verb-led, singular actions', () => {
    for (const action of GOOD) {
      const verdict = critiqueProposedAction(action)
      expect(verdict, `${action} → ${verdict.reasons.join(',')}`).toEqual({ ok: true, reasons: [] })
    }
  })

  it('rejects generic / hedged / multi-step / non-imperative actions', () => {
    for (const [action, reason] of BAD) {
      const verdict = critiqueProposedAction(action)
      expect(verdict.ok, `${action} should be rejected`).toBe(false)
      expect(verdict.reasons, `${action} → ${verdict.reasons.join(',')}`).toContain(reason)
    }
  })

  it('rejects the AC example specifically and reports a reason', () => {
    const verdict = critiqueProposedAction('Review your vendor agreements.')
    expect(verdict.ok).toBe(false)
    expect(verdict.reasons.length).toBeGreaterThan(0)
  })

  it('is deterministic — same input, same verdict', () => {
    const a = critiqueProposedAction('Review your vendor agreements.')
    const b = critiqueProposedAction('Review your vendor agreements.')
    expect(a).toEqual(b)
  })
})

describe('critiqueDescription (ENT-60)', () => {
  it('accepts a plain-language one-to-two-sentence description', () => {
    const ok = critiqueDescription(
      'You use Stripe to process payments but have no data processing agreement on file. EU rules require one before a vendor handles personal data on your behalf.',
    )
    expect(ok.ok).toBe(true)
  })

  it('rejects legal jargon', () => {
    const verdict = critiqueDescription(
      'Pursuant to the aforementioned obligations, the controller shall maintain records thereof.',
    )
    expect(verdict.ok).toBe(false)
    expect(verdict.reasons).toContain('legal_jargon')
  })

  it('rejects more than two sentences', () => {
    const verdict = critiqueDescription('You use Stripe. You have no DPA. This is a risk. Fix it.')
    expect(verdict.ok).toBe(false)
    expect(verdict.reasons).toContain('sentence_count')
  })
})
