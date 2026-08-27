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
import { WorkspaceUnavailable } from '@/components/console/workspace-unavailable'
import { SeverityBadge } from '@/components/feed/severity'
import { orgPath, resolveOrg } from '@/lib/auth/org'
import {
  getDashboard,
  listFindings,
  type Dashboard,
  type Finding,
} from '@/lib/findings/client'

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
 * Two reads, degrading separately, the same rule the feed follows: a failed
 * dashboard must not blank the decision list and a failed list must not blank
 * the posture. An empty state is a claim ("we looked, nothing needs you"), so
 * a failed read is reported as itself and never rendered as an empty list.
 *
 * Deliberately no trend chart. There is no history endpoint to draw one from,
 * and a chart invented from the present would be the fabricated confidence
 * this product exists to refuse.
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
  // should not wait on the slower of the two.
  const [dashboard, pending] = await Promise.all([
    getDashboard(session.accessToken, membership.orgId),
    listFindings(session.accessToken, membership.orgId, {
      status: 'pending',
      pageSize: 5,
    }),
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
    <main className="mx-auto w-full max-w-3xl px-5 py-8 md:px-8 md:py-10">
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

      <section className="mt-8" aria-labelledby="overview-decisions">
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
        <section className="mt-10" aria-labelledby="overview-orgs">
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
    <div className="mt-6 grid grid-cols-1 divide-y divide-border/60 rounded-2xl border border-border/60 bg-card sm:grid-cols-3 sm:divide-x sm:divide-y-0">
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
    <div className="flex items-start gap-3 p-4">
      <span className="mt-0.5 inline-flex size-9 shrink-0 items-center justify-center rounded-full border border-border/60 bg-background text-muted-foreground">
        {icon}
      </span>
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="mt-0.5 flex items-center gap-2 text-lg leading-tight font-semibold text-foreground">
          {valueDot ? (
            <span
              aria-hidden="true"
              className={`size-2 shrink-0 rounded-full ${valueDot}`}
            />
          ) : null}
          <span className="truncate">{value}</span>
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

/**
 * When something happened, in words a person scans: "2 hours ago" close up,
 * the date once it is far enough away that relative counting stops helping,
 * and an honest "Never" when it has not happened at all.
 */
function relativeTime(iso?: string): string {
  if (!iso) return 'Never'
  const then = new Date(iso).getTime()
  if (Number.isNaN(then)) return 'Never'

  const minutes = Math.round((Date.now() - then) / 60_000)
  if (minutes < 1) return 'Just now'
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`
  const days = Math.round(hours / 24)
  if (days < 7) return `${days} ${days === 1 ? 'day' : 'days'} ago`
  return new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
  }).format(then)
}
