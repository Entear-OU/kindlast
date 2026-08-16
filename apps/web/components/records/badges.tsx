/**
 * The pills that carry a record's state.
 *
 * Every label here is a lookup on a value the server computed, never a rule
 * re-implemented in the browser. `completeness` and `urgency` in particular are
 * derived in `domain/records` precisely so there is one definition of them, and
 * the Article 12(3) escalation window is a regulatory threshold rather than a
 * display choice.
 *
 * An unknown value renders as itself rather than as a default. A register that
 * silently showed "Minimal" for a classification it did not recognise would be
 * making a claim about a system nobody assessed.
 */

type Tone = 'neutral' | 'info' | 'warn' | 'danger' | 'done'

const TONES: Record<Tone, string> = {
  neutral: 'border-border/60 bg-muted/40 text-muted-foreground',
  info: 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300',
  warn: 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
  danger: 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300',
  done: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
}

function Pill({
  children,
  tone,
  title,
}: {
  children: React.ReactNode
  tone: Tone
  title?: string
}) {
  return (
    <span
      title={title}
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium ${TONES[tone]}`}
    >
      {children}
    </span>
  )
}

const COMPLETENESS: Record<
  string,
  { label: string; tone: Tone; hint: string }
> = {
  complete: {
    label: 'Complete',
    tone: 'done',
    hint: 'Every Article 30 field this record carries is filled in.',
  },
  incomplete: {
    label: 'Incomplete',
    tone: 'warn',
    hint: 'Some Article 30 fields are still empty.',
  },
  review_needed: {
    label: 'Needs review',
    tone: 'info',
    hint: 'Created for you when a finding was approved. Nobody has opened it yet.',
  },
}

export function CompletenessBadge({ value }: { value?: string }) {
  if (!value) return null
  const known = COMPLETENESS[value]
  if (!known) return <Pill tone="neutral">{value}</Pill>
  return (
    <Pill tone={known.tone} title={known.hint}>
      {known.label}
    </Pill>
  )
}

const RISK: Record<string, { label: string; tone: Tone }> = {
  unacceptable: { label: 'Unacceptable', tone: 'danger' },
  high: { label: 'High risk', tone: 'danger' },
  limited: { label: 'Limited', tone: 'warn' },
  minimal: { label: 'Minimal', tone: 'info' },
  unclassified: { label: 'Unclassified', tone: 'neutral' },
}

export function RiskBadge({ value }: { value?: string }) {
  if (!value) return null
  const known = RISK[value]
  if (!known) return <Pill tone="neutral">{value}</Pill>
  return (
    <Pill
      tone={known.tone}
      title={
        value === 'unclassified'
          ? 'Nobody has classified this system yet. That is not the same as low risk.'
          : undefined
      }
    >
      {known.label}
    </Pill>
  )
}

const DOCUMENTATION: Record<string, { label: string; tone: Tone }> = {
  missing: { label: 'Missing', tone: 'warn' },
  in_progress: { label: 'In progress', tone: 'info' },
  complete: { label: 'Complete', tone: 'done' },
}

export function DocumentationBadge({ value }: { value?: string }) {
  if (!value) return null
  const known = DOCUMENTATION[value]
  if (!known) return <Pill tone="neutral">{value}</Pill>
  return <Pill tone={known.tone}>{known.label}</Pill>
}

const URGENCY: Record<string, { label: string; tone: Tone }> = {
  overdue: { label: 'Overdue', tone: 'danger' },
  due_soon: { label: 'Due soon', tone: 'warn' },
  on_track: { label: 'On track', tone: 'info' },
  answered: { label: 'Answered', tone: 'done' },
}

export function UrgencyBadge({ value }: { value?: string }) {
  if (!value) return null
  const known = URGENCY[value]
  if (!known) return <Pill tone="neutral">{value}</Pill>
  return <Pill tone={known.tone}>{known.label}</Pill>
}

/**
 * The deadline in the words a handler acts on.
 *
 * Reads `daysUntilDue`, which the server computed by calendar date, rather than
 * subtracting dates here. Two implementations of a statutory countdown would
 * eventually disagree, and the one in the browser would disagree per timezone.
 *
 * Says nothing once the request is answered: a response that went out late is
 * still a response, and a log that keeps counting is asking somebody to act on
 * something already done.
 */
export function DueLabel({
  urgency,
  daysUntilDue,
}: {
  urgency?: string
  daysUntilDue?: number
}) {
  if (urgency === 'answered') return <NotApplicable />
  if (daysUntilDue === undefined) return <NotApplicable />

  if (daysUntilDue < 0) {
    const days = Math.abs(daysUntilDue)
    return <>{days === 1 ? '1 day overdue' : `${days} days overdue`}</>
  }
  if (daysUntilDue === 0) return <>Due today</>
  return (
    <>{daysUntilDue === 1 ? 'Due tomorrow' : `Due in ${daysUntilDue} days`}</>
  )
}

function NotApplicable() {
  return <span className="text-muted-foreground/60">Not applicable</span>
}

/**
 * A date, or a stated absence.
 *
 * `never` is the copy for a nullable timestamp that means the thing has not
 * happened: an AI system with no `lastReviewedAt` has never been reviewed, and
 * that is worth saying rather than leaving blank.
 */
export function DateValue({ value, never }: { value?: string; never: string }) {
  if (!value) return <span className="text-muted-foreground/60">{never}</span>
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime()))
    return <span className="text-muted-foreground/60">{never}</span>
  return (
    <time dateTime={value}>
      {parsed.toLocaleDateString(undefined, {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
      })}
    </time>
  )
}
