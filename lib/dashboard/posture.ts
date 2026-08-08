import type { FindingSeverity } from '@/lib/feed/findings'

/**
 * The dashboard headline (ENT-77): a single Green / Amber / Red band that tells
 * a founder in two seconds whether their compliance is in trouble.
 *
 * The rule is the AC, so it lives here as a pure function over already-loaded
 * data — exhaustively unit-tested, and reused by the indicator component and
 * (later) the briefing. The loader that turns Supabase rows into these inputs
 * is `loadPostureInputs` in `./data`.
 */

export type Posture = 'green' | 'amber' | 'red' | 'unassessed'

/**
 * A regulatory deadline's bearing on posture: its severity and how many days
 * remain. `daysRemaining < 0` means the deadline is overdue.
 */
export interface PostureDeadline {
  severity: FindingSeverity
  daysRemaining: number
}

export interface PostureInputs {
  /** Severities of every OPEN (pending) finding. */
  openSeverities: FindingSeverity[]
  /** Approaching or overdue regulatory deadlines. */
  deadlines: PostureDeadline[]
  /**
   * When the Watcher last swept this profile, or `null` if it never has
   * (ENT-161). Without it, "no open findings" is ambiguous: it could mean
   * nothing is wrong, or that nobody has looked yet. A brand-new profile is
   * always the second, and calling that Green tells a non-compliant business
   * it is on track.
   */
  watcherLastRunAt: string | null
}

/** The "near-term" window the AC pins posture to. */
export const NEAR_TERM_DAYS = 30

/**
 * Collapse the open findings and deadlines into one band.
 *
 *   * Red   — a Critical finding is open, or a Critical deadline is overdue.
 *   * Amber — a High finding is open, or a Critical/High deadline falls inside
 *             the 30-day window. (A near-term Critical deadline that isn't yet
 *             overdue still breaks Green per the AC, but it is not Red until it
 *             actually lapses — so it lands here.)
 *   * Unassessed — the Watcher has never swept this profile, so a quiet board
 *             means "not looked at yet", not "clean" (ENT-161).
 *   * Green — nothing Critical/High is pressing.
 *
 * Precedence is Red → Amber → Unassessed → Green: the worst applicable band
 * wins. Unassessed sits below Red/Amber deliberately — if we already know
 * something is on fire, saying "not yet assessed" would bury it.
 */
export function computePosture({
  openSeverities,
  deadlines,
  watcherLastRunAt,
}: PostureInputs): Posture {
  const hasOpen = (s: FindingSeverity) => openSeverities.includes(s)
  const deadlineOverdue = (s: FindingSeverity) =>
    deadlines.some((d) => d.severity === s && d.daysRemaining < 0)
  const deadlineNearTerm = (s: FindingSeverity) =>
    deadlines.some(
      (d) => d.severity === s && d.daysRemaining >= 0 && d.daysRemaining <= NEAR_TERM_DAYS,
    )

  if (hasOpen('critical') || deadlineOverdue('critical')) return 'red'

  if (hasOpen('high') || deadlineNearTerm('high') || deadlineNearTerm('critical')) return 'amber'

  if (watcherLastRunAt === null) return 'unassessed'

  return 'green'
}

export interface PostureMeta {
  posture: Posture
  /** The band name (AC: "Green / Amber / Red"). */
  label: string
  /** Plain-language summary shown next to the indicator. No jargon. */
  headline: string
  /** Tailwind classes for the large indicator dot, readable on the dark console. */
  dotClassName: string
  /** Text colour matching the band. */
  textClassName: string
}

const POSTURE_META: Record<Posture, PostureMeta> = {
  green: {
    posture: 'green',
    label: 'Green',
    headline: "You're on track. Nothing urgent right now.",
    dotClassName: 'bg-emerald-400 shadow-[0_0_60px_-5px] shadow-emerald-400/60',
    textClassName: 'text-emerald-300',
  },
  amber: {
    posture: 'amber',
    label: 'Amber',
    headline: 'Needs attention. A few things are coming due.',
    dotClassName: 'bg-amber-400 shadow-[0_0_60px_-5px] shadow-amber-400/60',
    textClassName: 'text-amber-300',
  },
  red: {
    posture: 'red',
    label: 'Red',
    headline: 'Action required. You have something critical open.',
    dotClassName: 'bg-rose-500 shadow-[0_0_60px_-5px] shadow-rose-500/60',
    textClassName: 'text-rose-300',
  },
  // Deliberately unlit: a glowing dot of any colour reads as a verdict, and we
  // do not have one yet (ENT-161).
  unassessed: {
    posture: 'unassessed',
    label: 'Not yet assessed',
    headline: "The Watcher hasn't swept your profile yet. Your first scan runs within 24 hours.",
    dotClassName: 'border-2 border-dashed border-zinc-600 bg-transparent',
    textClassName: 'text-zinc-300',
  },
}

export function postureMeta(posture: Posture): PostureMeta {
  return POSTURE_META[posture]
}

/** How the band is computed — surfaced as the indicator's tooltip (AC). */
export const POSTURE_TOOLTIP =
  'Green: no critical findings open and no critical or high deadlines within 30 days. ' +
  'Amber: an open high finding, or a critical/high deadline within 30 days. ' +
  'Red: an open critical finding, or a critical deadline overdue. ' +
  'Not yet assessed: the Watcher has not scanned your profile yet.'
