import Link from 'next/link'
import { ArrowUpRight } from 'lucide-react'

import { DueLabel, UrgencyBadge } from '@/components/records/badges'
import { orgPath } from '@/lib/auth/org'
import type { Dsar } from '@/lib/records/client'

/**
 * The statutory clocks, on the overview.
 *
 * WHY THIS IS THE ONE THING THE OVERVIEW COULD NOT DO WITHOUT
 *
 * Everything else on this page is a queue that waits. Three findings need a
 * decision, and they will still need it next week. A data-subject request is
 * not like that: Article 12(3) runs whether or not anyone opens the console,
 * and missing it is itself the breach rather than a signal of one. A home page
 * that shows the patient work and hides the running clock is showing the wrong
 * half.
 *
 * Nothing here is computed. `urgency` and `daysUntilDue` arrive derived from
 * the server, `DueLabel` turns the number into the words a handler acts on, and
 * the order is the order `ListDsars` returned, which is by `response_due_at`
 * ascending. Every one of those is a regulatory deadline expressed once. A
 * second expression in the browser would disagree eventually, and would
 * disagree per timezone in the meantime.
 *
 * Renders nothing when no clock is running, rather than an empty state saying
 * so. "No deadlines" is a claim about every request that exists, and this
 * component sees one page of them.
 */
export function DeadlineClocks({
  slug,
  dsars,
}: {
  slug: string
  dsars: Dsar[]
}) {
  // Answered is a clock that has stopped. A response that went out late is
  // still a response, and a countdown that keeps running asks somebody to act
  // on something already done.
  const running = dsars.filter((d) => d.urgency !== 'answered')
  // The heading belongs to the section rather than to the page, so that an
  // organisation with no running clock loses the whole block instead of
  // keeping a title with nothing under it.
  if (running.length === 0) return null

  return (
    <section className="mt-9" aria-labelledby="overview-clocks">
      <div className="flex items-baseline justify-between gap-4">
        <h2
          id="overview-clocks"
          className="text-[15px] font-semibold text-foreground"
        >
          On the clock
        </h2>
        <Link
          href={orgPath(slug, '/records/dsars')}
          className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          All requests
          <ArrowUpRight aria-hidden="true" className="size-3.5" />
        </Link>
      </div>

      <ul className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {running.map((d) => (
          <li key={d.dsarId}>
            <Link
              href={orgPath(slug, `/records/dsars/${d.dsarId}`)}
              className="flex h-full flex-col justify-between gap-3 rounded-2xl border border-border/60 bg-card p-4 transition-colors hover:border-border hover:bg-muted/40"
            >
              <div className="flex items-start justify-between gap-3">
                <span
                  data-testid="deadline-subject"
                  className="min-w-0 truncate text-sm font-medium text-foreground"
                >
                  {/* The subject's own name, which is customer data and is
                    rendered as text and nothing else. */}
                  {d.subjectName || 'Unnamed subject'}
                </span>
                <UrgencyBadge value={d.urgency} />
              </div>
              <span
                className={`text-sm ${
                  d.urgency === 'overdue'
                    ? 'font-semibold text-red-700'
                    : 'text-muted-foreground'
                }`}
              >
                <DueLabel urgency={d.urgency} daysUntilDue={d.daysUntilDue} />
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
