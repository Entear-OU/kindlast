import Link from 'next/link'

import type { SeverityCount } from '@/lib/dashboard/severity'
import { severityChip } from '@/lib/feed/findings'

/**
 * The open-items-by-severity counters (ENT-78): four tiles — Critical, High,
 * Medium, Low — each a link into the feed pre-filtered to that band, so a
 * founder can decide where to start without scrolling.
 *
 * The counts are server-rendered from the dashboard's single findings read, so
 * they refresh on every view (the AC's "or refresh on view") with no client
 * subscription.
 */
export function SeverityCounters({ counts }: { counts: SeverityCount[] }) {
  return (
    <section aria-label="Open items by severity" className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {counts.map(({ severity, count, href }) => {
        const chip = severityChip(severity)
        return (
          <Link
            key={severity}
            href={href}
            aria-label={`${chip.label}: ${count} open`}
            className="flex flex-col gap-2 rounded-xl border border-white/5 bg-white/[0.02] p-4 transition-colors hover:border-white/15 hover:bg-white/[0.04]"
          >
            <span
              className={`inline-flex w-fit rounded-full px-2 py-0.5 text-xs font-medium ${chip.className}`}
            >
              {chip.label}
            </span>
            <span className="text-3xl font-semibold tracking-tight text-zinc-100">{count}</span>
            <span className="text-xs text-zinc-500">open</span>
          </Link>
        )
      })}
    </section>
  )
}
