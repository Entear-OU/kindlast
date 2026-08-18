import { SOURCE_LABELS, type Evidence } from '@/lib/memory/client'

/**
 * What Kindlast observed (ENT-228).
 *
 * # TWO TIMESTAMPS, AND THE GAP IS THE POINT
 *
 * `observedAt` is when it was true at the source; `fetchedAt` is when we
 * learned it. They are routinely far apart, and a record of processing
 * activities last edited in March and first read by us in August is a
 * five-month blind spot. Showing one timestamp would hide it, so this shows
 * the gap when there is one worth showing.
 *
 * # SUPERSEDED ROWS STAY VISIBLE
 *
 * Marked rather than hidden. "We used to read this from your helpdesk" is
 * exactly what somebody auditing an older finding is looking for, and a list
 * that quietly dropped them would make the record look tidier than it is.
 */
export function EvidenceList({ evidence }: { evidence: Evidence[] }) {
  return (
    <ul className="mt-3 divide-y divide-border/60 rounded-xl border border-border/60 bg-background">
      {evidence.map((item) => (
        <li key={item.id} className="p-4">
          <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
            <p className="text-sm font-medium text-foreground">{item.kind}</p>
            <p className="text-xs text-muted-foreground">
              {item.source ? (SOURCE_LABELS[item.source] ?? item.source) : null}
            </p>
          </div>

          <p className="mt-1 text-xs text-muted-foreground">
            {item.observedAt ? (
              <>
                Recorded as true on{' '}
                <time dateTime={item.observedAt}>
                  {formatDay(item.observedAt)}
                </time>
              </>
            ) : null}
            {describeGap(item)}
          </p>

          {item.supersededBy ? (
            <p className="mt-1 text-xs text-muted-foreground">
              Superseded by a later reading.
            </p>
          ) : null}
        </li>
      ))}
    </ul>
  )
}

/**
 * Say when we learned it, but only when that is not the same day.
 *
 * A tool read live tells you nothing by repeating the date twice. A reading
 * that was five months stale when we took it tells you a lot, and this is the
 * only place a customer would find that out.
 */
function describeGap(item: Evidence): string {
  if (!item.observedAt || !item.fetchedAt) return ''
  const observed = new Date(item.observedAt)
  const fetched = new Date(item.fetchedAt)
  if (Number.isNaN(observed.getTime()) || Number.isNaN(fetched.getTime()))
    return ''

  const days = Math.floor(
    (fetched.getTime() - observed.getTime()) / (1000 * 60 * 60 * 24),
  )
  if (days < 1) return ''
  if (days === 1) return ', read by us a day later'
  return `, read by us ${days} days later`
}

function formatDay(iso: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}
