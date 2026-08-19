import type { Metadata } from 'next'

import { Footer } from '@/components/landing/footer'
import { Assessment } from '@/components/readiness/assessment'
import { OBLIGATIONS } from '@/lib/readiness/corpus'
import {
  HERO_EYEBROW,
  HERO_LEAD,
  HERO_LEAD_ACCENT,
  HERO_SUB,
} from '@/lib/readiness/copy'

export const metadata: Metadata = {
  title: 'Readiness assessment: where do you actually stand?',
  description:
    'Answer the questions a data protection officer would ask, with no account and no sign-up. Kindlast matches them against the GDPR and EU AI Act obligations it holds and quotes the source behind every one. Your answers never leave the page.',
}

/**
 * The public readiness assessment (ENT-189).
 *
 * # THE PAGE IS STATIC, AND THAT IS THE SECURITY DESIGN
 *
 * There is no route handler here, no server action, and nothing that runs per
 * request. The corpus is imported at build time, the applicability rules are a
 * pure function in the bundle, and the whole interview is React state in one
 * browser tab. ENT-189 asked for per-IP rate limiting, a bot check, a turn cap
 * and a spend alert on an unauthenticated model endpoint; there is no endpoint,
 * so the four controls have nothing to sit in front of.
 *
 * It also means this surface cannot reach core-api. It holds no token, declares
 * no scope, and would be default-denied by the scope interceptor if it tried,
 * which is the right relationship between a marketing page and a resource
 * server that holds customers' compliance records.
 *
 * # WHY IT OPENS ON THE LIGHT GROUND AND NOT A DARK PLATE
 *
 * The other three routes open on a full-bleed dark hero, and copying that here
 * would have put a 100dvh photograph between the visitor and the first
 * question. The page has one job and it is not to be admired: the hero is a
 * short warm band, the interview starts above the fold on a laptop, and
 * `SiteHeader` renders its solid bar because `/readiness` is not in
 * `DARK_HERO_ROUTES`.
 */
export default function ReadinessPage() {
  return (
    <>
      <section
        className="relative overflow-hidden pb-14 pt-16 sm:pb-16 sm:pt-24"
        style={{ backgroundColor: '#F5F4F0' }}
      >
        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p
            className="font-mono text-[11px] font-bold uppercase tracking-[0.2em]"
            style={{ color: '#00796B' }}
          >
            {HERO_EYEBROW}
          </p>

          <div className="mt-7 grid items-end gap-10 lg:grid-cols-[1.1fr_1fr]">
            <h1 className="max-w-[16ch] text-[2.75rem] font-black leading-[0.94] tracking-[-0.04em] text-[#0D1B2A] text-balance sm:text-[4rem]">
              {HERO_LEAD}
              <br />
              <span style={{ color: '#00796B' }}>{HERO_LEAD_ACCENT}</span>
            </h1>

            <div className="space-y-4 lg:pb-2">
              <p
                className="text-[1.0625rem] font-medium leading-[1.8] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                {HERO_SUB}
              </p>
              {/* The count comes from the corpus rather than from a copywriter,
                  so adding a regulation pack moves the number on this page
                  instead of leaving it quietly wrong. */}
              <p
                className="font-mono text-[11px] font-medium uppercase tracking-[0.16em]"
                style={{ color: 'rgba(13,27,42,0.38)' }}
              >
                {OBLIGATIONS.length} obligations &middot; GDPR and EU AI Act
                &middot; About two minutes
              </p>
            </div>
          </div>
        </div>
      </section>

      <section
        className="pb-28 sm:pb-36"
        style={{ backgroundColor: '#F5F4F0' }}
      >
        <Assessment />
      </section>

      <div className="print:hidden">
        <Footer />
      </div>
    </>
  )
}
