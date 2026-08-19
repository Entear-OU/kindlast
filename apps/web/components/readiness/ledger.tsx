import { citationLabel } from '@/lib/readiness/corpus'
import { ledgerCounts, type LedgerRow } from '@/lib/readiness/evaluate'

/**
 * The corpus, narrowing as the visitor answers (ENT-189).
 *
 * # WHY THE PROGRESS INDICATOR IS THE CORPUS ITSELF
 *
 * "Question 4 of 13" tells a visitor how much longer this will take, which is
 * the least interesting thing on the page. What the product actually does is
 * take a body of regulation and narrow it against what it knows about one
 * organisation, and that is invisible in every other way of showing it. So the
 * fifteen obligations are on screen from the first question, and each one
 * resolves as the answer that decides it arrives.
 *
 * It is the Watcher, demonstrated rather than described, on the page whose job
 * is to make somebody believe the Watcher exists.
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

const TEAL = '#00C9A7'
const INK = '#0D1B2A'

function Marker({ state }: { state: LedgerRow['state'] }) {
  if (state === 'applies') {
    return (
      <span
        aria-hidden="true"
        className="mt-[7px] block h-[9px] w-[9px] shrink-0"
        style={{ backgroundColor: TEAL }}
      />
    )
  }
  if (state === 'pending') {
    return (
      <span
        aria-hidden="true"
        className="mt-[7px] block h-[9px] w-[9px] shrink-0 border"
        style={{ borderColor: 'rgba(13,27,42,0.28)' }}
      />
    )
  }
  return (
    <span
      aria-hidden="true"
      className="mt-[11px] block h-px w-[9px] shrink-0"
      style={{ backgroundColor: 'rgba(13,27,42,0.28)' }}
    />
  )
}

const STATE_WORD: Record<LedgerRow['state'], string> = {
  applies: 'Matched',
  pending: 'Still open',
  narrowed: 'Set aside',
}

export function Ledger({ rows }: { rows: readonly LedgerRow[] }) {
  const counts = ledgerCounts(rows)

  return (
    <aside
      aria-label="The obligations Kindlast holds, and where each one stands"
      className="lg:sticky lg:top-[94px]"
    >
      <p
        className="font-mono text-[11px] font-medium uppercase tracking-[0.2em]"
        style={{ color: 'rgba(13,27,42,0.35)' }}
      >
        The corpus &middot; {rows.length} obligations
      </p>

      <ol
        className="mt-5 border-t"
        style={{ borderColor: 'rgba(13,27,42,0.1)' }}
      >
        {rows.map((row) => {
          const dim = row.state !== 'applies'
          return (
            <li
              key={row.obligation.slug}
              className="flex gap-3 border-b py-2.5 transition-opacity duration-300 motion-reduce:transition-none"
              style={{
                borderColor: 'rgba(13,27,42,0.06)',
                opacity: dim ? 0.42 : 1,
              }}
            >
              <Marker state={row.state} />
              <div className="min-w-0">
                <p
                  data-citation="true"
                  className="font-mono text-[10px] font-medium uppercase tracking-[0.14em]"
                  style={{ color: 'rgba(13,27,42,0.45)' }}
                >
                  {citationLabel(row.obligation.citation)}
                </p>
                <p
                  data-corpus="true"
                  className="mt-0.5 text-[13px] font-semibold leading-[1.35] tracking-[-0.01em]"
                  style={{ color: INK }}
                >
                  {row.obligation.title}
                </p>
                <span className="sr-only">{STATE_WORD[row.state]}</span>
              </div>
            </li>
          )
        })}
      </ol>

      {/* `aria-live` so somebody using a screen reader hears the column move
          when an answer changes it, rather than discovering it at the end. */}
      <p
        aria-live="polite"
        className="mt-4 font-mono text-[11px] font-medium uppercase tracking-[0.14em]"
        style={{ color: 'rgba(13,27,42,0.45)' }}
      >
        {counts.applies} matched &middot; {counts.narrowed} set aside &middot;{' '}
        {counts.pending} still open
      </p>
    </aside>
  )
}
