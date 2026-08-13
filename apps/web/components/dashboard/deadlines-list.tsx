import Link from 'next/link'

import {
  DEADLINE_WINDOW_DAYS,
  daysRemainingLabel,
  formatDueDate,
  type UpcomingDeadline,
} from '@/lib/dashboard/deadlines'
import { severityChip } from '@/lib/feed/findings'

/**
 * The upcoming-deadlines list (ENT-79): the next 60 days of regulatory dates,
 * soonest first. Each row carries the obligation title, due date, days
 * remaining, and links to the related finding. Empty when there's nothing due.
 */
export function DeadlinesList({ deadlines }: { deadlines: UpcomingDeadline[] }) {
  return (
    <section aria-label="Upcoming deadlines" className="space-y-3">
      <h2 className="text-sm font-semibold text-zinc-200">Upcoming deadlines</h2>

      {deadlines.length === 0 ? (
        <p className="rounded-xl border border-white/5 bg-white/[0.02] px-4 py-8 text-center text-sm text-zinc-500">
          No deadlines in the next {DEADLINE_WINDOW_DAYS} days
        </p>
      ) : (
        <ul className="divide-y divide-white/5 overflow-hidden rounded-xl border border-white/5 bg-white/[0.02]">
          {deadlines.map((d) => {
            const chip = severityChip(d.severity)
            const overdue = d.daysRemaining < 0
            return (
              <li key={d.findingId}>
                <Link
                  href={`/feed/${d.findingId}`}
                  className="flex items-center justify-between gap-4 px-4 py-3 transition-colors hover:bg-white/[0.03]"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-zinc-100">{d.title}</p>
                    <p className="mt-0.5 text-xs text-zinc-500">{formatDueDate(d.dueAt)}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-3">
                    <span
                      className={`hidden rounded-full px-2 py-0.5 text-xs font-medium sm:inline ${chip.className}`}
                    >
                      {chip.label}
                    </span>
                    <span
                      className={`text-xs font-medium ${overdue ? 'text-rose-300' : 'text-zinc-300'}`}
                    >
                      {daysRemainingLabel(d.daysRemaining)}
                    </span>
                  </div>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
