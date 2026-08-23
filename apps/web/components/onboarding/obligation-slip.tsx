import { citationLabel, citationUrl } from '@/lib/onboarding/corpus'
import {
  GAP_MEANS,
  HEADING_GAP,
  HEADING_LAW,
  HEADING_WHY,
  QUOTE_PROVENANCE,
  WHY_PROVENANCE,
} from '@/lib/onboarding/copy'
import type { AppliedObligation } from '@/lib/onboarding/evaluate'

/**
 * One obligation on the result (ENT-189, ENT-254).
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
 * # THERE USED TO BE A THIRD BLOCK AND THERE IS NOT ANY MORE
 *
 * `/readiness` carried a self-check here: the visitor's own answer to a
 * question the corpus attaches no gap token to, styled so it could not be read
 * as something Kindlast had found. ENT-254 dropped the two questions that fed
 * it, because this surface writes every answer down and neither had a fact key
 * to be written under. The reasoning is in `lib/onboarding/evaluate.ts`, and
 * the rendering rule it protected survives in the two halves above: a
 * self-reported weakness must never be dressed as a finding.
 */

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <p className="font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-muted-foreground">
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
  const { obligation, because, gaps, gapNotes } = applied
  const source = citationUrl(obligation.citation)

  return (
    <article
      className="signal-fade break-inside-avoid border-t border-border pt-8"
      // A short stagger so the result assembles rather than appearing whole.
      // `.signal-fade` is the app's own entry animation and already stops under
      // `prefers-reduced-motion`, so this inherits that.
      style={{ animationDelay: `${Math.min(index, 8) * 45}ms` }}
    >
      <div className="grid gap-8 lg:grid-cols-[11rem_1fr]">
        {/* The citation hangs in the margin, in the site's marginalia voice.
            It is the address; the paragraph beside it is what is at that
            address, and keeping them in separate columns says so. */}
        <div className="lg:pt-1">
          <p
            data-citation="true"
            className="font-mono text-[11px] font-bold uppercase tracking-[0.16em] text-muted-foreground"
          >
            {citationLabel(obligation.citation)}
          </p>
          {gaps.length > 0 ? (
            <p className="mt-2 inline-block font-mono text-[10px] font-bold uppercase tracking-[0.18em] text-primary">
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
            className="text-[1.25rem] font-semibold leading-[1.2] tracking-[-0.025em] text-balance text-foreground sm:text-[1.4rem]"
          >
            {obligation.title}
          </h3>

          {/* HALF ONE. Quoted, never rewritten. */}
          <div className="mt-6">
            <SectionLabel>{HEADING_LAW}</SectionLabel>
            <blockquote
              data-corpus="true"
              className="mt-3 border-l-2 border-primary pl-5"
            >
              <p className="max-w-[68ch] text-[0.9375rem] leading-[1.75] tracking-[-0.005em] text-foreground/85">
                {obligation.summary}
              </p>
            </blockquote>
            <p className="mt-3 text-[12px] leading-[1.6] text-muted-foreground">
              {QUOTE_PROVENANCE}
              {source ? (
                <>
                  {' '}
                  <a
                    href={source}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline underline-offset-2 transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
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
                  className="flex max-w-[68ch] gap-3 text-[0.9375rem] leading-[1.7] tracking-[-0.005em] text-muted-foreground"
                >
                  <span
                    aria-hidden="true"
                    className="mt-[9px] h-px w-3 shrink-0 bg-muted-foreground/60"
                  />
                  {sentence}
                </li>
              ))}
            </ul>
            <p className="mt-3 text-[12px] leading-[1.6] text-muted-foreground">
              {WHY_PROVENANCE}
            </p>
          </div>

          {gaps.length > 0 ? (
            <div className="mt-8 border-l-2 border-primary py-1 pl-5">
              <SectionLabel>{HEADING_GAP}</SectionLabel>
              <ul className="mt-3 space-y-2">
                {gapNotes.map((note) => (
                  <li
                    key={note}
                    className="max-w-[68ch] text-[0.9375rem] font-medium leading-[1.7] tracking-[-0.005em] text-foreground"
                  >
                    {note}
                  </li>
                ))}
              </ul>
              <p className="mt-3 max-w-[62ch] text-[12px] leading-[1.6] text-muted-foreground">
                {GAP_MEANS}
              </p>
            </div>
          ) : null}
        </div>
      </div>
    </article>
  )
}
