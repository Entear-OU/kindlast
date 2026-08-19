'use client'

import { useState } from 'react'

import { GitHubMark } from '@/components/icons/github-mark'
import { ObligationSlip } from '@/components/readiness/obligation-slip'
import { citationLabel } from '@/lib/readiness/corpus'
import {
  HEADING_SET_ASIDE,
  NOT_AN_AUDIT,
  NO_EMAIL_ASKED,
  NO_TRANSMISSION,
  REACHED_YOU_MEANS,
  SET_ASIDE_LEAD,
} from '@/lib/readiness/copy'
import { GITHUB_REPO_URL } from '@/lib/links'
import type { Assessment } from '@/lib/readiness/evaluate'

/**
 * The result (ENT-189).
 *
 * # IT IS A COUNT, NOT A SCORE
 *
 * ENT-189 puts "scoring that claims to be an audit result" out of scope, and a
 * percentage is the fastest way back into it: a number out of a hundred implies
 * a scale, a scale implies a pass mark, and a pass mark on a marketing page is
 * an audit result with a friendlier face. So the headline is two integers and
 * their denominator, and the denominator is how many obligations Kindlast
 * actually holds. Every one of the three numbers is checkable by scrolling.
 *
 * # WHAT DID NOT REACH THEM IS ON THE PAGE, WITH THE REASON
 *
 * Hiding the narrowed obligations would make the result look more decisive and
 * make it unverifiable. The reason each one was set aside is the visitor's own
 * answer, which is the only thing that makes the ones that DID reach them worth
 * believing.
 *
 * # THERE IS NO EMAIL BOX, AND THAT IS SAID OUT LOUD
 *
 * ENT-189 asked for one and also asked for a written position on the lawful
 * basis first. The position does not exist yet, and an email box needs a basis,
 * a notice, a retention answer, a processor and a route for a subject access
 * request about the assessment itself. Shipping the box and writing the
 * position afterwards is the failure mode the issue itself names as the most
 * embarrassing one available to a GDPR product. So the result is the page, and
 * the page says why it did not ask.
 */

const INK = '#0D1B2A'
const TEAL_INK = '#00796B'

export function Summary({
  assessment,
  onRestart,
}: {
  assessment: Assessment
  onRestart: () => void
}) {
  const [showSetAside, setShowSetAside] = useState(false)

  // Gaps first. An obligation the visitor told us has no control behind it is
  // the reason they are on this page, and burying it under the ones that are
  // fine would be optimising the page for looking thorough.
  const ordered = [...assessment.applies].sort(
    (a, b) => Number(b.gaps.length > 0) - Number(a.gaps.length > 0),
  )

  return (
    <div className="signal-fade">
      <header>
        <p
          className="font-mono text-[11px] font-bold uppercase tracking-[0.2em]"
          style={{ color: TEAL_INK }}
        >
          Result &middot; Not an audit
        </p>

        <h2
          className="mt-6 max-w-[20ch] text-[2.25rem] font-black leading-[1.02] tracking-[-0.04em] text-balance sm:text-[3.25rem]"
          style={{ color: INK }}
        >
          {assessment.applies.length} of {assessment.total} reached you.
          {assessment.withGaps > 0 ? (
            <>
              {' '}
              <span style={{ color: TEAL_INK }}>
                You named a gap in {assessment.withGaps}.
              </span>
            </>
          ) : null}
        </h2>

        <div className="mt-7 max-w-[62ch] space-y-4">
          <p
            className="text-[1.0625rem] font-medium leading-[1.78] tracking-[-0.01em]"
            style={{ color: 'rgba(13,27,42,0.55)' }}
          >
            {NOT_AN_AUDIT}
          </p>
          <p
            className="text-[0.9375rem] font-medium leading-[1.72]"
            style={{ color: 'rgba(13,27,42,0.42)' }}
          >
            {REACHED_YOU_MEANS}
          </p>
        </div>
      </header>

      <div className="mt-16 space-y-14">
        {ordered.map((applied, index) => (
          <ObligationSlip
            key={applied.obligation.slug}
            applied={applied}
            index={index}
          />
        ))}
      </div>

      {assessment.narrowed.length > 0 ? (
        <section
          className="mt-16 border-t pt-8"
          style={{ borderColor: 'rgba(13,27,42,0.14)' }}
        >
          <h3
            className="text-[1.375rem] font-black tracking-[-0.025em]"
            style={{ color: INK }}
          >
            {HEADING_SET_ASIDE} ({assessment.narrowed.length})
          </h3>
          <p
            className="mt-3 max-w-[62ch] text-[1rem] font-medium leading-[1.72]"
            style={{ color: 'rgba(13,27,42,0.5)' }}
          >
            {SET_ASIDE_LEAD}
          </p>

          <button
            type="button"
            aria-expanded={showSetAside}
            onClick={() => setShowSetAside((v) => !v)}
            className="mt-5 cursor-pointer font-mono text-[11px] font-bold uppercase tracking-[0.16em] underline underline-offset-4 transition-colors duration-150 hover:text-[#0D1B2A] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#00C9A7] print:hidden"
            style={{ color: 'rgba(13,27,42,0.5)' }}
          >
            {showSetAside ? 'Hide them' : 'Show all of them'}
          </button>

          <dl
            className={`mt-6 border-t print:block ${showSetAside ? '' : 'hidden'}`}
            style={{ borderColor: 'rgba(13,27,42,0.08)' }}
          >
            {assessment.narrowed.map((narrowed) => (
              <div
                key={narrowed.obligation.slug}
                className="grid gap-1 border-b py-4 lg:grid-cols-[11rem_1fr] lg:gap-8"
                style={{ borderColor: 'rgba(13,27,42,0.08)' }}
              >
                <dt
                  data-citation="true"
                  className="font-mono text-[11px] font-bold uppercase tracking-[0.16em]"
                  style={{ color: 'rgba(13,27,42,0.45)' }}
                >
                  {citationLabel(narrowed.obligation.citation)}
                </dt>
                <dd>
                  <p
                    data-corpus="true"
                    className="text-[1rem] font-semibold leading-[1.4] tracking-[-0.01em]"
                    style={{ color: 'rgba(13,27,42,0.72)' }}
                  >
                    {narrowed.obligation.title}
                  </p>
                  <p
                    className="mt-1 max-w-[64ch] text-[0.9375rem] font-medium leading-[1.65]"
                    style={{ color: 'rgba(13,27,42,0.45)' }}
                  >
                    {narrowed.reason}
                  </p>
                </dd>
              </div>
            ))}
          </dl>
        </section>
      ) : null}

      {/* The close. Three honest statements and two exits, and no form. */}
      <section
        className="mt-20 border-t pt-10"
        style={{ borderColor: 'rgba(13,27,42,0.14)' }}
      >
        <div className="grid gap-10 lg:grid-cols-[1fr_1fr]">
          <div className="space-y-4">
            <p
              className="text-[1rem] font-medium leading-[1.75]"
              style={{ color: 'rgba(13,27,42,0.55)' }}
            >
              {NO_TRANSMISSION}
            </p>
            <p
              className="text-[0.9375rem] font-medium leading-[1.72]"
              style={{ color: 'rgba(13,27,42,0.42)' }}
            >
              {NO_EMAIL_ASKED}
            </p>
          </div>

          <div className="flex flex-col items-start gap-4 print:hidden">
            <button
              type="button"
              onClick={() => window.print()}
              className="inline-flex cursor-pointer items-center rounded-full px-7 py-3.5 text-[16px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 hover:bg-[#162537] active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7] motion-reduce:transition-none motion-reduce:active:scale-100"
              style={{ backgroundColor: INK }}
            >
              Keep a copy
            </button>
            <p
              className="max-w-[36ch] text-[13px] font-medium leading-[1.6]"
              style={{ color: 'rgba(13,27,42,0.4)' }}
            >
              Prints, or saves as a PDF, from your own browser. It goes nowhere
              near us.
            </p>

            <a
              href={GITHUB_REPO_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-2 inline-flex items-center gap-2.5 rounded-full border px-7 py-3.5 text-[16px] font-semibold tracking-[-0.01em] transition-all duration-150 hover:bg-black/[0.04] active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7] motion-reduce:transition-none motion-reduce:active:scale-100"
              style={{ borderColor: 'rgba(13,27,42,0.2)', color: INK }}
            >
              <GitHubMark size={18} />
              Read the rules that decided this
            </a>

            <button
              type="button"
              onClick={onRestart}
              className="cursor-pointer font-mono text-[11px] font-bold uppercase tracking-[0.16em] underline underline-offset-4 transition-colors duration-150 hover:text-[#0D1B2A] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#00C9A7]"
              style={{ color: 'rgba(13,27,42,0.45)' }}
            >
              Start again
            </button>
          </div>
        </div>
      </section>
    </div>
  )
}
