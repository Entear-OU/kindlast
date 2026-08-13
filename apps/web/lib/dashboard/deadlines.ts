import type { FindingSeverity } from '@/lib/feed/findings'

/**
 * Upcoming deadlines list (ENT-79): the next 60 days of regulatory dates a
 * founder should plan around, sorted soonest-first.
 *
 * Deadlines are derived from the deadline/DSAR findings the Watcher already
 * emits — so every row links straight to its finding, and the due date and day
 * count are the Watcher's own (carried in `metadata.signal_metadata`). The pure
 * `buildUpcomingDeadlines` is the testable core; `loadUpcomingDeadlines` in
 * `./data` is the thin Supabase read that feeds it.
 */

/** The horizon the list covers (AC: "next 60 days"). */
export const DEADLINE_WINDOW_DAYS = 60

/** The deadline-bearing finding kinds. */
const DEADLINE_KINDS = new Set(['deadline', 'dsar'])

/** A finding row as the loader selects it, before it becomes a deadline. */
export interface DeadlineFindingRow {
  id: string
  severity: FindingSeverity
  regulatory_obligation: string | null
  detected: string
  metadata: {
    signal_kind?: string
    signal_metadata?: {
      days_remaining?: number | string
      effective_date?: string
      response_due_at?: string
    } | null
  } | null
}

export interface UpcomingDeadline {
  /** The finding this deadline came from — the row's link target. */
  findingId: string
  /** Obligation title (falls back to the finding's detected text). */
  title: string
  /** ISO date/timestamp the obligation is due. */
  dueAt: string
  /** Days until due; negative means overdue. */
  daysRemaining: number
  severity: FindingSeverity
}

/**
 * Turn the loaded finding rows into the deadlines list: keep the deadline/DSAR
 * findings that have a due date within the window, then sort by `due_at`
 * ascending (AC). Overdue deadlines (negative days) are kept — hiding a lapsed
 * obligation would be worse than showing it — and sort to the top.
 */
export function buildUpcomingDeadlines(rows: DeadlineFindingRow[]): UpcomingDeadline[] {
  const deadlines: UpcomingDeadline[] = []

  for (const row of rows) {
    if (!DEADLINE_KINDS.has(row.metadata?.signal_kind ?? '')) continue
    const meta = row.metadata?.signal_metadata
    const dueAt = meta?.effective_date ?? meta?.response_due_at
    if (!dueAt) continue

    const daysRemaining = Number(meta?.days_remaining ?? 0)
    if (daysRemaining > DEADLINE_WINDOW_DAYS) continue

    deadlines.push({
      findingId: row.id,
      title: row.regulatory_obligation ?? row.detected,
      dueAt,
      daysRemaining,
      severity: row.severity,
    })
  }

  return deadlines.sort((a, b) => a.dueAt.localeCompare(b.dueAt))
}

const MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
]

/**
 * Format a due date as e.g. "15 Jun 2026". Parses the YYYY-MM-DD prefix
 * directly so it's deterministic and free of timezone drift (no `Date`).
 */
export function formatDueDate(dueAt: string): string {
  const [y, m, d] = dueAt.slice(0, 10).split('-')
  const month = MONTHS[Number(m) - 1] ?? m
  return `${Number(d)} ${month} ${y}`
}

/** Plain-language day count: "Due today", "5 days left", "2 days overdue". */
export function daysRemainingLabel(daysRemaining: number): string {
  if (daysRemaining === 0) return 'Due today'
  if (daysRemaining < 0) {
    const n = Math.abs(daysRemaining)
    return `${n} day${n === 1 ? '' : 's'} overdue`
  }
  return `${daysRemaining} day${daysRemaining === 1 ? '' : 's'} left`
}
