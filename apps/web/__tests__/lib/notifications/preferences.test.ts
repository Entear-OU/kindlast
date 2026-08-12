import { describe, expect, it } from 'vitest'

import {
  inQuietHours,
  resolvePreferences,
  shouldEmailFinding,
} from '@/lib/notifications/preferences'

/**
 * ENT-76 — notification preference domain rules: the per-severity finding-email
 * gate, the quiet-hours window, and default resolution.
 */

describe('shouldEmailFinding (ENT-76)', () => {
  it('sends at or above the floor', () => {
    expect(shouldEmailFinding('medium', 'medium')).toBe(true)
    expect(shouldEmailFinding('high', 'medium')).toBe(true)
  })

  it('skips below the floor', () => {
    expect(shouldEmailFinding('low', 'medium')).toBe(false)
    expect(shouldEmailFinding('medium', 'high')).toBe(false)
  })

  it('always sends critical, even above the floor', () => {
    expect(shouldEmailFinding('critical', 'high')).toBe(true)
    expect(shouldEmailFinding('critical', 'critical')).toBe(true)
  })
})

describe('inQuietHours (ENT-76)', () => {
  // 2024-01-01 is a Monday; pick instants by UTC and read in Europe/Tallinn (UTC+2 winter).
  const at = (utcHour: number, utcMin = 0) => Date.UTC(2024, 0, 1, utcHour, utcMin)

  it('is never quiet when start/end is null or equal', () => {
    expect(inQuietHours(at(3), 'Europe/Tallinn', null, '07:00')).toBe(false)
    expect(inQuietHours(at(3), 'Europe/Tallinn', '22:00', null)).toBe(false)
    expect(inQuietHours(at(3), 'Europe/Tallinn', '08:00', '08:00')).toBe(false)
  })

  it('handles a same-day window', () => {
    // window 09:00–17:00 local; 12:00 local = 10:00 UTC
    expect(inQuietHours(at(10), 'Europe/Tallinn', '09:00', '17:00')).toBe(true)
    // 08:00 local = 06:00 UTC → before window
    expect(inQuietHours(at(6), 'Europe/Tallinn', '09:00', '17:00')).toBe(false)
  })

  it('handles a window that wraps past midnight', () => {
    // window 22:00–07:00 local. 23:00 local = 21:00 UTC → quiet
    expect(inQuietHours(at(21), 'Europe/Tallinn', '22:00', '07:00')).toBe(true)
    // 02:00 local = 00:00 UTC → quiet
    expect(inQuietHours(at(0), 'Europe/Tallinn', '22:00', '07:00')).toBe(true)
    // 09:00 local = 07:00 UTC → not quiet
    expect(inQuietHours(at(7), 'Europe/Tallinn', '22:00', '07:00')).toBe(false)
  })
})

describe('resolvePreferences (ENT-76)', () => {
  it('falls back email to the auth email and defaults the floor to medium', () => {
    const prefs = resolvePreferences(null, { authEmail: 'founder@example.com', plan: 'pro' })
    expect(prefs.email).toBe('founder@example.com')
    expect(prefs.minSeverityForEmail).toBe('medium')
    expect(prefs.deadlineAlertsEnabled).toBe(true)
    expect(prefs.timezone).toBe('Europe/Tallinn')
  })

  it('defaults the weekly briefing per plan when no row exists', () => {
    expect(resolvePreferences(null, { authEmail: null, plan: 'pro' }).weeklyBriefingEnabled).toBe(true)
    expect(resolvePreferences(null, { authEmail: null, plan: 'free' }).weeklyBriefingEnabled).toBe(false)
  })

  it('prefers stored values over defaults', () => {
    const prefs = resolvePreferences(
      {
        email: 'alt@example.com',
        min_severity_for_email: 'high',
        weekly_briefing_enabled: false,
        deadline_alerts_enabled: false,
        quiet_hours_start: '22:00:00',
        quiet_hours_end: '07:00:00',
        timezone: 'America/New_York',
      },
      { authEmail: 'founder@example.com', plan: 'pro' },
    )
    expect(prefs).toMatchObject({
      email: 'alt@example.com',
      minSeverityForEmail: 'high',
      weeklyBriefingEnabled: false,
      deadlineAlertsEnabled: false,
      quietHoursStart: '22:00:00',
      quietHoursEnd: '07:00:00',
      timezone: 'America/New_York',
    })
  })
})
