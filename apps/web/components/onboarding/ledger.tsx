import { citationLabel } from '@/lib/onboarding/corpus'
import { ledgerCounts, type LedgerRow } from '@/lib/onboarding/evaluate'

/**
 * The corpus, narrowing as the answers arrive (ENT-189, ENT-254).
 *
 * # WHY THE PROGRESS INDICATOR IS THE CORPUS ITSELF
 *
 * "Question 4 of 11" tells somebody how much longer this will take, which is
 * the least interesting thing on the page. What the product actually does is
 * take a body of regulation and narrow it against what it knows about one
 * organisation, and that is invisible in every other way of showing it. So the
 * fifteen obligations are on screen from the first question, and each one
 * resolves as the answer that decides it arrives.
 *
 * It is the Watcher, demonstrated rather than described, on the first screen a
 * customer sees. ENT-254 moved this out of the marketing site and into
 * onboarding for exactly that reason: the surface that makes somebody believe
 * the Watcher exists is worth more where they are about to rely on it than
 * where they were deciding whether to sign up.
 *
 * # THREE STATES, AND THE THIRD ONE IS THE HONEST PART
 *
 * Matched, set aside, and still open. The third is what stops the column being
 * a lie half way through: an obligation whose question has not been asked is
 * neither, and rendering it as either would be exactly the guess the whole
 * surface promises it does not make. `ledger()` carries the distinction and the
 * reason it exists.
 *
 * # THE MARKER IS A SHAPE AND A WORD, NOT A COLOUR
 *
 * Teal against dimmed ink would carry all three states for most people and none
 * of them for somebody who cannot separate the hues, so the marker differs in
 * shape (filled, outlined, a rule) and every row carries the state as text for
 * a screen reader. Colour is the fourth cue rather than the only one.
 */

function Marker({ state }: { state: LedgerRow['state'] }) {
  if (state === 'applies') {
    return (
      <span
        aria-hidden="true"
        className="mt-[7px] block h-[9px] w-[9px] shrink-0 bg-primary"
      />
    )
  }
  if (state === 'pending') {
    return (
      <span
        aria-hidden="true"
        className="mt-[7px] block h-[9px] w-[9px] shrink-0 border border-muted-foreground/60"
      />
    )
  }
  return (
    <span
      aria-hidden="true"
      className="mt-[11px] block h-px w-[9px] shrink-0 bg-muted-foreground/60"
    />
  )
}

const STATE_WORD: Record<LedgerRow['state'], string> = {
  applies: 'Matched',
  pending: 'Still open',
  narrowed: 'Set aside',
}

/**
 * The three counts, on their own.
 *
 * Below `lg` the column falls under the question, so a visitor on a phone would
 * answer the whole interview without ever seeing the corpus move, and the
 * column is the point. This is the same information as one line, placed above
 * the question there, and it carries the live region for both.
 */
export function LedgerSummary({
  rows,
  className,
}: {
  rows: readonly LedgerRow[]
  className?: string
}) {
  const counts = ledgerCounts(rows)
  return (
    <p
      aria-live="polite"
      className={`font-mono text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground ${className ?? ''}`}
    >
      <span className="text-primary">{counts.applies} matched</span> &middot;{' '}
      {counts.narrowed} set aside &middot; {counts.pending} still open
    </p>
  )
}

export function Ledger({ rows }: { rows: readonly LedgerRow[] }) {
  const counts = ledgerCounts(rows)

  return (
    <aside
      aria-label="The obligations Kindlast holds, and where each one stands"
      className="lg:sticky lg:top-[94px]"
    >
      <p className="font-mono text-[11px] font-medium uppercase tracking-[0.2em] text-muted-foreground">
        The corpus &middot; {rows.length} obligations
      </p>

      <ol className="mt-5 border-t border-border">
        {rows.map((row) => {
          const dim = row.state !== 'applies'
          return (
            <li
              key={row.obligation.slug}
              className="flex gap-3 border-b border-border/60 py-2.5 transition-opacity duration-300 motion-reduce:transition-none"
              style={{ opacity: dim ? 0.42 : 1 }}
            >
              <Marker state={row.state} />
              <div className="min-w-0">
                <p
                  data-citation="true"
                  className="font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground"
                >
                  {citationLabel(row.obligation.citation)}
                </p>
                <p
                  data-corpus="true"
                  className="mt-0.5 text-[13px] font-semibold leading-[1.35] tracking-[-0.01em] text-foreground"
                >
                  {row.obligation.title}
                </p>
                <span className="sr-only">{STATE_WORD[row.state]}</span>
              </div>
            </li>
          )
        })}
      </ol>

      {/* `aria-hidden`, because `LedgerSummary` above the question carries the
          same three numbers with the live region on it, and two live regions
          announcing the same change is worse than one. */}
      <p
        aria-hidden="true"
        className="mt-4 font-mono text-[11px] font-medium uppercase tracking-[0.14em] text-muted-foreground"
      >
        {counts.applies} matched &middot; {counts.narrowed} set aside &middot;{' '}
        {counts.pending} still open
      </p>
    </aside>
  )
}
