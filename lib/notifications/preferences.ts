/**
 * Notification preferences — the severity gate (ENT-61).
 *
 * Mirrors the Postgres enum types created in the ENT-61 migration
 * (`severity_level`, `email_frequency`) and the `notification_preferences`
 * table. The actual email sender / digest scheduler is the Comms agent's job
 * (a later epic); ENT-61 provides only the deterministic gate it will consult
 * and the preference store it will read.
 */

export type SeverityLevel = 'low' | 'medium' | 'high' | 'critical'
export type EmailFrequency = 'immediate' | 'daily' | 'weekly' | 'off'

const SEVERITY_RANK: Record<SeverityLevel, number> = {
  low: 1,
  medium: 2,
  high: 3,
  critical: 4,
}

// Minimum severity rank that warrants an email for each frequency. 'off' has no
// threshold (only the critical safety override gets through).
const FREQUENCY_THRESHOLD: Record<EmailFrequency, number> = {
  immediate: SEVERITY_RANK.high, // act-now channel — only high+
  daily: SEVERITY_RANK.medium, // digest — medium and up
  weekly: SEVERITY_RANK.high, // light-touch digest — high+
  off: Infinity,
}

/**
 * Should a finding of `severity` be emailed under the user's `frequency`?
 *
 * `critical` is a safety override and always returns true — even when email is
 * off — so a client never silently runs out a compliance clock. Otherwise the
 * severity must meet the frequency's threshold; `off` silences everything below
 * critical.
 */
export function shouldNotifyByEmail(
  severity: SeverityLevel,
  frequency: EmailFrequency,
): boolean {
  if (severity === 'critical') return true
  return SEVERITY_RANK[severity] >= FREQUENCY_THRESHOLD[frequency]
}
