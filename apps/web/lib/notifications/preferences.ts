/**
 * Notification preferences — the domain rules the Comms dispatchers consult
 * (ENT-76, replacing the ENT-61 frequency stand-in).
 *
 * Pure functions over the `notification_preferences` row: the per-severity gate
 * for finding emails, the quiet-hours window, and default resolution. No IO, so
 * the dispatchers stay testable.
 */

import type { Plan } from '@/lib/billing/plan'

export type SeverityLevel = 'low' | 'medium' | 'high' | 'critical'

const SEVERITY_RANK: Record<SeverityLevel, number> = {
  low: 1,
  medium: 2,
  high: 3,
  critical: 4,
}

/** Default severity floor for the immediate finding email (AC: Medium). */
export const DEFAULT_MIN_SEVERITY: SeverityLevel = 'medium'
export const DEFAULT_TIMEZONE = 'Europe/Tallinn'

/**
 * Should a finding of `severity` be emailed given the user's `minSeverity`
 * floor? `critical` is a safety override and always returns true — even if the
 * floor is somehow above it — so a client never silently runs out a compliance
 * clock. Otherwise the severity must meet or exceed the floor.
 */
export function shouldEmailFinding(
  severity: SeverityLevel,
  minSeverity: SeverityLevel,
): boolean {
  if (severity === 'critical') return true
  return SEVERITY_RANK[severity] >= SEVERITY_RANK[minSeverity]
}

/** Minutes-since-midnight for an `HH:MM[:SS]` time string, or null if unparseable. */
function minutesOfDay(value: string | null | undefined): number | null {
  if (!value) return null
  const m = /^(\d{1,2}):(\d{2})/.exec(value)
  if (!m) return null
  const h = Number(m[1])
  const min = Number(m[2])
  if (h > 23 || min > 59) return null
  return h * 60 + min
}

/** The local HH:MM (minutes since midnight) at `nowMs` in `timeZone`. */
function localMinutes(nowMs: number, timeZone: string): number {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).formatToParts(new Date(nowMs))
  const get = (type: string) => Number(parts.find((p) => p.type === type)?.value ?? '0')
  return (get('hour') % 24) * 60 + get('minute')
}

/**
 * Is `nowMs` inside the user's quiet-hours window (their `timeZone`)? Handles a
 * window that wraps past midnight (e.g. 22:00→07:00). A null/empty start or end
 * means no quiet hours (never quiet).
 */
export function inQuietHours(
  nowMs: number,
  timeZone: string,
  start: string | null | undefined,
  end: string | null | undefined,
): boolean {
  const startMin = minutesOfDay(start)
  const endMin = minutesOfDay(end)
  if (startMin === null || endMin === null || startMin === endMin) return false

  const now = localMinutes(nowMs, timeZone)
  return startMin < endMin
    ? now >= startMin && now < endMin // same-day window
    : now >= startMin || now < endMin // wraps past midnight
}

/** A user's notification preferences, with defaults resolved. */
export interface NotificationPreferences {
  email: string | null
  minSeverityForEmail: SeverityLevel
  weeklyBriefingEnabled: boolean
  deadlineAlertsEnabled: boolean
  quietHoursStart: string | null
  quietHoursEnd: string | null
  timezone: string
}

/** The raw row shape as stored (snake_case), all columns nullable on read. */
export interface NotificationPreferencesRow {
  email?: string | null
  min_severity_for_email?: SeverityLevel | null
  weekly_briefing_enabled?: boolean | null
  deadline_alerts_enabled?: boolean | null
  quiet_hours_start?: string | null
  quiet_hours_end?: string | null
  timezone?: string | null
}

/**
 * Fill defaults over a (possibly missing) preferences row. `email` falls back to
 * the auth email; the weekly-briefing default is plan-aware (AC: true for Pro,
 * false for Free) so a Free user who never opened settings isn't shown as opted
 * in. Other booleans default on; severity floor defaults to Medium.
 */
export function resolvePreferences(
  row: NotificationPreferencesRow | null | undefined,
  { authEmail, plan }: { authEmail: string | null; plan: Plan },
): NotificationPreferences {
  return {
    email: row?.email ?? authEmail,
    minSeverityForEmail: row?.min_severity_for_email ?? DEFAULT_MIN_SEVERITY,
    weeklyBriefingEnabled: row?.weekly_briefing_enabled ?? (plan === 'pro'),
    deadlineAlertsEnabled: row?.deadline_alerts_enabled ?? true,
    quietHoursStart: row?.quiet_hours_start ?? null,
    quietHoursEnd: row?.quiet_hours_end ?? null,
    timezone: row?.timezone ?? DEFAULT_TIMEZONE,
  }
}
