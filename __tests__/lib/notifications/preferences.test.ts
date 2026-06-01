import { describe, expect, it } from 'vitest'

import {
  shouldNotifyByEmail,
  type EmailFrequency,
  type SeverityLevel,
} from '@/lib/notifications/preferences'

/**
 * ENT-61 — severity gate for email-frequency preferences.
 *
 * The deterministic rule the Comms agent (later) will consult to decide whether
 * a finding of a given severity warrants an email under the user's chosen
 * frequency. Policy:
 *   - 'critical' is a safety override — always emails, even when email is off
 *     (a client must never silently run out a compliance clock).
 *   - 'off' silences everything below critical.
 *   - otherwise the severity must meet the frequency's threshold:
 *     immediate → high+, daily → medium+, weekly → high+.
 */

// Expected outcome for every (severity × frequency) pair.
const MATRIX: Record<SeverityLevel, Record<EmailFrequency, boolean>> = {
  low: { immediate: false, daily: false, weekly: false, off: false },
  medium: { immediate: false, daily: true, weekly: false, off: false },
  high: { immediate: true, daily: true, weekly: true, off: false },
  critical: { immediate: true, daily: true, weekly: true, off: true },
}

const SEVERITIES: SeverityLevel[] = ['low', 'medium', 'high', 'critical']
const FREQUENCIES: EmailFrequency[] = ['immediate', 'daily', 'weekly', 'off']

describe('shouldNotifyByEmail (ENT-61)', () => {
  it('matches the documented severity × frequency policy matrix', () => {
    for (const severity of SEVERITIES) {
      for (const frequency of FREQUENCIES) {
        expect(
          shouldNotifyByEmail(severity, frequency),
          `${severity} × ${frequency}`,
        ).toBe(MATRIX[severity][frequency])
      }
    }
  })

  it('critical always emails, even when email is off (safety override)', () => {
    for (const frequency of FREQUENCIES) {
      expect(shouldNotifyByEmail('critical', frequency)).toBe(true)
    }
  })

  it("'off' silences everything below critical", () => {
    expect(shouldNotifyByEmail('low', 'off')).toBe(false)
    expect(shouldNotifyByEmail('medium', 'off')).toBe(false)
    expect(shouldNotifyByEmail('high', 'off')).toBe(false)
  })

  it('is deterministic', () => {
    expect(shouldNotifyByEmail('high', 'daily')).toBe(shouldNotifyByEmail('high', 'daily'))
  })
})
