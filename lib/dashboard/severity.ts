import { FEED_SEVERITIES, type FindingSeverity } from '@/lib/feed/findings'

/**
 * Open-items-by-severity counters (ENT-78). A founder reads the four counts —
 * Critical, High, Medium, Low — and jumps straight to the matching slice of the
 * feed without scrolling.
 *
 * The counts are derived from the same open (pending) findings the posture rule
 * (ENT-77) already loads, so the dashboard makes a single read and the two
 * widgets can never disagree.
 */

export interface SeverityCount {
  severity: FindingSeverity
  count: number
  /** The feed, pre-filtered to this severity (AC). */
  href: string
}

/** The feed pre-filtered to one severity — the counter's destination. */
export function feedSeverityHref(severity: FindingSeverity): string {
  return `/feed?severity=${severity}`
}

/**
 * Count the open findings per severity, in fixed Critical → Low order, always
 * returning all four bands (a zero is still a counter the founder can read).
 */
export function countOpenBySeverity(openSeverities: FindingSeverity[]): SeverityCount[] {
  return FEED_SEVERITIES.map((severity) => ({
    severity,
    count: openSeverities.filter((s) => s === severity).length,
    href: feedSeverityHref(severity),
  }))
}

/**
 * The severity in `?severity=` if it names a real band, else null. Shared by the
 * feed page so a counter link lands on a correctly pre-filtered feed.
 */
export function parseSeverityParam(value: string | string[] | undefined): FindingSeverity | null {
  const raw = Array.isArray(value) ? value[0] : value
  return FEED_SEVERITIES.includes(raw as FindingSeverity) ? (raw as FindingSeverity) : null
}
