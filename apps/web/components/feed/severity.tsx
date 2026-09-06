/**
 * Severity and status, rendered (ENT-203).
 *
 * Colour is never the only signal. Each badge carries its word as well, because
 * a founder with a red-green deficiency reading a compliance dashboard is not
 * an edge case, and "which of these is the urgent one" is exactly the question
 * this surface exists to answer.
 */

// The text sits at the 700 shade, not the 300 these started on. The console
// is a white sheet and nothing sets `.dark`, so pale text on a 10% tint of the
// same hue was a badge whose word could not be read at the size it is drawn.
// Since colour is never the only signal here, the word is the signal, and an
// unreadable word leaves only the colour.
const SEVERITY_STYLES: Record<string, string> = {
  critical: 'border-red-500/40 bg-red-500/10 text-red-700',
  high: 'border-amber-500/40 bg-amber-500/10 text-amber-700',
  medium: 'border-sky-500/40 bg-sky-500/10 text-sky-700',
  low: 'border-border/60 bg-muted text-muted-foreground',
}

export function SeverityBadge({ severity }: { severity: string }) {
  // An unrecognised severity renders in the neutral style rather than
  // disappearing. The column is check-constrained, so this only happens if the
  // constraint moved, and a visible unknown is how anyone finds out.
  const style = SEVERITY_STYLES[severity] ?? SEVERITY_STYLES.low

  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium capitalize ${style}`}
    >
      {severity}
    </span>
  )
}

const STATUS_LABELS: Record<string, string> = {
  pending: 'Needs a decision',
  approved: 'Approved',
  rejected: 'Rejected',
  snoozed: 'Deferred',
}

/**
 * The status, in the words a person would use.
 *
 * `pending` becomes "Needs a decision" deliberately: pending describes the
 * row's state, and what the reader wants to know is whose move it is.
 */
export function StatusLabel({ status }: { status: string }) {
  return (
    <span className="text-xs text-muted-foreground">
      {STATUS_LABELS[status] ?? status}
    </span>
  )
}
