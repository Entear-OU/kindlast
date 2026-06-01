/**
 * Recent Executor activity + last Watcher run (ENT-80): the trust surface. A
 * founder sees what the Executor did recently and when the Watcher last swept,
 * so they believe the product is running — and can show an auditor the trail.
 *
 * Pure presentation + the staleness rule live here (deterministic, with an
 * injectable `now` per the codebase convention). The reads are in `./data`.
 */

export interface AuditEntry {
  id: string
  actionType: string
  targetTable: string
  targetId: string | null
  approvingUserId: string
  occurredAt: string
}

export interface RecentActivity {
  entries: AuditEntry[]
  /** The owning profile's last Watcher sweep; null if it has never run. */
  watcherLastRunAt: string | null
}

/** The Executor's action vocabulary (ENT-66/67/68), founder-facing. */
const ACTION_LABEL: Record<string, string> = {
  review: 'Reviewed',
  create_ropa: 'Created processing record',
  create_dsar: 'Logged data-subject request',
  create_ai_system: 'Registered AI system',
}

/** The compliance-record tables the Executor writes, founder-facing. */
const TARGET_LABEL: Record<string, string> = {
  processing_activities: 'Records of processing',
  dsars: 'DSAR log',
  ai_systems: 'AI systems register',
}

/** Humanise an unknown snake_case token: "mark_dsar_responded" → "Mark dsar responded". */
function humanize(token: string): string {
  const spaced = token.replace(/_/g, ' ').trim()
  return spaced ? spaced.charAt(0).toUpperCase() + spaced.slice(1) : token
}

export function actionLabel(actionType: string): string {
  return ACTION_LABEL[actionType] ?? humanize(actionType)
}

export function targetLabel(targetTable: string): string {
  return TARGET_LABEL[targetTable] ?? humanize(targetTable)
}

/**
 * Who approved the action. In the MVP the approver is the owner, so a match on
 * the current user reads as "You"; anything else is a (future) teammate.
 */
export function approverLabel(
  approvingUserId: string,
  currentUserId: string,
  currentUserEmail?: string | null,
): string {
  if (approvingUserId === currentUserId) return currentUserEmail || 'You'
  return 'A teammate'
}

/** A Watcher run is considered stale this many hours after it last ran (AC). */
export const STALE_AFTER_HOURS = 36

/** Whole hours between an ISO timestamp and `now` (negative if in the future). */
export function hoursSince(iso: string, now: Date = new Date()): number {
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return 0
  return (now.getTime() - then) / (1000 * 60 * 60)
}

/**
 * Is the last Watcher run stale? Never having run counts as stale — the founder
 * should be warned either way.
 */
export function isWatcherRunStale(lastRunAt: string | null, now: Date = new Date()): boolean {
  if (!lastRunAt) return true
  return hoursSince(lastRunAt, now) > STALE_AFTER_HOURS
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/**
 * Relative timestamp for the activity trail: "just now", "5 min ago",
 * "3 hours ago", "2 days ago", then an absolute "8 May 2026" past a month.
 * `now` is injectable so the formatting is deterministic to test.
 */
export function formatRelativeTime(iso: string | null, now: Date = new Date()): string {
  if (!iso) return 'never'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'

  const mins = Math.floor((now.getTime() - d.getTime()) / (1000 * 60))
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins} min ago`

  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours} hour${hours === 1 ? '' : 's'} ago`

  const days = Math.floor(hours / 24)
  if (days < 30) return `${days} day${days === 1 ? '' : 's'} ago`

  const base = `${d.getDate()} ${MONTHS[d.getMonth()]}`
  return d.getFullYear() === now.getFullYear() ? base : `${base} ${d.getFullYear()}`
}
