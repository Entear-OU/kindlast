import {
  AlertTriangle,
  CircleAlert,
  CircleCheck,
  CircleHelp,
} from 'lucide-react'

import type { Dashboard } from '@/lib/findings/client'

/**
 * The posture band and the open counts (ENT-203, ENT-161).
 *
 * The fourth state is the reason this component is not a traffic light.
 *
 * `not_assessed` is what a brand-new organisation is in, and it is visually
 * distinct from green rather than a paler version of it: a neutral dot, a
 * question mark, and a sentence saying nothing has run. The old console derived
 * posture by counting findings, so an organisation the Watcher had never
 * examined reported "You're on track. Nothing urgent right now." For a product
 * whose value is that a human can check a claim, a confident wrong answer is
 * the worst available failure.
 */

const BANDS: Record<
  string,
  { label: string; dot: string; text: string; Icon: typeof CircleCheck }
> = {
  not_assessed: {
    label: 'Not assessed',
    dot: 'bg-muted-foreground/40',
    text: 'text-muted-foreground',
    Icon: CircleHelp,
  },
  green: {
    label: 'On track',
    dot: 'bg-emerald-400',
    text: 'text-emerald-300',
    Icon: CircleCheck,
  },
  amber: {
    label: 'Needs attention',
    dot: 'bg-amber-400',
    text: 'text-amber-300',
    Icon: AlertTriangle,
  },
  red: {
    label: 'Action required',
    dot: 'bg-red-400',
    text: 'text-red-300',
    Icon: CircleAlert,
  },
}

export function PostureBand({ dashboard }: { dashboard: Dashboard }) {
  const band = BANDS[dashboard.posture] ?? BANDS.not_assessed
  const counts = dashboard.openBySeverity ?? {}
  const { Icon } = band

  return (
    <section
      aria-label="Compliance posture"
      className="rounded-xl border border-border/60 bg-background p-5"
    >
      <div className="flex items-start gap-3">
        <span
          aria-hidden="true"
          className={`mt-1.5 size-2.5 shrink-0 rounded-full ${band.dot}`}
        />
        <div className="min-w-0">
          <p
            className={`flex items-center gap-1.5 text-[15px] font-semibold ${band.text}`}
          >
            <Icon aria-hidden="true" className="size-4" />
            {band.label}
          </p>
          {dashboard.postureHeadline ? (
            <p className="mt-1 text-sm text-muted-foreground">
              {dashboard.postureHeadline}
            </p>
          ) : null}
        </div>
      </div>

      {/* The tally is only meaningful once something has looked. Rendering
          "0 open" for an unassessed organisation would say the same reassuring
          thing the band is carefully not saying. */}
      {dashboard.posture !== 'not_assessed' ? (
        <dl className="mt-5 grid grid-cols-4 gap-3 border-t border-border/60 pt-4">
          {(
            [
              ['Critical', counts.critical],
              ['High', counts.high],
              ['Medium', counts.medium],
              ['Low', counts.low],
            ] as const
          ).map(([label, value]) => (
            <div key={label}>
              <dt className="text-xs text-muted-foreground">{label}</dt>
              <dd className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">
                {value ?? 0}
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
    </section>
  )
}

/**
 * When the agents last ran.
 *
 * The honest answer to "is this thing working", which the console has not been
 * able to give. Two distinct silences: nothing to run against (onboarding is
 * not done) and nothing has run yet (no schedule). They need different words
 * because they need different actions.
 */
export function PipelineNote({ dashboard }: { dashboard: Dashboard }) {
  const pipeline = dashboard.pipeline ?? {}

  if (!pipeline.profileExists) {
    return (
      <p className="text-xs text-muted-foreground">
        The Watcher has nothing to check yet. Onboarding has not been completed
        for this organisation.
      </p>
    )
  }

  if (!pipeline.watcherLastRunAt) {
    return (
      <p className="text-xs text-muted-foreground">
        The Watcher has not run yet, so nothing here has been assessed.
      </p>
    )
  }

  return (
    <p className="text-xs text-muted-foreground">
      The Watcher last ran{' '}
      <time dateTime={pipeline.watcherLastRunAt}>
        {new Date(pipeline.watcherLastRunAt).toLocaleString()}
      </time>
      .
    </p>
  )
}
