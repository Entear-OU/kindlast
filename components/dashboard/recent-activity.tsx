import { AlertTriangle, Clock } from 'lucide-react'

import {
  actionLabel,
  approverLabel,
  formatRelativeTime,
  isWatcherRunStale,
  targetLabel,
  type RecentActivity as RecentActivityData,
} from '@/lib/dashboard/activity'

/**
 * Recent Executor actions + last Watcher run (ENT-80) — the trust surface.
 *
 * The Watcher's last run is shown prominently at the top, with a stale-run
 * warning when it's been quiet for over 36 hours. Beneath it, the last 10
 * Executor actions form the audit trail a founder can show a supervisory
 * authority.
 */
export function RecentActivity({
  activity,
  currentUserId,
  currentUserEmail,
}: {
  activity: RecentActivityData
  currentUserId: string
  currentUserEmail?: string | null
}) {
  const { entries, watcherLastRunAt } = activity
  const stale = isWatcherRunStale(watcherLastRunAt)

  return (
    <section aria-label="Recent activity" className="grid gap-4 lg:grid-cols-3">
      {/* Last Watcher run — prominent. */}
      <div className="rounded-xl border border-white/5 bg-white/[0.02] p-5">
        <p className="text-xs font-medium uppercase tracking-[0.18em] text-zinc-500">
          Last Watcher run
        </p>
        <p className="mt-2 flex items-center gap-2 text-2xl font-semibold tracking-tight text-zinc-100">
          <Clock size={18} className="text-zinc-500" aria-hidden="true" />
          {formatRelativeTime(watcherLastRunAt)}
        </p>
        {stale ? (
          <p
            role="status"
            className="mt-3 flex items-center gap-1.5 rounded-lg bg-amber-500/10 px-2.5 py-1.5 text-xs font-medium text-amber-300"
          >
            <AlertTriangle size={13} aria-hidden="true" />
            {watcherLastRunAt
              ? "The Watcher hasn't run in over 36 hours."
              : "The Watcher hasn't run yet."}
          </p>
        ) : null}
      </div>

      {/* Recent actions — the last 10 audit entries. */}
      <div className="rounded-xl border border-white/5 bg-white/[0.02] p-5 lg:col-span-2">
        <p className="text-xs font-medium uppercase tracking-[0.18em] text-zinc-500">
          Recent actions
        </p>
        {entries.length === 0 ? (
          <p className="mt-4 text-sm text-zinc-500">
            No actions yet. Approved findings the Executor acts on will appear here.
          </p>
        ) : (
          <ul className="mt-3 divide-y divide-white/5">
            {entries.map((e) => (
              <li key={e.id} className="flex items-center justify-between gap-4 py-2.5">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-zinc-100">
                    {actionLabel(e.actionType)}
                    <span className="text-zinc-500"> · {targetLabel(e.targetTable)}</span>
                  </p>
                  <p className="mt-0.5 text-xs text-zinc-500">
                    by {approverLabel(e.approvingUserId, currentUserId, currentUserEmail)}
                  </p>
                </div>
                <time className="shrink-0 text-xs text-zinc-500" dateTime={e.occurredAt}>
                  {formatRelativeTime(e.occurredAt)}
                </time>
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}
