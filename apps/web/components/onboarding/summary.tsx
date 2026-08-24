'use client'

import Link from 'next/link'
import { useState } from 'react'

import { ObligationSlip } from '@/components/onboarding/obligation-slip'
import { citationLabel } from '@/lib/onboarding/corpus'
import {
  HEADING_SET_ASIDE,
  NOT_AN_AUDIT,
  REACHED_YOU_MEANS,
  SET_ASIDE_LEAD,
  WHAT_HAPPENS_NEXT,
} from '@/lib/onboarding/copy'
import type { Assessment } from '@/lib/onboarding/evaluate'

/**
 * The result (ENT-189, ENT-254).
 *
 * # IT IS A COUNT, NOT A SCORE
 *
 * ENT-189 put "scoring that claims to be an audit result" out of scope, and a
 * percentage is the fastest way back into it: a number out of a hundred implies
 * a scale, a scale implies a pass mark, and a pass mark is an audit result with
 * a friendlier face. So the headline is two integers and their denominator, and
 * the denominator is how many obligations Kindlast actually holds. Every one of
 * the three numbers is checkable by scrolling.
 *
 * # WHAT DID NOT REACH THEM IS ON THE PAGE, WITH THE REASON
 *
 * Hiding the narrowed obligations would make the result look more decisive and
 * make it unverifiable. The reason each one was set aside is the person's own
 * answer, which is the only thing that makes the ones that DID reach them worth
 * believing.
 *
 * # WHAT CHANGED WHEN THIS MOVED INSIDE THE PRODUCT
 *
 * On the marketing site this was the end of the road, so it closed with a print
 * button, a link to the repository and an explanation of why there was no email
 * box. None of that applies to somebody who has just finished setting up an
 * account: the result is not the last thing they will see, it is the first
 * thing the agents will act on. So the close hands over to the feed and to the
 * memory page, which are where this goes next, and the honest sentences about
 * what the result is and is not stay exactly where they were.
 */
export function Summary({
  assessment,
  dashboardHref,
  memoryHref,
}: {
  assessment: Assessment
  dashboardHref: string
  memoryHref: string
}) {
  const [showSetAside, setShowSetAside] = useState(false)

  // Gaps first. An obligation somebody told us has no control behind it is the
  // reason they are here, and burying it under the ones that are fine would be
  // optimising the page for looking thorough.
  const ordered = [...assessment.applies].sort(
    (a, b) => Number(b.gaps.length > 0) - Number(a.gaps.length > 0),
  )

  return (
    <div className="signal-fade">
      <header>
        <p className="font-mono text-[11px] font-bold uppercase tracking-[0.2em] text-primary">
          Result &middot; Not an audit
        </p>

        <h2 className="mt-5 max-w-[22ch] text-[1.75rem] font-semibold leading-[1.08] tracking-[-0.035em] text-balance text-foreground sm:text-[2.25rem]">
          {assessment.applies.length} of {assessment.total} reached you.
          {assessment.withGaps > 0 ? (
            <>
              {' '}
              <span className="text-primary">
                You named a gap in {assessment.withGaps}.
              </span>
            </>
          ) : null}
        </h2>

        <div className="mt-6 max-w-[62ch] space-y-3">
          <p className="text-[0.9375rem] leading-[1.75] tracking-[-0.01em] text-muted-foreground">
            {NOT_AN_AUDIT}
          </p>
          <p className="text-sm leading-[1.7] text-muted-foreground">
            {REACHED_YOU_MEANS}
          </p>
        </div>
      </header>

      <div className="mt-12 space-y-12">
        {ordered.map((applied, index) => (
          <ObligationSlip
            key={applied.obligation.slug}
            applied={applied}
            index={index}
          />
        ))}
      </div>

      {assessment.narrowed.length > 0 ? (
        <section className="mt-14 border-t border-border pt-8">
          <h3 className="text-lg font-semibold tracking-[-0.02em] text-foreground">
            {HEADING_SET_ASIDE} ({assessment.narrowed.length})
          </h3>
          <p className="mt-2 max-w-[62ch] text-sm leading-[1.7] text-muted-foreground">
            {SET_ASIDE_LEAD}
          </p>

          <button
            type="button"
            aria-expanded={showSetAside}
            onClick={() => setShowSetAside((v) => !v)}
            className="mt-4 cursor-pointer font-mono text-[11px] font-bold uppercase tracking-[0.16em] text-muted-foreground underline underline-offset-4 transition-colors duration-150 hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {showSetAside ? 'Hide them' : 'Show all of them'}
          </button>

          <dl
            className={`mt-5 border-t border-border ${showSetAside ? '' : 'hidden'}`}
          >
            {assessment.narrowed.map((narrowed) => (
              <div
                key={narrowed.obligation.slug}
                className="grid gap-1 border-b border-border py-4 lg:grid-cols-[11rem_1fr] lg:gap-8"
              >
                <dt
                  data-citation="true"
                  className="font-mono text-[11px] font-bold uppercase tracking-[0.16em] text-muted-foreground"
                >
                  {citationLabel(narrowed.obligation.citation)}
                </dt>
                <dd>
                  <p
                    data-corpus="true"
                    className="text-[0.9375rem] font-medium leading-[1.4] tracking-[-0.01em] text-foreground"
                  >
                    {narrowed.obligation.title}
                  </p>
                  <p className="mt-1 max-w-[64ch] text-sm leading-[1.65] text-muted-foreground">
                    {narrowed.reason}
                  </p>
                </dd>
              </div>
            ))}
          </dl>
        </section>
      ) : null}

      <section className="mt-16 border-t border-border pt-8">
        <p className="max-w-[62ch] text-[0.9375rem] leading-[1.75] text-muted-foreground">
          {WHAT_HAPPENS_NEXT}
        </p>
        <div className="mt-6 flex flex-wrap items-center gap-4">
          <Link
            href={dashboardHref}
            className="inline-flex items-center rounded-full bg-primary px-7 py-3 text-[15px] font-medium text-primary-foreground transition-colors duration-150 hover:bg-primary/90 focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring"
          >
            Go to the dashboard
          </Link>
          <Link
            href={memoryHref}
            className="text-sm underline underline-offset-4 text-muted-foreground transition-colors duration-150 hover:text-foreground"
          >
            See and correct what Kindlast knows
          </Link>
        </div>
      </section>
    </div>
  )
}
