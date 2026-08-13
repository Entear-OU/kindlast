/**
 * Deadline-alert threshold model (ENT-75).
 *
 * A deadline finding's email re-fires only as its days-remaining crosses the
 * 30 / 14 / 7 / 1-day thresholds. `activeThreshold` buckets the live
 * days-remaining into the most urgent threshold it has reached; the dispatcher
 * sends once per (finding, threshold). Pure — the whole "never daily noise"
 * guarantee reduces to this function plus a per-threshold dedup log.
 */

export type DeadlineThreshold = 1 | 7 | 14 | 30

/** Thresholds, most-distant first — the order alerts step through over time. */
export const DEADLINE_THRESHOLDS: readonly DeadlineThreshold[] = [30, 14, 7, 1]

/**
 * The most urgent threshold `daysRemaining` has crossed into, or null when the
 * deadline is still more than 30 days out (no alert yet). Overdue (≤ 0) maps to
 * the 1-day bucket — the most urgent.
 */
export function activeThreshold(daysRemaining: number): DeadlineThreshold | null {
  if (daysRemaining <= 1) return 1
  if (daysRemaining <= 7) return 7
  if (daysRemaining <= 14) return 14
  if (daysRemaining <= 30) return 30
  return null
}
