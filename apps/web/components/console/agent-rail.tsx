import { MoreHorizontal, Phone, Sparkles, Video } from 'lucide-react'
import Link from 'next/link'

import { KindyComposer } from '@/components/console/kindy-composer'
import type { KindyAction } from '@/components/console/kindy-state'
import { orgPath } from '@/lib/auth/org'
import { relativeTime } from '@/lib/utils'

// The two channels that do not exist yet, kept as data so the card and its
// tooltips cannot disagree about what they are called.
const NOT_BUILT = [
  { icon: Phone, label: 'Call' },
  { icon: Video, label: 'Walkthrough' },
]

/**
 * One row of the Activity list: something an agent did, in the reader's
 * terms. Today that is a finding arriving; the shape is deliberately not
 * `Finding` so the rail never grows a dependency on the feed's whole type.
 */
export interface ActivityItem {
  id: string
  title: string
  severity: string
  at?: string
}

// Colour is never the only signal (the severity badge's own rule): the title
// and the finding page carry the word. The dot mirrors the feed's palette so
// the same severity never wears two colours in one console.
const SEVERITY_DOT: Record<string, string> = {
  critical: 'bg-red-500',
  high: 'bg-amber-500',
  medium: 'bg-sky-500',
  low: 'bg-muted-foreground/40',
}

/**
 * Kindy's panel (ENT-222, ENT-232, ENT-270, reshaped when the rail became a
 * contact card).
 *
 * The rail used to be a directory of the four agents. It is Kindy now: one
 * face over the whole pipeline, with the reference layout's anatomy and this
 * product's honesty. The contact row's dead channels are visibly disabled
 * controls, never live-looking ones (ENT-202). The composer is a real form
 * that carries words to the Analyst on the newest open finding. Activity is
 * what was actually raised, newest first, each row a door to the finding.
 * The four agents themselves live behind the card's "more" button, on the
 * agents page, where what each one is allowed to do is written down: that
 * link is the panel's one path to them, so it is load-bearing rather than
 * decorative.
 *
 * Kindy the orchestrator does not exist as a skill yet. The card claims
 * presentation, not capability: the composer's caption names exactly where
 * words go today, and when an orchestrator lands its status will arrive from
 * the catalogue the way every agent's did, with the parity test watching.
 */

/**
 * Rendered twice, once per layout, because the rail is a column on a wide
 * screen and a section beneath the content on a phone, and those are different
 * places in the DOM rather than the same place styled differently.
 *
 * The `variant` is what keeps that legal: two elements carrying the same `id`
 * is invalid HTML and gives a screen reader two things to land on, so the ids
 * are derived from it. It also gives the phone's tab bar something to link to.
 */
export function AgentRail({
  orgSlug,
  variant = 'desktop',
  activity,
  kindyAction,
}: {
  orgSlug: string
  variant?: 'desktop' | 'mobile'
  /**
   * The newest findings, fetched by the layout and handed down so this stays
   * a plain synchronous component a test can render. Absent when the read
   * failed, which renders as nothing-listed rather than as a claim that
   * nothing happened.
   */
  activity?: ActivityItem[]
  /**
   * The server action behind the composer, injected by the layout for the
   * same reason activity is: importing it here would drag `next/headers`
   * into every test that renders the chrome.
   */
  kindyAction: KindyAction
}) {
  const headingId = `agent-rail-heading-${variant}`

  return (
    <aside
      id={variant === 'mobile' ? 'agents' : undefined}
      aria-labelledby={headingId}
      className={
        variant === 'mobile'
          ? 'flex flex-col gap-6 border-t border-border/60 bg-background px-5 py-6'
          : 'flex h-full flex-col gap-6 overflow-y-auto px-5 py-6'
      }
    >
      <div className="rounded-2xl bg-card px-5 py-6 text-center shadow-[0_1px_3px_oklch(0_0_0/0.06)]">
        <span className="relative mx-auto flex size-14 items-center justify-center rounded-full bg-gradient-to-br from-[oklch(0.62_0.11_176)] to-[oklch(0.42_0.1_200)] text-white">
          <Sparkles aria-hidden="true" className="size-6" />
          {/* The dot claims exactly what the composer delivers: Kindy
              answers in writing today. It goes grey the day that stops being
              true, not the day someone remembers it. */}
          <span
            aria-hidden="true"
            className="absolute right-0 bottom-0 size-3 rounded-full border-2 border-card bg-emerald-500"
          />
        </span>
        <h2 id={headingId} className="mt-3 text-[15px] font-semibold">
          Kindy
        </h2>
        <p className="mt-0.5 text-xs text-muted-foreground">@kindy</p>

        {/* The reference's contact row, drawn honestly: the two channels that
            do not exist are disabled controls that say so, never silent ones
            (ENT-202), and the third goes somewhere real. */}
        <div className="mt-4 flex items-center justify-center gap-3">
          {NOT_BUILT.map(({ icon: Icon, label }) => (
            <button
              key={label}
              type="button"
              disabled
              title={`${label}: not built yet`}
              className="flex size-10 cursor-not-allowed items-center justify-center rounded-full border border-border/60 bg-background text-muted-foreground/50"
            >
              <Icon aria-hidden="true" className="size-4" />
              <span className="sr-only">{label} (not built yet)</span>
            </button>
          ))}
          {/* The one path from the console to the agents page, where what
              each agent is allowed to do is written down. Removing this
              orphans that surface, which is the ENT-245 failure shape. */}
          <Link
            href={orgPath(orgSlug, '/agents')}
            title="About Kindy's agents"
            className="flex size-10 items-center justify-center rounded-full border border-border/60 bg-background text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <MoreHorizontal aria-hidden="true" className="size-4" />
            <span className="sr-only">About Kindy&apos;s agents</span>
          </Link>
        </div>
      </div>

      <div>
        <p className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
          Activity
        </p>

        {activity && activity.length > 0 ? (
          <ul className="mt-3 space-y-3">
            {activity.map((item) => (
              <li key={item.id} className="flex items-start gap-2.5">
                <span
                  aria-hidden="true"
                  className={`mt-1.5 size-2 shrink-0 rounded-full ${SEVERITY_DOT[item.severity] ?? SEVERITY_DOT.low}`}
                />
                <div className="min-w-0">
                  <Link
                    href={orgPath(orgSlug, `/feed/${item.id}`)}
                    className="line-clamp-2 text-xs leading-relaxed font-medium text-foreground underline-offset-4 hover:underline"
                  >
                    {item.title}
                  </Link>
                  {item.at ? (
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      {relativeTime(item.at)}
                    </p>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
            {/* Absent data and empty data read the same here on purpose: the
                rail is chrome on every page, and a second error surface for a
                read the feed already reports would be noise. The feed is
                where an empty list is a claim. */}
            Nothing yet. What your agents raise lands here, newest first.
          </p>
        )}
      </div>

      {/* The composer answers here. The first cut navigated to the feed,
          and typing "hello" into a face's message box and landing on a list
          page was rightly reported as broken. See KindyComposer for the
          contract. */}
      <KindyComposer orgSlug={orgSlug} action={kindyAction} variant={variant} />
    </aside>
  )
}
