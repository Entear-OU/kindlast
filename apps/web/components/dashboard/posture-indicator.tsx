import { Info } from 'lucide-react'

import { POSTURE_TOOLTIP, postureMeta, type Posture } from '@/lib/dashboard/posture'

/**
 * The dashboard headline (ENT-77): the single Green / Amber / Red band, and the
 * largest element on the page. A founder reads it in two seconds; the plain
 * headline says what it means, and the tooltip (native `title`, also surfaced
 * on the "How this is scored" affordance) explains how the band is computed.
 */
export function PostureIndicator({ posture }: { posture: Posture }) {
  const meta = postureMeta(posture)

  return (
    <section
      aria-label="Overall compliance posture"
      className="rounded-2xl border border-white/5 bg-white/[0.02] p-8"
    >
      <div className="flex items-center gap-7">
        <span
          role="img"
          aria-label={`Posture: ${meta.label}`}
          title={POSTURE_TOOLTIP}
          className={`h-28 w-28 shrink-0 rounded-full ${meta.dotClassName}`}
        />
        <div className="min-w-0">
          <p className="text-xs font-medium uppercase tracking-[0.18em] text-zinc-500">
            Overall posture
          </p>
          <h2 className={`mt-1 text-5xl font-semibold tracking-tight ${meta.textClassName}`}>
            {meta.label}
          </h2>
          <p className="mt-3 max-w-md text-sm text-zinc-300">{meta.headline}</p>
          <span
            title={POSTURE_TOOLTIP}
            className="mt-4 inline-flex cursor-help items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300"
          >
            <Info size={13} aria-hidden="true" />
            How this is scored
          </span>
        </div>
      </div>
    </section>
  )
}
