import type { Metadata } from 'next'
import Link from 'next/link'
import { notFound, redirect } from 'next/navigation'
import {
  ArrowUpRight,
  CalendarDays,
  ChevronRight,
  Clock,
  ListChecks,
  Radar,
  ShieldCheck,
} from 'lucide-react'

import { currentSession } from '@/lib/auth/session'
import { DeadlineClocks } from '@/components/console/deadline-clocks'
import { OpenBreakdown } from '@/components/console/open-breakdown'
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { SeverityBadge } from '@/components/feed/severity'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import { relativeTime } from '@/lib/utils'
import {
  getDashboard,
  listFindings,
  type Dashboard,
  type Finding,
} from '@/lib/findings/client'
import { listDsars } from '@/lib/records/client'

/**
 * The section this page is, for the tab strip (ENT-269). The organisation
 * and the product name come from the template in `[org]/layout.tsx`.
 */
export const metadata: Metadata = {
  title: 'Overview',
}

/**
 * An organisation's home (ENT-197, re-homed by ENT-198, made a real overview
 * here).
 *
 * The page answers three questions in order: what needs me, where do I stand,
 * and is the machine actually running. Everything on it is a door to the page
 * that owns the answer rather than a destination of its own, and every number
 * is one the reader can check: the counts come from the same dashboard read
 * the feed renders, and the list is the feed's own pending filter, five rows
 * deep.
 *
 * "What needs me" is answered twice, and the sharper answer comes first. The
 * findings queue waits: three open findings are still three open findings next
 * week. A data-subject request does not wait, because Article 12(3) runs
 * whether or not anyone signs in, so the clocks sit above the queue however
 * short the list of them is.
 *
 * Three reads, degrading separately, the same rule the feed follows: a failed
 * dashboard must not blank the decision list and a failed list must not blank
 * the posture. An empty state is a claim ("we looked, nothing needs you"), so
 * a failed read is reported as itself and never rendered as an empty list. The
 * deadline section is the strictest case, because it draws nothing when no
 * clock is running: there, silence on failure would read as an all-clear, so
 * it alone reports its failure in place of an absence.
 *
 * Deliberately no trend chart, and no period-over-period deltas beside the
 * numbers. There is no history endpoint to draw either from, and a curve
 * invented from the present would be the fabricated confidence this product
 * exists to refuse. The severity breakdown holds that slot instead: it is the
 * one thing the strip structurally cannot say, because "three open, three of
 * them urgent" is the same sentence whether that is one critical and two high
 * or the reverse.
 */
export default async function OrgHomePage({
  params,
}: {
  params: Promise<{ org: string }>
}) {
  const { org: slug } = await params

  const session = await currentSession()
  if (!session)
    redirect(`/sign-in?returnTo=${encodeURIComponent(orgPath(slug))}`)

  const resolved = await resolveOrg(session.accessToken, slug)
  if (resolved.status === 'not-a-member') notFound()
  // The layout has already rendered the unavailable state around this.
  if (resolved.status === 'unavailable')
    return <WorkspaceUnavailable title="Overview" />

  const { me, membership } = resolved
  const orgName = membership.orgName || slug
  const others = me.memberships.filter((m) => m.orgSlug !== slug)

  // Concurrent rather than sequential: independent reads, and the header
  // should not wait on the slowest of the three.
  //
  // The requests are read unfiltered rather than by status. `ListDsars` takes
  // one status and the section wants two of them, open and in progress, so the
  // filtering that matters (has this clock stopped) happens in the component
  // against `urgency`, which is the field that actually answers it. Six rows
  // for a section that shows three, so a page whose first entries are answered
  // still has something to show.
  const [dashboard, pending, dsars] = await Promise.all([
    getDashboard(session.accessToken, membership.orgId),
    listFindings(session.accessToken, membership.orgId, {
      status: 'pending',
      pageSize: 5,
    }),
    listDsars(session.accessToken, membership.orgId, { pageSize: 6 }),
  ])

  // The first name when the identity provider gave us one, the whole display
  // name when it is a single word, and the organisation when it gave nothing.
  const first = me.user?.displayName?.trim().split(/\s+/)[0]

  const today = new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  }).format(new Date())

  return (
    <main className="mx-auto w-full max-w-4xl px-5 py-8 md:px-8 md:py-10">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-[26px] font-semibold tracking-[-0.02em] text-foreground">
            {first ? `Hello, ${first}` : orgName}
          </h1>
          <p className="mt-1.5 text-sm text-muted-foreground">
            {/* "you", not the organisation again: when the greeting has
                already fallen back to the name, repeating it here stutters. */}
            Where you stand. Every number here opens the page it came from.
          </p>
        </div>
        <span className="inline-flex shrink-0 items-center gap-2 rounded-full border border-border/60 bg-card px-3 py-1.5 text-xs font-medium text-muted-foreground">
          <CalendarDays aria-hidden="true" className="size-3.5" />
          {today}
        </span>
      </header>

      <PostureStrip dashboard={dashboard.ok ? dashboard.value : undefined} />

      {/* The section draws its own heading and disappears whole when no clock
          is running. A failed read still says so: this is the one block that
          renders nothing on an all-clear, so silence on failure would be
          indistinguishable from safety, which is what a deadline must never
          imply. */}
      {dsars.ok ? (
        <DeadlineClocks slug={slug} dsars={dsars.value.dsars ?? []} />
      ) : (
        <section className="mt-9" aria-labelledby="overview-clocks">
          <h2
            id="overview-clocks"
            className="text-[15px] font-semibold text-foreground"
          >
            On the clock
          </h2>
          <p className="mt-3 rounded-2xl border border-border/60 bg-card p-4 text-sm text-muted-foreground">
            The data-subject requests could not be read just now. Open the
            register to check the deadlines rather than reading this space as
            none.
          </p>
        </section>
      )}

      {/* Only with the dashboard read: the counts are the whole content, so
          without them this would be four zeroes claiming a clear queue. */}
      {dashboard.ok ? (
        <section className="mt-9" aria-labelledby="overview-breakdown">
          <h2
            id="overview-breakdown"
            className="text-[15px] font-semibold text-foreground"
          >
            Open findings
          </h2>
          <div className="mt-3">
            <OpenBreakdown counts={dashboard.value.openBySeverity ?? {}} />
          </div>
        </section>
      ) : null}

      <section className="mt-9" aria-labelledby="overview-decisions">
        <div className="flex items-baseline justify-between gap-4">
          <h2
            id="overview-decisions"
            className="text-[15px] font-semibold text-foreground"
          >
            Needs your decision
          </h2>
          <Link
            href={orgPath(slug, '/feed')}
            className="inline-flex items-center gap-1 text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
          >
            Open the feed
            <ArrowUpRight aria-hidden="true" className="size-3.5" />
          </Link>
        </div>

        {pending.ok ? (
          <DecisionList
            slug={slug}
            findings={pending.value.findings ?? []}
            assessed={
              !dashboard.ok || dashboard.value.posture !== 'not_assessed'
            }
          />
        ) : (
          <p className="mt-3 rounded-2xl border border-border/60 bg-card p-4 text-sm text-muted-foreground">
            The findings could not be read just now, so nothing is shown rather
            than an empty list that would claim you are done. Reload to try
            again.
          </p>
        )}
      </section>

      {others.length > 0 ? (
        <section className="mt-11" aria-labelledby="overview-orgs">
          <h2
            id="overview-orgs"
            className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase"
          >
            Other organisations
          </h2>
          {/* Links rather than labels: switching organisation is a navigation,
              which is the point of routing on the slug. */}
          <ul className="mt-3 space-y-2">
            {others.map((m) => (
              <li key={m.orgId} className="text-sm">
                {m.orgSlug ? (
                  <Link
                    href={orgPath(m.orgSlug)}
                    className="text-foreground underline underline-offset-4 hover:opacity-80"
                  >
                    {m.orgName || m.orgSlug}
                  </Link>
                ) : (
                  <span className="text-muted-foreground">
                    {m.orgName || m.orgId}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </main>
  )
}

const BAND_LABELS: Record<string, { label: string; dot: string }> = {
  not_assessed: { label: 'Not assessed', dot: 'bg-muted-foreground/40' },
  green: { label: 'On track', dot: 'bg-emerald-500' },
  amber: { label: 'Needs attention', dot: 'bg-amber-500' },
  red: { label: 'Action required', dot: 'bg-red-500' },
}

/**
 * The three facts, divided like a ledger line: what needs a decision, the
 * posture band, and when the Watcher last looked. The third is on the strip
 * because the other two are only worth believing next to it: a green posture
 * over a sweep that never ran is the confident wrong answer the old console
 * gave, and ENT-161 exists because of it.
 */
function PostureStrip({ dashboard }: { dashboard?: Dashboard }) {
  if (!dashboard) {
    return (
      <p className="mt-6 rounded-2xl border border-border/60 bg-card p-4 text-sm text-muted-foreground">
        The dashboard could not be read just now, so no posture is shown rather
        than a guessed one. Reload to try again.
      </p>
    )
  }

  const band = BAND_LABELS[dashboard.posture] ?? BAND_LABELS.not_assessed
  const counts = dashboard.openBySeverity ?? {}
  const urgent = (counts.critical ?? 0) + (counts.high ?? 0)

  return (
    // A ledger line rather than a card: hairlines above and below, the cells
    // divided by rules, and nothing boxed. The three facts are the page's
    // opening statement, and a card around them made them look like the first
    // of several widgets instead.
    <div className="mt-7 grid grid-cols-1 divide-y divide-border/60 border-y border-border/60 sm:grid-cols-3 sm:divide-x sm:divide-y-0">
      <StatCell
        icon={<ListChecks aria-hidden="true" className="size-4" />}
        label="Needs a decision"
        value={String(dashboard.openTotal ?? 0)}
        detail={
          urgent > 0
            ? `${urgent} of them critical or high`
            : 'nothing urgent among them'
        }
        detailTone={urgent > 0 ? 'text-amber-600' : 'text-muted-foreground'}
      />
      <StatCell
        icon={<ShieldCheck aria-hidden="true" className="size-4" />}
        label="Posture"
        value={band.label}
        valueDot={band.dot}
        detail={dashboard.postureHeadline || 'from the open findings'}
      />
      <StatCell
        icon={<Radar aria-hidden="true" className="size-4" />}
        label="Last sweep"
        value={relativeTime(dashboard.pipeline?.watcherLastRunAt)}
        detail={
          dashboard.pipeline?.watcherLastRunAt
            ? 'the Watcher looked and recorded it'
            : 'the Watcher has not looked yet'
        }
      />
    </div>
  )
}

function StatCell({
  icon,
  label,
  value,
  valueDot,
  detail,
  detailTone = 'text-muted-foreground',
}: {
  icon: React.ReactNode
  label: string
  value: string
  valueDot?: string
  detail: string
  detailTone?: string
}) {
  return (
    // The first cell loses its left padding from `sm` up so the leading icon
    // sits on the page's own left edge, in line with the heading above it.
    <div className="flex items-start gap-3 p-4 sm:px-5 sm:first:pl-0">
      <span className="mt-0.5 inline-flex size-10 shrink-0 items-center justify-center rounded-full bg-muted text-foreground/70">
        {icon}
      </span>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        {/* Wrapping rather than truncating. "Action required" is the posture
            a reader most needs, and at this size it did not fit the cell, so
            the strip reported "Action requir..." on exactly the day it
            mattered. A value that takes two lines is better than one that
            takes the wrong meaning. */}
        <p className="mt-1 flex items-start gap-2 text-[21px] leading-tight font-semibold tracking-[-0.02em] text-foreground">
          {valueDot ? (
            <span
              aria-hidden="true"
              className={`mt-[7px] size-2 shrink-0 rounded-full ${valueDot}`}
            />
          ) : null}
          <span className="line-clamp-2">{value}</span>
        </p>
        {/* Two lines, then clamped: every cell's detail was ellipsing at one
            line, and three ellipses in a row read as a rendering fault rather
            than as three sentences. */}
        <p className={`mt-0.5 line-clamp-2 text-xs leading-snug ${detailTone}`}>
          {detail}
        </p>
      </div>
    </div>
  )
}

const EFFORT_LABELS: Record<string, string> = {
  hours: 'Hours of work',
  days: 'Days of work',
  weeks: 'Weeks of work',
}

/**
 * The queue, five rows deep, each a door to the finding itself.
 *
 * The two empty states are different claims and are worded as such: an
 * unassessed organisation has not been looked at, and saying "nothing needs
 * you" to it would be the fabricated all-clear; an assessed one with no
 * pending rows has genuinely been looked at and cleared.
 */
function DecisionList({
  slug,
  findings,
  assessed,
}: {
  slug: string
  findings: Finding[]
  assessed: boolean
}) {
  if (findings.length === 0) {
    return (
      <p className="mt-3 rounded-2xl border border-border/60 bg-card p-4 text-sm text-muted-foreground">
        {assessed
          ? 'Nothing is waiting on you. New findings land here when the Watcher raises them.'
          : 'Nothing has looked at your compliance yet. The first sweep runs after onboarding is confirmed, and what it raises appears here.'}
      </p>
    )
  }

  return (
    <ul className="mt-3 overflow-hidden rounded-2xl border border-border/60 bg-card">
      {findings.map((f) => (
        <li
          key={f.findingId}
          className="border-b border-border/60 last:border-b-0"
        >
          <Link
            href={orgPath(slug, `/feed/${f.findingId}`)}
            className="group flex items-center gap-4 p-4 transition-colors hover:bg-muted/50"
          >
            <SeverityBadge severity={f.severity} />
            <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">
              {f.detected}
            </span>
            {f.effortEstimate ? (
              <span className="hidden shrink-0 items-center gap-1.5 text-xs text-muted-foreground sm:inline-flex">
                <Clock aria-hidden="true" className="size-3.5" />
                {EFFORT_LABELS[f.effortEstimate] ?? f.effortEstimate}
              </span>
            ) : null}
            <ChevronRight
              aria-hidden="true"
              className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5"
            />
          </Link>
        </li>
      ))}
    </ul>
  )
}
