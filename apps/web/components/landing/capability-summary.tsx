import Link from 'next/link'
import { ArrowRight } from 'lucide-react'

/**
 * The home page's short account of what Kindlast covers.
 *
 * ENT-190 moved the full capability detail to `/features`, so this section is
 * a summary and a fork in the road: readers who want the surface area go to
 * `/features`, readers who want to know whether the thing is real go to
 * `/how-it-works`. Keeping the summary to four lines is the point. The home
 * page's job is to be believable, not exhaustive.
 */

const AREAS = [
  {
    title: 'GDPR gap analysis',
    body: 'Findings tied to the specific article, not a generic checklist, with the one action that closes each gap.',
  },
  {
    title: 'EU AI Act classification',
    body: 'Your AI systems sorted into risk tiers, with the obligations and deadlines that follow from the tier.',
  },
  {
    title: 'ROPA and records',
    body: 'Records of processing kept current, because the agents notice when a new activity has no entry.',
  },
  {
    title: 'DSAR deadlines',
    body: 'Every data subject request tracked against its statutory clock, with alerts before the clock runs out.',
  },
]

export function CapabilitySummary() {
  return (
    <section id="capabilities" className="py-24 sm:py-32" style={{ backgroundColor: '#F5F4F0' }}>
      <div className="mx-auto max-w-5xl px-6 lg:px-8">

        <div className="grid items-start gap-12 lg:grid-cols-2">
          <div>
            <p
              className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
              style={{ color: 'rgba(13,27,42,0.3)' }}
            >
              What it covers
            </p>
            <h2 className="text-[3rem] font-black leading-none tracking-[-0.035em] text-[#0D1B2A] sm:text-[3.75rem] text-balance">
              Two regulations,
              <br />
              one running system
            </h2>
          </div>
          <div className="lg:pt-3">
            <p
              className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
              style={{ color: 'rgba(13,27,42,0.5)' }}
            >
              Kindlast holds a live picture of your obligations under the GDPR and
              the EU AI Act, and keeps checking it against what your business
              actually does. Nothing here waits for you to remember to open it.
            </p>
          </div>
        </div>

        <dl
          className="mt-16 grid gap-x-12 gap-y-10 pt-12 sm:grid-cols-2"
          style={{ borderTop: '1px solid rgba(13,27,42,0.08)' }}
        >
          {AREAS.map((area) => (
            <div key={area.title}>
              <dt className="text-[1.0625rem] font-extrabold tracking-[-0.02em] text-[#0D1B2A]">
                {area.title}
              </dt>
              <dd
                className="mt-2.5 max-w-[46ch] text-[1rem] font-medium leading-[1.72] tracking-[-0.005em]"
                style={{ color: 'rgba(13,27,42,0.48)' }}
              >
                {area.body}
              </dd>
            </div>
          ))}
        </dl>

        <div className="mt-14 flex flex-wrap items-center gap-x-8 gap-y-4">
          <Link
            href="/features"
            className="group inline-flex items-center gap-2 rounded-full bg-[#0D1B2A] px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 hover:bg-[#162537] active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
          >
            See the capabilities in detail
            <ArrowRight
              className="h-4 w-4 transition-transform duration-200 group-hover:translate-x-0.5"
              strokeWidth={2.25}
              aria-hidden="true"
            />
          </Link>
          <Link
            href="/how-it-works"
            className="text-[15px] font-semibold tracking-[-0.01em] underline underline-offset-4 transition-colors duration-150 hover:text-[#0D1B2A] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            style={{ color: 'rgba(13,27,42,0.5)' }}
          >
            See how it works
          </Link>
        </div>

      </div>
    </section>
  )
}
