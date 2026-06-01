import { describe, expect, it } from 'vitest'

import {
  POSTURE_TOOLTIP,
  computePosture,
  postureMeta,
  type PostureDeadline,
  type PostureInputs,
} from '@/lib/dashboard/posture'
import type { FindingSeverity } from '@/lib/feed/findings'

/**
 * ENT-77 — the overall Green / Amber / Red posture. The headline lives in the
 * pure `computePosture` rule (the AC), so it's exhaustively unit-tested here;
 * the loader that feeds it real rows is covered by the integration suite.
 */

function inputs(over: Partial<PostureInputs> = {}): PostureInputs {
  return { openSeverities: [], deadlines: [], ...over }
}

function deadline(severity: FindingSeverity, daysRemaining: number): PostureDeadline {
  return { severity, daysRemaining }
}

describe('computePosture (ENT-77)', () => {
  it('is Green with nothing open and no near-term deadlines', () => {
    expect(computePosture(inputs())).toBe('green')
    expect(
      computePosture(inputs({ openSeverities: ['low', 'medium'] })),
    ).toBe('green')
  })

  it('stays Green for a far-off critical/high deadline (beyond 30 days)', () => {
    expect(
      computePosture(inputs({ deadlines: [deadline('critical', 45), deadline('high', 60)] })),
    ).toBe('green')
  })

  it('is Red when a Critical finding is open', () => {
    expect(
      computePosture(inputs({ openSeverities: ['critical', 'low'] })),
    ).toBe('red')
  })

  it('is Red when a Critical deadline is overdue', () => {
    expect(
      computePosture(inputs({ deadlines: [deadline('critical', -1)] })),
    ).toBe('red')
  })

  it('is Amber when a High finding is open', () => {
    expect(computePosture(inputs({ openSeverities: ['high'] }))).toBe('amber')
  })

  it('is Amber for a High deadline within 30 days', () => {
    expect(computePosture(inputs({ deadlines: [deadline('high', 30)] }))).toBe('amber')
    expect(computePosture(inputs({ deadlines: [deadline('high', 0)] }))).toBe('amber')
  })

  it('is Amber for a near-term Critical deadline that is not yet overdue', () => {
    // Breaks Green ("no Critical/High deadline under 30 days") but is not
    // overdue, so it is Amber rather than Red.
    expect(computePosture(inputs({ deadlines: [deadline('critical', 10)] }))).toBe('amber')
  })

  it('prefers Red over Amber when both apply', () => {
    expect(
      computePosture(
        inputs({ openSeverities: ['critical', 'high'], deadlines: [deadline('high', 5)] }),
      ),
    ).toBe('red')
  })

  it('ignores a near-term Medium/Low deadline (Green)', () => {
    expect(
      computePosture(inputs({ deadlines: [deadline('medium', 5), deadline('low', 1)] })),
    ).toBe('green')
  })
})

describe('postureMeta (ENT-77)', () => {
  it('gives every posture a label, headline and styling', () => {
    for (const p of ['green', 'amber', 'red'] as const) {
      const meta = postureMeta(p)
      expect(meta.posture).toBe(p)
      expect(meta.label).toBeTruthy()
      expect(meta.headline).toBeTruthy()
      expect(meta.dotClassName).toBeTruthy()
    }
  })
})

describe('POSTURE_TOOLTIP (ENT-77)', () => {
  it('explains all three bands', () => {
    expect(POSTURE_TOOLTIP).toMatch(/green/i)
    expect(POSTURE_TOOLTIP).toMatch(/amber/i)
    expect(POSTURE_TOOLTIP).toMatch(/red/i)
  })
})
