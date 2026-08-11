import type { Metadata } from 'next'
import Link from 'next/link'
import { ArrowRight } from 'lucide-react'
import { Features } from '@/components/landing/features'
import { Footer } from '@/components/landing/footer'

export const metadata: Metadata = {
  title: 'Features: GDPR and EU AI Act compliance capabilities',
  description:
    'What Kindlast covers: GDPR gap analysis at article level, EU AI Act risk classification, a compliance score, audit-ready reports, and a privacy-first architecture you can self-host.',
}

/**
 * The capability detail, which used to be an inline section on the home page.
 *
 * ENT-190 gave it a route of its own so the home page could stay a claim
 * rather than a catalogue. The section owns the `h1` here because the page has
 * no other subject, which is what the `headingLevel` prop is for.
 *
 * The only exit is `/how-it-works`. Someone who has just read what the product
 * covers is exactly the person who now wants to know whether any of it is real.
 */
export default function FeaturesPage() {
  return (
    <>
      <Features headingLevel={1} />

      {/* Onward. Deliberately one exit, not a menu. */}
      <section className="relative overflow-hidden py-24 sm:py-28" style={{ backgroundColor: '#0D1B2A' }}>
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background:
              'radial-gradient(ellipse 60% 60% at 85% 5%, rgba(0,201,167,0.14) 0%, transparent 62%)',
          }}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.18em]" style={{ color: 'rgba(0,201,167,0.75)' }}>
            The part that matters
          </p>
          <h2 className="max-w-[18ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-white sm:text-[3.5rem] text-balance">
            A feature list is a promise. The pipeline is the proof.
          </h2>
          <p className="mt-7 max-w-[560px] text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/45">
            None of the above is worth much if it only happens when you remember
            to ask. Four agents keep it current on a schedule, and none of them
            can change a record without your explicit approval.
          </p>

          <div className="mt-10">
            <Link
              href="/how-it-works"
              className="group inline-flex items-center gap-2.5 whitespace-nowrap rounded-full bg-white px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-[#0D1B2A] transition-all duration-150 hover:bg-white/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              Follow one finding end to end
              <ArrowRight
                className="h-4 w-4 transition-transform duration-200 group-hover:translate-x-0.5"
                strokeWidth={2.25}
                aria-hidden="true"
              />
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
