import { citationLabel, citationUrl } from '@/lib/readiness/corpus'
import {
  GAP_MEANS,
  HEADING_GAP,
  HEADING_LAW,
  HEADING_SELF,
  HEADING_WHY,
  QUOTE_PROVENANCE,
  SELF_MEANS,
  WHY_PROVENANCE,
} from '@/lib/readiness/copy'
import type { AppliedObligation } from '@/lib/readiness/evaluate'

/**
 * One obligation on the result (ENT-189).
 *
 * # THE LINE DOWN THE MIDDLE OF THIS COMPONENT IS THE PRODUCT'S THESIS
 *
 * Above the rule: what Kindlast holds about the law, quoted from a corpus row,
 * unedited, with the citation and a link to the official text.
 *
 * Below it: why this obligation reached this visitor, written from their own
 * answers, citing nothing and asserting nothing legal.
 *
 * ENT-248 settled that split for a model's output, after a narrative cited
 * Article 30 correctly and stated the opposite of Article 30(5) in the same
 * paragraph. It is a rendering rule as much as a prompt rule, and this is the
 * rendering: the two halves never share a paragraph, each says where it came
 * from, and no component anywhere paraphrases the first half into the second.
 *
 * # AND THE THIRD BLOCK IS NEITHER, WHICH IS WHY IT LOOKS DIFFERENT
 *
 * A self-check is the visitor's own answer to a question the corpus attaches no
 * gap token to. It is not something Kindlast found, so it does not get the
 * gap's weight or its colour. Making it look like a finding would be asserting
 * something the Watcher never said.
 */

const INK = '#0D1B2A'
const TEAL = '#00C9A7'
const TEAL_INK = '#00796B'

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p
      className="font-mono text-[10px] font-bold uppercase tracking-[0.2em]"
      style={{ color: 'rgba(13,27,42,0.38)' }}
    >
      {children}
    </p>
  )
}

export function ObligationSlip({
  applied,
  index,
}: {
  applied: AppliedObligation
  index: number
}) {
  const { obligation, because, gaps, gapNotes, selfChecks } = applied
  const source = citationUrl(obligation.citation)
  const answered = selfChecks.filter((c) => c.answer !== undefined)

  return (
    <article
      className="signal-fade break-inside-avoid border-t pt-8"
      // A short stagger so the result assembles rather than appearing whole.
      // `.signal-fade` is the site's own entry animation and already stops
      // under `prefers-reduced-motion`, so this inherits that.
      style={{
        borderColor: 'rgba(13,27,42,0.14)',
        animationDelay: `${Math.min(index, 8) * 45}ms`,
      }}
    >
      <div className="grid gap-8 lg:grid-cols-[11rem_1fr]">
        {/* The citation hangs in the margin, in the site's marginalia voice.
            It is the address; the paragraph beside it is what is at that
            address, and keeping them in separate columns says so. */}
        <div className="lg:pt-1">
          <p
            data-citation="true"
            className="font-mono text-[11px] font-bold uppercase tracking-[0.16em]"
            style={{ color: 'rgba(13,27,42,0.55)' }}
          >
            {citationLabel(obligation.citation)}
          </p>
          {gaps.length > 0 ? (
            <p
              className="mt-2 inline-block font-mono text-[10px] font-bold uppercase tracking-[0.18em]"
              style={{ color: TEAL_INK }}
            >
              Gap you named
            </p>
          ) : null}
        </div>

        <div className="min-w-0">
          {/* The title is corpus text too, so it carries the same marker as
              the summary: `Controller-processor contracts (Article 28 DPAs)`
              is the curator's heading, not a sentence written for the web. */}
          <h3
            data-corpus="true"
            className="text-[1.375rem] font-black leading-[1.15] tracking-[-0.025em] text-balance sm:text-[1.625rem]"
            style={{ color: INK }}
          >
            {obligation.title}
          </h3>

          {/* HALF ONE. Quoted, never rewritten. */}
          <div className="mt-6">
            <SectionLabel>{HEADING_LAW}</SectionLabel>
            <blockquote
              data-corpus="true"
              className="mt-3 border-l-2 pl-5"
              style={{ borderColor: TEAL }}
            >
              <p
                className="max-w-[68ch] text-[1rem] font-medium leading-[1.78] tracking-[-0.005em]"
                style={{ color: 'rgba(13,27,42,0.78)' }}
              >
                {obligation.summary}
              </p>
            </blockquote>
            <p
              className="mt-3 text-[12px] font-medium leading-[1.6]"
              style={{ color: 'rgba(13,27,42,0.38)' }}
            >
              {QUOTE_PROVENANCE}
              {source ? (
                <>
                  {' '}
                  <a
                    href={source}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline underline-offset-2 transition-colors duration-150 hover:text-[#0D1B2A] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#00C9A7]"
                  >
                    Open it on EUR-Lex
                  </a>
                  .
                </>
              ) : null}
            </p>
          </div>

          {/* HALF TWO. Written from the answers, and it says so. */}
          <div className="mt-8">
            <SectionLabel>{HEADING_WHY}</SectionLabel>
            <ul className="mt-3 space-y-2">
              {because.map((sentence) => (
                <li
                  key={sentence}
                  className="flex max-w-[68ch] gap-3 text-[1rem] font-medium leading-[1.7] tracking-[-0.005em]"
                  style={{ color: 'rgba(13,27,42,0.62)' }}
                >
                  <span
                    aria-hidden="true"
                    className="mt-[9px] h-px w-3 shrink-0"
                    style={{ backgroundColor: 'rgba(13,27,42,0.28)' }}
                  />
                  {sentence}
                </li>
              ))}
            </ul>
            <p
              className="mt-3 text-[12px] font-medium leading-[1.6]"
              style={{ color: 'rgba(13,27,42,0.38)' }}
            >
              {WHY_PROVENANCE}
            </p>
          </div>

          {gaps.length > 0 ? (
            <div
              className="mt-8 border-l-2 py-1 pl-5"
              style={{ borderColor: TEAL_INK }}
            >
              <SectionLabel>{HEADING_GAP}</SectionLabel>
              <ul className="mt-3 space-y-2">
                {gapNotes.map((note) => (
                  <li
                    key={note}
                    className="max-w-[68ch] text-[1rem] font-semibold leading-[1.7] tracking-[-0.005em]"
                    style={{ color: INK }}
                  >
                    {note}
                  </li>
                ))}
              </ul>
              <p
                className="mt-3 max-w-[62ch] text-[12px] font-medium leading-[1.6]"
                style={{ color: 'rgba(13,27,42,0.38)' }}
              >
                {GAP_MEANS}
              </p>
            </div>
          ) : null}

          {answered.length > 0 ? (
            <div className="mt-8">
              <SectionLabel>{HEADING_SELF}</SectionLabel>
              <dl className="mt-3 space-y-3">
                {answered.map((check) => (
                  <div key={check.key} className="max-w-[68ch]">
                    <dt
                      className="text-[0.9375rem] font-medium leading-[1.6]"
                      style={{ color: 'rgba(13,27,42,0.55)' }}
                    >
                      {check.prompt}
                    </dt>
                    <dd
                      className="mt-0.5 text-[0.9375rem] font-semibold"
                      style={{ color: INK }}
                    >
                      {check.answer === 'yes'
                        ? 'You said yes.'
                        : check.answer === 'no'
                          ? 'You said no.'
                          : 'You said you were not sure.'}
                    </dd>
                  </div>
                ))}
              </dl>
              <p
                className="mt-3 max-w-[62ch] text-[12px] font-medium leading-[1.6]"
                style={{ color: 'rgba(13,27,42,0.38)' }}
              >
                {SELF_MEANS}
              </p>
            </div>
          ) : null}
        </div>
      </div>
    </article>
  )
}
