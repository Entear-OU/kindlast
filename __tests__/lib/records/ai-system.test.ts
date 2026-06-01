import { describe, expect, it } from 'vitest'

import {
  DOC_TONE,
  formatReviewed,
  RISK_LABEL,
  RISK_TONE,
} from '@/lib/records/ai-system'

/**
 * ENT-72 — pure helpers behind the AI Systems Register: the risk / docs pill
 * labels + tones, and the "last reviewed" label.
 */

describe('risk + documentation presentation (ENT-72)', () => {
  it('labels and tones the EU AI Act risk tiers', () => {
    expect(RISK_LABEL.high).toBe('High risk')
    expect(RISK_TONE.high).toBe('danger')
    expect(RISK_TONE.unacceptable).toBe('danger')
    expect(RISK_TONE.limited).toBe('warn')
    expect(RISK_TONE.minimal).toBe('info')
    expect(RISK_TONE.unclassified).toBe('muted')
  })

  it('tones the documentation status', () => {
    expect(DOC_TONE.complete).toBe('done')
    expect(DOC_TONE.in_progress).toBe('info')
    expect(DOC_TONE.missing).toBe('warn')
  })
})

describe('formatReviewed (ENT-72)', () => {
  const now = new Date('2026-05-20T12:00:00.000Z')

  it('says Never when not yet reviewed', () => {
    expect(formatReviewed(null, now)).toBe('Never')
    expect(formatReviewed('not-a-date', now)).toBe('Never')
  })

  it('says Today for the current day', () => {
    expect(formatReviewed('2026-05-20T08:00:00.000Z', now)).toBe('Today')
  })

  it('formats day + month, with a year for older dates', () => {
    expect(formatReviewed('2026-05-08T08:00:00.000Z', now)).toBe('8 May')
    expect(formatReviewed('2025-12-08T08:00:00.000Z', now)).toBe('8 Dec 2025')
  })
})
