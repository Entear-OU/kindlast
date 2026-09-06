import type { SeverityCounts } from '@/lib/findings/client'

/**
 * The open findings, by severity, as a proportional bar.
 *
 * WHY THIS AND NOT A TREND CHART
 *
 * The obvious thing to put in the middle of a dashboard is a line going up or
 * down, and this deliberately is not that. There is no history endpoint to
 * draw one from, so any curve here would be invented, and a compliance product
 * that invents a trend is the fabricated confidence the posture band already
 * refuses to be (ENT-161). This says only what is true right now, which the
 * strip above it cannot: the strip reports three open and three of them urgent,
 * and cannot tell you whether that is one critical and two high or the reverse.
 *
 * The bar is decorative and the legend is the content. Colour is never the only
 * signal here, the same rule the severity badge follows, so the bar is hidden
 * from assistive technology and every band states its name and its count in
 * text, including the bands sitting at zero. A zero is a fact worth printing:
 * "no critical findings" is the sentence a reader wants most, and dropping the
 * empty bands would leave them counting absences.
 */

const BANDS = [
  { key: 'critical', label: 'Critical', fill: 'bg-red-500' },
  { key: 'high', label: 'High', fill: 'bg-amber-500' },
  { key: 'medium', label: 'Medium', fill: 'bg-sky-500' },
  { key: 'low', label: 'Low', fill: 'bg-slate-400' },
] as const

export function OpenBreakdown({ counts }: { counts: SeverityCounts }) {
  const bands = BANDS.map((b) => ({ ...b, count: counts[b.key] ?? 0 }))
  const total = bands.reduce((sum, b) => sum + b.count, 0)

  return (
    <div className="rounded-2xl border border-border/60 bg-card px-5 py-4">
      {total === 0 ? (
        <p className="text-sm text-muted-foreground">
          Nothing open. Findings appear here as Kindy raises them.
        </p>
      ) : (
        <div
          data-testid="open-breakdown-bar"
          aria-hidden="true"
          className="flex h-2 gap-1 overflow-hidden rounded-full"
        >
          {bands
            // A band with no findings draws nothing. Rendering it at zero width
            // still paints the gap beside it, which reads as a hairline of
            // severity that is not there.
            .filter((b) => b.count > 0)
            .map((b) => (
              <span
                key={b.key}
                data-severity={b.key}
                className={`h-full rounded-full ${b.fill}`}
                style={{
                  width: `${Math.round((b.count / total) * 10000) / 100}%`,
                }}
              />
            ))}
        </div>
      )}

      {/* A flowing row, not a four-column grid. On the grid each cell was wide
          enough to strand its count an inch from its own label, so the eye had
          to pair them across a gap and the block read as a table with three
          columns missing. Count first, then the word it counts, which is the
          order the sentence is spoken in. */}
      <ul className="mt-4 flex flex-wrap items-baseline gap-x-6 gap-y-2">
        {bands.map((b) => (
          <li key={b.key} className="flex items-baseline gap-1.5">
            <span
              aria-hidden="true"
              className={`size-2 shrink-0 translate-y-[-1px] rounded-full ${b.fill} ${
                b.count === 0 ? 'opacity-30' : ''
              }`}
            />
            <span
              className={`text-sm font-semibold tabular-nums ${
                b.count === 0 ? 'text-muted-foreground/70' : 'text-foreground'
              }`}
            >
              {b.count}
            </span>
            <span className="text-sm text-muted-foreground">{b.label}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
